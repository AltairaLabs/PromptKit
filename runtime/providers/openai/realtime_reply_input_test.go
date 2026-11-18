package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Each reply's chunks name the caller's audio input the reply answers
// (providers.ReplyToInputKey), so the pipeline can tell a transcript that
// arrives late for a reply from the next turn's. The caller's transcript
// names only its own input, and a reply to typed text names none.
func TestRealtimeReplyInput_RepliesNameTheInputTheyAnswer(t *testing.T) {
	fs := newForgetServer(t, []forgetStep{{send: []map[string]any{
		committed("user_1"), responseCreated("R1"), audioDelta("R1", "item_R1"),
		responseDone("R1", "completed", assistantItem("item_R1")),
		committed("user_2"),
		{"type": "conversation.item.input_audio_transcription.completed", "item_id": "user_2",
			"content_index": 0, "transcript": "who is the uncle"},
		responseCreated("R2"), audioDelta("R2", "item_R2"),
		responseDone("R2", "completed", assistantItem("item_R2")),
	}}}, "")
	run := startForgetRun(t, fs)
	first := run.until(finishedResponse("R1"))
	second := run.until(finishedResponse("R2"))

	var got []string
	for _, c := range append(first, second...) {
		if _, ok := c.Metadata["transcription"]; ok {
			got = append(got, "transcript:"+c.Metadata[providers.InputItemIDKey].(string)+
				"|"+asString(c.Metadata[providers.ReplyToInputKey]))
			continue
		}
		got = append(got, asString(c.Metadata[providers.ReplyToInputKey]))
	}
	require.NotEmpty(t, got)
	assert.Equal(t, []string{"user_1", "user_1", "transcript:user_2|", "user_2", "user_2"}, got)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
