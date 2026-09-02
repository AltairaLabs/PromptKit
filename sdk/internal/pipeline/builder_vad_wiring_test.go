package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// recordingMessageLog records what the provider stage would persist.
type recordingMessageLog struct{ appended []types.Message }

func (l *recordingMessageLog) LogAppend(
	_ context.Context, _ string, _ int, messages []types.Message,
) (int, error) {
	l.appended = append(l.appended, messages...)
	return len(l.appended), nil
}

func (l *recordingMessageLog) LogLoad(
	_ context.Context, _ string, _ int,
) ([]types.Message, error) {
	return l.appended, nil
}

func (l *recordingMessageLog) LogLen(_ context.Context, _ string) (int, error) {
	return len(l.appended), nil
}

var _ statestore.MessageLog = (*recordingMessageLog)(nil)

// VAD mode must persist through the message log like every other topology.
//
// It never did. The sibling streaming ProviderConfig in builder.go has always
// set MessageLog and MessageLogConvID; the VAD one set neither, so a voice
// session wrote nothing per turn. IncrementalSaveStage cannot cover for that in
// a long-running session — it drains its input channel before writing, which
// for duplex/VAD means one write at session close.
func TestVADProviderConfig_CarriesTheMessageLog(t *testing.T) {
	log := &recordingMessageLog{}
	cfg := &Config{
		MessageLog:     log,
		ConversationID: "conv-vad-1",
	}

	got := vadProviderConfig(cfg)

	require.NotNil(t, got.MessageLog, "VAD must persist per turn, not only at session close")
	assert.Same(t, log, got.MessageLog)
	assert.Equal(t, "conv-vad-1", got.MessageLogConvID,
		"a log without the conversation id writes to the wrong conversation")
}

// Streaming stays on: the tool loop fires per EndOfTurn rather than once when
// the session closes (#1644). Bundled here because it shares the config that
// lost the message log, and losing it is the same class of silent omission.
func TestVADProviderConfig_StaysStreaming(t *testing.T) {
	got := vadProviderConfig(&Config{})
	assert.True(t, got.Streaming, "VAD is a continuous multi-turn session")
}

// Scalar settings still reach the stage.
func TestVADProviderConfig_CarriesModelSettings(t *testing.T) {
	got := vadProviderConfig(&Config{MaxTokens: 1234, Temperature: 0.25})
	assert.Equal(t, 1234, got.MaxTokens)
	assert.InDelta(t, 0.25, got.Temperature, 0.0001)
}
