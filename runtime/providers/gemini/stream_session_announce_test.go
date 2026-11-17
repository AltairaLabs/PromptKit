package gemini

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Announce has Gemini Live speak a text: Live has no request for a reply with
// given instructions, so the instruction to say it goes as a complete user
// turn.
func TestGeminiStreamSession_AnnounceSendsTheInstructionAsATurn(t *testing.T) {
	got := make(chan map[string]any, 1)
	server := newMockWebSocketServer(func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage() // setup
		data, _ := json.Marshal(ServerMessage{SetupComplete: &SetupComplete{}})
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m map[string]any
		_ = json.Unmarshal(msg, &m)
		got <- m
		_, _, _ = conn.ReadMessage()
	})
	defer server.Close()

	ctx := context.Background()
	session, err := NewStreamSession(ctx, server.URL(), "test-key", &StreamSessionConfig{ResponseModalities: []string{"AUDIO"}})
	if err != nil {
		t.Fatalf("NewStreamSession: %v", err)
	}
	defer session.Close()

	var a providers.Announcer = session
	spoken, err := a.Announce(ctx, "Sorry, I can't say that word.")
	if err != nil || !spoken {
		t.Fatalf("Announce = %v, %v; want spoken", spoken, err)
	}

	select {
	case m := <-got:
		cc, _ := m["client_content"].(map[string]any)
		if cc["turn_complete"] != true {
			t.Errorf("turn_complete = %v, want true", cc["turn_complete"])
		}
		turns, _ := cc["turns"].([]any)
		if len(turns) != 1 {
			t.Fatalf("turns = %v, want one", turns)
		}
		turn, _ := turns[0].(map[string]any)
		parts, _ := turn["parts"].([]any)
		part, _ := parts[0].(map[string]any)
		if want := providers.AnnounceInstructions("Sorry, I can't say that word."); part["text"] != want {
			t.Errorf("text = %v, want %q", part["text"], want)
		}
		if turn["role"] != "user" {
			t.Errorf("role = %v, want user", turn["role"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the server received nothing")
	}
}
