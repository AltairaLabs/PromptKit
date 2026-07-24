package stage_test

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reportingTTS is a TTS service that also implements tts.SpokenTextReporter,
// returning a canned spoken-text value so a test can assert the stage stamped it.
type reportingTTS struct {
	mockTTSService
	spokenText string
}

func (r *reportingTTS) SpokenText(_ string, _ tts.SynthesisConfig) string { return r.spokenText }

// synthesizedSpeech returns the first element the stage marked as synthesized
// speech, or nil if none was emitted.
func synthesizedSpeech(out []stage.StreamElement) *stage.StreamElement {
	for i := range out {
		if out[i].Meta.SynthesizedSpeech {
			return &out[i]
		}
	}
	return nil
}

// TestTTSStage_StampsSpokenTextFromReporter verifies that when the TTS provider
// implements tts.SpokenTextReporter, the stage stamps the real spoken text (post
// markup-lowering) onto the synthesized-speech element. See #1657.
func TestTTSStage_StampsSpokenTextFromReporter(t *testing.T) {
	svc := &reportingTTS{spokenText: "the real spoken words"}
	ttsStage := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	out := runStage(t, ttsStage, []stage.StreamElement{
		messageElement("assistant", "[whispers]the real spoken words"),
	}, 5*time.Second)

	speech := synthesizedSpeech(out)
	require.NotNil(t, speech, "expected a synthesized-speech element")
	assert.Equal(t, "the real spoken words", speech.Meta.SpokenText,
		"the stage must stamp the reporter's spoken text onto the element")
}

// TestTTSStage_NoReporterLeavesSpokenTextEmpty guards the degrade path: a
// provider that does not implement tts.SpokenTextReporter leaves SpokenText
// empty so consumers fall back to the reference text. See #1657.
func TestTTSStage_NoReporterLeavesSpokenTextEmpty(t *testing.T) {
	svc := &mockTTSService{} // no SpokenText method
	ttsStage := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	out := runStage(t, ttsStage, []stage.StreamElement{
		messageElement("assistant", "The capital of France is Paris."),
	}, 5*time.Second)

	speech := synthesizedSpeech(out)
	require.NotNil(t, speech, "expected a synthesized-speech element")
	assert.Empty(t, speech.Meta.SpokenText,
		"a provider without SpokenTextReporter must leave SpokenText empty")
}
