package stt_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
)

const cartesiaOK = `{"type":"transcript","text":"good morning","request_id":"r1","language":"en"}`

func TestCartesia_TranscribeBytes_SendsRawPCMWithEncodingQuery(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))
	pcm := pcmTurn(4800)

	text, err := svc.TranscribeBytes(context.Background(), pcm, stt.TranscriptionConfig{
		Format: stt.FormatPCM, SampleRate: 24000, Language: "fr",
	})
	require.NoError(t, err)
	assert.Equal(t, "good morning", text)

	got := rec.only()
	assert.Equal(t, "POST", got.method)
	assert.Equal(t, "/stt", got.path)
	assert.Equal(t, map[string]string{"encoding": "pcm_s16le", "sample_rate": "24000"}, got.query)
	assert.Equal(t, "Bearer sk_car_test", got.headers.Get("Authorization"))
	assert.Equal(t, "2026-08-14", got.headers.Get("Cartesia-Version"))
	assert.Equal(t, map[string]string{"model": "ink-whisper", "language": "fr"}, got.fields)
	assert.Equal(t, "audio.pcm", got.filename)
	assert.Equal(t, pcm, got.file)
}

func TestCartesia_TranscribeBytes_NoLanguageLeavesFieldOut(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL), base.WithModel("ink-whisper-next"))

	_, err := svc.TranscribeBytes(context.Background(), pcmTurn(320), stt.TranscriptionConfig{})
	require.NoError(t, err)

	got := rec.only()
	assert.Equal(t, map[string]string{"model": "ink-whisper-next"}, got.fields)
	assert.Equal(t, map[string]string{"encoding": "pcm_s16le", "sample_rate": "16000"}, got.query)
}

func TestCartesia_TranscribeBytes_StereoPCMIsWrappedAsWAV(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))
	pcm := pcmTurn(640)

	_, err := svc.TranscribeBytes(context.Background(), pcm, stt.TranscriptionConfig{Channels: 2})
	require.NoError(t, err)

	got := rec.only()
	assert.Empty(t, got.query, "a WAV upload is decoded from its container")
	assert.Equal(t, "audio.wav", got.filename)
	assert.Equal(t, stt.WrapPCMAsWAV(pcm, 16000, 2, 16), got.file)
}

func TestCartesia_TranscribeBytes_SendsMP3AsIs(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))
	mp3 := []byte("ID3-not-really-mp3")

	_, err := svc.TranscribeBytes(context.Background(), mp3, stt.TranscriptionConfig{Format: stt.FormatMP3})
	require.NoError(t, err)

	got := rec.only()
	assert.Empty(t, got.query)
	assert.Equal(t, "audio.mp3", got.filename)
	assert.Equal(t, mp3, got.file)
}

func TestCartesia_Transcribe_CostFromReportedDuration(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, `{"type":"transcript","text":"priced","duration":3600}`})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))

	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio: pcmTurn(32000), MIMEType: "audio/pcm", Hints: map[string]string{"sample_rate": "16000"},
	})
	require.NoError(t, err)
	assert.Equal(t, "priced", resp.Text)
	require.NotNil(t, resp.Cost)
	assert.Equal(t, map[string]float64{"second": 3600}, resp.Cost.Quantities)
	assert.InDelta(t, 3600*0.5*299.0/8_000_000, resp.Cost.TotalCost, 1e-12)
	assert.Equal(t, "cartesia", resp.Cost.ProviderName)
}

func TestCartesia_Transcribe_CostEstimatedFromPCMWhenUnreported(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))

	// 48000 bytes of 24 kHz 16-bit mono is 1 second.
	resp, err := svc.Transcribe(context.Background(), base.STTRequest{
		Audio: pcmTurn(48000), MIMEType: "audio/pcm", Hints: map[string]string{"sample_rate": "24000"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Cost)
	assert.Equal(t, map[string]float64{"second": 1}, resp.Cost.Quantities)
	assert.Equal(t, "24000", rec.only().query["sample_rate"])
}

func TestCartesia_Transcribe_EmptyAudioSendsNothing(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))

	_, err := svc.Transcribe(context.Background(), base.STTRequest{})
	assert.ErrorIs(t, err, stt.ErrEmptyAudio)
	_, err = svc.TranscribeBytes(context.Background(), nil, stt.TranscriptionConfig{})
	assert.ErrorIs(t, err, stt.ErrEmptyAudio)
	assert.Empty(t, rec.requests())
}

func TestCartesia_Errors(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		message   string
		retryable bool
		cause     error
	}{
		{"unauthorized", 401, `{"error":"Invalid API key"}`, "Invalid API key", false, nil},
		{"rate limited", 429, `{"error":"rate_limited","message":"Too many requests"}`,
			"Too many requests", true, stt.ErrRateLimited},
		{"bad request", 400, `{"message":"unsupported language"}`, "unsupported language", false, nil},
		{"server error", 500, `internal`, "internal", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newUploadRecorder(t, scriptedResponse{tc.status, tc.body})
			svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))

			_, err := svc.TranscribeBytes(context.Background(), pcmTurn(320), stt.TranscriptionConfig{})
			var te *stt.TranscriptionError
			require.True(t, errors.As(err, &te), "error %v is not a TranscriptionError", err)
			assert.Equal(t, "cartesia", te.Provider)
			assert.Equal(t, strconv.Itoa(tc.status), te.Code)
			assert.Equal(t, tc.message, te.Message)
			assert.Equal(t, tc.retryable, te.Retryable)
			if tc.cause != nil {
				assert.ErrorIs(t, err, tc.cause)
			}
		})
	}
}

func TestCartesia_RetriesServerErrorThenSucceeds(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{502, `bad gateway`}, scriptedResponse{200, cartesiaOK})
	svc := stt.NewCartesia("sk_car_test", base.WithBaseURL(rec.srv.URL))

	resp, err := stt.TranscribeWithRetry(context.Background(), svc,
		base.STTRequest{Audio: pcmTurn(320), MIMEType: "audio/pcm"},
		stt.RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, "good morning", resp.Text)
	assert.Len(t, rec.requests(), 2)
}

func TestCartesia_UnauthorizedIsNotRetried(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{401, `{"error":"Invalid API key"}`})
	svc := stt.NewCartesia("bad", base.WithBaseURL(rec.srv.URL))

	_, err := stt.TranscribeWithRetry(context.Background(), svc,
		base.STTRequest{Audio: pcmTurn(320), MIMEType: "audio/pcm"},
		stt.RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond})
	var te *stt.TranscriptionError
	require.True(t, errors.As(err, &te))
	require.Error(t, te.Cause)
	assert.Equal(t, "invalid API key", te.Cause.Error())
	assert.Len(t, rec.requests(), 1)
}

func TestCartesia_CreateFromSpec(t *testing.T) {
	rec := newUploadRecorder(t, scriptedResponse{200, cartesiaOK})
	svc, err := stt.CreateFromSpec(stt.ProviderSpec{
		Type:       "cartesia",
		BaseURL:    rec.srv.URL,
		Credential: credentials.NewAPIKeyCredential("spec-key"),
	})
	require.NoError(t, err)

	text, err := svc.TranscribeBytes(context.Background(), pcmTurn(320), stt.TranscriptionConfig{})
	require.NoError(t, err)
	assert.Equal(t, "good morning", text)
	got := rec.only()
	assert.Equal(t, "Bearer spec-key", got.headers.Get("Authorization"))
	assert.Equal(t, "ink-whisper", got.fields["model"])
}

func TestRegisteredTypes_IncludesElevenLabsAndCartesia(t *testing.T) {
	assert.Subset(t, stt.RegisteredTypes(), []string{"cartesia", "elevenlabs", "openai"})
}
