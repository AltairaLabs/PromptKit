// Package workflow defines types and logic for PromptPack workflow state machines (RFC 0005).
//
// A workflow is an event-driven state machine layered over a PromptPack's prompts.
// Each state references a prompt_task and defines transitions via named events.
package workflow

import "time"

// Spec is the top-level workflow definition from a PromptPack.
type Spec struct {
	Version int               `json:"version"`
	Entry   string            `json:"entry"`
	States  map[string]*State `json:"states"`
	Engine  map[string]any    `json:"engine,omitempty"`
}

// State defines a single state in the workflow state machine.
type State struct {
	PromptTask    string            `json:"prompt_task"`
	Description   string            `json:"description,omitempty"`
	OnEvent       map[string]string `json:"on_event,omitempty"`
	Persistence   Persistence       `json:"persistence,omitempty"`
	Orchestration Orchestration     `json:"orchestration,omitempty"`
}

// Persistence is the storage hint for a workflow state.
type Persistence string

// Persistence values.
const (
	PersistenceTransient  Persistence = "transient"
	PersistencePersistent Persistence = "persistent"
)

// Orchestration is the control mode for a workflow state.
type Orchestration string

// Orchestration values.
const (
	OrchestrationInternal Orchestration = "internal"
	OrchestrationExternal Orchestration = "external"
	OrchestrationHybrid   Orchestration = "hybrid"
)

// Context holds the runtime state of a workflow execution.
type Context struct {
	CurrentState string            `json:"current_state"`
	History      []StateTransition `json:"history"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// StateTransition records a single state transition.
type StateTransition struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
}
