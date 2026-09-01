// Package evals provides the core evaluation framework for PromptPack.
// Eval definitions travel with packs and can run both during Arena testing
// and at runtime in production via the SDK.
package evals

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// EvalTrigger names when an eval fires.
//
// The constants below are deliberately UNTYPED. $defs/Eval types `trigger` as a
// plain string with no enum, so the generated EvalDef.Trigger is a string; an
// untyped constant assigns to both that and EvalTrigger, which keeps the
// vocabulary in one place without forcing a conversion at every call site.
//
// EvalTrigger remains for the maps and signatures that key on it. It is not a
// safety mechanism: a Go named string type accepts any literal, so
// `var t EvalTrigger = "nonsense"` compiles. ValidTriggers is the actual check.
//
// EvalTrigger determines when an eval fires.
type EvalTrigger string

const (
	// TriggerEveryTurn fires the eval after every assistant turn.
	TriggerEveryTurn = "every_turn"
	// TriggerOnSessionComplete fires the eval when a session ends.
	TriggerOnSessionComplete = "on_session_complete"
	// TriggerSampleTurns fires the eval on a percentage of turns (hash-based).
	TriggerSampleTurns = "sample_turns"
	// TriggerSampleSessions fires the eval on a percentage of sessions (hash-based).
	TriggerSampleSessions = "sample_sessions"
	// TriggerOnConversationComplete fires the eval when a conversation ends.
	TriggerOnConversationComplete = "on_conversation_complete"
	// TriggerOnWorkflowStep fires the eval after each workflow step.
	TriggerOnWorkflowStep = "on_workflow_step"
)

// DefaultSamplePercentage is the default sampling rate when not specified.
const DefaultSamplePercentage = 5.0

// DefaultEvalGroup is the group assigned to evals that have no explicit groups.
const DefaultEvalGroup = "default"

// Well-known eval groups for automatic classification based on handler type.
// These are assigned as additional default groups alongside DefaultEvalGroup
// when an eval has no explicit groups configured.
const (
	// GroupFastRunning classifies evals that use simple, deterministic logic
	// (string matching, regex, JSON validation, etc.) and complete quickly.
	GroupFastRunning = "fast-running"

	// GroupLongRunning classifies evals that involve LLM calls, network requests,
	// or compute-intensive operations (cosine similarity, LLM judge, etc.).
	GroupLongRunning = "long-running"

	// GroupExternal classifies evals that call external systems — REST APIs,
	// A2A agents, subprocess exec, or LLM judge providers.
	GroupExternal = "external"
)

// longRunningTypes is the set of eval types classified as long-running.
var longRunningTypes = map[string]bool{
	"llm_judge":            true,
	"llm_judge_session":    true,
	"llm_judge_tool_calls": true,
	"outcome_equivalent":   true,
	"cosine_similarity":    true,
	"a2a_eval":             true,
	"a2a_eval_session":     true,
	"rest_eval":            true,
	"rest_eval_session":    true,
}

// externalTypes is the set of eval types classified as external.
// These are a subset of long-running types that call external systems.
var externalTypes = map[string]bool{
	"llm_judge":            true,
	"llm_judge_session":    true,
	"llm_judge_tool_calls": true,
	"a2a_eval":             true,
	"a2a_eval_session":     true,
	"rest_eval":            true,
	"rest_eval_session":    true,
}

// customTypeGroups holds dynamically registered type→groups mappings
// for eval types not in the built-in maps (e.g. exec handlers).
var customTypeGroups = make(map[string][]string)

// RegisterTypeGroups registers well-known groups for a dynamic eval type.
// This is used by exec eval handlers to self-classify as long-running/external.
func RegisterTypeGroups(evalType string, groups []string) {
	customTypeGroups[evalType] = groups
}

// DefaultGroupsForType returns the well-known groups for a given eval type.
// The result always includes DefaultEvalGroup plus any classification groups
// based on the handler's characteristics.
func DefaultGroupsForType(evalType string) []string {
	groups := []string{DefaultEvalGroup}

	// Check custom registrations first (exec handlers, etc.)
	if custom, ok := customTypeGroups[evalType]; ok {
		return append(groups, custom...)
	}

	isLongRunning := longRunningTypes[evalType]
	isExternal := externalTypes[evalType]

	if !isLongRunning && !isExternal {
		// Not in any known long-running/external map — classify as fast-running
		groups = append(groups, GroupFastRunning)
	}
	if isLongRunning {
		groups = append(groups, GroupLongRunning)
	}
	if isExternal {
		groups = append(groups, GroupExternal)
	}
	return groups
}

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
type MetricType = string

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
//
// Generated. It was hand-written, and diverged from the spec in two ways that
// only showed on disk:
//
//   - `params` lacked omitempty, so an eval without params emitted
//     "params": null, which the schema rejects (Expected: object). EVERY pack
//     containing an eval emitted an invalid document.
//   - `threshold` used a promptkit vocabulary ({passed, min_score, max_score})
//     where the spec defines {operator, value} with additionalProperties:false.
//     A spec-authored threshold loaded as all-nil and emitted as {}; a
//     promptkit one emitted a document the schema rejects. Nothing in promptkit
//     reads the field — an eval never states a pass/fail, only an assertion or
//     guardrail coerces one — so it existed solely to be serialized wrongly.
//
// The accessors below were methods. A type alias cannot carry methods
// ("cannot define new methods on non-local type"), so they are free functions
// now — which is the general shape for adopting any generated type that had
// behavior attached.
type EvalDef = packspec.Eval

// IsEnabled returns whether an eval is enabled. Defaults to true when Enabled
// is nil, because absent means enabled.
func IsEnabled(e *EvalDef) bool {
	if e == nil || e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

// SamplePercentage returns the sampling percentage, defaulting to
// DefaultSamplePercentage when unset.
func SamplePercentage(e *EvalDef) float64 {
	if e == nil || e.SamplePercentage == nil {
		return DefaultSamplePercentage
	}
	return *e.SamplePercentage
}

// Groups returns the groups an eval belongs to.
// When no explicit groups are configured, returns DefaultGroupsForType(e.Type),
// which includes DefaultEvalGroup plus well-known classification groups
// (fast-running, long-running, external) based on the eval type.
// When explicit groups are set, returns them as-is (overriding defaults).
func Groups(e *EvalDef) []string {
	if e == nil {
		return nil
	}
	if len(e.Groups) == 0 {
		return DefaultGroupsForType(e.Type)
	}
	return e.Groups
}

// Values dereferences a slice of eval pointers into values.
//
// The generated Prompt holds []*Eval because the schema implies pointers for
// optional object arrays, while the eval APIs take values. This converts at
// that boundary rather than pointerizing every signature behind it. A nil entry
// is skipped rather than dereferenced.
func Values(in []*EvalDef) []EvalDef {
	if in == nil {
		return nil
	}
	out := make([]EvalDef, 0, len(in))
	for _, e := range in {
		if e != nil {
			out = append(out, *e)
		}
	}
	return out
}

// Threshold is an eval's pass/fail threshold, as the spec defines it:
// {operator, value}. See EvalDef for why this replaced a divergent shape.
type Threshold = packspec.EvalThreshold

// Range defines the valid range for a metric value.
// Range is generated from the schema: an ALIAS for packspec.MetricDefRange.
// The schema nests the bounds inside MetricDef rather than naming them, so the
// generator hoists the shape under a derived name.
type Range = packspec.MetricDefRange

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
// MetricDef is generated from the schema: an ALIAS for packspec.MetricDef.
//
// $defs/MetricDef is additionalProperties:true — an envelope RFC 0006 expects
// runtimes to extend. It names labels, help, aggregation, alert_threshold and
// slo as examples and states they are NOT part of the specification. So there
// is no `Labels` field here and there should not be: labels are a runtime
// extension carried in Extra, alongside any other key a deployment adds.
//
// Read them with MetricLabels, which does the coercion once.
type MetricDef = packspec.MetricDef

// SetMetricLabels records Prometheus labels on a metric.
//
// The inverse of MetricLabels. Labels are a runtime extension, so they live in
// Extra rather than a named field; this keeps producers from hand-building the
// nested map[string]any shape and getting it subtly wrong. Passing an empty map
// removes the key rather than writing an empty object.
func SetMetricLabels(m *MetricDef, labels map[string]string) {
	if m == nil {
		return
	}
	if len(labels) == 0 {
		delete(m.Extra, "labels")
		return
	}
	if m.Extra == nil {
		m.Extra = map[string]any{}
	}
	raw := make(map[string]any, len(labels))
	for k, v := range labels {
		raw[k] = v
	}
	m.Extra["labels"] = raw
}

// MetricLabels returns the Prometheus labels a metric declares, or nil.
//
// labels live in MetricDef.Extra because the spec deliberately does not define
// them (RFC 0006: "the spec defines the envelope; runtimes extend it"). This
// keeps the type assertion in one place rather than at each call site, which is
// where a silent nil creeps in. A labels value of the wrong shape yields nil
// rather than a partial map — validation reports the shape error.
func MetricLabels(m *MetricDef) map[string]string {
	if m == nil {
		return nil
	}
	raw, ok := m.Extra["labels"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil
		}
		out[k] = s
	}
	return out
}

// EvalResult captures the outcome of a single eval execution.
// Handlers produce scores only (0.0–1.0). There is no pass/fail on evals.
// Assertion wrappers store pass/fail as a bool in Value.
type EvalResult struct {
	EvalID string `json:"eval_id"`
	Type   string `json:"type"`

	// Kind is the ROLE this result was produced in, STATED by whatever produced
	// it rather than inferred from Type downstream. It used to be inferred, by
	// string-matching Type against the two wrapper names, which quietly
	// misreported any other wrapper as a plain eval.
	//
	// The zero value is EvalKindEval: an eval nothing wrapped is a measurement.
	Kind events.EvalKind `json:"kind,omitempty"`

	// Passed is set ONLY by a role that coerces to a boolean — an assertion or
	// a guardrail. An eval MEASURES; it does not pass or fail, and for
	// EvalKindEval this is always nil.
	//
	// That is enforced, not merely documented: every handler result funnels
	// through EvalRunner.executeHandler, which strips this for any result that
	// is not an assertion or a guardrail. A handler that sets it cannot leak
	// one to a consumer. See TestExecuteHandler_StripsPassedFromAPlainEval.
	//
	// It is also not DERIVED. Deriving it is how an llm_judge scoring 0.9 came
	// to be reported as FAILED (#1861): `score >= 1.0` is the assertion's
	// default threshold showing through, not a judgement anyone made.
	Passed *bool `json:"passed,omitempty"`

	Score *float64 `json:"score,omitempty"`

	// Value is what the eval MEASURED — the handler's own output, in whatever
	// shape that handler produces: a rubric's per-criterion map, a classifier's
	// label, a reasoning service's JSON.
	//
	// A wrapper does not overwrite it. The assertion wrapper used to replace it
	// with its own boolean, destroying the inner eval's output — the judge
	// reasoning, the rubric breakdown — so the richest thing an eval produced
	// was thrown away by the act of asserting on it (#1875). The boolean now
	// has its own field, above.
	Value       any      `json:"value,omitempty"`
	MetricValue *float64 `json:"metric_value,omitempty"`
	Explanation string   `json:"explanation,omitempty"`
	DurationMs  int64    `json:"duration_ms"`
	Error       string   `json:"error,omitempty"`

	// Message is the AUTHOR-configured message for this eval — EvalDef.Message,
	// written in the pack as AssertionConfig.message — as distinct from
	// Explanation, which is whatever the handler computed.
	//
	// DO NOT DELETE THIS BECAUSE NOTHING IN THIS REPO SETS IT. Nothing does,
	// deliberately: the runtime has no reason to copy the message it was
	// handed back to itself. PromptArena fills it downstream from its own
	// assertion config (arena/engine/eval_orchestrator.go), moving any
	// existing value to Details["explanation"] first, so wiring it here would
	// only be overwritten.
	//
	// It is carried so a result serialized by the runtime and a result
	// populated by a consumer have the same shape. A grep for producers finds
	// none, which is exactly the evidence that would justify removing it —
	// hence this comment.
	Message    string          `json:"message,omitempty"`
	Details    map[string]any  `json:"details,omitempty"`
	Violations []EvalViolation `json:"violations,omitempty"`
	Skipped    bool            `json:"skipped,omitempty"`
	SkipReason string          `json:"skip_reason,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	TurnIndex  int             `json:"turn_index,omitempty"`
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

	// PriorResults holds results from evals that have already run in this
	// batch. This allows evals like guardrail_triggered to inspect the
	// outcomes of earlier evals without coupling to pipeline internals.
	PriorResults []EvalResult `json:"prior_results,omitempty"`

	// ContentScope tells content-matching handlers how much of the
	// conversation to examine. Empty (the default) means the whole
	// transcript, which is what evals and assertions want: "was this ever
	// said". ContentScopeCurrent means CurrentOutput only, which is what a
	// guardrail wants: it judges one specific message.
	//
	// This must be explicit. BuildEvalContext always populates CurrentOutput,
	// so its presence cannot be used to infer scope — doing so silently
	// collapses every eval and assertion to the last assistant turn.
	ContentScope string `json:"content_scope,omitempty"`
}

// Content scopes for EvalContext.ContentScope.
const (
	// ContentScopeTranscript examines every assistant message. The zero value,
	// and the behavior evals and assertions rely on.
	ContentScopeTranscript = ""

	// ContentScopeCurrent examines only EvalContext.CurrentOutput — the single
	// message the caller is judging. Set by the guardrail adapter, for which
	// the content under test may be a user message (input direction) that a
	// transcript scan filtered to assistant role would never see.
	ContentScopeCurrent = "current"
)

// ToolCallRecord is an alias for types.ToolCallRecord so existing code
// referencing evals.ToolCallRecord continues to compile unchanged.
type ToolCallRecord = types.ToolCallRecord
