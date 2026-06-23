package app

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeActivatable is a Page that also implements Activatable. It records
// whether Activate was called, how many times it was called, and stores the
// send func it received.
type fakeActivatable struct {
	name          string
	activated     bool
	activateCount int
	receivedSend  func(tea.Msg)
}

func (f *fakeActivatable) Init() tea.Cmd                  { return nil }
func (f *fakeActivatable) Update(tea.Msg) (Page, tea.Cmd) { return f, nil }
func (f *fakeActivatable) View() string                   { return f.name }
func (f *fakeActivatable) Title() string                  { return f.name }
func (f *fakeActivatable) SetSize(_, _ int)               {}
func (f *fakeActivatable) Activate(send func(tea.Msg)) tea.Cmd {
	f.activated = true
	f.activateCount++
	f.receivedSend = send
	return nil
}

// TestActivatable_PushedPageReceivesSend verifies that pushing an Activatable
// page calls Activate with the App's send function.
func TestActivatable_PushedPageReceivesSend(t *testing.T) {
	home := &namedFakePage{name: "home"}
	a := New(&AppContext{}, home)

	// Set a sentinel send func.
	var sentCount int64
	sentinel := func(tea.Msg) { atomic.AddInt64(&sentCount, 1) }
	a.SetSend(sentinel)

	child := &fakeActivatable{name: "child"}
	a.Update(PushPageMsg{Page: child})

	if !child.activated {
		t.Fatal("expected Activate to be called on pushed Activatable page")
	}
	if child.receivedSend == nil {
		t.Fatal("expected Activate to receive a non-nil send func")
	}

	// Verify the delivered send is the sentinel (call it, check counter).
	child.receivedSend(tea.KeyMsg{})
	if atomic.LoadInt64(&sentCount) != 1 {
		t.Fatalf("expected sentCount=1, got %d", atomic.LoadInt64(&sentCount))
	}
}

// TestActivatable_NonActivatablePageUnaffected verifies that pushing a plain
// Page (not Activatable) still works without errors.
func TestActivatable_NonActivatablePageUnaffected(t *testing.T) {
	home := &namedFakePage{name: "home"}
	a := New(&AppContext{}, home)
	a.SetSend(func(tea.Msg) {})

	child := &namedFakePage{name: "child"}
	a.Update(PushPageMsg{Page: child})

	if a.atRoot() {
		t.Fatal("expected stack depth > 1 after pushing a child page")
	}
	if a.top().Title() != "child" {
		t.Fatalf("expected top page to be child, got %q", a.top().Title())
	}
}

// TestActivatable_RootActivatedOnInit verifies that if the root page implements
// Activatable, App.Init() activates it.
func TestActivatable_RootActivatedOnInit(t *testing.T) {
	root := &fakeActivatable{name: "root"}
	a := New(&AppContext{}, root)

	var sentCount int64
	sentinel := func(tea.Msg) { atomic.AddInt64(&sentCount, 1) }
	a.SetSend(sentinel)

	a.Init()

	if !root.activated {
		t.Fatal("expected root Activatable to be activated by App.Init()")
	}
	if root.receivedSend == nil {
		t.Fatal("expected root to receive a non-nil send func from Init")
	}

	root.receivedSend(tea.KeyMsg{})
	if atomic.LoadInt64(&sentCount) != 1 {
		t.Fatalf("expected sentCount=1, got %d", atomic.LoadInt64(&sentCount))
	}
}

// TestActivatable_NilSendFallback verifies that Activate is still called with
// a no-op send when a.send is nil (headless/test usage).
func TestActivatable_NilSendFallback(t *testing.T) {
	root := &fakeActivatable{name: "root"}
	a := New(&AppContext{}, root)
	// Do NOT call SetSend — a.send is nil.

	// Init should still call Activate with a no-op (not nil) send.
	a.Init()

	if !root.activated {
		t.Fatal("expected Activate called even when a.send is nil")
	}
	if root.receivedSend == nil {
		t.Fatal("expected no-op send func, got nil")
	}
	// Calling the no-op must not panic.
	root.receivedSend(tea.KeyMsg{})
}

// TestActivatable_PopActivatesRoot verifies the linchpin of the RunPage
// activation flow: when a non-Activatable page (e.g. a splash) sits on top of
// an Activatable root (e.g. RunPage) and is popped, the root's Activate fires
// exactly once with the App's send func.
//
// This mirrors the pattern used at launch:
//
//	stack = [RunPage (Activatable), splash (non-Activatable)]
//	→ PopPageMsg dismisses splash → RunPage.Activate called once
func TestActivatable_PopActivatesRoot(t *testing.T) {
	root := &fakeActivatable{name: "run-page"}
	a := New(&AppContext{}, root)

	var sentCount int64
	sentinel := func(tea.Msg) { atomic.AddInt64(&sentCount, 1) }
	a.SetSend(sentinel)

	// Push a plain (non-Activatable) page on top — mimics a splash screen.
	splash := &namedFakePage{name: "splash"}
	a.Update(PushPageMsg{Page: splash})

	// Root must NOT have been activated yet (only the pushed page's Activate
	// fires on push, and splash is not Activatable).
	if root.activated {
		t.Fatal("root should not be activated before pop")
	}

	// Pop the splash — the root is now top and must be activated.
	a.Update(PopPageMsg{})

	if !root.activated {
		t.Fatal("expected root Activate to fire after pop")
	}
	if root.activateCount != 1 {
		t.Fatalf("expected Activate called exactly once, got %d", root.activateCount)
	}
	if root.receivedSend == nil {
		t.Fatal("expected root to receive a non-nil send func")
	}

	// Verify the delivered send is the sentinel.
	root.receivedSend(tea.KeyMsg{})
	if atomic.LoadInt64(&sentCount) != 1 {
		t.Fatalf("expected sentCount=1, got %d", atomic.LoadInt64(&sentCount))
	}
}

// TestActivatable_PopDoesNotReActivateRoot verifies idempotency: if the root
// was already activated (e.g. via Init), a subsequent pop that reveals it again
// must NOT call Activate a second time.
func TestActivatable_PopDoesNotReActivateRoot(t *testing.T) {
	root := &fakeActivatable{name: "run-page"}
	a := New(&AppContext{}, root)
	a.SetSend(func(tea.Msg) {})

	// Activate the root via Init (simulates direct root activation path).
	a.Init()
	if root.activateCount != 1 {
		t.Fatalf("expected 1 activation after Init, got %d", root.activateCount)
	}

	// Push a non-Activatable overlay then pop it — root is revealed again.
	splash := &namedFakePage{name: "splash"}
	a.Update(PushPageMsg{Page: splash})
	a.Update(PopPageMsg{})

	// Count must still be 1 — idempotency set blocks the second activation.
	if root.activateCount != 1 {
		t.Fatalf("expected activateCount to remain 1 after re-reveal, got %d", root.activateCount)
	}
}
