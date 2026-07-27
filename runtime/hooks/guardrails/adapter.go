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

// Aliases of the canonical direction constants, retained for existing callers.
// See runtime/hooks for what each value means; the vocabulary lives there so
// the pipeline stage need not import a concrete hook implementation to tag a
// firing.
const (
	DirectionInput  = hooks.DirectionInput
	DirectionOutput = hooks.DirectionOutput
	DirectionBoth   = hooks.DirectionBoth
)

// roleUser is the message role an input guardrail gates on.
const roleUser = "user"

// lastUserTurn returns the trailing user message an input guardrail gates on.
// ok is false when there is no trailing user message: BeforeCall runs once per
// round inside the tool loop, and rounds after the first end in a tool-result
// (or assistant) message rather than new user input. Single-sourced so the
// eval-backed adapter and the func-backed guardrail cannot drift apart.
func lastUserTurn(msgs []types.Message) (types.Message, bool) {
	if len(msgs) == 0 {
		return types.Message{}, false
	}
	last := msgs[len(msgs)-1]
	if last.Role != roleUser {
		return types.Message{}, false
	}
	return last, true
}

// GuardrailHookAdapter wraps an evals.EvalTypeHandler as a hooks.ProviderHook.
// This bridges the unified eval system to the pipeline's hook infrastructure,
// allowing any registered eval handler to be used as a guardrail.
//
// Guardrails always enforce: on a hit the adapter mutates the response
// (truncate or replace) and returns an Enforced decision so the pipeline
// continues. If you want observe-only behavior, declare an eval — not a
// guardrail — and assert on it in scenarios.
type GuardrailHookAdapter struct {
	handler   evals.EvalTypeHandler
	evalType  string
	params    map[string]any
	direction string // "input" | "output" | "both"
	message   string // User-facing message when content is blocked
}

// Compile-time interface checks.
var (
	_ hooks.ProviderHook     = (*GuardrailHookAdapter)(nil)
	_ hooks.ChunkInterceptor = (*GuardrailHookAdapter)(nil)
)

// Name returns the eval type identifier for this guardrail.
func (a *GuardrailHookAdapter) Name() string { return a.evalType }

// BeforeCall checks input when direction is "input" or "both".
//
// It evaluates only when the last message is a user message (see lastUserTurn).
// BeforeCall runs once per round inside the tool loop, where later rounds end in
// a tool-result message rather than user input — evaluating those would score the
// wrong content and rebill LLM-judged checks every round. The gate is
// deliberately content-based rather than round-based: a round check would also
// misfire on a round whose last message is an assistant message, and round
// numbering is per-ProviderStage (it restarts in each composition sub-pipeline),
// so it is not a reliable proxy for "there is new user input".
func (a *GuardrailHookAdapter) BeforeCall(
	ctx context.Context, req *hooks.ProviderRequest,
) hooks.Decision {
	if a.direction != DirectionInput && a.direction != DirectionBoth {
		return hooks.Allow
	}
	if req == nil {
		return hooks.Allow
	}

	lastMsg, ok := lastUserTurn(req.Messages)
	if !ok {
		return hooks.Allow
	}

	evalCtx := &evals.EvalContext{
		CurrentOutput: lastMsg.GetContent(),
		Messages:      req.Messages,
		// A guardrail judges one message. For the input direction that
		// message is the user's, which a transcript scan filtered to
		// assistant role would never see.
		ContentScope: evals.ContentScopeCurrent,
	}

	d := a.evaluate(ctx, evalCtx)
	if !d.Allow {
		// Supply the user-facing text for the canned assistant turn the
		// pipeline returns in place of the blocked call.
		req.Replacement = a.message
		if req.Replacement == "" {
			req.Replacement = prompt.DefaultBlockedMessage
		}
	}
	return d
}

// AfterCall checks provider output when direction is "output" or "both".
// When the guardrail triggers, it enforces in-place on resp.Message
// (truncating or replacing content) and returns an Enforced decision.
func (a *GuardrailHookAdapter) AfterCall(
	ctx context.Context, req *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	if a.direction == DirectionInput {
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
		// Judge this response only. Scanning the whole transcript would make
		// one tripped turn re-block every later turn in the conversation.
		ContentScope: evals.ContentScopeCurrent,
	}

	// Apply defaults for aliased eval types, then normalize legacy param names
	params := evals.ApplyDefaults(a.evalType, a.params)
	params = evals.NormalizeParams(a.evalType, params)

	result, err := a.handler.Eval(ctx, evalCtx, params)
	if err != nil {
		return hooks.Deny("guardrail error: " + err.Error())
	}

	if result.Score == nil || *result.Score < 1.0 {
		a.enforce(&resp.Message, params)
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

	// Derive pass/fail from Score (score < 1.0 = fail).
	if result.Score == nil || *result.Score < 1.0 {
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

	if result.Score == nil || *result.Score < 1.0 {
		// Truncate chunk content for length validators
		if maxLen := extractMaxLen(params); maxLen > 0 && len(chunk.Content) > maxLen {
			chunk.Content = chunk.Content[:maxLen]
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
