package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
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

// extractJudgeProvider retrieves the JudgeProvider from eval
// context metadata.
func extractJudgeProvider(
	evalCtx *evals.EvalContext,
) (JudgeProvider, error) {
	if evalCtx.Metadata == nil {
		return nil, fmt.Errorf(
			"judge_provider not found in metadata",
		)
	}

	raw, ok := evalCtx.Metadata["judge_provider"]
	if !ok {
		return nil, fmt.Errorf(
			"judge_provider not found in metadata",
		)
	}

	provider, ok := raw.(JudgeProvider)
	if !ok {
		return nil, fmt.Errorf(
			"judge_provider has wrong type: %T",
			raw,
		)
	}

	return provider, nil
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
