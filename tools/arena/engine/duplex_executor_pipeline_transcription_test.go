package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// TestBuildBaseSessionConfig_EnablesInputTranscription asserts that
// buildBaseSessionConfig always sets metadata["input_transcription"] = true so
// that the OpenAI Realtime provider enables whisper-based input-turn
// transcription (which it enables by default only when the key is absent).
// Making the intent explicit prevents accidental suppression if other code
// later writes input_transcription = false into the map before the config
// is consumed.
func TestBuildBaseSessionConfig_EnablesInputTranscription(t *testing.T) {
	de := &DuplexConversationExecutor{}
	req := &ConversationRequest{
		Scenario: &config.Scenario{
			ID: "voice-asm",
		},
	}

	cfg := de.buildBaseSessionConfig(req, 24000)
	require.NotNil(t, cfg, "buildBaseSessionConfig must return a non-nil config")
	require.NotNil(t, cfg.Metadata, "Metadata map must not be nil")

	val, ok := cfg.Metadata["input_transcription"]
	assert.True(t, ok, "Metadata must contain key 'input_transcription'")
	assert.Equal(t, true, val, "input_transcription must be true to enable realtime transcription events")
}
