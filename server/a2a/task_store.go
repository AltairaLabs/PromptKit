package a2aserver

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
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

// TaskStore defines the interface for task persistence and lifecycle management.
type TaskStore interface {
	Create(taskID, contextID string) (*a2a.Task, error)
	Get(taskID string) (*a2a.Task, error)
	SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error
	AddArtifacts(taskID string, artifacts []a2a.Artifact) error
	Cancel(taskID string) error
	List(contextID string, limit, offset int) ([]*a2a.Task, error)

	// EvictTerminal removes tasks in a terminal state whose last status
	// timestamp is older than the given cutoff time. It returns the IDs
	// of evicted tasks so callers can clean up associated resources.
	EvictTerminal(olderThan time.Time) []string
}

// TaskQuery selects a page of tasks for ListTasks.
type TaskQuery struct {
	// Owner, when set, restricts the query to the tasks that caller created
	// (see OwnedTaskStore).
	Owner string
	// ContextID, when set, restricts the query to one context.
	ContextID string
	// Status, when set, keeps only tasks in that state.
	Status *a2a.TaskState
	// StatusAfter, when set, keeps only tasks whose status changed after it.
	StatusAfter *time.Time
	// Limit and Offset select the page within the ordered result.
	Limit  int
	Offset int
}

// TaskPage is one page of a TaskQuery's result.
type TaskPage struct {
	// Tasks are ordered most recently updated first (A2A 1.0 §3.1.4).
	Tasks []*a2a.Task
	// Total is the number of tasks matching the query across all pages.
	Total int
}

// TaskQuerier is optionally implemented by a TaskStore that can filter, order
// and page tasks itself. Without it the server pages through List and does
// the filtering and ordering in memory, which is correct but reads every task
// in the context on each call.
type TaskQuerier interface {
	Query(q TaskQuery) (TaskPage, error)
}

// queryTasks runs q against store, natively when the store supports it.
func queryTasks(store TaskStore, q TaskQuery) (TaskPage, error) {
	if querier, ok := store.(TaskQuerier); ok {
		return querier.Query(q)
	}
	all, err := listAll(store, q.ContextID)
	if err != nil {
		return TaskPage{}, err
	}
	if q.Owner != "" {
		all = ownedBy(store, all, q.Owner)
	}
	return pageTasks(all, q), nil
}

// listAll reads every task in a context (every task, for an empty one)
// through List, a batch at a time.
func listAll(store TaskStore, contextID string) ([]*a2a.Task, error) {
	const batch = 500
	var all []*a2a.Task
	for offset := 0; ; offset += batch {
		tasks, err := store.List(contextID, batch, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, tasks...)
		if len(tasks) < batch {
			return all, nil
		}
	}
}

// ownedBy keeps the tasks owner created. A store that cannot say who owns a
// task keeps none of them.
func ownedBy(store TaskStore, tasks []*a2a.Task, owner string) []*a2a.Task {
	owned, ok := store.(OwnedTaskStore)
	if !ok {
		return nil
	}
	kept := tasks[:0]
	for _, t := range tasks {
		if o, err := owned.Owner(t.ID); err == nil && o == owner {
			kept = append(kept, t)
		}
	}
	return kept
}

// pageTasks filters, orders and pages tasks in memory.
func pageTasks(tasks []*a2a.Task, q TaskQuery) TaskPage {
	matched := make([]*a2a.Task, 0, len(tasks))
	for _, t := range tasks {
		if matchesQuery(t, q) {
			matched = append(matched, t)
		}
	}
	sortByRecency(matched)
	page := TaskPage{Total: len(matched)}
	if q.Offset >= len(matched) {
		return page
	}
	matched = matched[q.Offset:]
	if q.Limit > 0 && q.Limit < len(matched) {
		matched = matched[:q.Limit]
	}
	page.Tasks = matched
	return page
}

// matchesQuery reports whether t satisfies q's filters.
func matchesQuery(t *a2a.Task, q TaskQuery) bool {
	if q.ContextID != "" && t.ContextID != q.ContextID {
		return false
	}
	if q.Status != nil && t.Status.State != *q.Status {
		return false
	}
	if q.StatusAfter != nil && (t.Status.Timestamp == nil || !t.Status.Timestamp.After(*q.StatusAfter)) {
		return false
	}
	return true
}

// sortByRecency orders tasks by status timestamp, most recent first, with the
// ID breaking ties so pagination is deterministic.
func sortByRecency(tasks []*a2a.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		ti, tj := tasks[i].Status.Timestamp, tasks[j].Status.Timestamp
		switch {
		case ti != nil && tj != nil && !ti.Equal(*tj):
			return ti.After(*tj)
		case (ti == nil) != (tj == nil):
			return ti != nil
		default:
			return tasks[i].ID < tasks[j].ID
		}
	})
}

// InMemoryTaskStore is a concurrency-safe, in-memory implementation of
// TaskStore, TaskQuerier and OwnedTaskStore.
type InMemoryTaskStore struct {
	mu     sync.RWMutex
	tasks  map[string]*a2a.Task
	owners map[string]string // task_id → owner, for tasks created with one
}

// NewInMemoryTaskStore creates a new InMemoryTaskStore.
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{
		tasks:  make(map[string]*a2a.Task),
		owners: make(map[string]string),
	}
}

// Create initializes a new task in the submitted state.
func (s *InMemoryTaskStore) Create(taskID, contextID string) (*a2a.Task, error) {
	return s.CreateOwned(taskID, contextID, "")
}

// CreateOwned implements OwnedTaskStore.
func (s *InMemoryTaskStore) CreateOwned(taskID, contextID, owner string) (*a2a.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[taskID]; exists {
		return nil, ErrTaskAlreadyExists
	}
	if owner != "" {
		s.owners[taskID] = owner
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

// Owner implements OwnedTaskStore. A task created without an owner has "".
func (s *InMemoryTaskStore) Owner(taskID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.tasks[taskID]; !ok {
		return "", ErrTaskNotFound
	}
	return s.owners[taskID], nil
}

// Get retrieves a deep copy of a task by ID. The returned task is safe to
// read/modify without holding the store lock.
func (s *InMemoryTaskStore) Get(taskID string) (*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return cloneTask(task), nil
}

// SetState transitions the task to a new state with an optional status message.
func (s *InMemoryTaskStore) SetState(taskID string, state a2a.TaskState, msg *a2a.Message) error {
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
func (s *InMemoryTaskStore) AddArtifacts(taskID string, artifacts []a2a.Artifact) error {
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
	task.Status = a2a.TaskStatus{
		State:     a2a.TaskStateCanceled,
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
			delete(s.owners, id)
			evicted = append(evicted, id)
		}
	}
	return evicted
}

// Query implements TaskQuerier.
func (s *InMemoryTaskStore) Query(q TaskQuery) (TaskPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	candidates := make([]*a2a.Task, 0, len(s.tasks))
	for id, task := range s.tasks {
		if q.Owner != "" && s.owners[id] != q.Owner {
			continue
		}
		candidates = append(candidates, task)
	}
	page := pageTasks(candidates, q)
	for i, t := range page.Tasks {
		page.Tasks[i] = cloneTask(t)
	}
	return page, nil
}

// List returns deep copies of tasks matching the given contextID with pagination.
// If contextID is empty, all tasks are returned. Results are ordered most
// recently updated first, ID breaking ties, for deterministic pagination.
// Offset and limit control pagination.
func (s *InMemoryTaskStore) List(contextID string, limit, offset int) ([]*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*a2a.Task
	for _, task := range s.tasks {
		if contextID == "" || task.ContextID == contextID {
			matched = append(matched, task)
		}
	}

	sortByRecency(matched)

	// Apply offset.
	if offset >= len(matched) {
		return nil, nil
	}
	matched = matched[offset:]

	// Apply limit.
	if limit > 0 && limit < len(matched) {
		matched = matched[:limit]
	}

	// Return deep copies so callers cannot mutate the store.
	result := make([]*a2a.Task, len(matched))
	for i, t := range matched {
		result[i] = cloneTask(t)
	}
	return result, nil
}

// cloneTask returns a deep copy of a Task.
func cloneTask(t *a2a.Task) *a2a.Task {
	cp := *t

	// Clone Status.
	if t.Status.Timestamp != nil {
		ts := *t.Status.Timestamp
		cp.Status.Timestamp = &ts
	}
	if t.Status.Message != nil {
		cp.Status.Message = cloneMessage(t.Status.Message)
	}

	// Clone History.
	if len(t.History) > 0 {
		cp.History = make([]a2a.Message, len(t.History))
		for i := range t.History {
			cp.History[i] = *cloneMessage(&t.History[i])
		}
	}

	// Clone Artifacts.
	if len(t.Artifacts) > 0 {
		cp.Artifacts = make([]a2a.Artifact, len(t.Artifacts))
		for i := range t.Artifacts {
			cp.Artifacts[i] = cloneArtifact(&t.Artifacts[i])
		}
	}

	// Clone Metadata.
	if len(t.Metadata) > 0 {
		cp.Metadata = make(map[string]any, len(t.Metadata))
		for k, v := range t.Metadata {
			cp.Metadata[k] = v
		}
	}

	return &cp
}

// cloneMessage returns a deep copy of a Message.
func cloneMessage(m *a2a.Message) *a2a.Message {
	cp := *m
	if len(m.Parts) > 0 {
		cp.Parts = make([]a2a.Part, len(m.Parts))
		for i := range m.Parts {
			cp.Parts[i] = clonePart(&m.Parts[i])
		}
	}
	if len(m.Metadata) > 0 {
		cp.Metadata = make(map[string]any, len(m.Metadata))
		for k, v := range m.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

// cloneArtifact returns a deep copy of an Artifact.
func cloneArtifact(art *a2a.Artifact) a2a.Artifact {
	cp := *art
	if len(art.Parts) > 0 {
		cp.Parts = make([]a2a.Part, len(art.Parts))
		for i := range art.Parts {
			cp.Parts[i] = clonePart(&art.Parts[i])
		}
	}
	if len(art.Metadata) > 0 {
		cp.Metadata = make(map[string]any, len(art.Metadata))
		for k, v := range art.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

// clonePart returns a deep copy of a Part.
func clonePart(p *a2a.Part) a2a.Part {
	cp := *p
	if p.Text != nil {
		s := *p.Text
		cp.Text = &s
	}
	if p.URL != nil {
		s := *p.URL
		cp.URL = &s
	}
	if len(p.Metadata) > 0 {
		cp.Metadata = make(map[string]any, len(p.Metadata))
		for k, v := range p.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}
