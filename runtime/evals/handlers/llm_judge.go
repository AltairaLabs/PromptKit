package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// LLMJudgeHandler evaluates a single assistant turn using an LLM judge.
// Pure eval primitive (see docs/reference/checks.md for the
// `type: assertion` wrapper pattern that adds thresholds). The
// JudgeProvider must be supplied in evalCtx.Metadata["judge_provider"].
//
// Params:
//   - criteria (string, required): what to evaluate
//   - rubric (string, optional): detailed scoring guidance
//   - model (string, optional): model override for the judge
//   - system_prompt (string, optional): override default system prompt
type LLMJudgeHandler struct{}

// Type returns the eval type identifier.
func (h *LLMJudgeHandler) Type() string { return "llm_judge" }

// Eval runs the LLM judge on the current assistant output.
func (h *LLMJudgeHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	return runJudgeEval(ctx, evalCtx, h.Type(), params, evalCtx.CurrentOutput), nil
}

// runJudgeEval is the shared body for every LLM-judge eval: reject
// threshold params, resolve the JudgeProvider from context, build the
// judge request, call it, and convert the JudgeResult to an EvalResult.
// Handlers that need preprocessing (filtering tool calls, picking a
// session-level content view) build the `content` arg and call this.
//
// Centralizing here keeps the three judge handlers (llm_judge,
// llm_judge_session, llm_judge_tool_calls) from drifting and stops
// Sonar's CPD flagging the otherwise-identical Eval bodies.
func runJudgeEval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	handlerType string,
	params map[string]any,
	content string,
) *evals.EvalResult {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(handlerType, msg)
	}
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return &evals.EvalResult{
			Type:        handlerType,
			Score:       boolScore(false),
			Explanation: extractErr.Error(),
		}
	}

	opts := buildJudgeOpts(content, params)
	opts.Emitter = emitterFromEvalCtx(evalCtx)

	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return &evals.EvalResult{
			Type:        handlerType,
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("judge error: %v", judgeErr),
		}
	}

	return buildEvalResult(handlerType, judgeResult)
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

// rejectThresholdParams surfaces a clear Error when callers put a
// threshold (min_score / max_score) on an eval handler. Threshold
// judgment lives on the `type: assertion` wrapper — see
// runtime/evals/wrappers.go and runtime/evals/handlers/CLAUDE.md.
// Returns "" when the params pass.
//
// Shared by every eval handler family (classify-backed,
// llm_judge, safety, RAG) — see parseClassifyConfig for the
// classify-backed call site and llm_judge.go / ragJudgeCall /
// evalSafetyOutput for the others.
func rejectThresholdParams(params map[string]any) string {
	for _, banned := range []string{"min_score", "max_score"} {
		if _, present := params[banned]; present {
			return banned + " is not a valid param on an eval handler; " +
				"wrap with `type: assertion` and put the threshold there " +
				"(see runtime/evals/wrappers.go)"
		}
	}
	return ""
}
