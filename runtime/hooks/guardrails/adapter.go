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

	// A guardrail judges one message. For the input direction that message is
	// the user's, which a transcript scan filtered to assistant role would
	// never see — hence the explicit content argument rather than
	// BuildEvalContext's last-assistant inference. See
	// evals.BuildGuardrailEvalContext.
	//
	// nil latency: no call has completed, so there is nothing to judge. A
	// latency guardrail is output-only by nature.
	metadata := evals.SeedBudgetMetadata(req.Metadata, req.Messages, nil)
	evalCtx := evals.BuildGuardrailEvalContext(
		req.Messages, lastMsg.GetContent(), metadata,
	)

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
	var reqMetadata map[string]any
	if req != nil {
		msgs = make([]types.Message, len(req.Messages)+1)
		copy(msgs, req.Messages)
		msgs[len(req.Messages)] = resp.Message
		reqMetadata = req.Metadata
	} else {
		msgs = []types.Message{resp.Message}
	}

	// Spend and tokens come off the transcript's CostInfo — including this
	// response, which is why seeding happens after msgs is assembled. Latency is
	// the completed call's, which only exists on this side of the provider.
	metadata := evals.SeedBudgetMetadata(reqMetadata, msgs, &resp.LatencyMs)

	// Judge this response only. Scanning the whole transcript would make one
	// tripped turn re-block every later turn in the conversation.
	evalCtx := evals.BuildGuardrailEvalContext(
		msgs, resp.Message.GetContent(), metadata,
	)

	params, thresholds := a.evalParams()

	result, err := a.handler.Eval(ctx, evalCtx, params)
	if err != nil {
		return hooks.Deny("guardrail error: " + err.Error())
	}

	if thresholds.Triggered(result) {
		a.enforce(&resp.Message, params)
		return a.enforced(result)
	}

	return hooks.Allow
}

// evalParams returns the params the inner handler should see, plus the
// thresholds this adapter applies itself.
//
// Three things are load-bearing. Defaults and legacy-name normalization run
// first, so a threshold declared under an aliased name is still found. The
// threshold keys are stripped before the handler sees them: they are wrapper
// params by design, and several handler families call rejectThresholdParams,
// which returns an error result scoring 0 — read by the guardrail as "block".
// Forwarding a threshold therefore turned the guardrail into an always-block
// instead of merely ignoring the param (#1707).
//
// "direction" is stripped for the same reason. It selects which phase this
// adapter evaluates in, and it is already consumed — a.direction. Once
// guardrail_triggered gained a direction param of its own, meaning "match a
// firing recorded on that side", forwarding the wrapper's copy silently
// re-purposed it and made the guardrail conclude nothing had fired (#1718).
func (a *GuardrailHookAdapter) evalParams() (map[string]any, evals.ScoreThresholds) {
	params := evals.ApplyDefaults(a.evalType, a.params)
	params = evals.NormalizeParams(a.evalType, params)
	thresholds := evals.ExtractScoreThresholds(params)
	return stripDirection(evals.StripScoreThresholds(params)), thresholds
}

// stripDirection returns params without the wrapper's "direction" key. The
// input map is never mutated: adapters share their params across turns.
func stripDirection(params map[string]any) map[string]any {
	if _, present := params["direction"]; !present {
		return params
	}
	stripped := make(map[string]any, len(params))
	for k, v := range params {
		if k == "direction" {
			continue
		}
		stripped[k] = v
	}
	return stripped
}

// evaluate runs the handler and converts the EvalResult to a Decision.
func (a *GuardrailHookAdapter) evaluate(
	ctx context.Context, evalCtx *evals.EvalContext,
) hooks.Decision {
	params, thresholds := a.evalParams()

	result, err := a.handler.Eval(ctx, evalCtx, params)
	if err != nil {
		return hooks.Deny("guardrail error: " + err.Error())
	}

	if thresholds.Triggered(result) {
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

	params, thresholds := a.evalParams()

	result, err := streamable.EvalPartial(ctx, chunk.Content, params)
	if err != nil {
		return hooks.Deny("guardrail streaming error: " + err.Error())
	}

	if thresholds.Triggered(result) {
		// Substitute the enforced content, exactly as the non-streaming path
		// does. Previously this only truncated for length validators, so a
		// content blocker left the offending text in place and the caller
		// received the very pattern it was configured to block (#1697).
		//
		// Deltas already emitted to a consumer cannot be recalled — that is
		// inherent to streaming — but the accumulated content the stage keeps,
		// which becomes the assistant message, is corrected here.
		chunk.Content = a.enforcedContent(chunk.Content, params)
		return a.enforced(result)
	}

	return hooks.Allow
}

// enforce modifies the message content based on the validator type.
func (a *GuardrailHookAdapter) enforce(msg *types.Message, params map[string]any) {
	msg.Content = a.enforcedContent(msg.Content, params)
}

// enforcedContent returns the content that should replace the offending text.
//
// Single-sourced so the streaming and non-streaming paths cannot disagree about
// what enforcement means. They legitimately disagree about *detection* — the
// streaming check matches substrings so a pattern split across chunks is not
// missed, while the final check honors match_mode — but once either has fired,
// the substituted content must be the same.
//
// Getting that wrong leaked the banned text: a chunk check fired on a substring,
// the stream stopped, and the final check then judged the same content clean
// under word_boundary mode and left it in place (#1697).
func (a *GuardrailHookAdapter) enforcedContent(content string, params map[string]any) string {
	if maxLen := extractMaxLen(params); maxLen > 0 && len(content) > maxLen {
		logger.Info("Guardrail enforced: truncating content",
			"type", a.evalType, "original_length", len(content), "max_length", maxLen)
		return content[:maxLen]
	}

	// Content blocker — replace with the user-facing message.
	blockedMsg := a.message
	if blockedMsg == "" {
		blockedMsg = prompt.DefaultBlockedMessage
	}
	logger.Info("Guardrail enforced: content blocked", "type", a.evalType)
	return blockedMsg
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
