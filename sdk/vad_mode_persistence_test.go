package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// A voice session must persist each turn as it happens, not at session close.
//
// VAD mode's ProviderConfig set neither MessageLog nor MessageLogConvID, so the
// per-round write-through never ran and nothing reached the store mid-session.
// IncrementalSaveStage cannot stand in for it here: collectAndForward drains its
// input channel before writing, and in a duplex session that channel stays open
// for the whole call — so its single write lands at hang-up.
//
// This drives the real pipeline (AudioTurnStage → STT → ProviderStage) with
// scripted speech, and asserts the store has the turn BEFORE Close is called.
func TestVADMode_PersistsTurnsMidSession(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn in wall-clock time")
	}

	ctx := context.Background()
	store := statestore.NewMemoryStore()
	sttSvc := newScriptedSTT("what is the capital of france")
	provider := &turnRecordingProvider{}

	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithStateStore(store),
		// MemoryStore implements statestore.MessageLog; this is the
		// write-through path VAD mode never wired up.
		WithMessageLog(store),
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

	convID := conv.ID()
	require.NotEmpty(t, convID)

	speakOneUtterance(t, conv)

	require.True(t, provider.waitForTurns(1, 8*time.Second),
		"the model must fire during the session; fired %d times", provider.turnCount())

	// The session is still open — no Close, no hang-up. Anything in the log
	// now got there through the provider stage's per-round write-through.
	require.Eventually(t, func() bool {
		n, lenErr := store.LogLen(ctx, convID)
		return lenErr == nil && n > 0
	}, 5*time.Second, 20*time.Millisecond,
		"a voice turn must be persisted while the call is still up, not at hang-up")

	msgs, err := store.LogLoad(ctx, convID, 0)
	require.NoError(t, err)

	var userText string
	for _, m := range msgs {
		if m.Role == "user" {
			userText += m.GetContent()
		}
	}
	assert.Contains(t, userText, "what is the capital of france",
		"the persisted turn must be this turn's transcript")
}
