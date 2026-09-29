package a2aserver

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// subscriberBuffer is the channel buffer size for task event subscribers.
const subscriberBuffer = 64

// maxSubscribers is the maximum number of concurrent subscribers per task.
const maxSubscribers = 1000

// ErrTooManySubscribers is returned when a task has reached its subscriber limit.
var ErrTooManySubscribers = errors.New("a2a: too many subscribers")

// TaskEvent is one update to a task, as SubscribeToTask callers receive it.
// Exactly one field is set.
//
// It is version-neutral on purpose: a subscriber may speak a different
// protocol version, and carries a different JSON-RPC id, than the caller whose
// turn produced the event, so each subscriber encodes it for itself.
type TaskEvent struct {
	StatusUpdate   *a2a.TaskStatusUpdateEvent   `json:"statusUpdate,omitempty"`
	ArtifactUpdate *a2a.TaskArtifactUpdateEvent `json:"artifactUpdate,omitempty"`
}

// IsFinal reports whether the event ends a task's stream: a status update to a
// terminal or interrupted state.
func (e TaskEvent) IsFinal() bool {
	if e.StatusUpdate == nil {
		return false
	}
	s := e.StatusUpdate.Status.State
	return s.IsTerminal() || s.IsInterrupted()
}

// payload returns the event as the value a stream writer encodes.
func (e TaskEvent) payload() any {
	if e.StatusUpdate != nil {
		return e.StatusUpdate
	}
	return e.ArtifactUpdate
}

// localTaskEvents is the in-process TaskEventBus: subscribers see events
// from turns running in this process only.
type localTaskEvents struct {
	mu     sync.Mutex
	subs   map[string]map[uint64]chan TaskEvent
	nextID uint64
}

func newLocalTaskEvents() *localTaskEvents {
	return &localTaskEvents{subs: make(map[string]map[uint64]chan TaskEvent)}
}

// Publish implements TaskEventBus. A subscriber whose buffer is full misses the
// event rather than stalling the turn; a final event closes every subscriber.
func (l *localTaskEvents) Publish(_ context.Context, taskID string, evt TaskEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	subs := l.subs[taskID]
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			log.Printf("a2a: task %s: dropped event for slow subscriber (buffer full)", taskID)
		}
	}
	if evt.IsFinal() {
		for _, ch := range subs {
			close(ch)
		}
		delete(l.subs, taskID)
	}
	return nil
}

// Subscribe implements TaskEventBus. The channel is closed after a final
// event, or when ctx ends.
func (l *localTaskEvents) Subscribe(ctx context.Context, taskID string) (<-chan TaskEvent, error) {
	l.mu.Lock()
	subs := l.subs[taskID]
	if subs == nil {
		subs = make(map[uint64]chan TaskEvent)
		l.subs[taskID] = subs
	}
	if len(subs) >= maxSubscribers {
		l.mu.Unlock()
		return nil, ErrTooManySubscribers
	}
	id := l.nextID
	l.nextID++
	ch := make(chan TaskEvent, subscriberBuffer)
	subs[id] = ch
	l.mu.Unlock()

	go func() {
		<-ctx.Done()
		l.remove(taskID, id)
	}()
	return ch, nil
}

// remove drops one subscriber, closing its channel if a final event has not
// already done so.
func (l *localTaskEvents) remove(taskID string, id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	subs := l.subs[taskID]
	ch, ok := subs[id]
	if !ok {
		return
	}
	close(ch)
	delete(subs, id)
	if len(subs) == 0 {
		delete(l.subs, taskID)
	}
}

// closeAll ends every subscription, as on shutdown.
func (l *localTaskEvents) closeAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for taskID, subs := range l.subs {
		for _, ch := range subs {
			close(ch)
		}
		delete(l.subs, taskID)
	}
}
