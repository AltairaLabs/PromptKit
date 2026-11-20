package stt_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
)

// pcmTurn returns n bytes of distinguishable 16-bit PCM.
func pcmTurn(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + 3)
	}
	return b
}

const elevenLabsOK = `{"language_code":"en","language_probability":0.99,"text":"hello there","words":[]}`

func TestElevenLabs_TranscribeBytes_SendsRawPCMAt16k(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))
	pcm := pcmTurn(3200)

	text, err := svc.TranscribeBytes(context.Background(), pcm, stt.TranscriptionConfig{
		Format: stt.FormatPCM, SampleRate: 16000, Channels: 1, BitDepth: 16, Language: "en",
	})
	require.NoError(t, err)
	assert.Equal(t, "hello there", text)

	got := rec.only()
	assert.Equal(t, "POST", got.method)
	assert.Equal(t, "/speech-to-text", got.path)
	assert.Equal(t, "xi-key", got.headers.Get("xi-api-key"))
	assert.Empty(t, got.headers.Get("Authorization"))
	assert.Equal(t, map[string]string{
		"model_id":         "scribe_v3",
		"file_format":      "pcm_s16le_16",
		"language_code":    "en",
		"tag_audio_events": "false",
	}, got.fields)
	assert.Equal(t, "audio.pcm", got.filename)
	assert.Equal(t, pcm, got.file)
}

func TestElevenLabs_TranscribeBytes_WrapsOtherRatesAsWAV(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL), base.WithModel("scribe_v2"))
	pcm := pcmTurn(4800)

	_, err := svc.TranscribeBytes(context.Background(), pcm, stt.TranscriptionConfig{SampleRate: 24000})
	require.NoError(t, err)

	got := rec.only()
	assert.Equal(t, map[string]string{"model_id": "scribe_v2", "tag_audio_events": "false"}, got.fields)
	assert.Equal(t, "audio.wav", got.filename)
	assert.Equal(t, stt.WrapPCMAsWAV(pcm, 24000, 1, 16), got.file)
}

func TestElevenLabs_TranscribeBytes_SendsWAVAsIs(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))
	wav := stt.WrapPCMAsWAV(pcmTurn(320), 16000, 1, 16)

	_, err := svc.TranscribeBytes(context.Background(), wav, stt.TranscriptionConfig{Format: stt.FormatWAV})
	require.NoError(t, err)

	got := rec.only()
	assert.Equal(t, "audio.wav", got.filename)
	assert.Equal(t, wav, got.file)
	_, hasFormat := got.fields["file_format"]
	assert.False(t, hasFormat, "a WAV upload must not claim raw PCM")
}

func TestElevenLabs_Transcribe_CostFromReportedDuration(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200,
		`{"text":"priced","audio_duration_secs":90,"language_code":"en","language_probability":1,"words":[]}`})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))

	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio: pcmTurn(32000), MIMEType: "audio/pcm",
		Hints: map[string]string{"sample_rate": "16000", "language": "de"},
	})
	require.NoError(t, err)
	assert.Equal(t, "priced", resp.Text)
	require.NotNil(t, resp.Cost)
	assert.Equal(t, map[string]float64{"second": 90}, resp.Cost.Quantities)
	assert.InDelta(t, 90*0.20/3600, resp.Cost.TotalCost, 1e-12)
	assert.Equal(t, "elevenlabs", resp.Cost.ProviderName)
	assert.Equal(t, "de", rec.only().fields["language_code"])
}

func TestElevenLabs_Transcribe_CostEstimatedFromPCMWhenUnreported(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))

	// 64000 bytes of 16 kHz 16-bit mono is 2 seconds.
	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio: pcmTurn(64000), MIMEType: "audio/pcm", Hints: map[string]string{"sample_rate": "16000"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Cost)
	assert.Equal(t, map[string]float64{"second": 2}, resp.Cost.Quantities)
}

func TestElevenLabs_Transcribe_EmptyAudioSendsNothing(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))

	_, err := svc.Transcribe(context.Background(), base.STTRequest{})
	assert.ErrorIs(t, err, stt.ErrEmptyAudio)
	_, err = svc.TranscribeBytes(context.Background(), nil, stt.TranscriptionConfig{})
	assert.ErrorIs(t, err, stt.ErrEmptyAudio)
	assert.Empty(t, rec.requests())
}

func TestElevenLabs_Errors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		code      string
		message   string
		retryable bool
		cause     error
	}{
		{"unauthorized", 401, `{"detail":{"status":"invalid_api_key","message":"Invalid API key"}}`,
			"invalid_api_key", "Invalid API key", false, nil},
		{"rate limited", 429, `{"detail":{"status":"too_many_concurrent_requests","message":"slow down"}}`,
			"too_many_concurrent_requests", "slow down", true, stt.ErrRateLimited},
		{"validation", 422, `{"detail":[{"loc":["body","model_id"],"msg":"field required","type":"missing"}]}`,
			"422", "field required", false, nil},
		{"empty validation list", 422, `{"detail":[]}`, "422", `{"detail":[]}`, false, nil},
		{"server error", 503, `upstream unavailable`, "503", "upstream unavailable", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newUploadRecorder(t, scriptedResponse{tc.status, tc.body})
			svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))

			_, err := svc.TranscribeBytes(context.Background(), pcmTurn(320), stt.TranscriptionConfig{})
			var te *stt.TranscriptionError
			require.True(t, errors.As(err, &te), "error %v is not a TranscriptionError", err)
			assert.Equal(t, "elevenlabs", te.Provider)
			assert.Equal(t, tc.code, te.Code)
			assert.Equal(t, tc.message, te.Message)
			assert.Equal(t, tc.retryable, te.Retryable)
			if tc.cause != nil {
				assert.ErrorIs(t, err, tc.cause)
			}
		})
	}
}

func TestElevenLabs_UnauthorizedIsInvalidKey(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{401, `{"detail":{"status":"invalid_api_key","message":"x"}}`})
	svc := stt.NewElevenLabs("bad", base.WithBaseURL(rec.srv.URL))
	_, err := svc.TranscribeBytes(context.Background(), pcmTurn(320), stt.TranscriptionConfig{})
	var te *stt.TranscriptionError
	require.True(t, errors.As(err, &te))
	require.Error(t, te.Cause)
	assert.Equal(t, "invalid API key", te.Cause.Error())
}

func TestElevenLabs_RetriesRateLimitThenSucceeds(t *testing.T) {
	rec := newUploadRecorder(t,
		scriptedResponse{429, `{"detail":{"status":"rate_limited","message":"wait"}}`},
		scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))
	audio := pcmTurn(320)

	resp, err := stt.TranscribeWithRetry(context.Background(), svc,
		base.STTRequest{Audio: audio, MIMEType: "audio/pcm"},
		stt.RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, "hello there", resp.Text)
	got := rec.requests()
	require.Len(t, got, 2)
	assert.Equal(t, audio, got[1].file)
}

func TestElevenLabs_CreateFromSpec(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc, err := stt.CreateFromSpec(stt.ProviderSpec{
		Type:       "elevenlabs",
		Model:      "scribe_v2",
		BaseURL:    rec.srv.URL,
		Credential: credentials.NewAPIKeyCredential("spec-key"),
		AdditionalConfig: map[string]any{"pricing": map[string]any{
			"items": []any{map[string]any{"unit": "second", "rate": 0.5}},
		}},
	})
	require.NoError(t, err)

	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio: pcmTurn(32000), MIMEType: "audio/pcm", Hints: map[string]string{"sample_rate": "16000"},
	})
	require.NoError(t, err)
	assert.Equal(t, "hello there", resp.Text)
	got := rec.only()
	assert.Equal(t, "spec-key", got.headers.Get("xi-api-key"))
	assert.Equal(t, "scribe_v2", got.fields["model_id"])
	require.NotNil(t, resp.Cost)
	assert.InDelta(t, 0.5, resp.Cost.TotalCost, 1e-9)
}

func TestElevenLabs_TranscribeBytes_StereoPCMIsWrappedAsWAV(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))
	pcm := pcmTurn(640)

	_, err := svc.TranscribeBytes(context.Background(), pcm, stt.TranscriptionConfig{SampleRate: 16000, Channels: 2})
	require.NoError(t, err)

	got := rec.only()
	assert.Equal(t, "audio.wav", got.filename)
	assert.Equal(t, stt.WrapPCMAsWAV(pcm, 16000, 2, 16), got.file)
	_, hasFormat := got.fields["file_format"]
	assert.False(t, hasFormat, "stereo is not the raw format the endpoint takes")
}

// The current error shape carries code (and type) without status.
func TestElevenLabs_ErrorCodeWithoutStatus(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{400,
		`{"detail":{"type":"invalid_request","code":"audio_too_long","message":"too long"}}`})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))

	_, err := svc.TranscribeBytes(context.Background(), pcmTurn(320), stt.TranscriptionConfig{})
	var te *stt.TranscriptionError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, "audio_too_long", te.Code)
	assert.Equal(t, "too long", te.Message)
	assert.False(t, te.Retryable)
}

// Compressed audio with no reported duration has no seconds to price, so the
// response carries no cost rather than a zero one.
func TestElevenLabs_Transcribe_NoCostWithoutDuration(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, elevenLabsOK})
	svc := stt.NewElevenLabs("xi-key", base.WithBaseURL(rec.srv.URL))

	resp, err := svc.Transcribe(context.Background(), base.STTRequest{Audio: []byte("mp3-bytes"), MIMEType: "audio/mpeg"})
	require.NoError(t, err)
	assert.Equal(t, "hello there", resp.Text)
	assert.Nil(t, resp.Cost)
	assert.Equal(t, "audio.mp3", rec.only().filename)
}
