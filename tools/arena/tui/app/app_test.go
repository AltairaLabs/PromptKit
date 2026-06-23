package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// namedFakePage extends fakePage with a name field so we can distinguish pages
// in View output and verify SetSize calls.
type namedFakePage struct {
	name         string
	setSizeCalls int
	lastW, lastH int
}

func (f *namedFakePage) Init() tea.Cmd                  { return nil }
func (f *namedFakePage) Update(tea.Msg) (Page, tea.Cmd) { return f, nil }
func (f *namedFakePage) View() string                   { return f.name }
func (f *namedFakePage) Title() string                  { return f.name }
func (f *namedFakePage) SetSize(w, h int) {
	f.setSizeCalls++
	f.lastW, f.lastH = w, h
}

func TestApp_PushPop(t *testing.T) {
	home := &namedFakePage{name: "home"}
	a := New(&AppContext{}, home)

	// WindowSizeMsg stored and forwarded to top page.
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// push child page.
	a.Update(PushPageMsg{Page: &namedFakePage{name: "child"}})
	if a.atRoot() {
		t.Fatal("expected child on top")
	}
	if got := a.View(); !strings.Contains(got, "child") {
		t.Fatalf("view=%q", got)
	}

	// Esc pops back to home.
	_, _ = a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !a.atRoot() {
		t.Fatal("expected back at root")
	}
}

func TestApp_EscAtRootQuits(t *testing.T) {
	a := New(&AppContext{}, &namedFakePage{name: "view"})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected tea.Quit at root")
	}
	if got := cmd(); got != (tea.QuitMsg{}) {
		t.Fatalf("expected tea.QuitMsg{}, got %T %v", got, got)
	}
}

func TestApp_QKeyQuits(t *testing.T) {
	a := New(&AppContext{}, &namedFakePage{name: "home"})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected tea.Quit from 'q'")
	}
	if got := cmd(); got != (tea.QuitMsg{}) {
		t.Fatalf("expected tea.QuitMsg{}, got %T %v", got, got)
	}
}

func TestApp_CtrlCQuits(t *testing.T) {
	a := New(&AppContext{}, &namedFakePage{name: "home"})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected tea.Quit from Ctrl+C")
	}
	if got := cmd(); got != (tea.QuitMsg{}) {
		t.Fatalf("expected tea.QuitMsg{}, got %T %v", got, got)
	}
}

func TestApp_QuitMsgQuits(t *testing.T) {
	a := New(&AppContext{}, &namedFakePage{name: "home"})
	_, cmd := a.Update(QuitMsg{})
	if cmd == nil {
		t.Fatal("expected tea.Quit from QuitMsg")
	}
	if got := cmd(); got != (tea.QuitMsg{}) {
		t.Fatalf("expected tea.QuitMsg{}, got %T %v", got, got)
	}
}

func TestApp_WindowSizeForwardedToTopPage(t *testing.T) {
	home := &namedFakePage{name: "home"}
	a := New(&AppContext{}, home)

	a.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	if home.lastW != 100 || home.lastH != 40 {
		t.Fatalf("expected SetSize(100,40) on top page, got (%d,%d)", home.lastW, home.lastH)
	}
}

func TestApp_WindowSizeStoredAndAppliedOnPush(t *testing.T) {
	home := &namedFakePage{name: "home"}
	a := New(&AppContext{}, home)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	child := &namedFakePage{name: "child"}
	a.Update(PushPageMsg{Page: child})

	// The pushed page should have received SetSize immediately.
	if child.lastW != 80 || child.lastH != 24 {
		t.Fatalf("expected SetSize(80,24) on pushed page, got (%d,%d)", child.lastW, child.lastH)
	}
}

func TestApp_PopAtRootIsNoOp(t *testing.T) {
	a := New(&AppContext{}, &namedFakePage{name: "home"})
	_, cmd := a.Update(PopPageMsg{})
	// cmd should be nil (no-op) and we should still be at root.
	if !a.atRoot() {
		t.Fatal("expected still at root after pop no-op")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd, got %v", cmd)
	}
}

func TestApp_PopReappliesSize(t *testing.T) {
	root := &namedFakePage{name: "root"}
	a := New(&AppContext{}, root)

	// Store terminal dimensions.
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Push a child page; it gets sized immediately.
	child := &namedFakePage{name: "child"}
	a.Update(PushPageMsg{Page: child})

	// Verify child was sized.
	if child.lastW != 120 || child.lastH != 40 {
		t.Fatalf("expected child SetSize(120,40), got (%d,%d)", child.lastW, child.lastH)
	}

	// Now pop the child; root should be re-sized with the stored dimensions.
	a.Update(PopPageMsg{})

	// Verify root received SetSize with the terminal dimensions.
	if root.lastW != 120 || root.lastH != 40 {
		t.Fatalf("expected root SetSize(120,40) after pop, got (%d,%d)", root.lastW, root.lastH)
	}
}

func TestApp_InitCallsTopPageInit(t *testing.T) {
	// Init() should run and return without panic; we can't easily assert the
	// returned cmd without a real bubbletea runtime, so we just confirm no panic.
	a := New(&AppContext{}, &namedFakePage{name: "home"})
	if cmd := a.Init(); cmd != nil {
		t.Fatalf("expected nil cmd from Init, got %v", cmd)
	}
}

func TestApp_ConfigChangedClearsEngine(t *testing.T) {
	ctx := &AppContext{Engine: &engine.Engine{}}
	a := New(ctx, &namedFakePage{name: "home"})

	_, cmd := a.Update(ConfigChangedMsg{Path: "/some/arena.yaml"})
	if cmd != nil {
		t.Fatalf("expected nil cmd from ConfigChangedMsg, got %v", cmd)
	}
	if ctx.Engine != nil {
		t.Fatal("expected ctx.Engine to be nil after ConfigChangedMsg")
	}
}
