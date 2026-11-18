package tts

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// geminiWAV builds a mono 16-bit PCM WAV at rate holding pcm, followed by a
// trailing chunk the way Gemini TTS appends its C2PA manifest after the data.
func geminiWAV(pcm []byte, rate int, trailer string) []byte {
	le32 := func(v int) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, uint32(v)); return b }
	le16 := func(v int) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, uint16(v)); return b }
	var fmtBody []byte
	fmtBody = append(fmtBody, le16(1)...)
	fmtBody = append(fmtBody, le16(1)...)
	fmtBody = append(fmtBody, le32(rate)...)
	fmtBody = append(fmtBody, le32(rate*2)...)
	fmtBody = append(fmtBody, le16(2)...)
	fmtBody = append(fmtBody, le16(16)...)
	var body []byte
	body = append(body, "WAVE"...)
	body = append(body, "fmt "...)
	body = append(body, le32(len(fmtBody))...)
	body = append(body, fmtBody...)
	body = append(body, "data"...)
	body = append(body, le32(len(pcm))...)
	body = append(body, pcm...)
	if trailer != "" {
		body = append(body, "C2PA"...)
		body = append(body, le32(len(trailer))...)
		body = append(body, trailer...)
	}
	return append(append([]byte("RIFF"), le32(len(body))...), body...)
}

// geminiAudioResponse is a generateContent response carrying one inline audio part.
func geminiAudioResponse(mimeType string, data []byte) string {
	b, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
		"content": map[string]any{"role": "model", "parts": []any{map[string]any{
			"inlineData": map[string]any{"mimeType": mimeType, "data": base64.StdEncoding.EncodeToString(data)},
		}}},
		"finishReason": "STOP",
	}}})
	return string(b)
}

type geminiTTSCall struct {
	path, apiKey, auth, body string
}

// geminiTTSServer answers every request with status and response, and
// records what it was sent.
func geminiTTSServer(t *testing.T, status int, response string) (*httptest.Server, <-chan geminiTTSCall) {
	t.Helper()
	calls := make(chan geminiTTSCall, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls <- geminiTTSCall{
			path: r.URL.Path, apiKey: r.Header.Get("x-goog-api-key"),
			auth: r.Header.Get("Authorization"), body: string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

func readAllAudio(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	defer rc.Close()
	b, err := io.ReadAll(rc)
	require.NoError(t, err)
	return b
}

var testPCM = []byte{0x01, 0x00, 0xff, 0x7f, 0x00, 0x80, 0x10, 0x20}

// The request is generateContent with the text, AUDIO output and the voice;
// the reply is the WAV's samples only — no header, no trailing C2PA chunk.
func TestGeminiService_Synthesize_RequestAndPCM(t *testing.T) {
	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "manifest")))
	svc := NewGemini("g-key", base.WithBaseURL(srv.URL+"/v1beta"), base.WithModel("tts-model"))

	rc, err := svc.Synthesize(context.Background(), "Hello there", SynthesisConfig{Voice: "Kore", Format: FormatPCM16})
	require.NoError(t, err)
	assert.Equal(t, testPCM, readAllAudio(t, rc))

	call := <-calls
	assert.Equal(t, "/v1beta/models/tts-model:generateContent", call.path)
	assert.Equal(t, "g-key", call.apiKey)
	assert.Empty(t, call.auth)
	assert.JSONEq(t, `{
		"contents":[{"parts":[{"text":"Hello there"}]}],
		"generationConfig":{"responseModalities":["AUDIO"],
			"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Kore"}}}}
	}`, call.body)
}

// With no voice, model or format, the service speaks Gemini Live's default
// voice with its configured model and returns PCM16; a language is sent as
// the speech config's languageCode.
func TestGeminiService_Synthesize_Defaults(t *testing.T) {
	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "")))
	svc := NewGemini("k", base.WithBaseURL(srv.URL))

	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{Language: "en-GB"})
	require.NoError(t, err)
	assert.Equal(t, testPCM, readAllAudio(t, rc))

	call := <-calls
	assert.Equal(t, "/models/"+DefaultGeminiTTSModel+":generateContent", call.path)
	assert.JSONEq(t, `{
		"contents":[{"parts":[{"text":"Hi"}]}],
		"generationConfig":{"responseModalities":["AUDIO"],
			"speechConfig":{"languageCode":"en-GB","voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Puck"}}}}
	}`, call.body)
}

// A per-call model overrides the configured one.
func TestGeminiService_Synthesize_ConfigModelWins(t *testing.T) {
	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "")))
	svc := NewGemini("k", base.WithBaseURL(srv.URL), base.WithModel("configured"))
	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{Model: "per-call"})
	require.NoError(t, err)
	_ = readAllAudio(t, rc)
	assert.Equal(t, "/models/per-call:generateContent", (<-calls).path)
}

// Raw PCM replies (audio/L16 with a rate parameter) are returned as is, and
// any reply is resampled to the rate the caller asked for.
func TestGeminiService_Synthesize_RawPCMAndResampling(t *testing.T) {
	srv, _ := geminiTTSServer(t, http.StatusOK, geminiAudioResponse("audio/L16;codec=pcm;rate=24000", testPCM))
	svc := NewGemini("k", base.WithBaseURL(srv.URL))
	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{Format: FormatPCM16})
	require.NoError(t, err)
	assert.Equal(t, testPCM, readAllAudio(t, rc))

	long := make([]byte, 4800) // 100 ms at 24 kHz
	for i := range long {
		long[i] = byte(i)
	}
	srv2, _ := geminiTTSServer(t, http.StatusOK, geminiAudioResponse("audio/wav", geminiWAV(long, 24000, "x")))
	svc2 := NewGemini("k", base.WithBaseURL(srv2.URL))
	format := FormatPCM16
	format.SampleRate = 16000
	rc, err = svc2.Synthesize(context.Background(), "Hi", SynthesisConfig{Format: format})
	require.NoError(t, err)
	want, err := audio.ResamplePCM16(long, 24000, 16000)
	require.NoError(t, err)
	assert.Equal(t, want, readAllAudio(t, rc))

	srv3, _ := geminiTTSServer(t, http.StatusOK, geminiAudioResponse("audio/L16;codec=pcm;rate=16000", long))
	svc3 := NewGemini("k", base.WithBaseURL(srv3.URL))
	format.SampleRate = 24000
	rc, err = svc3.Synthesize(context.Background(), "Hi", SynthesisConfig{Format: format})
	require.NoError(t, err)
	want, err = audio.ResamplePCM16(long, 16000, 24000)
	require.NoError(t, err)
	assert.Equal(t, want, readAllAudio(t, rc), "the L16 rate parameter is the source rate")
}

func TestGeminiService_Synthesize_Errors(t *testing.T) {
	ctx := context.Background()

	_, err := NewGemini("k").Synthesize(ctx, "", SynthesisConfig{})
	assert.ErrorIs(t, err, ErrEmptyText)

	_, err = NewGemini("k").Synthesize(ctx, "Hi", SynthesisConfig{Format: FormatMP3})
	assert.ErrorIs(t, err, ErrInvalidFormat)
	assert.EqualError(t, err, `gemini: Gemini TTS returns pcm audio, not "mp3"`+
		": invalid or unsupported audio format")

	tests := []struct {
		name      string
		status    int
		response  string
		cause     error
		message   string
		retryable bool
	}{
		{"bad request", http.StatusBadRequest,
			`{"error":{"code":400,"message":"Voice name nope is not supported.","status":"INVALID_ARGUMENT"}}`,
			nil, "Voice name nope is not supported.", false},
		{"rate limited", http.StatusTooManyRequests,
			`{"error":{"code":429,"message":"Resource exhausted.","status":"RESOURCE_EXHAUSTED"}}`,
			ErrRateLimited, "Resource exhausted.", true},
		{"bad key", http.StatusUnauthorized, `{"error":{"code":401,"message":"API key not valid."}}`,
			nil, "API key not valid.", false},
		{"server", http.StatusServiceUnavailable, `{"error":{"code":503,"message":"overloaded"}}`,
			nil, "overloaded", true},
		{"no audio", http.StatusOK, `{"candidates":[{"content":{"parts":[{"text":"no"}]},"finishReason":"OTHER"}]}`,
			ErrSynthesisFailed, "no audio in the response (finish reason OTHER)", false},
		{"unknown mime", http.StatusOK, geminiAudioResponse("audio/mpeg", []byte("ID3")),
			ErrInvalidFormat, `unsupported audio type "audio/mpeg"`, false},
		{"stereo wav", http.StatusOK, geminiAudioResponse("audio/wav", stereoWAV()),
			ErrInvalidFormat, "WAV is not mono 16-bit PCM", false},
		{"truncated wav", http.StatusOK, geminiAudioResponse("audio/wav", []byte("RIFF\x00\x00\x00\x00WAVEfmt ")),
			ErrInvalidFormat, "WAV has no fmt and data chunks", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := geminiTTSServer(t, tt.status, tt.response)
			_, err := NewGemini("k", base.WithBaseURL(srv.URL)).Synthesize(ctx, "Hi", SynthesisConfig{})
			var se *SynthesisError
			require.ErrorAs(t, err, &se)
			assert.Equal(t, "gemini", se.Provider)
			assert.Equal(t, tt.message, se.Message)
			assert.Equal(t, tt.retryable, se.Retryable)
			if tt.cause != nil {
				assert.True(t, errors.Is(err, tt.cause), "cause %v, got %v", tt.cause, se.Cause)
			}
		})
	}
}

func stereoWAV() []byte {
	w := geminiWAV(testPCM, 24000, "")
	binary.LittleEndian.PutUint16(w[22:], 2) // channels
	return w
}

// The voice set is Gemini's prebuilt voices, the ones Gemini Live speaks
// with, so a cue voice can be matched to a session voice by name.
func TestGeminiService_SupportedVoicesAndFormats(t *testing.T) {
	svc := NewGemini("k")
	voices := svc.SupportedVoices()
	require.Len(t, voices, 30)
	ids := make([]string, len(voices))
	for i, v := range voices {
		ids[i] = v.ID
		assert.Equal(t, v.ID, v.Name)
		assert.NotEmpty(t, v.Description, v.ID)
	}
	assert.Contains(t, ids, "Puck")
	assert.Contains(t, ids, "Kore")
	assert.Contains(t, ids, "Sulafat")
	assert.Equal(t, "Zephyr", voices[0].ID)
	assert.Equal(t, "Bright", voices[0].Description)

	assert.Equal(t, []AudioFormat{{Name: "pcm", MIMEType: "audio/pcm", SampleRate: 24000, BitDepth: 16, Channels: 1}},
		svc.SupportedFormats())
	assert.Equal(t, "gemini", svc.Name())
	assert.Equal(t, "gemini", svc.ImplName())
	assert.Equal(t, DefaultGeminiTTSModel, svc.ModelName())
}

// On Vertex the request goes to the location's Google publisher model with
// the platform credential and no API key.
func TestGeminiService_Vertex(t *testing.T) {
	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "")))
	svc := NewGeminiOnVertex(bearerCred("gcp-token"), "proj", "us-central1",
		base.WithModel("vertex-tts"), base.WithBaseURL(srv.URL+"/v1/projects/proj/locations/us-central1/publishers/google/models"))
	rc, err := svc.Synthesize(context.Background(), "Hi", SynthesisConfig{Voice: "Charon"})
	require.NoError(t, err)
	assert.Equal(t, testPCM, readAllAudio(t, rc))
	call := <-calls
	assert.Equal(t, "/v1/projects/proj/locations/us-central1/publishers/google/models/vertex-tts:generateContent", call.path)
	assert.Equal(t, "Bearer gcp-token", call.auth)
	assert.Empty(t, call.apiKey)

	assert.Equal(t, "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models",
		NewGeminiOnVertex(bearerCred("t"), "p", "us-central1").BaseURL)
	assert.Equal(t, "https://aiplatform.googleapis.com/v1/projects/p/locations/global/publishers/google/models",
		NewGeminiOnVertex(bearerCred("t"), "p", "global").BaseURL)
}

type bearerCred string

func (b bearerCred) Apply(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+string(b))
	return nil
}

func (bearerCred) Type() string { return "gcp" }

var _ credentials.Credential = bearerCred("")

// SynthesizeTTS streams the same PCM Synthesize returns.
func TestGeminiService_SynthesizeTTS(t *testing.T) {
	srv, calls := geminiTTSServer(t, http.StatusOK,
		geminiAudioResponse("audio/wav", geminiWAV(testPCM, 24000, "")))
	svc := NewGemini("k", base.WithBaseURL(srv.URL))
	stream, err := svc.SynthesizeTTS(context.Background(), base.TTSRequest{Text: "Hi", Voice: "Aoede"})
	require.NoError(t, err)
	got, _, err := base.ReadAllAudio(stream)
	require.NoError(t, err)
	assert.Equal(t, testPCM, got)
	assert.Contains(t, (<-calls).body, `"voiceName":"Aoede"`)
}
