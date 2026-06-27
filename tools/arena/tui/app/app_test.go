package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// initTrackingPage is a fake Page that records Init calls so tests can assert
// once-only semantics.
type initTrackingPage struct {
	name      string
	initCount int
}

func (f *initTrackingPage) Init() tea.Cmd {
	f.initCount++
	return nil
}
func (f *initTrackingPage) Update(tea.Msg) (Page, tea.Cmd) { return f, nil }
func (f *initTrackingPage) View() string                   { return f.name }
func (f *initTrackingPage) Title() string                  { return f.name }
func (f *initTrackingPage) SetSize(_, _ int)               {}

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

// TestApp_SplashSeedInitOnReveal mirrors the production Run() seeding pattern:
// stack = [root, splash], App.Init() inits only the splash (top page), then
// PopPageMsg dismisses splash and root.Init() must fire exactly once (C1 fix).
func TestApp_SplashSeedInitOnReveal(t *testing.T) {
	root := &initTrackingPage{name: "root"}
	a := New(&AppContext{}, root)

	// Simulate Run(): append splash directly on top of root (not via push).
	splash := &initTrackingPage{name: "splash"}
	a.stack = append(a.stack, splash)

	// App.Init() → initAndActivate(splash) → splash gets inited once.
	a.Init()

	if splash.initCount != 1 {
		t.Fatalf("expected splash initCount=1 after App.Init(), got %d", splash.initCount)
	}
	if root.initCount != 0 {
		t.Fatalf("expected root initCount=0 before splash dismiss, got %d", root.initCount)
	}

	// Splash dismiss → PopPageMsg → root becomes top → root.Init() must fire.
	a.Update(PopPageMsg{})

	if root.initCount != 1 {
		t.Fatalf("expected root initCount=1 after splash dismiss, got %d", root.initCount)
	}

	// Sanity: root is now top (atRoot).
	if !a.atRoot() {
		t.Fatal("expected app to be at root after splash dismiss")
	}
}

// TestApp_SplashSeedNoDoubleInit verifies that pushing then popping a child
// page after the splash has been dismissed does NOT re-Init the root (C1 fix:
// once-only guarantee for already-inited pages like mid-run RunPage).
func TestApp_SplashSeedNoDoubleInit(t *testing.T) {
	root := &initTrackingPage{name: "root"}
	a := New(&AppContext{}, root)

	// Seed as in Run(): splash on top, App.Init() inits splash.
	splash := &initTrackingPage{name: "splash"}
	a.stack = append(a.stack, splash)
	a.Init()

	// Dismiss splash → root inited once.
	a.Update(PopPageMsg{})
	if root.initCount != 1 {
		t.Fatalf("setup: expected root initCount=1, got %d", root.initCount)
	}

	// Push a child page, then pop back to root — root must NOT be re-inited.
	child := &initTrackingPage{name: "child"}
	a.Update(PushPageMsg{Page: child})
	a.Update(PopPageMsg{})

	if root.initCount != 1 {
		t.Fatalf("expected root initCount to remain 1 after push+pop, got %d", root.initCount)
	}
	if child.initCount != 1 {
		t.Fatalf("expected child initCount=1 after push, got %d", child.initCount)
	}
}

// closeableFakePage records whether Close was called, to verify navigation pops
// release page resources (e.g. ChatPage's voice driver).
type closeableFakePage struct {
	namedFakePage
	closed int
}

func (f *closeableFakePage) Close() { f.closed++ }

func TestApp_PopClosesPage(t *testing.T) {
	a := New(&AppContext{}, &namedFakePage{name: "home"})
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	child := &closeableFakePage{namedFakePage: namedFakePage{name: "chat"}}
	a.Update(PushPageMsg{Page: child})

	// Navigate away (esc → pop). The popped page must be Closed so its voice
	// driver / mic is released.
	_, _ = a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !a.atRoot() {
		t.Fatal("expected back at root after pop")
	}
	if child.closed != 1 {
		t.Fatalf("popped page Close() called %d times, want 1", child.closed)
	}
}
