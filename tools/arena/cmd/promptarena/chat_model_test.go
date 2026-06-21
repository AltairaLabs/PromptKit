package main

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
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

// TestChatModel_EvalMsgSetsStatusLine verifies that a chatEvalMsg with scored
// results populates m.statusLine with the formatted scores.
func TestChatModel_EvalMsgSetsStatusLine(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24

	sess, err := eng.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: eng.ProviderIDs()[0],
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
		RunEvals:   true,
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	m.session = sess
	m.state = stateChat
	m.initPanel()

	score := 1.0
	evalMsg := chatEvalMsg{results: []evals.EvalResult{{Type: "json_valid", Score: &score}}}
	m2, _ := m.Update(evalMsg)
	cm := m2.(*chatModel)

	if !strings.Contains(cm.statusLine, "json_valid") {
		t.Fatalf("expected statusLine to contain 'json_valid', got: %q", cm.statusLine)
	}
	if !strings.Contains(cm.statusLine, "1.00") {
		t.Fatalf("expected statusLine to contain score '1.00', got: %q", cm.statusLine)
	}
}

// TestChatModel_HandleStreamDone_ReturnsCmdWhenRunEvalsTrue verifies that
// handleStreamDone returns a non-nil tea.Cmd when runEvals is true,
// and nil when runEvals is false.
func TestChatModel_HandleStreamDone_ReturnsCmdWhenRunEvalsTrue(t *testing.T) {
	eng := fixtureEngine(t)

	// runEvals = true → expect a non-nil cmd
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	sess, err := eng.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: eng.ProviderIDs()[0],
		TaskType:   "basic",
		Variables:  map[string]string{"company": "Acme"},
		RunEvals:   true,
	})
	if err != nil {
		t.Fatalf("NewInteractiveSession: %v", err)
	}
	m.session = sess
	m.state = stateChat
	m.runEvals = true

	cmd := m.handleStreamDone()
	if cmd == nil {
		t.Fatal("expected non-nil cmd when runEvals=true")
	}

	// runEvals = false → expect nil cmd
	m2 := newChatModel(eng)
	m2.session = sess
	m2.state = stateChat
	m2.runEvals = false
	cmd2 := m2.handleStreamDone()
	if cmd2 != nil {
		t.Fatal("expected nil cmd when runEvals=false")
	}
}

// TestChatModel_NoDuplication verifies that after two turns the panel content
// matches the state store exactly — SetData replaces, never accumulates event
// appends, so each message appears at most twice (table row + detail pane).
func TestChatModel_NoDuplication(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 120, 40

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

	// Turn 1: send a message and let handleStreamDone refresh the panel.
	ch, err := sess.SendUserMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("SendUserMessage turn 1: %v", err)
	}
	for range ch {
	}
	m.handleStreamDone()

	// Turn 2: send another message and refresh again.
	ch2, err := sess.SendUserMessage(context.Background(), "world")
	if err != nil {
		t.Fatalf("SendUserMessage turn 2: %v", err)
	}
	for range ch2 {
	}
	m.handleStreamDone()

	// Verify that the state store has messages.
	msgs, err := sess.Messages(context.Background())
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected messages in state store after two turns")
	}

	// After two handleStreamDone calls, the panel's internal RunResult must have
	// the same number of messages as the state store — SetData replaces rather
	// than appends, so repeated calls never accumulate duplicates.
	res := &statestore.RunResult{RunID: sess.ConversationID(), Messages: msgs}
	m.panel.SetData(sess.ConversationID(), "", "mock", res)
	view := stripANSI(m.View())

	// "hello" is the first user message.  It may appear in the table row and
	// the detail pane, but never more than that.
	helloCount := strings.Count(view, "hello")
	if helloCount == 0 {
		t.Fatal("expected 'hello' to appear in panel view")
	}
	// More than 2 occurrences means duplication across panel re-renders.
	if helloCount > 2 {
		t.Fatalf("message 'hello' appears %d times — duplication detected (want ≤2)", helloCount)
	}
}

// TestChatModel_ExitOnCtrlC verifies that Ctrl+C causes the model to return tea.Quit.
func TestChatModel_ExitOnCtrlC(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	_ = m.Init()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd from Ctrl+C, got nil")
	}
	// Execute the command and check it returns tea.Quit.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from Ctrl+C cmd, got %T", msg)
	}
}

// TestChatModel_ExitOnEsc verifies that Esc causes the model to return tea.Quit.
func TestChatModel_ExitOnEsc(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	_ = m.Init()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd from Esc, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from Esc cmd, got %T", msg)
	}
}

// TestChatModel_FooterContainsQuit verifies that the chat state View() renders a
// footer that mentions "quit" so users know how to exit.
func TestChatModel_FooterContainsQuit(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 120, 40

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

	out := stripANSI(m.View())
	if !strings.Contains(strings.ToLower(out), "quit") {
		t.Fatalf("expected footer to contain 'quit', got:\n%s", out)
	}
}

// TestChatModel_SetupFooterContainsQuit verifies that setup-state views also render
// a footer with quit instructions.
func TestChatModel_SetupFooterContainsQuit(t *testing.T) {
	eng := fixtureEngine(t)
	m := newChatModel(eng)
	m.width, m.height = 80, 24
	_ = m.Init()
	// After Init with single agent+provider, state is stateEnterVars.
	out := stripANSI(m.View())
	if !strings.Contains(strings.ToLower(out), "quit") {
		t.Fatalf("expected setup state view to contain 'quit', got:\n%s", out)
	}
}
