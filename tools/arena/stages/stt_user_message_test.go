package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// textElem builds a Text-only stream element (as the STT stage emits).
func textElem(text string) stage.StreamElement {
	return stage.NewTextElement(text)
}

func TestSTTUserMessageStage_WrapsTextIntoUserMessage(t *testing.T) {
	s := NewSTTUserMessageStage()
	results := runStage(t, s, []stage.StreamElement{textElem("hello there")})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Message, "text element should be wrapped into a Message")
	assert.Equal(t, "user", results[0].Message.Role)
	assert.Equal(t, "hello there", results[0].Message.Content)
	assert.Nil(t, results[0].Text, "wrapped element should not retain a bare Text field")
}

func TestSTTUserMessageStage_PreservesMeta(t *testing.T) {
	s := NewSTTUserMessageStage()
	in := stage.NewTextElement("transcribed")
	in.Meta.Passthrough = true

	results := runStage(t, s, []stage.StreamElement{in})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Message)
	assert.True(t, results[0].Meta.Passthrough, "element metadata should be preserved")
}

func TestSTTUserMessageStage_ForwardsExistingMessageUnchanged(t *testing.T) {
	s := NewSTTUserMessageStage()
	in := newTestMessageElement("assistant", "already a message")

	results := runStage(t, s, []stage.StreamElement{in})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Message)
	assert.Equal(t, "assistant", results[0].Message.Role, "existing message must not be rewritten")
	assert.Equal(t, "already a message", results[0].Message.Content)
}

func TestSTTUserMessageStage_ForwardsNonTextElements(t *testing.T) {
	s := NewSTTUserMessageStage()
	audio := stage.NewAudioElement(&stage.AudioData{Samples: []byte{0, 1}, SampleRate: 16000, Channels: 1})
	eos := stage.NewEndOfStreamElement()

	results := runStage(t, s, []stage.StreamElement{audio, eos})

	require.Len(t, results, 2)
	require.NotNil(t, results[0].Audio, "audio element should pass through")
	assert.Nil(t, results[0].Message)
	assert.True(t, results[1].EndOfStream, "EndOfStream should pass through")
}

func TestSTTUserMessageStage_EmptyTranscriptPassesThrough(t *testing.T) {
	s := NewSTTUserMessageStage()
	// Whitespace-only transcript: not wrapped (nothing meaningful to persist).
	results := runStage(t, s, []stage.StreamElement{textElem("   ")})

	require.Len(t, results, 1)
	assert.Nil(t, results[0].Message, "blank transcript should not become a user message")
	require.NotNil(t, results[0].Text)
}
