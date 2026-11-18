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

// blockedReplyBeforeMetaKey, set to true on a user message marked blocked,
// says the reply that answered it is the assistant message right before it
// (a spoken input blocked after its reply completed). That reply is left out
// of model context with it.
const blockedReplyBeforeMetaKey = "_blocked_input_reply_before"

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

// mergeMeta returns base's entries overlaid with add's, or nil when both are
// empty. Neither map is written.
func mergeMeta(base, add map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(add) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(base)+len(add))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}

// isMarkedBlockedInput reports whether m carries the blocked-input mark.
func isMarkedBlockedInput(m *types.Message) bool {
	marked, _ := m.Meta[types.MetaBlockedInput].(bool)
	return marked
}

// isBlockedInputReply reports whether m is the canned reply substituted for a
// turn an input guardrail or BeforeCall hook blocked.
func isBlockedInputReply(m *types.Message) bool {
	if m.Role != roleAssistant || len(m.ToolCalls) > 0 {
		return false
	}
	if isMarkedBlockedInput(m) {
		return true
	}
	return m.FinishReason == types.FinishReasonSafety && hasInputValidation(m)
}

// isBlockedInputRequest reports whether m is a user message marked as blocked
// itself: a spoken input whose transcript was blocked only after the reply to
// it completed (DuplexProviderStage.emitLateUserTurn). There is no canned
// reply to mark then: the reply that answered it was already emitted, and is
// left out with it by blockedReplyBeforeMetaKey (or its own mark, when a
// TranscriptReorderStage moved the request ahead of it).
func isBlockedInputRequest(m *types.Message) bool {
	return m.Role == roleUser && isMarkedBlockedInput(m)
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
// turn: each canned reply and the user message it answered, and each user
// message marked as blocked itself. It returns nil when msgs holds none.
//
// Known gap: a BeforeCall hook that blocks a later round of a tool loop (it
// judged a tool result, not the user's message) leaves its canned reply after
// that tool result. Only the reply is left out then; the tool exchange, which
// the model already saw on the round before, is kept, and so is the result
// the hook judged.
func blockedInputTurns(msgs []types.Message) []bool {
	var mask []bool
	for i := range msgs {
		reply := isBlockedInputReply(&msgs[i])
		if !reply && !isBlockedInputRequest(&msgs[i]) {
			continue
		}
		if mask == nil {
			mask = make([]bool, len(msgs))
		}
		mask[i] = true
		if i > 0 && answeredBy(&msgs[i], &msgs[i-1], reply) {
			mask[i-1] = true
		}
	}
	return mask
}

// answeredBy reports whether prev, the message before the blocked message m,
// belongs to m's blocked turn: the user message a canned reply answered, or
// the reply a blocked request marked blockedReplyBeforeMetaKey was answered
// by.
func answeredBy(m, prev *types.Message, reply bool) bool {
	if reply {
		return prev.Role == roleUser
	}
	before, _ := m.Meta[blockedReplyBeforeMetaKey].(bool)
	return before && prev.Role == roleAssistant && len(prev.ToolCalls) == 0
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

// ExcludeFromModelContext returns msgs without what never goes back to a
// model: interrupted replies (types.ExcludeInterrupted) and blocked input
// turns, the canned reply marked Meta[types.MetaBlockedInput] and the user
// message it answered. A transcript keeps both; any path that builds model
// context from one (a carry-forward summary, a host's own summarizer) passes
// it through this first. msgs is returned as-is when it holds neither.
func ExcludeFromModelContext(msgs []types.Message) []types.Message {
	return excludeBlockedInputTurns(types.ExcludeInterrupted(msgs))
}
