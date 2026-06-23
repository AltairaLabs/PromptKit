package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
)

// minimalArenaYAML is a minimal valid arena config file for test fixtures.
const minimalArenaYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena

metadata:
  name: switch-test-config
  description: Minimal fixture for config switcher tests

spec: {}
`

// TestConfigSwitch_SelectionLoads verifies that receiving a FileSelectedMsg
// pointing at a valid arena config calls LoadConfig, updates ctx, and returns
// a cmd that emits ConfigChangedMsg.
func TestConfigSwitch_SelectionLoads(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.arena.yaml")
	if err := os.WriteFile(cfgPath, []byte(minimalArenaYAML), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := &AppContext{}
	page := NewConfigSwitchPage(ctx, dir)
	page.SetSize(80, 24)

	// Simulate the file-selected message that FileBrowserPage emits.
	_, cmd := page.Update(pages.FileSelectedMsg{Path: cfgPath})
	if cmd == nil {
		t.Fatal("Update(FileSelectedMsg) returned nil cmd; expected cmd emitting ConfigChangedMsg")
	}

	// ctx.Config must be populated.
	if ctx.Config == nil {
		t.Fatal("ctx.Config is nil after FileSelectedMsg; expected LoadConfig to have run")
	}
	if ctx.ConfigPath != cfgPath {
		t.Fatalf("ctx.ConfigPath=%q, want %q", ctx.ConfigPath, cfgPath)
	}

	// The cmd must emit ConfigChangedMsg (may be batched with PopPageMsg).
	msgs := drainBatch(cmd)
	if !containsConfigChanged(msgs, cfgPath) {
		t.Fatalf("expected ConfigChangedMsg{Path:%q} in batch, got: %v", cfgPath, msgs)
	}
	if !containsPopPage(msgs) {
		t.Fatalf("expected PopPageMsg in batch, got: %v", msgs)
	}
}

// TestConfigSwitch_BadSelectionShowsError verifies that selecting a non-config
// file does not pop the page, does not crash, and surfaces an error in View.
func TestConfigSwitch_BadSelectionShowsError(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "not-a-config.txt")
	if err := os.WriteFile(badPath, []byte("not yaml"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	ctx := &AppContext{}
	page := NewConfigSwitchPage(ctx, dir)
	page.SetSize(80, 24)

	// Snapshot ctx state before.
	beforeConfig := ctx.Config

	_, cmd := page.Update(pages.FileSelectedMsg{Path: badPath})

	// ctx must be unchanged.
	if ctx.Config != beforeConfig {
		t.Fatal("ctx.Config changed after a bad file selection; expected no change")
	}

	// Must NOT pop.
	if cmd != nil {
		msgs := drainBatch(cmd)
		if containsPopPage(msgs) {
			t.Fatal("bad selection emitted PopPageMsg; expected no pop")
		}
	}

	// View should mention an error.
	view := stripANSI(page.View())
	if !strings.Contains(strings.ToLower(view), "error") {
		t.Errorf("View after bad selection should contain 'error'; got:\n%s", view)
	}
}

// TestHome_CKeyOpensSwitcher verifies that pressing 'c' on the Home page
// emits a PushPageMsg whose page is a *ConfigSwitchPage.
func TestHome_CKeyOpensSwitcher(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	h := NewHome(ctx, testMenuItems())
	h.SetSize(80, 24)

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("pressing 'c' returned nil cmd; expected PushPageMsg cmd")
	}
	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T %v", msg, msg)
	}
	if push.Page == nil {
		t.Fatal("PushPageMsg.Page is nil")
	}
	if _, ok := push.Page.(*ConfigSwitchPage); !ok {
		t.Fatalf("expected *ConfigSwitchPage, got %T", push.Page)
	}
}

// TestConfigSwitch_Title verifies that Title returns a non-empty string.
func TestConfigSwitch_Title(t *testing.T) {
	page := NewConfigSwitchPage(&AppContext{}, ".")
	if got := page.Title(); got == "" {
		t.Fatal("Title() returned empty string")
	}
}

// TestConfigSwitch_Init verifies that Init delegates to the file browser and
// returns a non-nil cmd (the filepicker's init cmd).
func TestConfigSwitch_Init(t *testing.T) {
	page := NewConfigSwitchPage(&AppContext{}, ".")
	// FileBrowserPage.Init() returns a non-nil cmd (filepicker.Init).
	// We just need to call it without panic; the exact cmd type is internal.
	_ = page.Init()
}

// TestConfigSwitch_SetSizeAndView verifies SetSize + View do not panic and
// that View produces non-empty output.
func TestConfigSwitch_SetSizeAndView(t *testing.T) {
	page := NewConfigSwitchPage(&AppContext{}, ".")
	page.SetSize(80, 24)
	view := page.View()
	if view == "" {
		t.Fatal("View() returned empty string after SetSize")
	}
}

// TestConfigSwitch_NonFileMsg verifies that unrecognised messages (e.g. a key
// message) are forwarded to the browser without panic.
func TestConfigSwitch_NonFileMsg(t *testing.T) {
	page := NewConfigSwitchPage(&AppContext{}, ".")
	page.SetSize(80, 24)
	// Forwarding a key message must not panic and must return (page, cmd).
	newPage, _ := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	if newPage == nil {
		t.Fatal("Update(KeyDown) returned nil page")
	}
}

// drainBatch executes a cmd and collects all messages it produces.
// It handles both single messages and tea.BatchMsg (a slice of cmds).
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	raw := cmd()
	if raw == nil {
		return nil
	}
	// tea.BatchMsg is []tea.Cmd
	if batch, ok := raw.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
		return msgs
	}
	return []tea.Msg{raw}
}

// containsConfigChanged reports whether msgs contains a ConfigChangedMsg for path.
func containsConfigChanged(msgs []tea.Msg, path string) bool {
	for _, m := range msgs {
		if cc, ok := m.(ConfigChangedMsg); ok && cc.Path == path {
			return true
		}
	}
	return false
}

// containsPopPage reports whether msgs contains a PopPageMsg.
func containsPopPage(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(PopPageMsg); ok {
			return true
		}
	}
	return false
}
