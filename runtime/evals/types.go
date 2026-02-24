// Package evals provides the core evaluation framework for PromptPack.
// Eval definitions travel with packs and can run both during Arena testing
// and at runtime in production via the SDK.
package evals

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// EvalTrigger determines when an eval fires.
type EvalTrigger string

const (
	// TriggerEveryTurn fires the eval after every assistant turn.
	TriggerEveryTurn EvalTrigger = "every_turn"
	// TriggerOnSessionComplete fires the eval when a session ends.
	TriggerOnSessionComplete EvalTrigger = "on_session_complete"
	// TriggerSampleTurns fires the eval on a percentage of turns (hash-based).
	TriggerSampleTurns EvalTrigger = "sample_turns"
	// TriggerSampleSessions fires the eval on a percentage of sessions (hash-based).
	TriggerSampleSessions EvalTrigger = "sample_sessions"
	// TriggerOnConversationComplete fires the eval when a conversation ends.
	TriggerOnConversationComplete EvalTrigger = "on_conversation_complete"
	// TriggerOnWorkflowStep fires the eval after each workflow step.
	TriggerOnWorkflowStep EvalTrigger = "on_workflow_step"
)

// DefaultSamplePercentage is the default sampling rate when not specified.
const DefaultSamplePercentage = 5.0

// ValidTriggers is the set of valid trigger values.
var ValidTriggers = map[EvalTrigger]bool{
	TriggerEveryTurn:              true,
	TriggerOnSessionComplete:      true,
	TriggerSampleTurns:            true,
	TriggerSampleSessions:         true,
	TriggerOnConversationComplete: true,
	TriggerOnWorkflowStep:         true,
}

// MetricType defines the Prometheus metric type for eval results.
type MetricType string

const (
	// MetricGauge represents a gauge metric (set to a value).
	MetricGauge MetricType = "gauge"
	// MetricCounter represents a counter metric (increment only).
	MetricCounter MetricType = "counter"
	// MetricHistogram represents a histogram metric (observe values).
	MetricHistogram MetricType = "histogram"
	// MetricBoolean represents a boolean metric (0 or 1).
	MetricBoolean MetricType = "boolean"
)

// ValidMetricTypes is the set of valid metric type values.
var ValidMetricTypes = map[MetricType]bool{
	MetricGauge:     true,
	MetricCounter:   true,
	MetricHistogram: true,
	MetricBoolean:   true,
}

// EvalDef defines a single evaluation within a PromptPack.
// Evals are defined at pack level and/or prompt level. Prompt-level
// evals override pack-level evals by ID.
type EvalDef struct {
	ID               string         `json:"id" yaml:"id"`
	Type             string         `json:"type" yaml:"type"`
	Trigger          EvalTrigger    `json:"trigger" yaml:"trigger"`
	Params           map[string]any `json:"params" yaml:"params"`
	Description      string         `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled          *bool          `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	SamplePercentage *float64       `json:"sample_percentage,omitempty" yaml:"sample_percentage,omitempty"`
	Metric           *MetricDef     `json:"metric,omitempty" yaml:"metric,omitempty"`
	Threshold        *Threshold     `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	Message          string         `json:"message,omitempty" yaml:"message,omitempty"`
	When             *EvalWhen      `json:"when,omitempty" yaml:"when,omitempty"`
}

// IsEnabled returns whether this eval is enabled.
// Defaults to true when Enabled is nil.
func (e *EvalDef) IsEnabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// GetSamplePercentage returns the sampling percentage.
// Defaults to DefaultSamplePercentage when SamplePercentage is nil.
func (e *EvalDef) GetSamplePercentage() float64 {
	if e.SamplePercentage == nil {
		return DefaultSamplePercentage
	}
	return *e.SamplePercentage
}

// Range defines the valid range for a metric value.
type Range struct {
	Min *float64 `json:"min,omitempty" yaml:"min,omitempty"`
	Max *float64 `json:"max,omitempty" yaml:"max,omitempty"`
}

// Threshold defines pass/fail criteria for an eval result.
type Threshold struct {
	Passed   *bool    `json:"passed,omitempty" yaml:"passed,omitempty"`
	MinScore *float64 `json:"min_score,omitempty" yaml:"min_score,omitempty"`
	MaxScore *float64 `json:"max_score,omitempty" yaml:"max_score,omitempty"`
}

// Apply adjusts the EvalResult based on threshold criteria.
func (t *Threshold) Apply(result *EvalResult) {
	if t == nil {
		return
	}
	if t.Passed != nil && !result.Passed {
		return
	}
	if t.MinScore != nil && result.Score != nil {
		result.Passed = result.Passed && *result.Score >= *t.MinScore
	}
	if t.MaxScore != nil && result.Score != nil {
		result.Passed = result.Passed && *result.Score <= *t.MaxScore
	}
}

// EvalWhen specifies preconditions that must be met for an eval to run.
type EvalWhen struct {
	ToolCalled        string `json:"tool_called,omitempty" yaml:"tool_called,omitempty"`
	ToolCalledPattern string `json:"tool_called_pattern,omitempty" yaml:"tool_called_pattern,omitempty"`
	AnyToolCalled     bool   `json:"any_tool_called,omitempty" yaml:"any_tool_called,omitempty"`
	MinToolCalls      int    `json:"min_tool_calls,omitempty" yaml:"min_tool_calls,omitempty"`
}

// EvalViolation represents a single eval violation within a conversation or session.
type EvalViolation struct {
	TurnIndex   int            `json:"turn_index"`
	Description string         `json:"description"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

// MetricDef defines a Prometheus-style metric associated with an eval.
// The Extra field captures additionalProperties from the schema.
type MetricDef struct {
	Name  string     `json:"name" yaml:"name"`
	Type  MetricType `json:"type" yaml:"type"`
	Range *Range     `json:"range,omitempty" yaml:"range,omitempty"`

	// Extra holds additional properties beyond the defined schema fields.
	// This supports the RFC's additionalProperties: true on metric.
	Extra map[string]any `json:"-" yaml:"-"`
}

// MarshalJSON implements custom JSON marshaling to include Extra fields
// as top-level properties alongside the known fields.
func (m MetricDef) MarshalJSON() ([]byte, error) {
	// Build a map with known fields
	result := make(map[string]any)
	result["name"] = m.Name
	result["type"] = m.Type
	if m.Range != nil {
		result["range"] = m.Range
	}
	// Merge extra fields
	for k, v := range m.Extra {
		if k != "name" && k != "type" && k != "range" {
			result[k] = v
		}
	}
	return json.Marshal(result)
}

// UnmarshalJSON implements custom JSON unmarshaling to capture
// additional properties into the Extra field.
func (m *MetricDef) UnmarshalJSON(data []byte) error {
	// Unmarshal known fields via an alias to avoid recursion
	type metricAlias MetricDef
	var alias metricAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*m = MetricDef(alias)

	// Capture all fields into a raw map
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract extra fields (anything not name, type, range)
	knownFields := map[string]bool{"name": true, "type": true, "range": true}
	for k, v := range raw {
		if !knownFields[k] {
			if m.Extra == nil {
				m.Extra = make(map[string]any)
			}
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				return err
			}
			m.Extra[k] = val
		}
	}
	return nil
}

// EvalResult captures the outcome of a single eval execution.
type EvalResult struct {
	EvalID      string          `json:"eval_id"`
	Type        string          `json:"type"`
	Passed      bool            `json:"passed"`
	Score       *float64        `json:"score,omitempty"`
	MetricValue *float64        `json:"metric_value,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
	DurationMs  int64           `json:"duration_ms"`
	Error       string          `json:"error,omitempty"`
	Message     string          `json:"message,omitempty"`
	Details     map[string]any  `json:"details,omitempty"`
	Violations  []EvalViolation `json:"violations,omitempty"`
	Skipped     bool            `json:"skipped,omitempty"`
	SkipReason  string          `json:"skip_reason,omitempty"`
}

// EvalContext provides data to eval handlers.
// For turn-level evals: Messages contains history up to the current turn.
// For session-level evals: Messages contains the full conversation.
type EvalContext struct {
	Messages      []types.Message  `json:"messages"`
	TurnIndex     int              `json:"turn_index"`
	CurrentOutput string           `json:"current_output"`
	ToolCalls     []ToolCallRecord `json:"tool_calls,omitempty"`
	SessionID     string           `json:"session_id"`
	PromptID      string           `json:"prompt_id"`
	Variables     map[string]any   `json:"variables,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty"`
	Extras        map[string]any   `json:"extras,omitempty"`
}

// ToolCallRecord is an alias for types.ToolCallRecord so existing code
// referencing evals.ToolCallRecord continues to compile unchanged.
type ToolCallRecord = types.ToolCallRecord
