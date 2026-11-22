package heard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestCutReply_RecordsWhatWasHeardOnACopy(t *testing.T) {
	msg := &types.Message{Role: "assistant", Content: "It is sunny today.", FinishReason: "stop",
		Meta: map[string]interface{}{"keep": "me"}}

	cut := CutReply(msg, 1500*time.Millisecond, 4*time.Second, types.PlayoutCutBargeIn)

	assert.Equal(t, map[string]interface{}{
		"keep":                   "me",
		types.MetaPlayoutCut:     types.PlayoutCutBargeIn,
		types.MetaHeardMs:        int64(1500),
		types.MetaAudioMs:        int64(4000),
		types.MetaPlayedFraction: 0.375,
	}, cut.Meta)
	assert.Equal(t, "It is sunny today.", cut.Content)
	assert.Equal(t, "stop", cut.FinishReason)
	assert.Equal(t, map[string]interface{}{"keep": "me"}, msg.Meta, "the original Message is not changed")
}

func TestCutReply_RoundsThePlayedFractionToThreePlaces(t *testing.T) {
	cut := CutReply(&types.Message{}, time.Second, 3*time.Second, types.PlayoutCutBargeIn)
	assert.Equal(t, 0.333, cut.Meta[types.MetaPlayedFraction])

	cut = CutReply(&types.Message{}, 2*time.Second, 3*time.Second, types.PlayoutCutBargeIn)
	assert.Equal(t, 0.667, cut.Meta[types.MetaPlayedFraction])
}

func TestCutReply_NoAudioPlayedNothing(t *testing.T) {
	cut := CutReply(&types.Message{}, 0, 0, types.PlayoutCutBargeIn)
	assert.Equal(t, 0.0, cut.Meta[types.MetaPlayedFraction])
	assert.Equal(t, int64(0), cut.Meta[types.MetaAudioMs])
}
