package stage

import (
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDuplexStageForUnit() *DuplexProviderStage {
	return NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
}

// TestDuplexChunkToElement_MediaDefaults covers the audio sample-rate/channel
// defaulting branch when the provider omits them.
func TestDuplexChunkToElement_MediaDefaults(t *testing.T) {
	s := newDuplexStageForUnit()
	elem := s.chunkToElement(&providers.StreamChunk{
		MediaData: &providers.StreamMediaData{Data: []byte{1, 2, 3, 4}}, // SampleRate 0, Channels 0
	})
	require.NotNil(t, elem.Audio, "audio element built from media")
	assert.Equal(t, 24000, elem.Audio.SampleRate, "sample rate defaults to 24000")
	assert.Equal(t, 1, elem.Audio.Channels, "channels default to 1")
}

// TestDuplexChunkToElement_TextForwarded covers the live-text branch.
func TestDuplexChunkToElement_TextForwarded(t *testing.T) {
	s := newDuplexStageForUnit()
	elem := s.chunkToElement(&providers.StreamChunk{Content: "live text"})
	require.NotNil(t, elem.Text)
	assert.Equal(t, "live text", *elem.Text)
}

// TestDuplexChunkToElement_FinishWithToolCalls covers turn completion creating a
// message from chunk tool calls, the latency branch, and the long-content
// preview truncation branch.
func TestDuplexChunkToElement_FinishWithToolCalls(t *testing.T) {
	s := newDuplexStageForUnit()
	s.turnStartTime = time.Now().Add(-50 * time.Millisecond)
	s.accumulatedText.WriteString(strings.Repeat("x", contentPreviewMaxLen+5)) // exercise preview truncation
	fr := "tool_calls"
	elem := s.chunkToElement(&providers.StreamChunk{
		FinishReason: &fr,
		ToolCalls:    []types.MessageToolCall{{ID: "1", Name: "lookup"}},
	})
	require.NotNil(t, elem.Message, "message created at turn complete")
	assert.True(t, elem.EndOfStream)
	require.Len(t, elem.Message.ToolCalls, 1)
	assert.Equal(t, "lookup", elem.Message.ToolCalls[0].Name)
	assert.GreaterOrEqual(t, elem.Message.LatencyMs, int64(0), "latency computed from turnStartTime")
}

// TestDuplexChunkToElement_InterruptedBuildsPartial covers the interruption
// branch, including buildAssistantParts emitting an audio part from accumulated
// media and takeReasoning attaching reasoning.
func TestDuplexChunkToElement_InterruptedBuildsPartial(t *testing.T) {
	s := newDuplexStageForUnit()
	s.accumulatedText.WriteString("partial answer")
	s.accumulatedMedia = []byte{1, 2, 3, 4}
	s.accumulatedReasoning.WriteString("mid-thought")
	elem := s.chunkToElement(&providers.StreamChunk{Interrupted: true})
	require.NotNil(t, elem.Message, "partial message built on interruption")
	assert.True(t, elem.Meta.Interrupted)
	require.NotNil(t, elem.Message.Reasoning)
	assert.Equal(t, "mid-thought", elem.Message.Reasoning.Text)
	hasAudio := false
	for _, p := range elem.Message.Parts {
		if p.Type == types.ContentTypeAudio {
			hasAudio = true
		}
	}
	assert.True(t, hasAudio, "buildAssistantParts includes audio from accumulatedMedia")
}

// TestDuplexChunkToElement_TurnIDAndTranscription covers popping a queued turn_id
// on EndOfStream and attaching input transcription.
func TestDuplexChunkToElement_TurnIDAndTranscription(t *testing.T) {
	s := newDuplexStageForUnit()
	s.turnIDQueue = []string{"turn-1"}
	s.accumulatedText.WriteString("hello")
	s.inputTranscription.WriteString("user said hi")
	fr := "complete"
	elem := s.chunkToElement(&providers.StreamChunk{FinishReason: &fr})
	require.True(t, elem.EndOfStream)
	require.NotNil(t, elem.Meta.Transcription)
	assert.Equal(t, "user said hi", elem.Meta.Transcription.Text)
	require.NotNil(t, elem.Meta.TurnID)
	assert.Equal(t, "turn-1", *elem.Meta.TurnID)
}

// TestDuplexChunkToElement_TurnIDWithoutTranscription covers the EndOfStream
// turn_id pop branch when no transcription was captured.
func TestDuplexChunkToElement_TurnIDWithoutTranscription(t *testing.T) {
	s := newDuplexStageForUnit()
	s.turnIDQueue = []string{"turn-2"}
	s.accumulatedText.WriteString("answer")
	fr := "complete"
	elem := s.chunkToElement(&providers.StreamChunk{FinishReason: &fr})
	require.True(t, elem.EndOfStream)
	assert.Nil(t, elem.Meta.Transcription, "no transcription captured")
}

// TestDuplexBuildAssistantParts covers both the text-only and the
// text+audio-media shapes.
func TestDuplexBuildAssistantParts(t *testing.T) {
	s := newDuplexStageForUnit()

	textOnly := s.buildAssistantParts("just text")
	require.Len(t, textOnly, 1)
	assert.Equal(t, types.ContentTypeText, textOnly[0].Type)

	s.accumulatedMedia = []byte{9, 8, 7, 6}
	withMedia := s.buildAssistantParts("text plus audio")
	require.Len(t, withMedia, 2)
	assert.Equal(t, types.ContentTypeAudio, withMedia[1].Type)
	require.NotNil(t, withMedia[1].Media)
}
