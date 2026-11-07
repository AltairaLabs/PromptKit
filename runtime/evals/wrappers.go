package evals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// WrapperTypeAssertion is the eval type name for the assertion wrapper handler.
const WrapperTypeAssertion = "assertion"

// WrapperTypeGuardrail is the eval type name for the guardrail wrapper handler.
const WrapperTypeGuardrail = "guardrail"

// scoreThresholdParamKeys are the wrapper-level param names that convert an
// inner eval's score into a verdict. They are deliberately *not* valid on an
// eval handler — handlers call rejectThresholdParams to refuse them — so a
// wrapper must consume them and strip them before invoking its inner handler.
var scoreThresholdParamKeys = []string{"min_score", "max_score"}

// ScoreThresholds turns an eval score into a verdict. It is the shared
// implementation used by the "guardrail" eval type here and by the pipeline's
// guardrail hook adapter; evals themselves never apply it.
type ScoreThresholds struct {
	// Min triggers on a score below it. Max triggers on a score above it. Both
	// are optional; with neither set, Triggered requires a score of 1.0.
	Min *float64
	Max *float64

	// MinExclusive makes Min strict: a score equal to Min also triggers.
	// MaxExclusive does the same for Max. Both are false for min_score and
	// max_score, which are inclusive; ScoreThresholdsFromSpec sets them for the
	// spec's gt and lt operators.
	MinExclusive bool
	MaxExclusive bool
}

// Threshold operators ScoreThresholdsFromSpec accepts, in the word and symbol
// forms. Matching ignores case and surrounding space.
const (
	thresholdOpGTE = "gte"
	thresholdOpGT  = "gt"
	thresholdOpLTE = "lte"
	thresholdOpLT  = "lt"
	thresholdOpEQ  = "eq"
)

// thresholdOperators maps each accepted operator spelling to its word form.
var thresholdOperators = map[string]string{
	thresholdOpGTE: thresholdOpGTE, ">=": thresholdOpGTE,
	thresholdOpGT: thresholdOpGT, ">": thresholdOpGT,
	thresholdOpLTE: thresholdOpLTE, "<=": thresholdOpLTE,
	thresholdOpLT: thresholdOpLT, "<": thresholdOpLT,
	thresholdOpEQ: thresholdOpEQ, "==": thresholdOpEQ,
}

// ScoreThresholdsFromSpec converts an eval's spec-shaped threshold
// ({operator, value}) into ScoreThresholds. The operators are gte (>=), gt (>),
// lte (<=), lt (<) and eq (==); gt and lt are strict. A nil threshold yields
// the zero ScoreThresholds. An unknown operator or a missing value is an error.
func ScoreThresholdsFromSpec(t *Threshold) (ScoreThresholds, error) {
	if t == nil {
		return ScoreThresholds{}, nil
	}
	op, ok := thresholdOperators[strings.ToLower(strings.TrimSpace(t.Operator))]
	if !ok {
		return ScoreThresholds{}, fmt.Errorf(
			"threshold.operator %q is not supported; use one of gte (>=), gt (>), lte (<=), lt (<), eq (==)",
			t.Operator)
	}
	if t.Value == nil {
		return ScoreThresholds{}, fmt.Errorf("threshold.value is required with operator %q", t.Operator)
	}
	v := *t.Value
	switch op {
	case thresholdOpGTE:
		return ScoreThresholds{Min: &v}, nil
	case thresholdOpGT:
		return ScoreThresholds{Min: &v, MinExclusive: true}, nil
	case thresholdOpLTE:
		return ScoreThresholds{Max: &v}, nil
	case thresholdOpLT:
		return ScoreThresholds{Max: &v, MaxExclusive: true}, nil
	default: // thresholdOpEQ
		return ScoreThresholds{Min: &v, Max: &v}, nil
	}
}

// wrapperThresholds returns the thresholds a wrapper applies: its min_score and
// max_score params, or the eval's declared threshold. Declaring both is an
// error rather than a silent precedence rule, because the two can disagree.
func wrapperThresholds(params map[string]any, spec *Threshold) (ScoreThresholds, error) {
	fromParams := ExtractScoreThresholds(params)
	if spec == nil {
		return fromParams, nil
	}
	if fromParams.Min != nil || fromParams.Max != nil {
		return ScoreThresholds{}, fmt.Errorf(
			"threshold and min_score/max_score are both set; declare the pass line once")
	}
	return ScoreThresholdsFromSpec(spec)
}

// thresholdWrapper is implemented by the wrappers that apply EvalDef.Threshold.
// EvalRunner calls evalWithThreshold in place of Eval when the def declares a
// threshold, since Eval receives only params.
type thresholdWrapper interface {
	evalWithThreshold(
		ctx context.Context, evalCtx *EvalContext, params map[string]any, threshold *Threshold,
	) (*EvalResult, error)
}

// checkWrapperThreshold reports a def's threshold that its wrapper could not
// apply. Defs whose handler is not a wrapper are not checked: a plain eval
// carries its threshold for a consumer and never applies it.
func checkWrapperThreshold(handler EvalTypeHandler, def *EvalDef) error {
	if def.Threshold == nil {
		return nil
	}
	if _, ok := handler.(thresholdWrapper); !ok {
		return nil
	}
	_, err := wrapperThresholds(def.Params, def.Threshold)
	return err
}

// ExtractScoreThresholds reads min_score and max_score from wrapper params.
// A value that is missing or not a number is left unset.
func ExtractScoreThresholds(params map[string]any) ScoreThresholds {
	return ScoreThresholds{
		Min: extractOptionalFloat64(params, "min_score"),
		Max: extractOptionalFloat64(params, "max_score"),
	}
}

// StripScoreThresholds returns params without min_score and max_score, so the
// keys never reach an inner eval handler. Handlers reject those keys with an
// error result that scores 0, which a guardrail reads as a block. The input map
// is never mutated.
func StripScoreThresholds(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	present := false
	for _, key := range scoreThresholdParamKeys {
		if _, ok := params[key]; ok {
			present = true
			break
		}
	}
	if !present {
		return params
	}
	stripped := make(map[string]any, len(params))
	for k, v := range params {
		stripped[k] = v
	}
	for _, key := range scoreThresholdParamKeys {
		delete(stripped, key)
	}
	return stripped
}

// Triggered reports whether a result trips a guardrail under these thresholds.
//
// A nil result or nil score means the handler could not judge, and Triggered
// returns true: a guardrail fails closed, as the pipeline's guardrail hook does.
// The assertion wrapper makes the opposite choice for a nil score and passes it.
func (t ScoreThresholds) Triggered(result *EvalResult) bool {
	if result == nil || result.Score == nil {
		return true
	}
	minScore := t.Min
	if minScore == nil && t.Max == nil {
		perfect := 1.0
		minScore = &perfect
	}
	return !(ScoreThresholds{
		Min: minScore, Max: t.Max, MinExclusive: t.MinExclusive, MaxExclusive: t.MaxExclusive,
	}).within(*result.Score)
}

// within reports whether score satisfies every bound that is set.
func (t ScoreThresholds) within(score float64) bool {
	if t.Min != nil && (score < *t.Min || (t.MinExclusive && score == *t.Min)) {
		return false
	}
	if t.Max != nil && (score > *t.Max || (t.MaxExclusive && score == *t.Max)) {
		return false
	}
	return true
}

// ResultDetailReplacement is the EvalResult.Details key under which a check
// supplies the text that replaces a turn it denies, when the check itself
// decides what to answer (topic_policy with on_deny set to respond). A guardrail
// uses it when the validator sets no message of its own.
const ResultDetailReplacement = "replacement"

// ResultReplacement returns the replacement text a result supplies under
// ResultDetailReplacement, or "" when it supplies none.
func ResultReplacement(result *EvalResult) string {
	if result == nil {
		return ""
	}
	s, _ := result.Details[ResultDetailReplacement].(string)
	return s
}

// extractOptionalFloat64 extracts a float64 from params, handling int->float64.
func extractOptionalFloat64(params map[string]any, key string) *float64 {
	v, ok := params[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	default:
		return nil
	}
}

// extractParamString extracts a string param with a default.
func extractParamString(params map[string]any, key, defaultVal string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return defaultVal
}

// extractEvalParams extracts the nested eval_params map from wrapper params.
// Returns nil if eval_params is not set or not a map.
func extractEvalParams(params map[string]any) map[string]any {
	if ep, ok := params["eval_params"].(map[string]any); ok {
		return ep
	}
	return nil
}

// AssertionEvalHandler is a registered eval type ("assertion") that wraps an
// inner eval and applies pass/fail judgment based on score thresholds.
//
// The result is the inner eval's result with Kind set to EvalKindAssertion and
// Passed set. Passed is false when the inner result carries an Error, true when
// it carries no score, and otherwise true when the score satisfies min_score and
// max_score. With neither set, min_score defaults to 1.0.
//
// When EvalRunner runs a def with a Threshold, the threshold sets the pass line
// instead (see ScoreThresholdsFromSpec). A def that sets both a Threshold and
// min_score or max_score is an error result.
//
// Params structure:
//
//	{
//	  "eval_type":  "llm_judge",           // inner eval type (required)
//	  "eval_params": { "criteria": "..." }, // params for inner eval
//	  "min_score":  0.8,                    // minimum passing score (optional)
//	  "max_score":  1.0                     // maximum passing score (optional)
//	}
type AssertionEvalHandler struct {
	registry *EvalTypeRegistry
}

// Type returns the registered eval type name.
func (h *AssertionEvalHandler) Type() string { return WrapperTypeAssertion }

// Eval resolves the inner handler from the registry, executes it, and applies
// threshold-based pass/fail judgment.
func (h *AssertionEvalHandler) Eval(
	ctx context.Context, evalCtx *EvalContext, params map[string]any,
) (*EvalResult, error) {
	return h.evalWithThreshold(ctx, evalCtx, params, nil)
}

// evalWithThreshold is Eval with the def's declared threshold, which replaces
// min_score and max_score as the pass line.
func (h *AssertionEvalHandler) evalWithThreshold(
	ctx context.Context, evalCtx *EvalContext, params map[string]any, threshold *Threshold,
) (*EvalResult, error) {
	handler, innerParams, err := prepareInner(h.registry, WrapperTypeAssertion, params)
	if err != nil {
		return nil, err
	}

	thresholds, err := wrapperThresholds(params, threshold)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", WrapperTypeAssertion, err)
	}

	result, err := handler.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	// State the role and the boolean. Do NOT touch Value: the assertion's
	// judgement is its own field, and the inner eval's output — the rubric
	// breakdown, the judge's structured reasoning — is the thing a consumer
	// most wants and the thing this used to overwrite (#1875).
	passed := h.passes(result, thresholds)
	result.Kind = events.EvalKindAssertion
	result.Passed = &passed
	return result, nil
}

// passes determines pass/fail from thresholds. With no bound set, the inner
// eval must score 1.0.
func (h *AssertionEvalHandler) passes(result *EvalResult, t ScoreThresholds) bool {
	// An inner Error means no measurement was taken. Its score is a
	// placeholder 0, which a max_score gate would read as a clean pass.
	if result.Error != "" {
		return false
	}
	if t.Min == nil && t.Max == nil {
		// Default: inner eval must score 1.0 to pass the assertion.
		defaultMin := 1.0
		t.Min = &defaultMin
	}
	if result.Score == nil {
		return true
	}
	return t.within(*result.Score)
}

// GuardrailEvalHandler is a registered eval type ("guardrail") that wraps an
// inner eval and determines whether the guardrail was triggered.
//
// The result is the inner eval's result with Kind set to EvalKindGuardrail,
// Passed set to the inverse of triggered, and Details["triggered"] and
// Details["action"] set. A guardrail is triggered under ScoreThresholds.Triggered:
// the score is below min_score or above max_score (min_score defaults to 1.0
// when neither is set), or the inner result has no score.
//
// When EvalRunner runs a def with a Threshold, the threshold sets the line
// instead, as for AssertionEvalHandler.
//
// Params structure:
//
//	{
//	  "eval_type":   "content_excludes",     // inner eval type (required)
//	  "eval_params": { "patterns": ["..."] }, // params for inner eval
//	  "action":      "block",                 // recorded in Details["action"] (optional, default: "block")
//	  "min_score":   0.8,                     // triggers below this score (optional)
//	  "max_score":   1.0                      // triggers above this score (optional)
//	}
type GuardrailEvalHandler struct {
	registry *EvalTypeRegistry
}

// Type returns the registered eval type name.
func (h *GuardrailEvalHandler) Type() string { return WrapperTypeGuardrail }

// Eval resolves the inner handler from the registry, executes it, and determines
// whether the guardrail was triggered.
func (h *GuardrailEvalHandler) Eval(
	ctx context.Context, evalCtx *EvalContext, params map[string]any,
) (*EvalResult, error) {
	return h.evalWithThreshold(ctx, evalCtx, params, nil)
}

// evalWithThreshold is Eval with the def's declared threshold, which replaces
// min_score and max_score as the trigger line.
func (h *GuardrailEvalHandler) evalWithThreshold(
	ctx context.Context, evalCtx *EvalContext, params map[string]any, threshold *Threshold,
) (*EvalResult, error) {
	handler, innerParams, err := prepareInner(h.registry, WrapperTypeGuardrail, params)
	if err != nil {
		return nil, err
	}

	action := extractParamString(params, "action", "block")
	thresholds, err := wrapperThresholds(params, threshold)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", WrapperTypeGuardrail, err)
	}

	result, err := handler.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	// Shared with the pipeline's guardrail hook adapter so the two cannot drift
	// again. This converges one difference: a nil score now counts as triggered
	// (fail-closed) where this path previously read it as not-triggered. A
	// guardrail whose handler could not produce a score has not cleared
	// anything, and no test pinned the old behavior.
	triggered := thresholds.Triggered(result)

	// A guardrail coerces to a boolean just as an assertion does, so it states
	// one the same way. It had none before: its outcome reached a consumer only
	// as Details["triggered"], a convention every consumer had to know and one
	// the event schema said nothing about (#1874). Passed is the inverse of
	// triggered — a guardrail that fired did not pass.
	//
	// Details keeps both keys. They are what existing consumers read, and
	// "action" is not expressible as a boolean anyway.
	passed := !triggered
	result.Kind = events.EvalKindGuardrail
	result.Passed = &passed

	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details["triggered"] = triggered
	result.Details["action"] = action
	return result, nil
}

// innerEvalType reports the eval type a wrapper delegates to, and whether the
// param was usable.
func innerEvalType(params map[string]any) (string, bool) {
	t, ok := params["eval_type"].(string)
	return t, ok && t != ""
}

// prepareInner resolves a wrapper's inner handler and the params to call it
// with, applying the same treatment the direct declaration path gives an eval:
// the inner type's defaults, its param aliases, and its own ValidateParams.
//
// Without this a wrapper silently disarmed whatever it wrapped. Two separate
// failures, both observed: a wrapped check never saw its own ParamDefaults, so
// an eval type declaring `direction: input` reverted to the factory's output
// default and never gated the user's turn; and the inner ValidateParams never
// ran, so a misspelled key inside eval_params loaded clean and produced a
// policy with none of its exclusions. Direct declarations had both protections
// and wrapped ones had neither.
func prepareInner(
	registry *EvalTypeRegistry, wrapper string, params map[string]any,
) (EvalTypeHandler, map[string]any, error) {
	evalType, ok := innerEvalType(params)
	if !ok {
		return nil, nil, fmt.Errorf("%s handler requires eval_type param", wrapper)
	}

	handler, err := registry.Get(evalType)
	if err != nil {
		return nil, nil, fmt.Errorf("%s inner eval: %w", wrapper, err)
	}

	inner := ApplyDefaults(evalType, extractEvalParams(params))
	inner = NormalizeParams(evalType, inner)

	if pv, vok := handler.(ParamValidator); vok {
		if verr := pv.ValidateParams(inner); verr != nil {
			return nil, nil, fmt.Errorf("%s inner eval %q: %w", wrapper, evalType, verr)
		}
	}
	return handler, inner, nil
}

// innerTimeout reports the wrapped eval's own timeout, so the runner gives a
// wrapped long-running check (tool_exec, an exec eval) the same time it would
// get declared directly.
func innerTimeout(registry *EvalTypeRegistry, params map[string]any) time.Duration {
	evalType, ok := innerEvalType(params)
	if !ok || registry == nil {
		return 0
	}
	handler, err := registry.Get(evalType)
	if err != nil {
		return 0
	}
	ht, ok := handler.(HandlerTimeout)
	if !ok {
		return 0
	}
	return ht.EvalTimeout(ApplyDefaults(evalType, extractEvalParams(params)))
}

// EvalTimeout delegates to the wrapped eval.
func (h *AssertionEvalHandler) EvalTimeout(params map[string]any) time.Duration {
	return innerTimeout(h.registry, params)
}

// EvalTimeout delegates to the wrapped eval.
func (h *GuardrailEvalHandler) EvalTimeout(params map[string]any) time.Duration {
	return innerTimeout(h.registry, params)
}

// ValidateParams lets a wrapped eval's own param validation run at load time,
// the same as a directly declared one. The guardrail factory and
// sdk.ValidatePack both look for this interface, so implementing it here is
// what makes `type: guardrail` wrapping a check with a typo'd param fail to
// load rather than load unprotected.
func (h *AssertionEvalHandler) ValidateParams(params map[string]any) error {
	_, _, err := prepareInner(h.registry, WrapperTypeAssertion, params)
	return err
}

// ValidateParams mirrors AssertionEvalHandler.ValidateParams.
func (h *GuardrailEvalHandler) ValidateParams(params map[string]any) error {
	_, _, err := prepareInner(h.registry, WrapperTypeGuardrail, params)
	return err
}
