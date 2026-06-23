package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// msgSink is a thread-safe collector for the messages a RunPage delivers via
// its send func during a background run.
type msgSink struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *msgSink) send(msg tea.Msg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
}

func (s *msgSink) snapshot() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tea.Msg, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// newRunFixtureContext loads the run-config fixture, builds a mock-provider
// engine, and returns an AppContext plus a 1-run plan ready to execute.
func newRunFixtureContext(t *testing.T) (*AppContext, *engine.Engine, *engine.RunPlan) {
	t.Helper()
	fixturePath := filepath.Join("testdata", "run-config", "config.arena.yaml")
	ctx := &AppContext{Version: "vTEST"}
	if err := ctx.LoadConfig(fixturePath); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	eng, err := ctx.EnsureEngine()
	if err != nil {
		t.Fatalf("EnsureEngine: %v", err)
	}
	// Replace providers with the generic mock so the run executes offline.
	if err := eng.EnableMockProviderMode(""); err != nil {
		t.Fatalf("EnableMockProviderMode: %v", err)
	}
	plan, err := eng.GenerateRunPlan(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("GenerateRunPlan: %v", err)
	}
	if len(plan.Combinations) != 1 {
		t.Fatalf("expected 1 combination, got %d", len(plan.Combinations))
	}
	return ctx, eng, plan
}

// waitForFinish polls the RunPage until ExecuteRuns has finished or the timeout
// elapses.
func waitForFinish(t *testing.T, p *RunPage) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, finished, _ := p.Results(); finished {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not finish within timeout")
}

// TestRunPage_Title verifies Title returns "Run".
func TestRunPage_Title(t *testing.T) {
	ctx, eng, plan := newRunFixtureContext(t)
	p := NewRunPage(ctx, eng, plan, 1, "config.arena.yaml", 1)
	if got := p.Title(); got != "Run" {
		t.Fatalf("expected Title()=Run, got %q", got)
	}
}

// TestRunPage_Activate_RunsToCompletion drives a real mock-provider run through
// Activate, then replays the captured messages through Update and asserts the
// model reflects a completed run.
func TestRunPage_Activate_RunsToCompletion(t *testing.T) {
	ctx, eng, plan := newRunFixtureContext(t)
	p := NewRunPage(ctx, eng, plan, 1, "config.arena.yaml", 1)
	p.SetSize(120, 40)

	sink := &msgSink{}
	cmd := p.Activate(sink.send)
	if cmd == nil {
		t.Fatal("expected non-nil Init cmd from Activate")
	}

	waitForFinish(t, p)

	// Give the event bus a moment to drain any trailing messages into the sink.
	time.Sleep(50 * time.Millisecond)

	// Replay every captured message through Update, exactly as the App would
	// forward them to the top page.
	for _, msg := range sink.snapshot() {
		p.Update(msg)
	}

	if got := p.Model().CompletedCount(); got != 1 {
		t.Fatalf("expected 1 completed run after replay, got %d (active=%d)",
			got, len(p.Model().ActiveRuns()))
	}

	runs := p.Model().ActiveRuns()
	if len(runs) != 1 {
		t.Fatalf("expected 1 active run row, got %d", len(runs))
	}
}

// TestRunPage_SelectRunPushesConversation verifies that selecting a completed
// run emits a PushPageMsg carrying a *ConversationViewPage.
func TestRunPage_SelectRunPushesConversation(t *testing.T) {
	ctx, eng, plan := newRunFixtureContext(t)
	p := NewRunPage(ctx, eng, plan, 1, "config.arena.yaml", 1)
	p.SetSize(120, 40)

	sink := &msgSink{}
	_ = p.Activate(sink.send)
	waitForFinish(t, p)
	time.Sleep(50 * time.Millisecond)
	for _, msg := range sink.snapshot() {
		p.Update(msg)
	}

	runs := p.Model().ActiveRuns()
	if len(runs) == 0 {
		t.Fatal("expected at least one run after execution")
	}
	runID := runs[0].RunID

	// Confirm the run is loadable from the store (drill-down precondition).
	store, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		t.Fatal("engine state store is not ArenaStateStore")
	}
	if _, err := store.GetResult(context.Background(), runID); err != nil {
		t.Fatalf("run result not in store: %v", err)
	}

	// Render once so the runs table has a populated cursor, then press Enter on
	// the runs pane to select the highlighted run.
	_ = p.View()
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command after selecting a run")
	}

	msg := drainForPush(t, cmd)
	push, ok := msg.(PushPageMsg)
	if !ok {
		t.Fatalf("expected PushPageMsg, got %T", msg)
	}
	if _, ok := push.Page.(*ConversationViewPage); !ok {
		t.Fatalf("expected *ConversationViewPage, got %T", push.Page)
	}
}

// drainForPush runs cmd (possibly a tea.Batch) and returns the first PushPageMsg
// it produces.
func drainForPush(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if m := c(); m != nil {
				if _, isPush := m.(PushPageMsg); isPush {
					return m
				}
			}
		}
		t.Fatal("no PushPageMsg found in batch")
	}
	return msg
}

// TestGoldenRunPage snapshots the RunPage first frame before any runs land,
// across the app size matrix.
func TestGoldenRunPage(t *testing.T) {
	for _, sz := range goldenAppSizes {
		t.Run(sz.name, func(t *testing.T) {
			ctx, eng, plan := newRunFixtureContext(t)
			p := NewRunPage(ctx, eng, plan, 1, "run-test-config", 1)
			p.SetSize(sz.w, sz.h)
			_ = p.View() // warm up lazy panel init
			out := stripANSI(p.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
