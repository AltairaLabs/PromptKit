package a2a

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Task store errors.
var (
	ErrTaskNotFound      = errors.New("a2a: task not found")
	ErrTaskAlreadyExists = errors.New("a2a: task already exists")
	ErrInvalidTransition = errors.New("a2a: invalid state transition")
	ErrTaskTerminal      = errors.New("a2a: task is in a terminal state")
)

// terminalStates are states from which no further transitions are allowed.
var terminalStates = map[TaskState]bool{
	TaskStateCompleted: true,
	TaskStateFailed:    true,
	TaskStateCanceled:  true,
	TaskStateRejected:  true,
}

// validTransitions defines the allowed state machine transitions.
var validTransitions = map[TaskState]map[TaskState]bool{
	TaskStateSubmitted: {
		TaskStateWorking: true,
	},
	TaskStateWorking: {
		TaskStateCompleted:     true,
		TaskStateFailed:        true,
		TaskStateCanceled:      true,
		TaskStateInputRequired: true,
		TaskStateAuthRequired:  true,
		TaskStateRejected:      true,
	},
	TaskStateInputRequired: {
		TaskStateWorking:  true,
		TaskStateCanceled: true,
	},
	TaskStateAuthRequired: {
		TaskStateWorking:  true,
		TaskStateCanceled: true,
	},
}

// TaskStore defines the interface for task persistence and lifecycle management.
type TaskStore interface {
	Create(taskID, contextID string) (*Task, error)
	Get(taskID string) (*Task, error)
	SetState(taskID string, state TaskState, msg *Message) error
	AddArtifacts(taskID string, artifacts []Artifact) error
	Cancel(taskID string) error
	List(contextID string, limit, offset int) ([]*Task, error)
}

// InMemoryTaskStore is a concurrency-safe, in-memory implementation of TaskStore.
type InMemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewInMemoryTaskStore creates a new InMemoryTaskStore.
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{
		tasks: make(map[string]*Task),
	}
}

// Create initializes a new task in the submitted state.
func (s *InMemoryTaskStore) Create(taskID, contextID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[taskID]; exists {
		return nil, ErrTaskAlreadyExists
	}

	now := time.Now().UTC()
	task := &Task{
		ID:        taskID,
		ContextID: contextID,
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: &now,
		},
	}
	s.tasks[taskID] = task

	return task, nil
}

// Get retrieves a task by ID.
func (s *InMemoryTaskStore) Get(taskID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// SetState transitions the task to a new state with an optional status message.
func (s *InMemoryTaskStore) SetState(taskID string, state TaskState, msg *Message) error {
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
	task.Status = TaskStatus{
		State:     state,
		Message:   msg,
		Timestamp: &now,
	}
	return nil
}

// AddArtifacts appends artifacts to a task.
func (s *InMemoryTaskStore) AddArtifacts(taskID string, artifacts []Artifact) error {
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
	task.Status = TaskStatus{
		State:     TaskStateCanceled,
		Timestamp: &now,
	}
	return nil
}

// List returns tasks matching the given contextID with pagination.
// If contextID is empty, all tasks are returned. Offset and limit control pagination.
func (s *InMemoryTaskStore) List(contextID string, limit, offset int) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*Task
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
