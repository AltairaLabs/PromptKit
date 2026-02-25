package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// LLMJudgeHandler evaluates a single assistant turn using an
// LLM judge. The JudgeProvider must be supplied in
// evalCtx.Metadata["judge_provider"].
//
// Params:
//   - criteria (string, required): what to evaluate
//   - rubric (string, optional): detailed scoring guidance
//   - model (string, optional): model override for the judge
//   - system_prompt (string, optional): override default system prompt
//   - min_score (float64, optional): minimum score to pass
type LLMJudgeHandler struct{}

// Type returns the eval type identifier.
func (h *LLMJudgeHandler) Type() string { return "llm_judge" }

// Eval runs the LLM judge on the current assistant output.
func (h *LLMJudgeHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: extractErr.Error(),
		}, nil
	}

	opts := buildJudgeOpts(evalCtx.CurrentOutput, params)

	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("judge error: %v", judgeErr),
		}, nil
	}

	return buildEvalResult(h.Type(), judgeResult, params), nil
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
	if v, ok := params["min_score"].(float64); ok {
		opts.MinScore = &v
	}

	return opts
}

// buildEvalResult converts a JudgeResult into an EvalResult,
// applying the min_score threshold when provided.
func buildEvalResult(
	evalType string,
	jr *JudgeResult,
	params map[string]any,
) *evals.EvalResult {
	score := jr.Score
	passed := jr.Passed

	if minScore, ok := params["min_score"].(float64); ok {
		passed = score >= minScore
	}

	return &evals.EvalResult{
		Type:        evalType,
		Passed:      passed,
		Score:       &score,
		Explanation: jr.Reasoning,
	}
}
