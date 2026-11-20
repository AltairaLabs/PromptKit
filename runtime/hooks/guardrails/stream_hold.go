package guardrails

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

var _ hooks.StreamHolder = (*GuardrailHookAdapter)(nil)

// StreamHold implements hooks.StreamHolder: how much of a streamed reply this
// guardrail holds back from the caller until it has judged it.
func (a *GuardrailHookAdapter) StreamHold() hooks.StreamHoldMode { return a.streamHold }

// resolveStreamHold reads a guardrail's stream_hold and stream_hold_timeout
// params. Without stream_hold, the mode is the handler's default: segment or
// turn for a partial judge (evals.PartialJudge), tail for a chunk check that
// holds what could start a match (evals.PartialHolder), none otherwise. A mode
// the handler cannot honor, or an unknown one, is an error.
func resolveStreamHold(
	typeName string, handler evals.EvalTypeHandler, direction string, params map[string]any,
) (hooks.StreamHoldMode, time.Duration, error) {
	timeout, err := parseStreamHoldTimeout(params[guardrailParamStreamHoldTimeout])
	if err != nil {
		return "", 0, err
	}
	raw, set := params[guardrailParamStreamHold]
	if !set {
		return defaultStreamHold(handler, direction), timeout, nil
	}
	mode, _ := raw.(string)
	switch m := hooks.StreamHoldMode(mode); m {
	case hooks.StreamHoldNone:
		return m, timeout, nil
	case hooks.StreamHoldTail, hooks.StreamHoldSegment, hooks.StreamHoldTurn:
		if direction == DirectionInput {
			return "", 0, fmt.Errorf("stream_hold %q: an input guardrail holds no reply", mode)
		}
		return m, timeout, checkStreamHold(typeName, handler, m)
	}
	return "", 0, fmt.Errorf("stream_hold %q is not one of %q, %q, %q or %q", fmt.Sprint(raw),
		hooks.StreamHoldNone, hooks.StreamHoldTail, hooks.StreamHoldSegment, hooks.StreamHoldTurn)
}

// checkStreamHold reports a mode the handler cannot honor.
func checkStreamHold(typeName string, handler evals.EvalTypeHandler, mode hooks.StreamHoldMode) error {
	switch mode {
	case hooks.StreamHoldNone, hooks.StreamHoldTurn:
		return nil
	case hooks.StreamHoldSegment:
		if _, ok := handler.(evals.PartialJudge); !ok {
			return fmt.Errorf("stream_hold %q: %s cannot judge a partial reply", mode, typeName)
		}
	case hooks.StreamHoldTail:
		if !isStreamable(handler) {
			return fmt.Errorf("stream_hold %q: %s does not judge chunks", mode, typeName)
		}
	}
	return nil
}

// defaultStreamHold is a guardrail's mode when it sets no stream_hold.
func defaultStreamHold(handler evals.EvalTypeHandler, direction string) hooks.StreamHoldMode {
	if direction == DirectionInput {
		return hooks.StreamHoldNone
	}
	if pj, ok := handler.(evals.PartialJudge); ok {
		if pj.SegmentHoldByDefault() {
			return hooks.StreamHoldSegment
		}
		return hooks.StreamHoldTurn
	}
	if _, ok := handler.(evals.PartialHolder); ok && isStreamable(handler) {
		return hooks.StreamHoldTail
	}
	return hooks.StreamHoldNone
}

func isStreamable(handler evals.EvalTypeHandler) bool {
	if _, ok := handler.(evals.StreamableEvalHandler); ok {
		return true
	}
	_, ok := handler.(evals.StreamableRequestEvalHandler)
	return ok
}

// parseStreamHoldTimeout reads stream_hold_timeout: a duration string ("2s")
// or a number of seconds. Absent is zero, the host's default.
func parseStreamHoldTimeout(raw any) (time.Duration, error) {
	var d time.Duration
	switch v := raw.(type) {
	case nil:
		return 0, nil
	case string:
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("stream_hold_timeout %q: %w", v, err)
		}
		d = parsed
	case int:
		d = time.Duration(v) * time.Second
	case int64:
		d = time.Duration(v) * time.Second
	case float64:
		d = time.Duration(v * float64(time.Second))
	default:
		return 0, fmt.Errorf("stream_hold_timeout must be a duration such as \"2s\", got %T", raw)
	}
	if d <= 0 {
		return 0, errors.New("stream_hold_timeout must be positive")
	}
	return d, nil
}

// JudgePartialReply implements hooks.StreamHolder: it judges reply.Text, the
// reply to req so far, as a partial reply (evals.EvalContext.Partial), in the
// context AfterCall judges the completed reply in. It is bounded by the
// guardrail's stream_hold_timeout, else reply.Timeout, and fails closed when
// the handler errors or runs past it. Its started and passed events, and a
// firing, carry the segment's index and end.
func (a *GuardrailHookAdapter) JudgePartialReply(
	ctx context.Context, req *hooks.ProviderRequest, reply hooks.PartialReply,
) hooks.Decision {
	if a.direction == DirectionInput || req == nil {
		return hooks.Allow
	}
	msg := types.Message{Role: "assistant", Content: reply.Text}
	msgs := make([]types.Message, len(req.Messages)+1)
	copy(msgs, req.Messages)
	msgs[len(req.Messages)] = msg

	metadata := a.withJudge(evals.SeedBudgetMetadata(req.Metadata, msgs, nil))
	evalCtx := evals.BuildGuardrailEvalContext(msgs, reply.Text, metadata)
	evalCtx.TurnIndex = req.TurnIndex
	evalCtx.Caller = req.Caller
	evalCtx.Partial = true

	timeout := a.streamHoldTimeout
	if timeout <= 0 {
		timeout = reply.Timeout
	}
	d := a.evaluateWith(ctx, evalCtx, &msg, evalRun{
		timeout: timeout, segmentIndex: reply.SegmentIndex, segmentEnd: len(reply.Text),
	})
	if !d.Allow {
		if d.Metadata == nil {
			d.Metadata = map[string]any{}
		}
		d.Metadata[hooks.MetadataKeySegmentIndex] = reply.SegmentIndex
		d.Metadata[hooks.MetadataKeySegmentEnd] = len(reply.Text)
	}
	return d
}
