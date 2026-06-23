package app

import (
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// goldenHomeSizes is the size matrix for home golden snapshots.
var goldenHomeSizes = []struct {
	name string
	w, h int
}{
	{"80x24", 80, 24},
	{"120x40", 120, 40},
}

// fakeDestPage is a minimal Page used as the destination for menu items in tests.
type fakeDestPage struct{ name string }

func (f *fakeDestPage) Init() tea.Cmd                  { return nil }
func (f *fakeDestPage) Update(tea.Msg) (Page, tea.Cmd) { return f, nil }
func (f *fakeDestPage) View() string                   { return f.name }
func (f *fakeDestPage) Title() string                  { return f.name }
func (f *fakeDestPage) SetSize(_, _ int)               {}

// testMenuItems builds a small set of fake menu items for unit tests.
// "enabled" is always selectable; "config-req" requires config.
func testMenuItems() []menuItem {
	return []menuItem{
		{
			label:       "enabled",
			needsConfig: false,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "enabled-dest"} },
		},
		{
			label:       "config-req",
			needsConfig: true,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "config-req-dest"} },
		},
	}
}

// TestHome_EnterEnabledPushes verifies that pressing Enter on an enabled item
// returns a cmd that resolves to PushPageMsg carrying the item's page.
func TestHome_EnterEnabledPushes(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	h := NewHome(ctx, testMenuItems())
	h.SetSize(80, 24)

	// cursor starts at 0 — "enabled" item.
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update(Enter) on enabled item returned nil cmd, expected PushPageMsg cmd")
	}
	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T %v", msg, msg)
	}
	if push.Page == nil {
		t.Fatal("PushPageMsg.Page is nil")
	}
	fp, ok := push.Page.(*fakeDestPage)
	if !ok {
		t.Fatalf("expected *fakeDestPage, got %T", push.Page)
	}
	if fp.name != "enabled-dest" {
		t.Fatalf("expected page name %q, got %q", "enabled-dest", fp.name)
	}
}

// TestHome_DisabledWhenNoConfig verifies that when ctx.Config is nil, an item
// with needsConfig==true does NOT emit a PushPageMsg on Enter.
// The item list here has ONLY a config-required item so that cursor starts on it.
func TestHome_DisabledWhenNoConfig(t *testing.T) {
	// No config — HasConfig() returns false.
	ctx := &AppContext{Version: "vTEST"}
	// Single item that requires config — cursor will land on index 0 (disabled).
	items := []menuItem{
		{
			label:       "config-only",
			needsConfig: true,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "config-only-dest"} },
		},
	}
	h := NewHome(ctx, items)
	h.SetSize(80, 24)

	// Cursor is at index 0 which is disabled (needsConfig, no config).
	// Press Enter — must NOT produce a PushPageMsg.
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(PushPageMsg); ok {
			t.Fatal("Enter on disabled item emitted PushPageMsg — expected no push")
		}
	}
}

// TestHome_DisabledItemNotSelectable verifies that a config-required item is
// skipped by cursor movement when config is absent.
func TestHome_DisabledItemSkipped(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := []menuItem{
		{
			label:       "always-on",
			needsConfig: false,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "always-on"} },
		},
		{
			label:       "needs-cfg",
			needsConfig: true,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "needs-cfg"} },
		},
		{
			label:       "also-always-on",
			needsConfig: false,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "also-always-on"} },
		},
	}
	h := NewHome(ctx, items)
	h.SetSize(80, 24)

	// Down should skip the disabled item and land on "also-always-on".
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from Enter on third item")
	}
	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T", msg)
	}
	fp, ok := push.Page.(*fakeDestPage)
	if !ok {
		t.Fatalf("expected *fakeDestPage, got %T", push.Page)
	}
	if fp.name != "also-always-on" {
		t.Fatalf("cursor should have skipped disabled item; got page %q", fp.name)
	}
}

// TestHome_EnabledWithConfig verifies that when ctx.Config is set, a
// needsConfig item IS selectable.
func TestHome_EnabledWithConfig(t *testing.T) {
	ctx := &AppContext{
		Config:     &config.Config{},
		ConfigPath: "/tmp/arena/my-arena.yaml",
		Version:    "vTEST",
	}
	h := NewHome(ctx, testMenuItems())
	h.SetSize(80, 24)

	// Move to "config-req" (index 1).
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyDown})

	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from Enter on config-req item when config is set")
	}
	msg := cmd()
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T", msg)
	}
	if push.Page == nil {
		t.Fatal("PushPageMsg.Page is nil")
	}
}

// TestHome_ViewContainsLabels verifies that enabled item labels appear in View.
func TestHome_ViewContainsLabels(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	h := NewHome(ctx, testMenuItems())
	h.SetSize(80, 24)
	view := stripANSI(h.View())

	for _, label := range []string{"enabled", "config-req"} {
		if !strings.Contains(view, label) {
			t.Errorf("View does not contain menu item label %q", label)
		}
	}
}

// TestHome_ViewConfigIndicator verifies that the config indicator line is
// rendered appropriately for both states.
func TestHome_ViewConfigIndicator(t *testing.T) {
	t.Run("with config", func(t *testing.T) {
		ctx := &AppContext{
			Config:     &config.Config{},
			ConfigPath: "/projects/my-arena/arena.yaml",
			Version:    "vTEST",
		}
		h := NewHome(ctx, testMenuItems())
		h.SetSize(80, 24)
		view := stripANSI(h.View())
		if !strings.Contains(view, "config:") {
			t.Errorf("View with config should contain 'config:', got:\n%s", view)
		}
	})

	t.Run("no config", func(t *testing.T) {
		ctx := &AppContext{Version: "vTEST"}
		h := NewHome(ctx, testMenuItems())
		h.SetSize(80, 24)
		view := stripANSI(h.View())
		if !strings.Contains(view, "no config") {
			t.Errorf("View without config should contain 'no config', got:\n%s", view)
		}
	})
}

// TestHome_Title verifies Title() returns a non-empty string.
func TestHome_Title(t *testing.T) {
	h := NewHome(&AppContext{Version: "vTEST"}, testMenuItems())
	if got := h.Title(); got == "" {
		t.Fatal("Title() returned empty string, expected a non-empty title")
	}
}

// TestHome_Init verifies that Init returns nil (no background command).
func TestHome_Init(t *testing.T) {
	h := NewHome(&AppContext{Version: "vTEST"}, testMenuItems())
	if cmd := h.Init(); cmd != nil {
		t.Fatal("Init() should return nil cmd, got non-nil")
	}
}

// TestHome_UpKeyMovesCursorBack verifies that pressing Up (and k) moves the
// cursor to the previous enabled item, exercising prevEnabled.
func TestHome_UpKeyMovesCursorBack(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := []menuItem{
		{
			label:       "first",
			needsConfig: false,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "first"} },
		},
		{
			label:       "second",
			needsConfig: false,
			make:        func(_ *AppContext) Page { return &fakeDestPage{name: "second"} },
		},
	}
	h := NewHome(ctx, items)
	h.SetSize(80, 24)

	// Move down to "second".
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyDown})

	// Press Up — should go back to "first".
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyUp})
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd after Up then Enter")
	}
	push, ok := cmd().(PushPageMsg)
	if !ok {
		t.Fatal("expected PushPageMsg")
	}
	fp := push.Page.(*fakeDestPage)
	if fp.name != "first" {
		t.Fatalf("expected cursor back on 'first', got %q", fp.name)
	}
}

// TestHome_VimKeysNavigate verifies that j and k move the cursor (vim-style).
func TestHome_VimKeysNavigate(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := []menuItem{
		{
			label: "a",
			make:  func(_ *AppContext) Page { return &fakeDestPage{name: "a"} },
		},
		{
			label: "b",
			make:  func(_ *AppContext) Page { return &fakeDestPage{name: "b"} },
		},
	}
	h := NewHome(ctx, items)
	h.SetSize(80, 24)

	// j moves down.
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_, cmd := h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd after j then Enter")
	}
	push, ok := cmd().(PushPageMsg)
	if !ok {
		t.Fatal("expected PushPageMsg after j")
	}
	if push.Page.(*fakeDestPage).name != "b" {
		t.Fatalf("j should move to 'b', got %q", push.Page.(*fakeDestPage).name)
	}

	// k moves back up.
	_, _ = h.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	_, cmd = h.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd after k then Enter")
	}
	push, ok = cmd().(PushPageMsg)
	if !ok {
		t.Fatal("expected PushPageMsg after k")
	}
	if push.Page.(*fakeDestPage).name != "a" {
		t.Fatalf("k should move back to 'a', got %q", push.Page.(*fakeDestPage).name)
	}
}

// TestHome_ConfigNameEdgeCases verifies configName with an empty path.
func TestHome_ConfigNameEdgeCases(t *testing.T) {
	name := configName("")
	if name == "" {
		t.Fatal("configName('') should return a non-empty fallback")
	}
}

// TestGoldenHome_WithConfig captures a stable snapshot of the home page when a
// config is loaded.
func TestGoldenHome_WithConfig(t *testing.T) {
	ctx := &AppContext{
		Config:     &config.Config{},
		ConfigPath: "/projects/my-arena/arena.yaml",
		Version:    "vTEST",
	}
	items := testMenuItems()
	for _, sz := range goldenHomeSizes {
		t.Run(sz.name, func(t *testing.T) {
			h := NewHome(ctx, items)
			h.SetSize(sz.w, sz.h)
			out := stripANSI(h.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}

// TestGoldenHome_NoConfig captures a stable snapshot of the home page when no
// config is loaded.
func TestGoldenHome_NoConfig(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := testMenuItems()
	for _, sz := range goldenHomeSizes {
		t.Run(sz.name, func(t *testing.T) {
			h := NewHome(ctx, items)
			h.SetSize(sz.w, sz.h)
			out := stripANSI(h.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
