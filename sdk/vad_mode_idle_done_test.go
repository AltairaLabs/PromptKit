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

// An idle VAD session ends on its own: once the idle timeout passes with no
// activity, Done() closes and SessionError() reports the idle timeout, with no
// further input from the caller. It used to wait for the next input, so a
// caller waiting on Done() hung (#2823).
func TestVADMode_IdleTimeoutClosesDoneWithoutFurtherInput(t *testing.T) {
	const idle = 50 * time.Millisecond
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(&turnRecordingProvider{}),
		WithSkipSchemaValidation(),
		WithIdleTimeout(idle),
		WithVADMode(newScriptedSTT(), newConvMockTTSService(), &VADModeConfig{
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
	var lastErr error
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for chunk := range responseCh {
			if chunk.Error != nil {
				lastErr = chunk.Error
			}
		}
	}()

	// One chunk starts the session's pipeline; nothing follows it.
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{
			MIMEType: "audio/pcm", Data: pcmSilence(20*time.Millisecond, perTurnTestSampleRate),
		},
	}))

	done, err := conv.Done()
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(idle + 2*time.Second):
		t.Fatal("Done() did not close after the idle timeout with no further input")
	}
	assert.True(t, errors.Is(conv.SessionError(), stage.ErrIdleTimeout),
		"SessionError() = %v, want the idle timeout", conv.SessionError())

	<-drained
	assert.True(t, errors.Is(lastErr, stage.ErrIdleTimeout),
		"Response() must deliver the idle timeout before closing; last error %v", lastErr)
}
