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
