package guardrails

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// Direction constants for guardrail hook evaluation.
const (
	directionInput  = "input"
	directionOutput = "output"
	directionBoth   = "both"
)

// GuardrailHookAdapter wraps an evals.EvalTypeHandler as a hooks.ProviderHook.
// This bridges the unified eval system to the pipeline's hook infrastructure,
// allowing any registered eval handler to be used as a guardrail.
type GuardrailHookAdapter struct {
	handler   evals.EvalTypeHandler
	evalType  string
	params    map[string]any
	direction string // "input" | "output" | "both"
}

// Compile-time interface check.
var _ hooks.ProviderHook = (*GuardrailHookAdapter)(nil)

// Name returns the eval type identifier for this guardrail.
func (a *GuardrailHookAdapter) Name() string { return a.evalType }

// BeforeCall checks input messages when direction is "input" or "both".
// For input direction, it evaluates the last user message.
func (a *GuardrailHookAdapter) BeforeCall(
	ctx context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	if a.direction != directionInput && a.direction != directionBoth {
		return hooks.Allow
	}
	if req == nil || len(req.Messages) == 0 {
		return hooks.Allow
	}

	// Evaluate the last message content
	lastMsg := req.Messages[len(req.Messages)-1]
	evalCtx := &evals.EvalContext{
		CurrentOutput: lastMsg.GetContent(),
		Messages:      req.Messages,
	}

	return a.evaluate(ctx, evalCtx)
}

// AfterCall checks provider output when direction is "output" or "both".
func (a *GuardrailHookAdapter) AfterCall(
	ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	if a.direction == directionInput {
		return hooks.Allow
	}

	evalCtx := &evals.EvalContext{
		CurrentOutput: resp.Message.GetContent(),
	}
	if req != nil {
		evalCtx.Messages = req.Messages
	}

	return a.evaluate(ctx, evalCtx)
}

// evaluate runs the handler and converts the EvalResult to a Decision.
func (a *GuardrailHookAdapter) evaluate(
	ctx context.Context, evalCtx *evals.EvalContext,
) hooks.Decision {
	// Normalize legacy param names before invoking the handler
	params := evals.NormalizeParams(a.evalType, a.params)

	result, err := a.handler.Eval(ctx, evalCtx, params)
	if err != nil {
		return hooks.Deny("guardrail error: " + err.Error())
	}

	// Check score-based failure: score < 1.0 means violation
	if result.Score != nil && *result.Score < 1.0 {
		return a.deny(result)
	}

	// Check Passed for handlers that set it directly
	if !result.Passed {
		return a.deny(result)
	}

	return hooks.Allow
}

// deny builds a DenyWithMetadata decision from an EvalResult.
func (a *GuardrailHookAdapter) deny(result *evals.EvalResult) hooks.Decision {
	return hooks.DenyWithMetadata(result.Explanation, map[string]any{
		"validator_type": a.evalType,
		"score":          result.Score,
		"value":          result.Value,
	})
}
