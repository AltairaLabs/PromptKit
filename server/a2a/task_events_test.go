package a2aserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// subscriberCount reports a task's subscribers.
func (l *localTaskEvents) subscriberCount(taskID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.subs[taskID])
}

func statusEvent(state a2a.TaskState) TaskEvent {
	return TaskEvent{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: "t", Status: a2a.TaskStatus{State: state}}}
}

func TestTaskEvent_IsFinal(t *testing.T) {
	assert.False(t, statusEvent(a2a.TaskStateWorking).IsFinal())
	assert.True(t, statusEvent(a2a.TaskStateCompleted).IsFinal())
	assert.True(t, statusEvent(a2a.TaskStateInputRequired).IsFinal())
	assert.False(t, TaskEvent{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{}}.IsFinal())
}

func TestLocalTaskEvents_DeliversAndClosesOnFinal(t *testing.T) {
	bus := newLocalTaskEvents()
	ch, err := bus.Subscribe(context.Background(), "t")
	require.NoError(t, err)
	other, err := bus.Subscribe(context.Background(), "other")
	require.NoError(t, err)

	require.NoError(t, bus.Publish(context.Background(), "t", statusEvent(a2a.TaskStateWorking)))
	require.NoError(t, bus.Publish(context.Background(), "t", statusEvent(a2a.TaskStateCompleted)))

	var got []a2a.TaskState
	for evt := range ch {
		got = append(got, evt.StatusUpdate.Status.State)
	}
	assert.Equal(t, []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateCompleted}, got,
		"the final event is delivered, then the channel closes")
	assert.Zero(t, bus.subscriberCount("t"))
	assert.Equal(t, 1, bus.subscriberCount("other"), "other tasks are untouched")
	select {
	case <-other:
		t.Fatal("no event was published to the other task")
	default:
	}
}

func TestLocalTaskEvents_UnsubscribesOnContextEnd(t *testing.T) {
	bus := newLocalTaskEvents()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := bus.Subscribe(ctx, "t")
	require.NoError(t, err)
	cancel()
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel closes when the subscriber goes away")
	case <-time.After(time.Second):
		t.Fatal("subscription did not end with its context")
	}
	assert.Zero(t, bus.subscriberCount("t"))
}

func TestLocalTaskEvents_LimitAndCloseAll(t *testing.T) {
	bus := newLocalTaskEvents()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < maxSubscribers; i++ {
		_, err := bus.Subscribe(ctx, "t")
		require.NoError(t, err)
	}
	_, err := bus.Subscribe(ctx, "t")
	assert.ErrorIs(t, err, ErrTooManySubscribers)

	bus.closeAll()
	assert.Zero(t, bus.subscriberCount("t"))
}

func TestLocalTaskEvents_SlowSubscriberDoesNotBlock(t *testing.T) {
	bus := newLocalTaskEvents()
	_, err := bus.Subscribe(context.Background(), "t")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberBuffer*2; i++ {
			_ = bus.Publish(context.Background(), "t", statusEvent(a2a.TaskStateWorking))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publishing to a full subscriber blocked")
	}
}
