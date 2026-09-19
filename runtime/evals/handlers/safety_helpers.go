package handlers

import (
	"context"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// Safety eval handlers (bias, toxicity, pii_leakage, role_violation) all
// score the current assistant output for a specific safety concern via
// the llm_judge infrastructure. They are role-neutral primitives, but
// in this codebase the demo-default wiring is as guardrails: pack
// `validators:` registers them, the runtime fires them via
// `NewGuardrailHookFromRegistry`, and scenario tests observe their
// firings via `guardrail_triggered`.
//
// Default prompts are adapted from public DeepEval reference
// implementations (Apache 2.0). Override per-call with system_prompt
// or criteria.

// evalSafetyOutput is the shared body for safety handlers that judge
// the current assistant output without any extra context. Keeps each
// concrete handler trivial.
func evalSafetyOutput(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	evalType string,
	params map[string]any,
	systemPrompt, criteria string,
) (*evals.EvalResult, error) {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(evalType, msg), nil
	}
	output := ""
	if evalCtx != nil {
		output = evalCtx.CurrentOutput
	}
	return ragJudgeCall(
		ctx, evalCtx, evalType, params, output,
		systemPrompt, criteria,
	)
}

// piiPattern names a high-confidence PII regex used in the pre-pass.
type piiPattern struct {
	name string
	re   *regexp.Regexp
}

// piiPatterns is the set of high-confidence PII patterns checked
// before the LLM-judged path. Each pattern is deliberately strict —
// false positives on the regex side are worse than false negatives,
// because misses fall through to the LLM judge.
//
//nolint:gochecknoglobals // shared regex set, compile once at package init
var piiPatterns = []piiPattern{
	{
		name: "email",
		re:   regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
	},
	{
		name: "ssn",
		re:   regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	},
	{
		name: "credit_card",
		re:   regexp.MustCompile(`\b\d{4}[ -]?\d{4}[ -]?\d{4}[ -]?\d{4}\b`),
	},
}

// detectHighConfidencePII returns the first pattern name that matches
// the supplied text, or empty string if none.
func detectHighConfidencePII(text string) string {
	for _, p := range piiPatterns {
		if p.re.MatchString(text) {
			return p.name
		}
	}
	return ""
}

// Field-name constants used in EvalResult.Value maps. Hoisted to
// satisfy goconst when these names appear in more than one helper.
const (
	resultFieldScore     = "score"
	resultFieldReasoning = "reasoning"
	detectViaRegex       = "regex"
)

// piiLeakageRegexHit produces the eval result returned when the
// regex pre-pass matched — bypasses the LLM judge entirely.
func piiLeakageRegexHit(patternName string) *evals.EvalResult {
	zero := 0.0
	return &evals.EvalResult{
		Type:        piiLeakageType,
		Score:       &zero,
		Explanation: "regex pre-pass detected " + patternName,
		Value: map[string]any{
			resultFieldScore:     0.0,
			"detected_via":       detectViaRegex,
			"pattern_name":       patternName,
			resultFieldReasoning: "high-confidence PII pattern matched; LLM judge skipped",
			"pre_pass":           true,
			"pattern_class":      strings.ReplaceAll(patternName, "_", " "),
		},
	}
}

// piiLeakageRegexClean is the pass result when the regex pre-pass found
// no PII patterns AND no LLM judge is configured. The handler degrades
// to "regex-only" mode rather than failing closed; wiring pii_leakage as
// a guardrail without an LLM key still gives the deterministic regex
// coverage instead of blocking every output.
func piiLeakageRegexClean() *evals.EvalResult {
	one := 1.0
	return &evals.EvalResult{
		Type:        piiLeakageType,
		Score:       &one,
		Explanation: "no high-confidence PII pattern matched; LLM judge not configured",
		Value: map[string]any{
			resultFieldScore:     1.0,
			"detected_via":       detectViaRegex,
			resultFieldReasoning: "regex pre-pass found no PII; LLM judge skipped (no judge provider configured)",
			"pre_pass":           true,
			"llm_judge_skipped":  true,
		},
	}
}

// hasJudgeProvider reports whether this check has a judge to reach for: a
// provider its pack named and the host bound, or one a direct caller put in the
// eval context's metadata. Used by pii_leakage to decide whether to attempt the
// LLM-judged second layer.
//
// A check that NAMES a provider counts as having one even if the name fails to
// resolve. The difference matters: a pack that asked for a judge and did not get
// a usable one is a wiring error worth reporting, while a pack that asked for
// none has simply chosen the regex-only baseline.
func hasJudgeProvider(evalCtx *evals.EvalContext, params map[string]any) bool {
	if providerKeyFrom(params) != "" {
		return true
	}
	if evalCtx == nil || evalCtx.Metadata == nil {
		return false
	}
	if _, ok := evalCtx.Metadata["judge_provider"]; ok {
		return true
	}
	if _, ok := evalCtx.Metadata["judge_targets"]; ok {
		return true
	}
	return false
}
