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
// conversation leaves it out too. Where a block is made, the canned reply is
// marked Meta[types.MetaBlockedInput] = true (markBlockedInput), whether or not
// the hook that blocked it named a validator. A conversation stored before
// that mark existed is still read by the guardrail's record: FinishReason
// safety and a validation with direction input. An output block is marked
// neither way and is kept: its replacement stands in for the model's reply.

// markBlockedInput marks msg as the canned reply of a blocked input turn.
func markBlockedInput(msg *types.Message) {
	msg.Meta = withBlockedInputMark(msg.Meta)
}

// withBlockedInputMark returns a copy of meta with the blocked-input mark set.
// It copies so a decision's metadata map shared elsewhere is not written.
func withBlockedInputMark(meta map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(meta)+1)
	for k, v := range meta {
		out[k] = v
	}
	out[types.MetaBlockedInput] = true
	return out
}

// isBlockedInputReply reports whether m is the canned reply substituted for a
// turn an input guardrail or BeforeCall hook blocked.
func isBlockedInputReply(m *types.Message) bool {
	if m.Role != roleAssistant || len(m.ToolCalls) > 0 {
		return false
	}
	if marked, _ := m.Meta[types.MetaBlockedInput].(bool); marked {
		return true
	}
	return m.FinishReason == types.FinishReasonSafety && hasInputValidation(m)
}

// hasInputValidation reports whether m records a guardrail firing on the
// input side: the compatibility read for replies stored before the mark.
func hasInputValidation(m *types.Message) bool {
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
