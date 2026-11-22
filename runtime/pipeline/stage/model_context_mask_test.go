package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

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
