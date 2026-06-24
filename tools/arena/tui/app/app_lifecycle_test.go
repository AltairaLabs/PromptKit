package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestApp_ChatPageRevealedAfterSplash_AutoAdvances drives the REAL launch
// lifecycle — the path no existing test covered: the stack is seeded as
// [ChatPage, Splash] (exactly as Run does), and dismissing the splash must
// reveal ChatPage and run its setup so a single-agent / single-provider config
// lands directly in chat, NOT stuck on an empty "Select an agent" picker.
//
// Regression: initAndActivate ran Init() before Activate(), so ChatPage.Init()
// read a nil engine (Activate is what wires it), bailed early, and left the page
// in its zero-value state (chatStateSelectAgent) with no agents — the empty
// picker users hit at launch. The component-level tests called Activate() then
// Init() (the opposite order), so they never exercised this.
func TestApp_ChatPageRevealedAfterSplash_AutoAdvances(t *testing.T) {
	fixturePath := filepath.Join("testdata", "chat-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	chat := NewChatPage(ctx)
	app := New(ctx, chat)
	// Mirror Run(): splash is appended on top of the root so dismissing it
	// reveals the root.
	app.stack = append(app.stack, NewSplash(ctx))
	app.SetSend(func(tea.Msg) {})

	_ = app.Init()                  // inits the splash (top); ChatPage NOT yet inited
	_, _ = app.Update(PopPageMsg{}) // dismiss splash → reveal + init/activate ChatPage

	if chat.engineErr != nil {
		t.Fatalf("unexpected engine error after reveal: %v", chat.engineErr)
	}
	if chat.state == chatStateSelectAgent {
		t.Fatalf("ChatPage stuck on empty 'Select an agent' picker after splash dismiss "+
			"(agents=%d) — Init ran before the engine was wired", len(chat.agents))
	}
	if chat.state != chatStateChat {
		t.Fatalf("expected chatStateChat after revealing a single-agent config, got state=%v", chat.state)
	}
}

// TestChatPage_VoiceEnded_ReflectedInStatus verifies the UI reflects a voice
// session ending (idle timeout, mic close, or pipeline error) instead of
// looking hung with a dead meter.
func TestChatPage_VoiceEnded_ReflectedInStatus(t *testing.T) {
	p := NewChatPage(&AppContext{Version: "vTEST"})
	p.SetSize(80, 24)

	// Clean end (idle timeout / mic close): nil err.
	_, _ = p.Update(voiceEndedMsg{})
	if !strings.Contains(p.statusLine, "ended") {
		t.Fatalf("expected status to reflect the ended voice session, got %q", p.statusLine)
	}

	// Error end: the driver error surfaces in the status.
	_, _ = p.Update(voiceEndedMsg{err: fmt.Errorf("pipeline boom")})
	if !strings.Contains(p.statusLine, "boom") {
		t.Fatalf("expected status to include the driver error, got %q", p.statusLine)
	}
}
