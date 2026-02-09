package sdk

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

// Task store errors.
var (
	ErrTaskNotFound      = errors.New("a2a: task not found")
	ErrTaskAlreadyExists = errors.New("a2a: task already exists")
	ErrInvalidTransition = errors.New("a2a: invalid state transition")
	ErrTaskTerminal      = errors.New("a2a: task is in a terminal state")
)

// terminalStates are states from which no further transitions are allowed.
var terminalStates = map[a2a.TaskState]bool{
	a2a.TaskStateCompleted: true,
	a2a.TaskStateFailed:    true,
	a2a.TaskStateCanceled:  true,
	a2a.TaskStateRejected:  true,
}

// validTransitions defines the allowed state machine transitions.
var validTransitions = map[a2a.TaskState]map[a2a.TaskState]bool{
	a2a.TaskStateSubmitted: {
		a2a.TaskStateWorking: true,
	},
	a2a.TaskStateWorking: {
		a2a.TaskStateCompleted:     true,
		a2a.TaskStateFailed:        true,
		a2a.TaskStateCanceled:      true,
		a2a.TaskStateInputRequired: true,
		a2a.TaskStateAuthRequired:  true,
		a2a.TaskStateRejected:      true,
	},
	a2a.TaskStateInputRequired: {
		a2a.TaskStateWorking:  true,
		a2a.TaskStateCanceled: true,
	},
	a2a.TaskStateAuthRequired: {
		a2a.TaskStateWorking:  true,
		a2a.TaskStateCanceled: true,
	},
}

// A2ATaskStore defines the interface for task persistence and lifecycle management.
type A2ATaskStore interface {
	Create(taskID, contextID string) (*a2a.Task, error)
	Get(taskID string) (*a2a.Task, error)
	SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error
	AddArtifacts(taskID string, artifacts []a2a.Artifact) error
	Cancel(taskID string) error
	List(contextID string, limit, offset int) ([]*a2a.Task, error)
}

// InMemoryA2ATaskStore is a concurrency-safe, in-memory implementation of A2ATaskStore.
type InMemoryA2ATaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*a2a.Task
}

// NewInMemoryA2ATaskStore creates a new InMemoryA2ATaskStore.
func NewInMemoryA2ATaskStore() *InMemoryA2ATaskStore {
	return &InMemoryA2ATaskStore{
		tasks: make(map[string]*a2a.Task),
	}
}

// Create initializes a new task in the submitted state.
func (s *InMemoryA2ATaskStore) Create(taskID, contextID string) (*a2a.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[taskID]; exists {
		return nil, ErrTaskAlreadyExists
	}

	now := time.Now().UTC()
	task := &a2a.Task{
		ID:        taskID,
		ContextID: contextID,
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateSubmitted,
			Timestamp: &now,
		},
	}
	s.tasks[taskID] = task

	return task, nil
}

// Get retrieves a task by ID.
func (s *InMemoryA2ATaskStore) Get(taskID string) (*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// SetState transitions the task to a new state with an optional status message.
func (s *InMemoryA2ATaskStore) SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error {
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
	task.Status = a2a.TaskStatus{
		State:     state,
		Message:   msg,
		Timestamp: &now,
	}
	return nil
}

// AddArtifacts appends artifacts to a task.
func (s *InMemoryA2ATaskStore) AddArtifacts(taskID string, artifacts []a2a.Artifact) error {
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
func (s *InMemoryA2ATaskStore) Cancel(taskID string) error {
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
	task.Status = a2a.TaskStatus{
		State:     a2a.TaskStateCanceled,
		Timestamp: &now,
	}
	return nil
}

// List returns tasks matching the given contextID with pagination.
// If contextID is empty, all tasks are returned. Offset and limit control pagination.
func (s *InMemoryA2ATaskStore) List(contextID string, limit, offset int) ([]*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*a2a.Task
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
