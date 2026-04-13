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
	// and no on_max_visits fallback is configured. The concrete error returned
	// is typically a *MaxVisitsExceededError wrapping this sentinel; callers
	// wanting structured details should use errors.As.
	ErrMaxVisitsExceeded = errors.New("max visits exceeded")

	// ErrBudgetExhausted is returned when a workflow-level budget limit is
	// reached. The concrete error returned is typically a *BudgetExhaustedError
	// wrapping this sentinel; callers wanting structured details should use
	// errors.As.
	ErrBudgetExhausted = errors.New("workflow budget exhausted")
)

// MaxVisitsExceededError is the structured error returned from ProcessEvent
// when a state has reached its max_visits cap and no on_max_visits fallback
// is configured. It wraps ErrMaxVisitsExceeded so errors.Is still matches.
type MaxVisitsExceededError struct {
	// FromState is the state the transition was leaving.
	FromState string
	// OriginalTarget is the state whose max_visits was reached.
	OriginalTarget string
	// Event is the transition event that triggered the attempt.
	Event string
	// VisitCount is the number of times OriginalTarget had already been entered.
	VisitCount int
	// MaxVisits is the declared limit on OriginalTarget.
	MaxVisits int
}

// Error returns a human-readable description.
func (e *MaxVisitsExceededError) Error() string {
	return fmt.Sprintf("%s: state %q visited %d times (max %d)",
		ErrMaxVisitsExceeded.Error(), e.OriginalTarget, e.VisitCount, e.MaxVisits)
}

// Unwrap returns the sentinel for errors.Is.
func (e *MaxVisitsExceededError) Unwrap() error { return ErrMaxVisitsExceeded }

// Budget limit names, used by BudgetExhaustedError.Limit.
const (
	BudgetLimitTotalVisits = "max_total_visits"
	BudgetLimitToolCalls   = "max_tool_calls"
	BudgetLimitWallTimeSec = "max_wall_time_sec"
)

// BudgetExhaustedError is the structured error returned from ProcessEvent
// when a workflow-level budget is reached. It wraps ErrBudgetExhausted so
// errors.Is still matches.
type BudgetExhaustedError struct {
	// Limit is one of BudgetLimitTotalVisits, BudgetLimitToolCalls,
	// BudgetLimitWallTimeSec.
	Limit string
	// Current is the observed value at the time the limit was hit.
	Current int
	// Max is the configured limit.
	Max int
	// CurrentState is the state the workflow was in when the budget tripped.
	CurrentState string
}

// Error returns a human-readable description.
func (e *BudgetExhaustedError) Error() string {
	return fmt.Sprintf("%s: %s %d reached limit %d",
		ErrBudgetExhausted.Error(), e.Limit, e.Current, e.Max)
}

// Unwrap returns the sentinel for errors.Is.
func (e *BudgetExhaustedError) Unwrap() error { return ErrBudgetExhausted }

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
	PromptTask    string                  `json:"prompt_task"`
	Description   string                  `json:"description,omitempty"`
	OnEvent       map[string]string       `json:"on_event,omitempty"`
	Persistence   Persistence             `json:"persistence,omitempty"`
	Orchestration Orchestration           `json:"orchestration,omitempty"`
	Skills        string                  `json:"skills,omitempty"`
	Terminal      bool                    `json:"terminal,omitempty"`      // RFC 0009: explicit terminal marker
	MaxVisits     int                     `json:"max_visits,omitempty"`    // RFC 0009: max times this state can be entered
	OnMaxVisits   string                  `json:"on_max_visits,omitempty"` // RFC 0009: redirect on max_visits
	Artifacts     map[string]*ArtifactDef `json:"artifacts,omitempty"`     // RFC 0009: artifact slot declarations
}

// ArtifactDef declares a named artifact slot on a workflow state.
// Artifacts are workflow-scoped: if two states declare the same name, they
// reference the same artifact. Values are accessible as {{artifacts.<name>}}.
type ArtifactDef struct {
	Type        string `json:"type"`                  // MIME type (e.g., "text/plain", "application/json")
	Description string `json:"description,omitempty"` // What the artifact contains
	Mode        string `json:"mode,omitempty"`        // "replace" (default) or "append"
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
	CurrentState    string             `json:"current_state"`
	History         []StateTransition  `json:"history"`
	Metadata        map[string]any     `json:"metadata,omitempty"`
	VisitCounts     map[string]int     `json:"visit_counts,omitempty"`     // RFC 0009: per-state visit counts
	TotalToolCalls  int                `json:"total_tool_calls,omitempty"` // RFC 0009: workflow-wide tool call count
	Artifacts       map[string]string  `json:"artifacts,omitempty"`        // RFC 0009: current artifact values
	ArtifactHistory []ArtifactSnapshot `json:"artifact_history,omitempty"` // RFC 0009: artifact values at each transition
	StartedAt       time.Time          `json:"started_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// ArtifactSnapshot captures artifact values at a specific state transition.
type ArtifactSnapshot struct {
	FromState string            `json:"from_state"`
	ToState   string            `json:"to_state"`
	Event     string            `json:"event"`
	Values    map[string]string `json:"values"`
	Timestamp time.Time         `json:"timestamp"`
}

// StateTransition records a single state transition.
type StateTransition struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
}
