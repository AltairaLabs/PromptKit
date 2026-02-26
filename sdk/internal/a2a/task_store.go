package a2a

import (
	"errors"
	"fmt"
	"sync"
	"time"

	rta2a "github.com/AltairaLabs/PromptKit/runtime/a2a"
)

// Task store errors.
var (
	ErrTaskNotFound      = errors.New("a2a: task not found")
	ErrTaskAlreadyExists = errors.New("a2a: task already exists")
	ErrInvalidTransition = errors.New("a2a: invalid state transition")
	ErrTaskTerminal      = errors.New("a2a: task is in a terminal state")
)

// terminalStates are states from which no further transitions are allowed.
var terminalStates = map[rta2a.TaskState]bool{
	rta2a.TaskStateCompleted: true,
	rta2a.TaskStateFailed:    true,
	rta2a.TaskStateCanceled:  true,
	rta2a.TaskStateRejected:  true,
}

// validTransitions defines the allowed state machine transitions.
var validTransitions = map[rta2a.TaskState]map[rta2a.TaskState]bool{
	rta2a.TaskStateSubmitted: {
		rta2a.TaskStateWorking: true,
	},
	rta2a.TaskStateWorking: {
		rta2a.TaskStateCompleted:     true,
		rta2a.TaskStateFailed:        true,
		rta2a.TaskStateCanceled:      true,
		rta2a.TaskStateInputRequired: true,
		rta2a.TaskStateAuthRequired:  true,
		rta2a.TaskStateRejected:      true,
	},
	rta2a.TaskStateInputRequired: {
		rta2a.TaskStateWorking:  true,
		rta2a.TaskStateCanceled: true,
	},
	rta2a.TaskStateAuthRequired: {
		rta2a.TaskStateWorking:  true,
		rta2a.TaskStateCanceled: true,
	},
}

// TaskStore defines the interface for task persistence and lifecycle management.
type TaskStore interface {
	Create(taskID, contextID string) (*rta2a.Task, error)
	Get(taskID string) (*rta2a.Task, error)
	SetState(taskID string, state rta2a.TaskState, msg *rta2a.Message) error
	AddArtifacts(taskID string, artifacts []rta2a.Artifact) error
	Cancel(taskID string) error
	List(contextID string, limit, offset int) ([]*rta2a.Task, error)

	// EvictTerminal removes tasks in a terminal state whose last status
	// timestamp is older than the given cutoff time. It returns the IDs
	// of evicted tasks so callers can clean up associated resources.
	EvictTerminal(olderThan time.Time) []string
}

// InMemoryTaskStore is a concurrency-safe, in-memory implementation of TaskStore.
type InMemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*rta2a.Task
}

// NewInMemoryTaskStore creates a new InMemoryTaskStore.
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{
		tasks: make(map[string]*rta2a.Task),
	}
}

// Create initializes a new task in the submitted state.
func (s *InMemoryTaskStore) Create(taskID, contextID string) (*rta2a.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[taskID]; exists {
		return nil, ErrTaskAlreadyExists
	}

	now := time.Now().UTC()
	task := &rta2a.Task{
		ID:        taskID,
		ContextID: contextID,
		Status: rta2a.TaskStatus{
			State:     rta2a.TaskStateSubmitted,
			Timestamp: &now,
		},
	}
	s.tasks[taskID] = task

	return task, nil
}

// Get retrieves a task by ID.
func (s *InMemoryTaskStore) Get(taskID string) (*rta2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// SetState transitions the task to a new state with an optional status message.
func (s *InMemoryTaskStore) SetState(taskID string, state rta2a.TaskState, msg *rta2a.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}

	current := task.Status.State

	if terminalStates[current] {
		return fmt.Errorf("%w: cannot transition from terminal state %q", ErrTaskTerminal, current)
	}

	allowed, ok := validTransitions[current]
	if !ok || !allowed[state] {
		return fmt.Errorf("%w: %q → %q", ErrInvalidTransition, current, state)
	}

	now := time.Now().UTC()
	task.Status = rta2a.TaskStatus{
		State:     state,
		Message:   msg,
		Timestamp: &now,
	}
	return nil
}

// AddArtifacts appends artifacts to a task.
func (s *InMemoryTaskStore) AddArtifacts(taskID string, artifacts []rta2a.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	task.Artifacts = append(task.Artifacts, artifacts...)
	return nil
}

// Cancel transitions the task to the canceled state from any non-terminal state.
func (s *InMemoryTaskStore) Cancel(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}

	if terminalStates[task.Status.State] {
		return fmt.Errorf("%w: cannot cancel task in terminal state %q", ErrTaskTerminal, task.Status.State)
	}

	now := time.Now().UTC()
	task.Status = rta2a.TaskStatus{
		State:     rta2a.TaskStateCanceled,
		Timestamp: &now,
	}
	return nil
}

// EvictTerminal removes tasks in a terminal state whose last status timestamp
// is older than cutoff. It returns the IDs of evicted tasks.
func (s *InMemoryTaskStore) EvictTerminal(cutoff time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var evicted []string
	for id, task := range s.tasks {
		if !terminalStates[task.Status.State] {
			continue
		}
		if task.Status.Timestamp != nil && task.Status.Timestamp.Before(cutoff) {
			delete(s.tasks, id)
			evicted = append(evicted, id)
		}
	}
	return evicted
}

// List returns tasks matching the given contextID with pagination.
// If contextID is empty, all tasks are returned. Offset and limit control pagination.
func (s *InMemoryTaskStore) List(contextID string, limit, offset int) ([]*rta2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*rta2a.Task
	for _, task := range s.tasks {
		if contextID == "" || task.ContextID == contextID {
			matched = append(matched, task)
		}
	}

	// Apply offset.
	if offset >= len(matched) {
		return nil, nil
	}
	matched = matched[offset:]

	// Apply limit.
	if limit > 0 && limit < len(matched) {
		matched = matched[:limit]
	}

	return matched, nil
}
