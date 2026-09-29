package a2aserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// Per-caller task scoping (#2089).
//
// A server that serves several callers must not let one of them read, cancel,
// list or subscribe to another's tasks. With WithTaskOwner the server asks the
// host who each request is from, records that owner on every task it creates,
// and answers a request for someone else's task exactly as it answers one for
// a task that does not exist (A2A 1.0 §3.3.2: do not reveal what the caller
// may not access).

// OwnerFunc identifies the caller of a request, typically from what the host's
// authentication middleware put on the request context. It must return a
// non-empty identity for every caller allowed to use the server.
type OwnerFunc func(r *http.Request) string

// OwnedTaskStore is a TaskStore that records which caller created each task.
// WithTaskOwner requires one; InMemoryTaskStore is one.
//
// A store that also implements TaskQuerier must honor TaskQuery.Owner.
type OwnedTaskStore interface {
	TaskStore
	// CreateOwned creates a task, as Create does, recording owner as its
	// creator.
	CreateOwned(taskID, contextID, owner string) (*a2a.Task, error)
	// Owner returns the owner recorded for a task, or ErrTaskNotFound.
	Owner(taskID string) (string, error)
}

// WithTaskOwner scopes every task to the caller that created it. owner
// identifies the caller of each request; GetTask, CancelTask, ListTasks and
// SubscribeToTask then see only the caller's own tasks, and ListTasks without
// a contextId lists them all. With NewServer, a message into a conversation
// another caller opened is refused; with NewStatelessServer the handler owns
// contexts and decides.
//
// The task store must implement OwnedTaskStore (the default in-memory store
// does); NewServer panics otherwise, since serving with scoping silently off
// would be worse than not starting. A request whose owner is empty is refused.
func WithTaskOwner(owner OwnerFunc) Option {
	return func(s *Server) { s.owner = owner }
}

// errOwnershipUnsupported explains a WithTaskOwner/TaskStore mismatch.
var errOwnershipUnsupported = errors.New(
	"a2a: WithTaskOwner needs a TaskStore that implements OwnedTaskStore")

// ownedStore returns the store's owner-aware half, or nil when tasks are not
// scoped by owner.
func (s *Server) ownedStore() OwnedTaskStore {
	if s.owner == nil {
		return nil
	}
	return s.taskStore.(OwnedTaskStore)
}

// checkOwnershipConfig panics when WithTaskOwner is set on a store that cannot
// record owners.
func (s *Server) checkOwnershipConfig() {
	if s.owner == nil {
		return
	}
	if _, ok := s.taskStore.(OwnedTaskStore); !ok {
		panic(fmt.Errorf("%w (got %T)", errOwnershipUnsupported, s.taskStore))
	}
}

// createTask creates a task for the call's caller.
func (s *Server) createTask(call *rpcCall, taskID, contextID string) error {
	if owned := s.ownedStore(); owned != nil {
		_, err := owned.CreateOwned(taskID, contextID, call.owner)
		return err
	}
	_, err := s.taskStore.Create(taskID, contextID)
	return err
}

// callerOwns reports whether the call's caller may see taskID.
func (s *Server) callerOwns(call *rpcCall, taskID string) bool {
	owned := s.ownedStore()
	if owned == nil {
		return true
	}
	owner, err := owned.Owner(taskID)
	return err == nil && owner == call.owner
}

// getTaskFor returns taskID if it exists and the call's caller may see it;
// otherwise it answers TaskNotFound itself and returns nil.
func (s *Server) getTaskFor(call *rpcCall, taskID string) *a2a.Task {
	task, err := s.taskStore.Get(taskID)
	if err != nil || !s.callerOwns(call, taskID) {
		call.fail(a2a.ErrCodeTaskNotFound, "Task not found")
		return nil
	}
	return task
}

// errContextTaken is returned when a caller names a context whose
// conversation another caller opened.
var errContextTaken = errors.New("a2a: context belongs to another caller")

// checkContextTasks refuses to open a conversation for contextID when the
// context's tasks belong to another caller — as they do after that caller's
// conversation was evicted, or when it lives on another replica. It reads the
// store, so it runs only when a conversation is opened, not per message.
func (s *Server) checkContextTasks(call *rpcCall, contextID string) error {
	owned := s.ownedStore()
	if owned == nil {
		return nil
	}
	page, err := queryTasks(s.taskStore, TaskQuery{ContextID: contextID, Limit: 1})
	if err != nil {
		return fmt.Errorf("check access to context %s: %w", contextID, err)
	}
	if len(page.Tasks) == 0 {
		return nil
	}
	if owner, ownerErr := owned.Owner(page.Tasks[0].ID); ownerErr == nil && owner == call.owner {
		return nil
	}
	return errContextTaken
}

// TaskEventBus carries task updates to SubscribeToTask callers.
//
// The default is in-process: a subscriber sees updates only from turns this
// server instance runs. A host running several replicas behind one task store
// supplies a shared implementation (Redis pub/sub, NATS, ...) with
// WithTaskEventBus, or pins each task's callers to one replica.
type TaskEventBus interface {
	// Publish delivers evt to the task's subscribers, wherever they are.
	Publish(ctx context.Context, taskID string, evt TaskEvent) error
	// Subscribe returns the task's events from now on. The server stops
	// reading after a final event (TaskEvent.IsFinal); the implementation
	// must stop delivering, and release the subscription, when ctx ends.
	Subscribe(ctx context.Context, taskID string) (<-chan TaskEvent, error)
}

// TaskCanceler stops a task's in-flight turn wherever it runs.
//
// The default is in-process: CancelTask stops a turn only on the instance
// running it. A host running several replicas supplies a shared implementation
// with WithTaskCanceler, or pins each task's callers to one replica.
type TaskCanceler interface {
	// Cancel asks whichever instance is running taskID to stop it. The task
	// is already marked canceled in the store when this is called.
	Cancel(ctx context.Context, taskID string) error
	// Listen registers the function that stops a turn running on this
	// instance; the implementation calls it for every cancel request, from
	// any instance, and it is a no-op for tasks this instance is not
	// running. The server calls Listen once, when it is created, and stop
	// when it shuts down.
	Listen(cancelLocal func(taskID string)) (stop func())
}

// WithTaskEventBus sets how task updates reach SubscribeToTask callers.
// Default: in-process only.
func WithTaskEventBus(bus TaskEventBus) Option {
	return func(s *Server) { s.events = bus }
}

// WithTaskCanceler sets how CancelTask reaches the instance running a task.
// Default: in-process only.
func WithTaskCanceler(c TaskCanceler) Option {
	return func(s *Server) { s.canceler = c }
}

// localCanceler is the in-process TaskCanceler.
type localCanceler struct {
	cancel func(taskID string)
}

// Cancel implements TaskCanceler.
func (l *localCanceler) Cancel(_ context.Context, taskID string) error {
	if l.cancel != nil {
		l.cancel(taskID)
	}
	return nil
}

// Listen implements TaskCanceler.
func (l *localCanceler) Listen(cancelLocal func(taskID string)) (stop func()) {
	l.cancel = cancelLocal
	return func() {}
}
