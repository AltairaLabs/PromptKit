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

	"github.com/AltairaLabs/PromptKit/runtime/packspec"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
// Generated from the schema: an ALIAS for packspec.WorkflowBudget.
// Optional limits are pointers so "no limit" is distinct from a limit of zero.
type Budget = packspec.WorkflowBudget

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

// Spec is a pack's workflow state-machine specification.
//
// Generated. It was hand-written only because its states map held a
// hand-written State; it carries no methods of its own.
type Spec = packspec.WorkflowConfig

// State is a single state within a workflow.
//
// Generated. terminal, max_visits and orchestration are pointers on the
// generated type because the spec makes them optional — which is more correct
// than the values they replaced, since absent is now distinguishable from the
// zero. Use IsTerminal, MaxVisitsOf and OrchestrationOf rather than reading
// them directly; they resolve the documented default in one place.
type State = packspec.WorkflowState

// IsTerminal reports whether a state ends the workflow.
//
// terminal is *bool on the generated type because the spec makes it optional,
// and absent means "not terminal" rather than "unset". A state with no
// outgoing events is terminal regardless of the flag.
func IsTerminal(s *State) bool {
	if s == nil {
		return true
	}
	return packspec.Deref(s.Terminal, false) || len(s.OnEvent) == 0
}

// MaxVisitsOf returns a state's visit cap, or 0 when uncapped.
func MaxVisitsOf(s *State) int {
	if s == nil {
		return 0
	}
	return packspec.Deref(s.MaxVisits, 0)
}

// OrchestrationOf returns a state's control mode, defaulting to
// OrchestrationInternal when the state does not declare one.
func OrchestrationOf(s *State) string {
	if s == nil {
		return OrchestrationInternal
	}
	if s.Orchestration == nil || *s.Orchestration == "" {
		return OrchestrationInternal
	}
	return *s.Orchestration
}

// HoldsFloor reports whether the agent keeps the turn on entering s, rather
// than yielding the conversation to the user (RFC 0014's `control`).
//
// True means the turn runs on: the tool loop swaps to this state's prompt and
// tools and takes another round, so a routing or processing state can speak
// without waiting for a user message.
//
// # An absent control does NOT resolve to "user" here, and that is deliberate
//
// RFC 0014 defaults the field to "user" and describes that as "the behavior of
// every state before v1.7.0". That is not true of this runtime. In-turn state
// handoff shipped 2026-08-14 (#1747); RFC 0014 arrived with the v1.7.0 schema
// on 2026-08-31, seventeen days later. Before it the spec said nothing about
// who holds the turn after a transition — there was no default to deviate
// from — and this runtime already ran the destination state, which is what
// lets a pack route through several states and produce one reply.
//
// Honoring the RFC default would silently invert that for every pack written
// before it, turning routing states into dead stops. So a DECLARED control is
// honored exactly as specified, and an absent one keeps the behavior packs
// already rely on.
//
// This is a permanent, deliberate divergence, confined to what absent means.
// It is not an oversight and not a migration step, so do not "fix" it back to
// the spec default: doing so changes the behavior of every pack that never
// declares the field, which is all of them written before v1.7.0.
//
// The RFC's claim that "user" was the behavior of every state before v1.7.0 is
// what this contradicts, and correcting that wording is
// AltairaLabs/promptpack-spec#79. The default itself is not in dispute.
//
// A nil state yields: there is nothing to run on into.
func HoldsFloor(s *State) bool {
	if s == nil {
		return false
	}
	// Anything but an explicit "user" runs on: "agent", absent, and — only in
	// a pack that skipped validation — an unrecognized value, which
	// validateControl rejects rather than quietly resolving to a default.
	return packspec.Deref(s.Control, "") != ControlUser
}

// ArtifactDef declares a named artifact slot on a workflow state.
//
// Generated from the schema: an ALIAS for packspec.ArtifactDef.
//
// Mode is a *string, not a string: the spec defaults it to "replace", so
// "absent" and "explicitly empty" are different facts and a plain field would
// collapse them. Read it through ArtifactMode, which applies the default.
type ArtifactDef = packspec.ArtifactDef

// ArtifactMode returns an artifact slot's merge mode, applying the spec default
// of "replace" when the pack did not set one.
func ArtifactMode(def *ArtifactDef) string {
	if def == nil || def.Mode == nil {
		return ArtifactModeReplace
	}
	return *def.Mode
}

// Artifact merge modes (schema $defs/ArtifactDef.mode).
const (
	ArtifactModeReplace = "replace"
	ArtifactModeAppend  = "append"
)

// TransitionResult is returned by ProcessEvent to communicate what happened.
// Redirects (e.g., max_visits exceeded → on_max_visits) are successful
// transitions, not errors.
type TransitionResult struct {
	From           string           `json:"from"`
	To             string           `json:"to"`
	Event          string           `json:"event"`
	Redirected     bool             `json:"redirected,omitempty"`
	RedirectReason string           `json:"redirect_reason,omitempty"`
	OriginalTarget string           `json:"original_target,omitempty"`
	HostExtras     tools.HostExtras `json:"host_extras,omitempty"`
}

// Persistence is the storage hint for a workflow state.
type Persistence string

// Persistence values.
const (
	PersistenceTransient  = "transient"
	PersistencePersistent = "persistent"
)

// Orchestration is the control mode for a workflow state.
type Orchestration string

// Orchestration values.
const (
	OrchestrationInternal = "internal"
	OrchestrationExternal = "external"
	OrchestrationHybrid   = "hybrid"
	// OrchestrationComposition runs a declarative composition step-graph for the
	// state instead of an LLM-driven turn (RFC 0010).
	OrchestrationComposition = "composition"
)

// Turn control values (schema $defs/WorkflowState.control, RFC 0014).
//
// Orthogonal to Orchestration: that declares who INITIATES a transition, this
// declares who holds the turn AFTER one. Read a state's value through
// HoldsFloor, which documents how an absent value resolves here.
const (
	// ControlUser yields the conversation to the user on entering the state.
	ControlUser = "user"
	// ControlAgent runs another agent round in the state without yielding,
	// for transient routing and processing states.
	ControlAgent = "agent"
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
