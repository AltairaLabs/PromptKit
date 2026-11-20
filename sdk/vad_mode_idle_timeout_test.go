package sdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// vadIdleTimeout is the idle timeout the tests open VAD sessions with: short,
// so the tests outlast it several times over in about a second.
const vadIdleTimeout = 300 * time.Millisecond

// openVADModeConvWithIdle opens a VAD-mode duplex conversation with the given
// idle timeout and drains its Response().
func openVADModeConvWithIdle(
	t *testing.T, sttSvc *scriptedSTTService, provider *turnRecordingProvider, idle time.Duration,
) *Conversation {
	t.Helper()
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithIdleTimeout(idle),
		WithVADMode(sttSvc, newConvMockTTSService(), &VADModeConfig{
			SilenceDuration:   300 * time.Millisecond,
			MinSpeechDuration: 100 * time.Millisecond,
			MaxTurnDuration:   5 * time.Second,
			SampleRate:        perTurnTestSampleRate,
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
	return conv
}

// sendSilenceFor streams silence in real time for d, 20 ms per chunk, as an
// open microphone in a quiet room does while the caller listens.
func sendSilenceFor(t *testing.T, conv *Conversation, d time.Duration) {
	t.Helper()
	const frame = 20 * time.Millisecond
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(frame) {
		require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: pcmSilence(frame, perTurnTestSampleRate)},
		}))
	}
}

// A VAD session whose caller's microphone stays open is not idle: the audio
// it sends is activity, as in ASM mode. A caller who listens to a reply for
// longer than the idle timeout, sending only silence, keeps the session, and
// their next utterance is answered.
func TestVADMode_CallerAudioKeepsTheSessionAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real VAD turns in real time")
	}
	sttSvc := newScriptedSTT("first question", "second question")
	provider := &turnRecordingProvider{}
	conv := openVADModeConvWithIdle(t, sttSvc, provider, vadIdleTimeout)

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "the first utterance must fire the model")

	sendSilenceFor(t, conv, 4*vadIdleTimeout)

	require.NoError(t, conv.SessionError(), "the session must not have ended while audio kept arriving")
	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(2, 8*time.Second),
		"the second utterance must fire the model; fired %d times", provider.turnCount())
	assert.Contains(t, userTextsIn(provider.turnAt(1)), "second question")
}

// With no audio for longer than the idle timeout, the idle timeout still ends
// a VAD session: the next audio finds the session over, ended by the idle
// timeout.
func TestVADMode_NoAudioStillEndsTheSessionOnIdleTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn in real time")
	}
	provider := &turnRecordingProvider{}
	conv := openVADModeConvWithIdle(t, newScriptedSTT("first question"), provider, vadIdleTimeout)

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "the utterance must fire the model")
	time.Sleep(3 * vadIdleTimeout)
	// The stages notice the canceled session on their next input.
	_ = conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: pcmSilence(20*time.Millisecond, perTurnTestSampleRate)},
	})

	done, err := conv.Done()
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the idle session did not end")
	}
	assert.True(t, errors.Is(conv.SessionError(), stage.ErrIdleTimeout),
		"want the idle timeout, got %v", conv.SessionError())
}
