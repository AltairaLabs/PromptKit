package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// LLMJudgeHandler evaluates a single assistant turn using an LLM judge
// as a pure eval primitive. It emits the judge's `score` field as
// EvalResult.Score and the judge's reasoning + `passed` opinion in
// Details. Threshold judgment lives on the `type: assertion` wrapper:
//
//   - type: assertion
//     params:
//     eval_type: llm_judge
//     eval_params: { criteria: "...", judge: my-judge }
//     min_score: 0.7
//
// The JudgeProvider must be supplied in evalCtx.Metadata["judge_provider"].
//
// Params:
//   - criteria (string, required): what to evaluate
//   - rubric (string, optional): detailed scoring guidance
//   - model (string, optional): model override for the judge
//   - system_prompt (string, optional): override default system prompt
//
// Putting min_score / max_score on this handler is rejected — the
// assertion wrapper is the canonical home for thresholds.
type LLMJudgeHandler struct{}

// Type returns the eval type identifier.
func (h *LLMJudgeHandler) Type() string { return "llm_judge" }

// Eval runs the LLM judge on the current assistant output.
func (h *LLMJudgeHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: extractErr.Error(),
		}, nil
	}

	opts := buildJudgeOpts(evalCtx.CurrentOutput, params)
	opts.Emitter = emitterFromEvalCtx(evalCtx)

	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("judge error: %v", judgeErr),
		}, nil
	}

	return buildEvalResult(h.Type(), judgeResult), nil
}

// extractJudgeProvider retrieves the JudgeProvider from eval context metadata.
// It first checks for a pre-built "judge_provider" (SDK path), then falls back
// to creating one from "judge_targets" ProviderSpecs (Arena path).
func extractJudgeProvider(
	evalCtx *evals.EvalContext,
) (JudgeProvider, error) {
	if evalCtx.Metadata == nil {
		return nil, fmt.Errorf(
			"judge_provider not found in metadata",
		)
	}

	// Direct JudgeProvider (SDK path)
	if raw, ok := evalCtx.Metadata["judge_provider"]; ok {
		provider, ok := raw.(JudgeProvider)
		if !ok {
			return nil, fmt.Errorf(
				"judge_provider has wrong type: %T",
				raw,
			)
		}
		return provider, nil
	}

	// Fall back to judge_targets (Arena path) — create provider from spec
	if raw, ok := evalCtx.Metadata["judge_targets"]; ok {
		return judgeProviderFromTargets(raw)
	}

	return nil, fmt.Errorf(
		"judge_provider not found in metadata",
	)
}

// judgeProviderFromTargets selects a judge ProviderSpec from the targets map
// and wraps it in a SpecJudgeProvider.
func judgeProviderFromTargets(raw any) (JudgeProvider, error) {
	targets := coerceJudgeTargets(raw)
	if len(targets) == 0 {
		return nil, fmt.Errorf("judge_targets present but empty or wrong type")
	}

	// Select first available judge (the "judge" param selection happens
	// at the assertion config level, not here — the metadata carries
	// the resolved target)
	for k := range targets {
		spec := targets[k]
		return NewSpecJudgeProvider(&spec), nil
	}

	return nil, fmt.Errorf("no judge targets available")
}

// coerceJudgeTargets normalizes metadata targets into a typed map.
func coerceJudgeTargets(raw any) map[string]providers.ProviderSpec {
	switch t := raw.(type) {
	case map[string]providers.ProviderSpec:
		return t
	case map[string]any:
		out := make(map[string]providers.ProviderSpec, len(t))
		for k, v := range t {
			if spec, ok := v.(providers.ProviderSpec); ok {
				out[k] = spec
			}
		}
		return out
	default:
		return nil
	}
}

// emitterFromEvalCtx extracts an *events.Emitter from eval context metadata, if present.
func emitterFromEvalCtx(evalCtx *evals.EvalContext) *events.Emitter {
	if evalCtx == nil || evalCtx.Metadata == nil {
		return nil
	}
	if e, ok := evalCtx.Metadata["emitter"].(*events.Emitter); ok {
		return e
	}
	return nil
}

// buildJudgeOpts constructs JudgeOpts from content and params.
func buildJudgeOpts(
	content string, params map[string]any,
) JudgeOpts {
	opts := JudgeOpts{Content: content}

	if v, ok := params["criteria"].(string); ok {
		opts.Criteria = v
	}
	if v, ok := params["rubric"].(string); ok {
		opts.Rubric = v
	}
	if v, ok := params["model"].(string); ok {
		opts.Model = v
	}
	if v, ok := params["system_prompt"].(string); ok {
		opts.SystemPrompt = v
	}

	return opts
}

// buildEvalResult converts a JudgeResult into an EvalResult. The
// handler is a pure eval primitive: it emits the judge's raw `score`
// as EvalResult.Score and preserves the judge's `passed` opinion and
// reasoning in Details for reporting. Threshold judgment lives on the
// `type: assertion` wrapper, not here.
func buildEvalResult(
	evalType string,
	jr *JudgeResult,
) *evals.EvalResult {
	score := jr.Score
	return &evals.EvalResult{
		Type:        evalType,
		Score:       &score,
		MetricValue: &score,
		Explanation: jr.Reasoning,
		Details: map[string]any{
			resultFieldScore:     score,
			"passed":             jr.Passed,
			resultFieldReasoning: jr.Reasoning,
		},
	}
}

// keyMinScore / keyMaxScore are the threshold-key constants the
// rejection guard checks. Both keys are scenario-config sugar that
// belong on the `type: assertion` wrapper, never on an eval handler.
const (
	keyMinScore = "min_score"
	keyMaxScore = "max_score"
)

// rejectThresholdParams surfaces a clear Error when callers put
// min_score / max_score on the inner eval. Threshold judgment
// belongs on the `type: assertion` wrapper; silently accepting a
// no-op param hides a config mistake. Returns "" when the params
// pass.
func rejectThresholdParams(params map[string]any) string {
	for _, banned := range []string{keyMinScore, keyMaxScore} {
		if _, present := params[banned]; present {
			return banned + " is not a valid param on an LLM-judge eval; " +
				"wrap with `type: assertion` and put the threshold there " +
				"(see runtime/evals/wrappers.go)"
		}
	}
	return ""
}
