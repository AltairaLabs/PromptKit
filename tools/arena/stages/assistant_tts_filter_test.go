package stages

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssistantTTSFilterStage_ForwardsAssistantMessage(t *testing.T) {
	s := NewAssistantTTSFilterStage()
	in := newTestMessageElement("assistant", "I can help with that.")

	results := runStage(t, s, []stage.StreamElement{in})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Message)
	assert.Equal(t, "assistant", results[0].Message.Role)
	assert.Equal(t, "I can help with that.", results[0].Message.Content)
}

func TestAssistantTTSFilterStage_DropsUserMessage(t *testing.T) {
	s := NewAssistantTTSFilterStage()
	// Simulates the user transcript wrapped by STTUserMessageStage.
	in := newTestMessageElement("user", "hello there")

	results := runStage(t, s, []stage.StreamElement{in})

	assert.Empty(t, results, "user message must be dropped by the filter")
}

func TestAssistantTTSFilterStage_DropsSystemMessage(t *testing.T) {
	s := NewAssistantTTSFilterStage()
	in := newTestMessageElement("system", "You are a helpful assistant.")

	results := runStage(t, s, []stage.StreamElement{in})

	assert.Empty(t, results, "system message must be dropped by the filter")
}

func TestAssistantTTSFilterStage_ForwardsAudioElement(t *testing.T) {
	s := NewAssistantTTSFilterStage()
	audio := stage.NewAudioElement(&stage.AudioData{Samples: []byte{0, 1}, SampleRate: 16000, Channels: 1})

	results := runStage(t, s, []stage.StreamElement{audio})

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Audio, "audio element must pass through unchanged")
}

func TestAssistantTTSFilterStage_ForwardsEndOfStream(t *testing.T) {
	s := NewAssistantTTSFilterStage()
	eos := stage.NewEndOfStreamElement()

	results := runStage(t, s, []stage.StreamElement{eos})

	require.Len(t, results, 1)
	assert.True(t, results[0].EndOfStream, "EndOfStream must pass through unchanged")
}

func TestAssistantTTSFilterStage_MixedElements(t *testing.T) {
	s := NewAssistantTTSFilterStage()
	inputs := []stage.StreamElement{
		newTestMessageElement("user", "hello"),
		newTestMessageElement("assistant", "hi there"),
		newTestMessageElement("system", "system prompt"),
		stage.NewEndOfStreamElement(),
	}

	results := runStage(t, s, inputs)

	// Only the assistant message and EndOfStream should survive.
	require.Len(t, results, 2)
	require.NotNil(t, results[0].Message)
	assert.Equal(t, "assistant", results[0].Message.Role)
	assert.True(t, results[1].EndOfStream)
}
