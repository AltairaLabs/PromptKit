package tui

// integration_test.go exercises the message flow that drives the live TUI
// during a run: RunStarted → MessageCreated turns → AudioLevelMsg → RunCompleted.
// Before these tests existed the TUI had several silent-failure regressions
// (conversation page stuck on "Waiting", audio meter never animating, no
// auto-refresh on completion) that only manifested when a human ran the
// binary. Driving Model.Update synthetically covers them in seconds.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// stateStoreStub serves a fixed RunResult on demand so we can simulate
// the post-completion state-store load without booting the real arena
// state machine.
type stateStoreStub struct {
	result *statestore.RunResult
}

func (s *stateStoreStub) GetResult(_ context.Context, _ string) (*statestore.RunResult, error) {
	return s.result, nil
}

// stubMonitor records SetActiveRun calls so tests can assert that the
// run-selection wiring fires when the user toggles into a conversation.
type stubMonitor struct {
	activeRunID string
	calls       []string
}

func (s *stubMonitor) SetActiveRun(runID string) bool {
	s.calls = append(s.calls, runID)
	s.activeRunID = runID
	return true
}

func (s *stubMonitor) ActiveRunID() string { return s.activeRunID }

// newRunningModel sets up a Model in a state representative of mid-run:
// one active run, ready for live message events. A stub state store is
// attached because the conversation page short-circuits with a "no state
// store attached" placeholder otherwise — matching production where the
// engine always wires one in.
func newRunningModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel("test.yaml", 1)
	m.width = 160
	m.height = 50
	m.isTUIMode = true
	m.SetStateStore(&stateStoreStub{result: &statestore.RunResult{RunID: "run-1"}})
	m.activeRuns = []RunInfo{{
		RunID:     "run-1",
		Scenario:  "duplex-scripted-text",
		Provider:  "mock-duplex",
		Status:    StatusRunning,
		StartTime: time.Now(),
	}}
	return m
}

func TestLiveMessages_PopulateConversationPanelDuringRun(t *testing.T) {
	m := newRunningModel(t)
	m.currentPage = pageConversation
	m.activeRuns[0].Selected = true
	m.initializeConversationData(&m.activeRuns[0])

	// Before any messages: panel shows the waiting state.
	view := m.View()
	if !strings.Contains(view, "Waiting") {
		t.Fatal("expected 'Waiting' before messages arrive")
	}

	// Bus delivers user turn 1.
	m.Update(MessageCreatedMsg{
		ConversationID: "run-1",
		Role:           "user",
		Content:        "Hello, can you hear me?",
		Index:          0,
		Time:           time.Now(),
	})
	// Bus delivers assistant turn 1.
	m.Update(MessageCreatedMsg{
		ConversationID: "run-1",
		Role:           "assistant",
		Content:        "Hello! Yes, I can hear you. I'm Nova, ready to chat.",
		Index:          1,
		Time:           time.Now(),
	})

	view = m.View()
	if strings.Contains(view, "Waiting") {
		t.Error("expected the waiting state to clear after messages arrived")
	}
	// The conversation table truncates content; a substring check is enough
	// to confirm both turns reached the rendered view.
	if !strings.Contains(view, "Hello, can you hear") {
		t.Errorf("expected user turn 1 snippet in panel, view: %s", trimmedSnippet(view))
	}
	if !strings.Contains(view, "Nova") {
		t.Errorf("expected assistant turn 1 in panel, view: %s", trimmedSnippet(view))
	}
}

func TestLiveMessages_CachedRegardlessOfCurrentPage(t *testing.T) {
	m := newRunningModel(t)
	// Stay on the runs page — message arrives before the user navigates.

	m.Update(MessageCreatedMsg{
		ConversationID: "run-1",
		Role:           "user",
		Content:        "first turn",
		Index:          0,
		Time:           time.Now(),
	})

	// Now navigate.
	m.currentPage = pageConversation
	m.activeRuns[0].Selected = true
	m.initializeConversationData(&m.activeRuns[0])

	view := m.View()
	if !strings.Contains(view, "first turn") {
		t.Errorf("expected cached message to appear after navigation, view: %s", trimmedSnippet(view))
	}
}

func TestAudioMeter_RendersAfterRMSFrameLands(t *testing.T) {
	m := newRunningModel(t)
	m.currentPage = pageConversation
	m.activeRuns[0].Selected = true
	m.initializeConversationData(&m.activeRuns[0])

	// Before any RMS frames the meter is suppressed (audioActive == false).
	view := m.View()
	if strings.Contains(view, "user  [") {
		t.Fatal("meter should be hidden before any RMS frame")
	}

	m.Update(AudioLevelMsg{UserLevel: 0.5, AgentLevel: 0.0})

	view = m.View()
	if !strings.Contains(view, "user") {
		t.Errorf("expected 'user' meter label after RMS frame, view: %s", trimmedSnippet(view))
	}
	if !strings.Contains(view, "[████████░░░░░░░░]") {
		t.Errorf("expected half-filled bar at level 0.5, view: %s", trimmedSnippet(view))
	}
	if !strings.Contains(view, "50%") {
		t.Errorf("expected 50%% percent label, view: %s", trimmedSnippet(view))
	}
}

func TestRunCompleted_RefreshesConversationPanelFromStateStore(t *testing.T) {
	m := newRunningModel(t)
	m.SetStateStore(&stateStoreStub{
		result: &statestore.RunResult{
			RunID:      "run-1",
			ScenarioID: "duplex-scripted-text",
			ProviderID: "mock-duplex",
			Messages: []types.Message{
				{Role: "user", Content: "Hello, can you hear me?"},
				{Role: "assistant", Content: "Hello! Yes, I can hear you."},
				{Role: "user", Content: "What's your name?"},
				{Role: "assistant", Content: "My name is Nova."},
			},
		},
	})
	m.currentPage = pageConversation
	m.activeRuns[0].Selected = true
	m.initializeConversationData(&m.activeRuns[0])

	// While running we have nothing cached yet — view should still show
	// the waiting state.
	view := m.View()
	if !strings.Contains(view, "Waiting") {
		t.Fatal("expected waiting state during the run")
	}

	// Run completes; the model should pull the final messages from the
	// state store without the user re-navigating.
	m.Update(RunCompletedMsg{
		RunID:    "run-1",
		Duration: time.Second,
		Cost:     0.001,
		Time:     time.Now(),
	})

	view = m.View()
	if strings.Contains(view, "Waiting") {
		t.Error("waiting state should be gone after RunCompleted reload")
	}
	if !strings.Contains(view, "What's your name?") {
		t.Errorf("expected post-completion messages from state store, view: %s", trimmedSnippet(view))
	}
}

func TestSelectingRun_SwitchesAudioMonitor(t *testing.T) {
	m := newRunningModel(t)
	m.activeRuns = append(m.activeRuns, RunInfo{
		RunID:     "run-2",
		Scenario:  "duplex-scripted-text",
		Provider:  "mock-duplex",
		Status:    StatusRunning,
		StartTime: time.Now(),
	})
	monitor := &stubMonitor{}
	m.SetAudioMonitor(monitor)

	// One render populates the runs table from m.activeRuns. Without this
	// the table has zero rows and toggleSelection's bounds check no-ops.
	_ = m.View()

	table := m.mainPage.RunsPanel().Table()

	// Select run-2.
	table.SetCursor(1)
	m.toggleSelection()
	if monitor.activeRunID != "run-2" {
		t.Errorf("expected audio monitor switched to run-2, got %q (calls=%v)",
			monitor.activeRunID, monitor.calls)
	}

	// Toggling run-2 again deselects it (back to run-list view) — no
	// SetActiveRun fires for that direction. Re-select run-1 to confirm
	// the switch goes the other way too.
	m.toggleSelection() // deselect run-2
	table.SetCursor(0)
	m.toggleSelection() // select run-1
	if monitor.activeRunID != "run-1" {
		t.Errorf("expected audio monitor switched to run-1, got %q (calls=%v)",
			monitor.activeRunID, monitor.calls)
	}
}

// trimmedSnippet abbreviates a rendered View output for readable test
// failure messages — full views are 50+ lines of lipgloss output that
// dominate the failure log.
func trimmedSnippet(view string) string {
	const max = 1500
	collapsed := strings.Join(strings.Fields(view), " ")
	if len(collapsed) > max {
		return collapsed[:max] + "..."
	}
	return collapsed
}
