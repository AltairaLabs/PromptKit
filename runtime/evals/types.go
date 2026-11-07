// Package evals provides the evaluation framework for PromptPack.
// Eval definitions travel with packs and run both during Arena testing and at
// runtime in production via the SDK.
//
// An eval is a pure measurement. A handler produces a score and never states
// pass or fail, and the runner clears any pass/fail a handler sets on a plain
// eval. Judgment against a threshold belongs to the wrapper that applies it: the
// "assertion" wrapper for tests and the "guardrail" wrapper for runtime
// enforcement (see AssertionEvalHandler, GuardrailEvalHandler and
// ScoreThresholds).
//
// EvalRunner executes EvalDef values against an EvalContext, and
// EvalTypeRegistry maps eval type names to handlers. The built-in handlers live
// in the evals/handlers package, which registers them when imported.
package evals

import (
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// EvalTrigger names when an eval fires.
//
// The Trigger* constants are untyped, so each assigns to both EvalTrigger and
// the plain string EvalDef.Trigger. EvalTrigger does not validate: a Go named
// string type accepts any literal, so `var t EvalTrigger = "nonsense"` compiles.
// ValidTriggers is the check.
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
	// TriggerOnWorkflowStep fires the eval after each committed workflow state
	// transition, against the turn that produced it. See
	// EvalRunner.RunWorkflowStepEvals.
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

	// GroupExternal classifies evals that call external systems: REST APIs,
	// A2A agents, subprocess exec, or LLM judge providers.
	GroupExternal = "external"
)

// Eval type names that appear in both the long-running and external sets.
const (
	typeLLMJudge          = "llm_judge"
	typeLLMJudgeSession   = "llm_judge_session"
	typeLLMJudgeToolCalls = "llm_judge_tool_calls"
	typeA2AEval           = "a2a_eval"
	typeA2AEvalSession    = "a2a_eval_session"
	typeRESTEval          = "rest_eval"
	typeRESTEvalSession   = "rest_eval_session"
)

// longRunningTypes is the set of eval types classified as long-running.
var longRunningTypes = map[string]bool{
	typeLLMJudge:          true,
	typeLLMJudgeSession:   true,
	typeLLMJudgeToolCalls: true,
	"outcome_equivalent":  true,
	"cosine_similarity":   true,
	typeA2AEval:           true,
	typeA2AEvalSession:    true,
	typeRESTEval:          true,
	typeRESTEvalSession:   true,
}

// externalTypes is the set of eval types classified as external.
// These are a subset of long-running types that call external systems.
var externalTypes = map[string]bool{
	typeLLMJudge:          true,
	typeLLMJudgeSession:   true,
	typeLLMJudgeToolCalls: true,
	typeA2AEval:           true,
	typeA2AEvalSession:    true,
	typeRESTEval:          true,
	typeRESTEvalSession:   true,
}

// GroupDeclarer is an optional interface an EvalTypeHandler implements to
// declare the classification groups its eval type belongs to, such as
// GroupLongRunning and GroupExternal for a handler that calls a judge model.
// DefaultGroupsForType returns DefaultEvalGroup followed by these groups.
//
// The declaration is recorded when the handler is passed to RegisterDefault or
// EvalTypeRegistry.Register, under the handler's Type and under any alias
// registered for it. Like RegisterTypeGroups, the record is process-wide, not
// per registry: the latest handler registered for a type sets its groups.
// A handler that does not implement GroupDeclarer is classified by the
// built-in table, which defaults to GroupFastRunning.
type GroupDeclarer interface {
	// EvalGroups returns the handler's classification groups, without
	// DefaultEvalGroup.
	EvalGroups() []string
}

// Type-to-groups state, guarded by typeGroupsMu:
//   - customTypeGroups holds RegisterTypeGroups calls (exec handlers, etc.);
//   - declaredTypeGroups holds what GroupDeclarer handlers declared;
//   - groupAliases maps an alias type to the type it resolves to, so an alias
//     is classified the same as its target.
var (
	customTypeGroups   = make(map[string][]string)
	declaredTypeGroups = make(map[string][]string)
	groupAliases       = make(map[string]string)
	typeGroupsMu       sync.RWMutex
)

// RegisterTypeGroups registers the classification groups DefaultGroupsForType
// returns for a dynamic eval type, such as an exec handler that classifies itself
// as long-running or external. It takes precedence over groups a handler
// declares through GroupDeclarer. It keeps a copy of groups, and is safe to call
// concurrently with DefaultGroupsForType.
func RegisterTypeGroups(evalType string, groups []string) {
	typeGroupsMu.Lock()
	defer typeGroupsMu.Unlock()
	customTypeGroups[evalType] = append([]string(nil), groups...)
}

// recordDeclaredGroups records the groups h declares. A handler that does
// not implement GroupDeclarer clears any declaration a handler it replaced
// made, so the type falls back to the table.
func recordDeclaredGroups(h EvalTypeHandler) {
	d, ok := h.(GroupDeclarer)
	typeGroupsMu.Lock()
	defer typeGroupsMu.Unlock()
	if !ok {
		delete(declaredTypeGroups, h.Type())
		return
	}
	declaredTypeGroups[h.Type()] = append([]string(nil), d.EvalGroups()...)
}

// recordGroupAlias records that aliasType is classified as targetType.
func recordGroupAlias(aliasType, targetType string) {
	typeGroupsMu.Lock()
	defer typeGroupsMu.Unlock()
	groupAliases[aliasType] = targetType
}

// lookupTypeGroups returns the registered or declared groups for evalType,
// following one alias hop. RegisterTypeGroups wins over a declaration.
func lookupTypeGroups(evalType string) ([]string, bool) {
	typeGroupsMu.RLock()
	defer typeGroupsMu.RUnlock()
	candidates := []string{evalType}
	if target, ok := groupAliases[evalType]; ok {
		candidates = append(candidates, target)
	}
	for _, t := range candidates {
		if g, ok := customTypeGroups[t]; ok {
			return g, true
		}
		if g, ok := declaredTypeGroups[t]; ok {
			return g, true
		}
	}
	return nil, false
}

// DefaultGroupsForType returns the groups for a given eval type. The result
// always starts with DefaultEvalGroup, followed by, in order of precedence: the
// groups registered with RegisterTypeGroups for that type; the groups its
// handler declares through GroupDeclarer; or the built-in classification
// (GroupFastRunning, GroupLongRunning or GroupExternal). An alias registered with
// RegisterDefaultAlias or EvalTypeRegistry.RegisterAlias is classified as its
// target.
func DefaultGroupsForType(evalType string) []string {
	groups := []string{DefaultEvalGroup}

	if found, ok := lookupTypeGroups(evalType); ok {
		return append(groups, found...)
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
// evals override pack-level evals by ID (see ResolveEvals).
//
// EvalDef is an alias for packspec.Eval, which is generated from the PromptPack
// schema. A type alias cannot carry methods, so the accessors IsEnabled,
// SamplePercentage and Groups are free functions.
//
// Threshold is applied by the wrapper that states a verdict: on a def of type
// "assertion" or "guardrail", EvalRunner hands it to the wrapper as its pass
// line in place of min_score and max_score (see ScoreThresholdsFromSpec). On
// any other def it is carried, not applied: an eval never states pass or fail,
// and a consumer with its own assertion layer may read it.
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
// (fast-running, long-running, external) based on the eval type. An
// "assertion" or "guardrail" wrapper is classified by the eval it wraps (its
// eval_type param), since that is the work it does.
// When explicit groups are set, returns them as-is (overriding defaults).
func Groups(e *EvalDef) []string {
	if e == nil {
		return nil
	}
	if len(e.Groups) != 0 {
		return e.Groups
	}
	if e.Type == WrapperTypeAssertion || e.Type == WrapperTypeGuardrail {
		if inner, ok := innerEvalType(e.Params); ok {
			return DefaultGroupsForType(inner)
		}
	}
	return DefaultGroupsForType(e.Type)
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

// Threshold is the {operator, value} threshold shape the PromptPack spec defines
// for an eval. The assertion and guardrail wrappers apply it; on a plain eval it
// is carried but not applied. ScoreThresholdsFromSpec lists the operators.
type Threshold = packspec.EvalThreshold

// Range defines the valid range for a metric value. It is an alias for
// packspec.MetricDefRange.
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

// MetricDef defines a Prometheus-style metric associated with an eval. It is an
// alias for packspec.MetricDef.
//
// The spec leaves MetricDef open to runtime extension, so keys the spec does not
// name, including labels, are carried in the Extra field. Read labels with
// MetricLabels and write them with SetMetricLabels.
type MetricDef = packspec.MetricDef

// SetMetricLabels records Prometheus labels on a metric.
//
// It is the inverse of MetricLabels and stores the labels under the "labels" key
// of MetricDef.Extra. Passing an empty map removes the key. A nil metric is
// ignored.
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
// Labels live in the "labels" key of MetricDef.Extra. A labels value of the wrong
// shape, or one with a non-string label value, yields nil rather than a partial
// map; ValidateEvals does not report it.
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
// Handlers produce scores only (0.0 to 1.0); there is no pass/fail on a plain
// eval. The assertion and guardrail wrappers state a verdict in Passed.
type EvalResult struct {
	EvalID string `json:"eval_id"`
	Type   string `json:"type"`

	// Kind is the role this result was produced in, stated by whatever produced
	// it. The zero value is EvalKindEval: an eval nothing wrapped is a
	// measurement.
	Kind events.EvalKind `json:"kind,omitempty"`

	// Passed is set only by a role that coerces a score to a boolean: an
	// assertion or a guardrail. For a guardrail it is the inverse of
	// Details["triggered"]. An eval measures and does not pass or fail, so for
	// EvalKindEval it is always nil.
	//
	// EvalRunner enforces this: it clears Passed on any handler result whose Kind
	// is not assertion or guardrail. Passed is never derived from Score.
	Passed *bool `json:"passed,omitempty"`

	Score *float64 `json:"score,omitempty"`

	// Value is what the eval measured: the handler's own output, in whatever
	// shape that handler produces, such as a rubric's per-criterion map, a
	// classifier's label, or a reasoning service's JSON. The assertion and
	// guardrail wrappers leave it unchanged.
	Value       any      `json:"value,omitempty"`
	MetricValue *float64 `json:"metric_value,omitempty"`

	// Metrics are named sub-scores the eval produced alongside its score, such as
	// a rubric's clarity and accuracy from one judge call, or an exec program's
	// functional_score and security_score.
	//
	// The names come from the handler's output rather than from the pack, which
	// declares one metric per eval, so they are not trusted: a judge model can
	// return a new key on any call. Reports and events carry them as they are.
	// Prometheus export is opt-in on the host's collector and bounded there
	// (metrics.CollectorOpts.EvalDimensionLimit). Put a large per-test breakdown
	// in Details instead.
	Metrics map[string]float64 `json:"metrics,omitempty"`

	Explanation string `json:"explanation,omitempty"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`

	// Message is the author-configured message for this eval, as distinct from
	// Explanation, which is what the handler computed. The runtime never sets
	// it. A consumer such as PromptArena fills it from its own assertion
	// configuration, so that a result serialized by the runtime and one populated
	// by a consumer have the same shape.
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

	// ToolsOffered holds the tool names this turn handed the provider: the
	// descriptor set that reaches ToolSupport.BuildTooling, after the prompt's
	// allowed_tools, the implicit capability tools, exclusions and skill tool
	// grants have all been applied. Sorted and deduplicated.
	//
	// This is what the model could see, as opposed to ToolCalls, which is what
	// it chose to do. Only the former shows that a skill's allowed-tools grant
	// took effect, because a grant that works and one that does nothing produce
	// identical ToolCalls whenever the model does not call the granted tool.
	//
	// Hosts populate it; it is empty when the host does not.
	ToolsOffered []string       `json:"tools_offered,omitempty"`
	SessionID    string         `json:"session_id"`
	PromptID     string         `json:"prompt_id"`
	Variables    map[string]any `json:"variables,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Extras       map[string]any `json:"extras,omitempty"`

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
	// The scope is explicit because BuildEvalContext always populates
	// CurrentOutput, so its presence cannot indicate scope.
	ContentScope string `json:"content_scope,omitempty"`
}

// Content scopes for EvalContext.ContentScope.
const (
	// ContentScopeTranscript examines every assistant message. The zero value,
	// and the behavior evals and assertions rely on.
	ContentScopeTranscript = ""

	// ContentScopeCurrent examines only EvalContext.CurrentOutput, the single
	// message the caller is judging. The guardrail adapter sets it, because the
	// content under test may be a user message (input direction) that a scan of
	// assistant messages would never see.
	ContentScopeCurrent = "current"
)

// ToolCallRecord is an alias for types.ToolCallRecord.
type ToolCallRecord = types.ToolCallRecord
