package app

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeActivatable is a Page that also implements Activatable. It records
// whether Activate was called and stores the send func it received.
type fakeActivatable struct {
	name         string
	activated    bool
	receivedSend func(tea.Msg)
}

func (f *fakeActivatable) Init() tea.Cmd                  { return nil }
func (f *fakeActivatable) Update(tea.Msg) (Page, tea.Cmd) { return f, nil }
func (f *fakeActivatable) View() string                   { return f.name }
func (f *fakeActivatable) Title() string                  { return f.name }
func (f *fakeActivatable) SetSize(_, _ int)               {}
func (f *fakeActivatable) Activate(send func(tea.Msg)) tea.Cmd {
	f.activated = true
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

	if !a.atRoot() == false {
		// atRoot should be false (child is on stack)
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
