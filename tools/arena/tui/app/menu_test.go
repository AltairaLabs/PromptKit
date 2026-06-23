package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDefaultMenu_ItemCount verifies that DefaultMenu returns the expected
// number of items.
func TestDefaultMenu_ItemCount(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := DefaultMenu(ctx)
	if len(items) != 4 {
		t.Fatalf("expected 4 menu items, got %d", len(items))
	}
}

// TestDefaultMenu_ViewAlwaysEnabled verifies that the View item does not
// require a config (needsConfig == false).
func TestDefaultMenu_ViewAlwaysEnabled(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := DefaultMenu(ctx)
	if items[0].needsConfig {
		t.Fatal("View item should not require config")
	}
}

// TestDefaultMenu_RunChatInspectNeedConfig verifies that Run, Chat, and
// Inspect items all require a loaded config.
func TestDefaultMenu_RunChatInspectNeedConfig(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := DefaultMenu(ctx)
	for _, item := range items[1:] {
		if !item.needsConfig {
			t.Errorf("item %q should require config, needsConfig=false", item.label)
		}
	}
}

// TestDefaultMenu_ViewMakePage verifies that the View item's make function
// returns a non-nil Page.
func TestDefaultMenu_ViewMakePage(t *testing.T) {
	ctx := &AppContext{Version: "vTEST", ResultsDir: "/tmp"}
	items := DefaultMenu(ctx)
	p := items[0].make(ctx)
	if p == nil {
		t.Fatal("View make() returned nil page")
	}
}

// TestDefaultMenu_StubMakePages verifies that Run/Chat/Inspect make functions
// return placeholder pages (non-nil) that have the correct Title.
func TestDefaultMenu_StubMakePages(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := DefaultMenu(ctx)
	wantTitles := []string{"Run", "Chat", "Inspect"}
	for i, title := range wantTitles {
		item := items[i+1]
		p := item.make(ctx)
		if p == nil {
			t.Fatalf("item[%d] make() returned nil", i+1)
		}
		if got := p.Title(); got != title {
			t.Errorf("item[%d].Title() = %q, want %q", i+1, got, title)
		}
	}
}

// ---------------------------------------------------------------------------
// placeholder tests
// ---------------------------------------------------------------------------

// TestPlaceholder_Init verifies that Init returns nil (no background command).
func TestPlaceholder_Init(t *testing.T) {
	p := placeholderPage("Run", "#1455")
	if cmd := p.Init(); cmd != nil {
		t.Fatal("Init() should return nil")
	}
}

// TestPlaceholder_UpdateKeyEmitsPopPageMsg verifies that any key message causes
// the placeholder to emit PopPageMsg so the App returns to Home.
func TestPlaceholder_UpdateKeyEmitsPopPageMsg(t *testing.T) {
	p := placeholderPage("Run", "#1455")
	p.SetSize(80, 24)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Update(KeyMsg) returned nil cmd, expected PopPageMsg cmd")
	}
	msg := cmd()
	if _, ok := msg.(PopPageMsg); !ok {
		t.Fatalf("expected PopPageMsg, got %T %v", msg, msg)
	}
}

// TestPlaceholder_UpdateNonKeyIsNoOp verifies that non-key messages are
// ignored.
func TestPlaceholder_UpdateNonKeyIsNoOp(t *testing.T) {
	p := placeholderPage("Run", "#1455")
	_, cmd := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil {
		t.Fatal("Update(non-key) should return nil cmd")
	}
}

// TestPlaceholder_ViewContainsTitle verifies that View includes the page title
// and the tracking issue.
func TestPlaceholder_ViewContainsTitle(t *testing.T) {
	p := placeholderPage("Chat", "#1455")
	p.SetSize(80, 24)
	view := stripANSI(p.View())

	if !strings.Contains(view, "Chat") {
		t.Errorf("View does not contain title %q", "Chat")
	}
	if !strings.Contains(view, "#1455") {
		t.Errorf("View does not contain issue %q", "#1455")
	}
	if !strings.Contains(view, "press any key to return") {
		t.Errorf("View does not contain return hint")
	}
}

// TestPlaceholder_Title verifies Title() returns the page title.
func TestPlaceholder_Title(t *testing.T) {
	p := placeholderPage("Inspect", "#1455")
	if got := p.Title(); got != "Inspect" {
		t.Fatalf("Title() = %q, want %q", got, "Inspect")
	}
}

// TestPlaceholder_SetSize verifies that SetSize stores the dimensions.
func TestPlaceholder_SetSize(t *testing.T) {
	ph := placeholderPage("Run", "#1455").(*placeholder)
	ph.SetSize(120, 40)
	if ph.w != 120 || ph.h != 40 {
		t.Fatalf("SetSize(120,40): got w=%d h=%d", ph.w, ph.h)
	}
}
