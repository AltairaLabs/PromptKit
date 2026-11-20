package tts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// liveDefaultCartesiaVoice is "Katie - Friendly Fixer", verified live against
// GET /voices/{id} and POST /tts/bytes (#2822). The previous default
// (a0e99841-…, "Barbershop Man") now 404s, which made a voiceless service
// speak nothing.
const liveDefaultCartesiaVoice = "f786b574-daa5-4673-aa0c-cbe3e8534c02"

// cartesiaVoiceNotFoundBody is the body Cartesia's REST API returns for a
// voice it does not serve (captured live, Cartesia-Version 2026-03-01).
const cartesiaVoiceNotFoundBody = `{"error_code":"voice_not_found",` +
	`"message":"The requested voice was not found.","title":"Voice not found"}`

func voiceNotFoundServer(t *testing.T, gotVoice *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req cartesiaRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		*gotVoice = req.Voice.ID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(cartesiaVoiceNotFoundBody))
	}))
}

func TestCartesia_Synthesize_RejectedVoice_ErrorNamesVoice(t *testing.T) {
	var gotVoice string
	srv := voiceNotFoundServer(t, &gotVoice)
	defer srv.Close()

	s := NewCartesia("test-key", base.WithBaseURL(srv.URL))
	_, err := s.Synthesize(context.Background(), "Hello", SynthesisConfig{Voice: "no-such-voice"})

	require.Error(t, err)
	assert.Equal(t, "no-such-voice", gotVoice)
	assert.True(t, errors.Is(err, ErrInvalidVoice), "a rejected voice is ErrInvalidVoice: %v", err)
	assert.Equal(t,
		`cartesia: voice "no-such-voice": The requested voice was not found.: invalid or unsupported voice`,
		err.Error())
	var synthErr *SynthesisError
	require.True(t, errors.As(err, &synthErr))
	assert.Equal(t, "voice_not_found", synthErr.Code)
	assert.False(t, synthErr.Retryable)
}

func TestCartesia_Synthesize_RejectedDefaultVoice_ErrorNamesDefault(t *testing.T) {
	var gotVoice string
	srv := voiceNotFoundServer(t, &gotVoice)
	defer srv.Close()

	s := NewCartesia("test-key", base.WithBaseURL(srv.URL))
	_, err := s.Synthesize(context.Background(), "Hello", SynthesisConfig{})

	require.Error(t, err)
	assert.Equal(t, liveDefaultCartesiaVoice, gotVoice, "an empty voice sends the default")
	assert.Equal(t,
		`cartesia: voice "`+liveDefaultCartesiaVoice+
			`": The requested voice was not found.: invalid or unsupported voice`,
		err.Error())
}

// cartesiaWSFrameServer upgrades the connection, reads the synthesis request
// (recording its voice) and answers with one frame.
func cartesiaWSFrameServer(t *testing.T, frame string, gotVoice *string) *CartesiaService {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var req struct {
			Voice struct {
				ID string `json:"id"`
			} `json:"voice"`
		}
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		*gotVoice = req.Voice.ID
		_ = conn.WriteMessage(websocket.TextMessage, []byte(frame))
		_, _, _ = conn.ReadMessage() // hold the connection until the client closes
	}))
	t.Cleanup(srv.Close)
	s := NewCartesia("test-key")
	s.wsURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	return s
}

func firstStreamError(t *testing.T, s *CartesiaService, voice string) error {
	t.Helper()
	chunks, err := s.SynthesizeStream(context.Background(), "Hello", SynthesisConfig{Voice: voice})
	require.NoError(t, err)
	for c := range chunks {
		if c.Error != nil {
			return c.Error
		}
	}
	return nil
}

func TestCartesia_SynthesizeStream_RejectedVoice_ErrorNamesVoice(t *testing.T) {
	// The frame Cartesia's websocket sends at the API version the service
	// dials with (captured live).
	frame := `{"type":"error","context_id":"x","status_code":404,"done":true,` +
		`"error":"Voice not found: The requested voice was not found."}`
	var gotVoice string
	s := cartesiaWSFrameServer(t, frame, &gotVoice)

	err := firstStreamError(t, s, "no-such-voice")

	require.Error(t, err)
	assert.Equal(t, "no-such-voice", gotVoice)
	assert.True(t, errors.Is(err, ErrInvalidVoice), "a rejected voice is ErrInvalidVoice: %v", err)
	assert.Equal(t,
		`cartesia: voice "no-such-voice": Voice not found: The requested voice was not found.: `+
			`invalid or unsupported voice`,
		err.Error())
}

// A newer API version's error frame carries error_code/title/message and no
// "error" field. It must still surface as an error, not end the stream silently.
func TestCartesia_SynthesizeStream_ErrorFrameWithoutErrorField_Surfaces(t *testing.T) {
	frame := `{"type":"error","context_id":"x","status_code":404,"done":true,` +
		`"error_code":"voice_not_found","title":"Voice not found",` +
		`"message":"The requested voice was not found."}`
	var gotVoice string
	s := cartesiaWSFrameServer(t, frame, &gotVoice)

	err := firstStreamError(t, s, "")

	require.Error(t, err, "an error frame must not end the stream as silence")
	assert.Equal(t, liveDefaultCartesiaVoice, gotVoice)
	assert.True(t, errors.Is(err, ErrInvalidVoice), "a rejected voice is ErrInvalidVoice: %v", err)
	assert.Equal(t,
		`cartesia: voice "`+liveDefaultCartesiaVoice+
			`": Voice not found: The requested voice was not found.: invalid or unsupported voice`,
		err.Error())
	var synthErr *SynthesisError
	require.True(t, errors.As(err, &synthErr))
	assert.Equal(t, "voice_not_found", synthErr.Code)
}
