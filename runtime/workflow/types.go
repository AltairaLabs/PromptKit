// Package workflow defines types and logic for PromptPack workflow state machines (RFC 0005).
//
// A workflow is an event-driven state machine layered over a PromptPack's prompts.
// Each state references a prompt_task and defines transitions via named events.
package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for workflow execution.
var (
	// ErrMaxVisitsExceeded is returned when a state's max_visits limit is reached
	// and no on_max_visits fallback is configured.
	ErrMaxVisitsExceeded = errors.New("max visits exceeded")

	// ErrBudgetExhausted is returned when a workflow-level budget limit is reached.
	ErrBudgetExhausted = errors.New("workflow budget exhausted")
)

// Budget defines workflow-level resource limits from the engine block.
type Budget struct {
	MaxTotalVisits int `json:"max_total_visits,omitempty"`
	MaxToolCalls   int `json:"max_tool_calls,omitempty"`
	MaxWallTimeSec int `json:"max_wall_time_sec,omitempty"`
}

// ParseConfig parses an untyped workflow config (typically from config.Workflow
// which is stored as interface{}) into a typed Spec. Returns nil, nil when
// raw is nil.
func ParseConfig(raw interface{}) (*Spec, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshaling workflow config: %w", err)
	}
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing workflow config: %w", err)
	}
	return &spec, nil
}

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
	Skills        string            `json:"skills,omitempty"`
	Terminal      bool              `json:"terminal,omitempty"`      // RFC 0009: explicit terminal marker
	MaxVisits     int               `json:"max_visits,omitempty"`    // RFC 0009: max times this state can be entered
	OnMaxVisits   string            `json:"on_max_visits,omitempty"` // RFC 0009: redirect target when max_visits reached
}

// TransitionResult is returned by ProcessEvent to communicate what happened.
// Redirects (e.g., max_visits exceeded → on_max_visits) are successful
// transitions, not errors.
type TransitionResult struct {
	From           string `json:"from"`
	To             string `json:"to"`
	Event          string `json:"event"`
	Redirected     bool   `json:"redirected,omitempty"`
	RedirectReason string `json:"redirect_reason,omitempty"`
	OriginalTarget string `json:"original_target,omitempty"`
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
	CurrentState   string            `json:"current_state"`
	History        []StateTransition `json:"history"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	VisitCounts    map[string]int    `json:"visit_counts,omitempty"`     // RFC 0009: per-state visit counts
	TotalToolCalls int               `json:"total_tool_calls,omitempty"` // RFC 0009: workflow-wide tool call count
	StartedAt      time.Time         `json:"started_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// StateTransition records a single state transition.
type StateTransition struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
}
