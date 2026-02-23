// Package generate provides pluggable session source adapters for scenario generation.
// It enables turning production session data (with failing assertions) into
// reproducible test scenarios for PromptArena.
package generate

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// SessionSourceAdapter provides session data for scenario generation.
// Implementations query external systems (e.g., session APIs, local recordings)
// and return structured session data that can be converted into Arena scenarios.
type SessionSourceAdapter interface {
	// Name returns the adapter's unique identifier (e.g., "recordings", "omnia").
	Name() string

	// List returns session summaries matching the given options.
	List(ctx context.Context, opts ListOptions) ([]SessionSummary, error)

	// Get returns the full session detail including messages and eval results.
	Get(ctx context.Context, sessionID string) (*SessionDetail, error)
}

// ListOptions controls which sessions are returned by List.
type ListOptions struct {
	// FilterPassed filters by pass/fail status: nil=all, *true=passed only, *false=failed only.
	FilterPassed *bool
	// FilterEvalType filters by assertion type (e.g., "content_matches").
	FilterEvalType string
	// Limit caps the number of results. 0 means unlimited.
	Limit int
}

// SessionSummary is a lightweight representation of a session for listing.
type SessionSummary struct {
	ID          string
	Source      string
	ScenarioID  string
	ProviderID  string
	Timestamp   time.Time
	TurnCount   int
	HasFailures bool
	Tags        []string
	Metadata    map[string]interface{}
}

// SessionDetail contains the full session data needed for scenario generation.
type SessionDetail struct {
	SessionSummary
	// Messages is the complete conversation history.
	Messages []types.Message
	// EvalResults contains conversation-level assertion results (nil for recordings).
	EvalResults []assertions.ConversationValidationResult
	// TurnEvalResults contains per-turn assertion results keyed by turn index.
	TurnEvalResults map[int][]TurnEvalResult
}

// TurnEvalResult represents the result of a single turn-level assertion.
type TurnEvalResult struct {
	Type    string
	Passed  bool
	Message string
	Params  map[string]interface{}
}
