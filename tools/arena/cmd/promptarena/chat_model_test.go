package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/panels"
)

// ansiEscape strips ANSI escape sequences from a string so that plain-text
// assertions work even when lipgloss/glamour render with colour codes.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func fixtureEngine(t *testing.T) *engine.Engine {
	t.Helper()
	cfg := filepath.Join("..", "..", "engine", "testdata", "interactive", "config.arena.yaml")
	eng, err := engine.NewEngineFromConfigFile(filepath.Clean(cfg))
	if err != nil {
		t.Fatalf("NewEngineFromConfigFile: %v", err)
	}
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// TestChatModel_AutoSelectsSingleAgentAndProvider verifies that Init() with a
// single agent + single provider auto-advances to the variable prompt.
func TestChatModel_AutoSelectsSingleAgentAndProvider(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	_ = m.Init()
	// Single agent "basic" + single provider "mock" → should advance to var prompt.
	out := stripANSI(m.View())
	if !strings.Contains(strings.ToLower(out), "company") {
		t.Fatalf("expected variable prompt for 'company', got:\n%s", out)
	}
}

// TestChatModel_MessageCreatedMsgAppendsToPanel verifies that a
// tui.MessageCreatedMsg for the session's conversation ID reaches the panel.
func TestChatModel_MessageCreatedMsgAppendsToPanel(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	_ = m.Init()

	// Force the model into chat state with a session.
	sess, err := eng.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: eng.ProviderIDs()[0],
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	m.session = sess
	m.state = stateChat
	// Initialize the panel with the session's conversation ID.
	m.initPanel()

	// Simulate receiving a MessageCreatedMsg for this session.
	msg := tui.MessageCreatedMsg{
		ConversationID: sess.ConversationID(),
		Role:           "assistant",
		Content:        "hello from the mock provider",
		Index:          1,
		Time:           time.Now(),
	}
	m2, _ := m.Update(msg)
	_ = m2

	out := stripANSI(m.View())
	if !strings.Contains(out, "hello from the mock provider") {
		t.Fatalf("expected panel to show message content, got:\n%s", out)
	}
}

// TestChatModel_MessageCreatedMsgIgnoresOtherConversations verifies that
// messages for other conversation IDs are ignored.
func TestChatModel_MessageCreatedMsgIgnoresOtherConversations(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	_ = m.Init()

	sess, err := eng.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: eng.ProviderIDs()[0],
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	m.session = sess
	m.state = stateChat
	m.initPanel()

	// Message for a DIFFERENT conversation ID - should be ignored.
	msg := tui.MessageCreatedMsg{
		ConversationID: "other-conversation-id",
		Role:           "assistant",
		Content:        "should not appear",
		Index:          1,
		Time:           time.Now(),
	}
	m.Update(msg)

	out := stripANSI(m.View())
	if strings.Contains(out, "should not appear") {
		t.Fatalf("expected message for other conversation to be ignored, got:\n%s", out)
	}
}

// TestChatModel_WindowSizeSetsPanel verifies window resize is handled.
func TestChatModel_WindowSizeSetsPanel(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	_ = m.Init()

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", m.width, m.height)
	}
}

// TestChatModel_PanelRoundtrip verifies ConversationPanel is correctly initialized.
func TestChatModel_PanelRoundtrip(t *testing.T) {
	p := panels.NewConversationPanel()
	p.SetDimensions(80, 24)
	// Panel without data should not crash on View.
	_ = p.View()
}
