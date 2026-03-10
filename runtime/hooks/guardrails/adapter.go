package guardrails

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
//
// When a guardrail triggers, the adapter enforces in-place (truncating or
// replacing content) and returns an Enforced decision so the pipeline continues.
type GuardrailHookAdapter struct {
	handler     evals.EvalTypeHandler
	evalType    string
	params      map[string]any
	direction   string // "input" | "output" | "both"
	message     string // User-facing message when content is blocked
	monitorOnly bool   // When true, evaluate but don't enforce (no content modification)
}

// Compile-time interface checks.
var (
	_ hooks.ProviderHook     = (*GuardrailHookAdapter)(nil)
	_ hooks.ChunkInterceptor = (*GuardrailHookAdapter)(nil)
)

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
// When the guardrail triggers, it enforces in-place on resp.Message
// (truncating or replacing content) and returns an Enforced decision.
func (a *GuardrailHookAdapter) AfterCall(
	ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	if a.direction == directionInput {
		return hooks.Allow
	}

	// Build messages list: request messages + the response being evaluated.
	var msgs []types.Message
	if req != nil {
		msgs = make([]types.Message, len(req.Messages)+1)
		copy(msgs, req.Messages)
		msgs[len(req.Messages)] = resp.Message
	} else {
		msgs = []types.Message{resp.Message}
	}

	evalCtx := &evals.EvalContext{
		CurrentOutput: resp.Message.GetContent(),
		Messages:      msgs,
	}

	// Apply defaults for aliased eval types, then normalize legacy param names
	params := evals.ApplyDefaults(a.evalType, a.params)
	params = evals.NormalizeParams(a.evalType, params)

	result, err := a.handler.Eval(ctx, evalCtx, params)
	if err != nil {
		return hooks.Deny("guardrail error: " + err.Error())
	}

	if !result.IsPassed() {
		if !a.monitorOnly {
			a.enforce(&resp.Message, params)
		}
		return a.enforced(result)
	}

	return hooks.Allow
}

// evaluate runs the handler and converts the EvalResult to a Decision.
func (a *GuardrailHookAdapter) evaluate(
	ctx context.Context, evalCtx *evals.EvalContext,
) hooks.Decision {
	// Apply defaults for aliased eval types, then normalize legacy param names
	params := evals.ApplyDefaults(a.evalType, a.params)
	params = evals.NormalizeParams(a.evalType, params)

	result, err := a.handler.Eval(ctx, evalCtx, params)
	if err != nil {
		return hooks.Deny("guardrail error: " + err.Error())
	}

	// Use IsPassed() which derives pass/fail from Score (score < 1.0 = fail).
	if !result.IsPassed() {
		return a.enforced(result)
	}

	return hooks.Allow
}

// OnChunk evaluates streaming chunks via StreamableEvalHandler.EvalPartial.
// When a guardrail triggers, it truncates the chunk content and returns
// an Enforced decision so the provider stage can stop reading but continue
// the pipeline.
func (a *GuardrailHookAdapter) OnChunk(
	ctx context.Context, chunk *providers.StreamChunk,
) hooks.Decision {
	streamable, ok := a.handler.(evals.StreamableEvalHandler)
	if !ok {
		return hooks.Allow
	}

	params := evals.ApplyDefaults(a.evalType, a.params)
	params = evals.NormalizeParams(a.evalType, params)

	result, err := streamable.EvalPartial(ctx, chunk.Content, params)
	if err != nil {
		return hooks.Deny("guardrail streaming error: " + err.Error())
	}

	if !result.IsPassed() {
		if !a.monitorOnly {
			// Truncate chunk content for length validators
			if maxLen := extractMaxLen(params); maxLen > 0 && len(chunk.Content) > maxLen {
				chunk.Content = chunk.Content[:maxLen]
			}
		}
		return a.enforced(result)
	}

	return hooks.Allow
}

// enforce modifies the message content based on the validator type.
func (a *GuardrailHookAdapter) enforce(msg *types.Message, params map[string]any) {
	if maxLen := extractMaxLen(params); maxLen > 0 && len(msg.Content) > maxLen {
		logger.Info("Guardrail enforced: truncating content",
			"type", a.evalType, "original_length", len(msg.Content), "max_length", maxLen)
		msg.Content = msg.Content[:maxLen]
		return
	}

	// Content blocker — replace with user-facing message
	blockedMsg := a.message
	if blockedMsg == "" {
		blockedMsg = prompt.DefaultBlockedMessage
	}
	logger.Info("Guardrail enforced: content blocked", "type", a.evalType)
	msg.Content = blockedMsg
}

// enforced builds an Enforced decision from an EvalResult.
func (a *GuardrailHookAdapter) enforced(result *evals.EvalResult) hooks.Decision {
	return hooks.Enforced(result.Explanation, map[string]any{
		"validator_type": a.evalType,
		"score":          result.Score,
		"value":          result.Value,
		"monitor_only":   a.monitorOnly,
	})
}

// extractMaxLen extracts the max length parameter from params.
func extractMaxLen(params map[string]any) int {
	for _, key := range []string{"max_characters", "max", "max_chars"} {
		if v, ok := params[key]; ok {
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case int64:
				return int(val)
			}
		}
	}
	return 0
}
