package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ModelContextMask marks what ExcludeFromModelContext leaves out: an
// interrupted reply, and a blocked input turn's request and canned reply.
func TestModelContextMask(t *testing.T) {
	blockedReply := types.Message{Role: "assistant", Content: "No.", Meta: map[string]interface{}{types.MetaBlockedInput: true}}
	msgs := []types.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "Hel", FinishReason: types.FinishReasonInterrupted},
		{Role: "user", Content: "blocked"},
		blockedReply,
		{Role: "user", Content: "fine"},
	}
	mask := ModelContextMask(msgs)
	assert.Equal(t, []bool{false, true, true, true, false}, mask)
	kept := ExcludeFromModelContext(msgs)
	assert.Equal(t, []types.Message{msgs[0], msgs[4]}, kept)

	assert.Nil(t, ModelContextMask([]types.Message{{Role: "user", Content: "hi"}}))
	assert.Equal(t, []bool{true}, ModelContextMask([]types.Message{msgs[1]}))
}

// A message marked types.MetaExcludedFromModel is left out alone: a canned
// reply carrying it, which would otherwise take the user message before it
// out too, keeps that message, and so does the compatibility read of a reply
// stored before the mark (safety with an input validation).
func TestModelContextMask_ExcludedAloneKeepsThePreviousMessage(t *testing.T) {
	alone := map[string]interface{}{types.MetaBlockedInput: true, types.MetaExcludedFromModel: true}
	legacy := types.Message{Role: "assistant", Content: "No.", FinishReason: types.FinishReasonSafety,
		Validations: []types.ValidationResult{{Details: map[string]interface{}{"direction": "input"}}},
		Meta:        map[string]interface{}{types.MetaExcludedFromModel: true}}
	msgs := []types.Message{
		{Role: "user", Content: "innocent"},
		{Role: "assistant", Content: "No.", Meta: alone},
		{Role: "user", Content: "also innocent"},
		legacy,
		{Role: "user", Content: "said alone", Meta: map[string]interface{}{types.MetaExcludedFromModel: true}},
	}
	assert.Equal(t, []bool{false, true, false, true, true}, ModelContextMask(msgs))
	assert.Equal(t, []types.Message{msgs[0], msgs[2]}, ExcludeFromModelContext(msgs))
}

// What a caller heard of a spoken reply they cut off, truncated to it
// (MetaHeardPrefix), is not masked: it goes back to the model, to a summary
// and to the retrieval index, as model context does. A realtime reply cut the
// same way but keeping its whole text is masked, as any interrupted reply.
func TestModelContextMask_KeepsAHeardPrefix(t *testing.T) {
	heard := types.Message{Role: "assistant", Content: "The fox woke early.", FinishReason: types.FinishReasonInterrupted,
		Meta: map[string]interface{}{types.MetaPlayoutCut: types.PlayoutCutBargeIn, types.MetaHeardPrefix: true}}
	realtime := types.Message{Role: "assistant", Content: "The fox woke early. It ran to the river.",
		FinishReason: types.FinishReasonInterrupted, Meta: map[string]interface{}{types.MetaPlayoutCut: types.PlayoutCutBargeIn}}
	msgs := []types.Message{{Role: "user", Content: "story"}, heard, {Role: "user", Content: "again"}, realtime}

	assert.Equal(t, []bool{false, false, false, true}, ModelContextMask(msgs))
	assert.Equal(t, msgs[:3], ExcludeFromModelContext(msgs))
	assert.Equal(t, msgs[:3], summaryBatch(msgs, len(msgs)), "a summary sees what model context does")
}
