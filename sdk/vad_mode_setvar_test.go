package sdk

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeVADVarPack writes a pack whose system prompt carries a variable, so a
// test can see which value each model call was rendered with.
func writeVADVarPack(t *testing.T) string {
	t.Helper()
	packFile := filepath.Join(t.TempDir(), "test.pack.json")
	packContent := `{
		"name": "test-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "topic={{topic}}"
			}
		}
	}`
	require.NoError(t, os.WriteFile(packFile, []byte(packContent), 0o600))
	return packFile
}

// TestVADModeSetVar_RendersOncePerSession is the VAD-mode half of #1962. VAD
// mode fires the model once per utterance, but the pipeline itself still runs
// once for the session: variable providers resolve and the template renders
// at the top of that single run, and every later ProviderStage call reads the
// same TurnState.SystemPrompt. So a variable set before the first chunk is
// rendered, and one set between utterances is not — turn two carries turn
// one's prompt. The ASM-mode counterpart is in sdk/integration.
func TestVADModeSetVar_RendersOncePerSession(t *testing.T) {
	if testing.Short() {
		t.Skip("drives two real VAD turns in wall-clock time")
	}
	sttSvc := newScriptedSTT("first utterance", "second utterance")
	provider := &turnRecordingProvider{}
	conv, err := OpenDuplex(writeVADVarPack(t), "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(sttSvc, newConvMockTTSService(), &VADModeConfig{
			SilenceDuration:   300 * time.Millisecond,
			MinSpeechDuration: 100 * time.Millisecond,
			MaxTurnDuration:   5 * time.Second,
			SampleRate:        perTurnTestSampleRate,
			Language:          "en",
			Voice:             "alloy",
			Speed:             1.0,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	responseCh, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range responseCh { //nolint:revive // drained so the output stage never blocks
		}
	}()

	conv.SetVar("topic", "first")
	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "model must fire for the first utterance")
	assert.Equal(t, "topic=first", provider.systemAt(0),
		"a variable set before the first chunk is rendered into the session's prompt")

	conv.SetVar("topic", "second")
	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(2, 8*time.Second), "model must fire for the second utterance")
	assert.Equal(t, "topic=first", provider.systemAt(1),
		"the prompt is rendered once per session: a SetVar between utterances does not re-render")

	got, ok := conv.GetVar("topic")
	require.True(t, ok)
	assert.Equal(t, "second", got, "the value is stored even though it cannot be rendered")
}
