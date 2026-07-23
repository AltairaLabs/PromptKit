package session

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
)

// TestStreamElementToStreamChunk_SynthesizedSpeechCarriesAudioNotDelta guards
// the fix for the voice-path double-count: the TTS output element carries the
// spoken audio plus, for reference, the text it was asked to speak. That text is
// the same reply already delivered as streaming Text deltas, so mapping it onto
// Delta (which means "new LLM output") delivers the reply text twice — and the
// submitted text is not even the true spoken text, since providers strip/rewrite
// markup at synthesis. A synthesized-speech element must therefore surface its
// audio and leave Delta empty.
func TestStreamElementToStreamChunk_SynthesizedSpeechCarriesAudioNotDelta(t *testing.T) {
	spoken := "The capital of France is Paris."
	elem := stage.StreamElement{
		Text: &spoken,
		Audio: &stage.AudioData{
			Samples:    []byte{0x01, 0x02, 0x03, 0x04},
			SampleRate: 24000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}
	elem.Meta.SynthesizedSpeech = true

	chunk := streamElementToStreamChunk(&elem)

	assert.Empty(t, chunk.Delta, "synthesized speech must not present its text as a new LLM delta")
	assert.Empty(t, chunk.Content, "synthesized speech must not present its text as content")
	if assert.NotNil(t, chunk.MediaData, "the audio payload must be delivered") {
		assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, chunk.MediaData.Data)
	}
}

// TestStreamElementToStreamChunk_PlainTextStillDeltas is the regression guard:
// an ordinary streamed Text element (an LLM delta, no synthesized-speech marker)
// must still map to Delta and Content.
func TestStreamElementToStreamChunk_PlainTextStillDeltas(t *testing.T) {
	delta := "Paris"
	elem := stage.StreamElement{Text: &delta}

	chunk := streamElementToStreamChunk(&elem)

	assert.Equal(t, "Paris", chunk.Delta, "an ordinary text element is an LLM delta and must map to Delta")
	assert.Equal(t, "Paris", chunk.Content)
}
