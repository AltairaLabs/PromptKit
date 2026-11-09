package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestSendSystemContext_NilSession(t *testing.T) {
	// Test that SendSystemContext handles closed session correctly
	session := &StreamSession{
		closed: true,
	}

	err := session.SendSystemContext(context.Background(), "test")
	if err == nil {
		t.Error("Expected error when sending to closed session")
	}
}

// recordLiveClient runs a Live API stand-in that completes setup and then
// forwards every client message, raw, until the client sends the text "done".
func recordLiveClient(t *testing.T) (wsURL string, msgs <-chan []string) {
	t.Helper()
	out := make(chan []string, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		replySetupComplete(t, conn)
		var got []string
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				out <- got
				return
			}
			if strings.Contains(string(data), `"done"`) {
				out <- got
				return
			}
			got = append(got, string(data))
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), out
}

// liveMessageKind names a client message by what the Live API sees.
func liveMessageKind(t *testing.T, raw string) string {
	t.Helper()
	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("client sent non-JSON %q: %v", raw, err)
	}
	// The Live API parses proto3 JSON, which accepts both field spellings;
	// the session sends audio as realtimeInput and activity signals as
	// realtime_input.
	ri, ok := m["realtimeInput"]
	if !ok {
		if ri, ok = m["realtime_input"]; !ok {
			return raw
		}
	}
	for _, k := range []string{"activityStart", "activityEnd"} {
		if _, ok := ri[k]; ok {
			return k
		}
	}
	return "audio"
}

// EndInput is the turn boundary in manual turn control (VAD disabled): it
// sends activityEnd, and the next turn's first audio is preceded by a fresh
// activityStart. With automatic activity detection it sends nothing, because
// the API rejects activity signals in that mode.
func TestEndInput_WireMessagesPerTurnControlMode(t *testing.T) {
	audio := &types.MediaChunk{Data: make([]byte, 320), Timestamp: time.Now()}
	cases := []struct {
		name string
		vad  *VADConfig
		want []string
	}{
		{"manual", &VADConfig{Disabled: true},
			[]string{"activityStart", "audio", "audio", "activityEnd", "activityStart", "audio"}},
		{"automatic", nil, []string{"audio", "audio", "audio"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, msgs := recordLiveClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := NewStreamSession(ctx, url, "k", &StreamSessionConfig{VAD: tc.vad})
			if err != nil {
				t.Fatalf("NewStreamSession: %v", err)
			}
			defer s.Close()

			for _, step := range []func() error{
				func() error { return s.SendChunk(ctx, audio) },
				func() error { return s.SendChunk(ctx, audio) },
				func() error { s.EndInput(); return nil },
				func() error { return s.SendChunk(ctx, audio) },
				func() error { return s.SendText(ctx, "done") },
			} {
				if err := step(); err != nil {
					t.Fatalf("send: %v", err)
				}
			}

			var got []string
			select {
			case raw := <-msgs:
				for _, m := range raw {
					got = append(got, liveMessageKind(t, m))
				}
			case <-ctx.Done():
				t.Fatal("server never saw the end marker")
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("client messages = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildTextMessage_TurnComplete(t *testing.T) {
	// Test buildTextMessage with turnComplete flag
	text := "Hello"

	// Test with turn_complete = false
	msg := buildTextMessage(text, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	clientContent, ok := msg["client_content"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected client_content in message")
	}

	turnComplete, ok := clientContent["turn_complete"].(bool)
	if !ok {
		t.Fatal("Expected turn_complete field")
	}

	if turnComplete {
		t.Error("Expected turn_complete to be false")
	}

	// Test with turn_complete = true
	msg2 := buildTextMessage(text, true)
	clientContent2, _ := msg2["client_content"].(map[string]interface{})
	turnComplete2, _ := clientContent2["turn_complete"].(bool)

	if !turnComplete2 {
		t.Error("Expected turn_complete to be true")
	}
}

func TestBuildClientMessage_AudioPCM(t *testing.T) {
	// Test buildClientMessage with audio data
	audioData := make([]byte, 1000)
	for i := range audioData {
		audioData[i] = byte(i % 256)
	}

	chunk := types.MediaChunk{
		Data:      audioData,
		Timestamp: time.Now(),
	}

	msg := buildClientMessage(chunk, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	// Audio must use the CURRENT realtimeInput.audio wire format. The legacy
	// realtime_input.media_chunks form is deprecated and closes current Gemini
	// Live models (e.g. gemini-*-live) with websocket close 1007. (#1666)
	if _, legacy := msg["realtime_input"]; legacy {
		t.Fatal("audio must not use the deprecated realtime_input.media_chunks form")
	}

	realtimeInput, ok := msg["realtimeInput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput in message")
	}

	audio, ok := realtimeInput["audio"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput.audio object")
	}

	// Gemini Live requires the sample rate in the audio mimeType.
	if mimeType, _ := audio["mimeType"].(string); mimeType != "audio/pcm;rate=16000" {
		t.Errorf("Expected mimeType 'audio/pcm;rate=16000', got %v", mimeType)
	}

	if data, _ := audio["data"].(string); data == "" {
		t.Error("Expected base64 encoded data")
	}
}

func TestBuildClientMessage_ImageJPEG(t *testing.T) {
	// Test buildClientMessage with image data - should use TypeScript SDK format
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // Fake JPEG header

	chunk := types.MediaChunk{
		Data:      imageData,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"mime_type": "image/jpeg",
		},
	}

	msg := buildClientMessage(chunk, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	// Image should use camelCase "realtimeInput" format (TypeScript SDK style)
	realtimeInput, ok := msg["realtimeInput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput (camelCase) in message for images")
	}

	// Should use singular "media" object, not "media_chunks" array
	media, ok := realtimeInput["media"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected media object in realtimeInput")
	}

	mimeType, ok := media["mimeType"].(string)
	if !ok || mimeType != "image/jpeg" {
		t.Errorf("Expected mimeType 'image/jpeg', got %v", mimeType)
	}

	data, ok := media["data"].(string)
	if !ok || data == "" {
		t.Error("Expected base64 encoded data")
	}
}

func TestBuildClientMessage_VideoPNG(t *testing.T) {
	// Test buildClientMessage with video/PNG data
	videoData := []byte{0x89, 0x50, 0x4E, 0x47} // Fake PNG header

	chunk := types.MediaChunk{
		Data:      videoData,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"mime_type": "video/mp4",
		},
	}

	msg := buildClientMessage(chunk, false)
	if msg == nil {
		t.Fatal("Expected non-nil message")
	}

	// Video should also use camelCase "realtimeInput" format
	realtimeInput, ok := msg["realtimeInput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected realtimeInput (camelCase) in message for video")
	}

	media, ok := realtimeInput["media"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected media object in realtimeInput")
	}

	mimeType, ok := media["mimeType"].(string)
	if !ok || mimeType != "video/mp4" {
		t.Errorf("Expected mimeType 'video/mp4', got %v", mimeType)
	}
}

func TestCompleteTurn_ClosedSession(t *testing.T) {
	session := &StreamSession{
		closed: true,
	}

	err := session.CompleteTurn(context.Background())
	if err == nil {
		t.Error("Expected error when completing turn on closed session")
	}
}
