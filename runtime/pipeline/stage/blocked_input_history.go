package stage

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A turn an input guardrail blocked is kept in the transcript — the user's
// message and the canned reply that answered it are what the user saw — but
// the model never answered it, and it must never be shown it later either:
// sending the blocked request on the next turn would only delay the block by
// one turn. Every path that turns history into model context (the provider
// request, summaries, extracted memories, the retrieval index) leaves the pair
// out. The guardrails' and evals' view keeps it: topic_policy deliberately
// reads a denied user message (see isSubstitutedAssistantTurn).
//
// The pair is recognized from what the state store persists, so a resumed
// conversation leaves it out too: the canned reply is an assistant message
// with FinishReason safety, no tool calls, and a guardrail validation recorded
// with direction input (runBeforeCallHooks, recordGuardrailFiring). An output
// block carries direction output and is kept: its replacement already stands
// in for the model's reply.

// isBlockedInputReply reports whether m is the canned reply substituted for a
// turn an input guardrail blocked.
func isBlockedInputReply(m *types.Message) bool {
	if m.Role != roleAssistant || m.FinishReason != types.FinishReasonSafety || len(m.ToolCalls) > 0 {
		return false
	}
	for i := range m.Validations {
		if dir, _ := m.Validations[i].Details["direction"].(string); dir == hooks.DirectionInput {
			return true
		}
	}
	return false
}

// blockedInputTurns marks the messages of msgs that belong to a blocked input
// turn: each canned reply and the user message it answered. It returns nil
// when msgs holds none.
func blockedInputTurns(msgs []types.Message) []bool {
	var mask []bool
	for i := range msgs {
		if !isBlockedInputReply(&msgs[i]) {
			continue
		}
		if mask == nil {
			mask = make([]bool, len(msgs))
		}
		mask[i] = true
		if i > 0 && msgs[i-1].Role == roleUser {
			mask[i-1] = true
		}
	}
	return mask
}

// excludeBlockedInputTurns returns msgs without the blocked input turns, for
// any path that sends history to a model. msgs is returned as-is when it
// holds none.
func excludeBlockedInputTurns(msgs []types.Message) []types.Message {
	return withoutMasked(msgs, blockedInputTurns(msgs))
}

// withoutMasked returns msgs without the entries mask marks; a nil mask keeps
// them all.
func withoutMasked(msgs []types.Message, mask []bool) []types.Message {
	if mask == nil {
		return msgs
	}
	out := make([]types.Message, 0, len(msgs))
	for i := range msgs {
		if !mask[i] {
			out = append(out, msgs[i])
		}
	}
	return out
}

// excludeFromModelContext drops what never goes back to a model: interrupted
// replies and blocked input turns.
func excludeFromModelContext(msgs []types.Message) []types.Message {
	return excludeBlockedInputTurns(types.ExcludeInterrupted(msgs))
}
