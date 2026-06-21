package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// goldenSizes is the terminal matrix every page is snapshotted across.
// The last entry is below the minimum supported size to capture degradation.
var goldenSizes = []struct {
	name string
	w, h int
}{
	{"80x24", 80, 24},
	{"100x30", 100, 30},
	{"120x40", 120, 40},
	{"160x50", 160, 50},
	{"60x20", 60, 20}, // sub-minimum: degradation
}

// newGoldenModel builds a Model in a deterministic state for snapshotting.
//
// Two non-deterministic inputs must be neutralized so the goldens are
// byte-stable:
//
//   - isTUIMode is normally set by CheckTerminalSize() reading the real tty.
//     Under `go test` there is no tty, so View() would short-circuit to "".
//     We force it true (private field, same package) exactly as the existing
//     tui_test.go / integration_test.go suites do.
//   - startTime drives the elapsed-time clock in the header. View() computes
//     time.Since(startTime).Truncate(time.Second); pinning startTime to "now"
//     keeps the truncated elapsed at 0s (rendered as "0ms") for the whole
//     sub-second interaction, so the header is stable.
func newGoldenModel() *Model {
	m := NewModel("", 0)
	m.isTUIMode = true
	m.startTime = time.Now()
	return m
}

// readFinal drains the program's final framebuffer into a string.
func readFinal(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	b := tm.FinalOutput(t)
	data := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, rerr := b.Read(buf)
		data = append(data, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	return string(data)
}

// goldenFixedTime is a constant timestamp used for every seeded run and
// message so any time-derived rendering stays byte-stable.
var goldenFixedTime = time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

func TestGoldenMainPage(t *testing.T) {
	for _, sz := range goldenSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newGoldenModel()
			tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(sz.w, sz.h))
			// Drive the render size, then quit so the final frame is captured.
			tm.Send(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
			teatest.RequireEqualOutput(t, []byte(readFinal(t, tm)))
		})
	}
}

// goldenConversationResult is the fixed RunResult the conversation page
// renders from. Using a completed run with an explicit Duration avoids the
// runs panel's live time.Since(StartTime) clock, which would be non-stable.
func goldenConversationResult() *statestore.RunResult {
	return &statestore.RunResult{
		RunID:      "run-1",
		ScenarioID: "demo-scenario",
		ProviderID: "mock",
		Region:     "us",
		Duration:   2 * time.Second,
		Messages: []types.Message{
			{Role: "user", Content: "Hello, can you help me?"},
			{Role: "assistant", Content: "Of course! What do you need?"},
		},
	}
}

// TestGoldenConversationPage snapshots the conversation page. The page needs a
// selected run and an attached state store, so the model is seeded into the
// conversation-page state synchronously before handing it to teatest — the same
// pattern the existing tui_test.go / integration_test.go suites use. This is
// preferred over driving Enter through teatest because the runs table is only
// populated on render, making async key navigation order-dependent. A completed
// run with a fixed Duration (not a running run) keeps the output byte-stable,
// since running runs render a live time.Since(StartTime) clock.
func TestGoldenConversationPage(t *testing.T) {
	for _, sz := range goldenSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newGoldenModel()
			m.SetStateStore(&stateStoreStub{result: goldenConversationResult()})
			m.activeRuns = []RunInfo{{
				RunID:     "run-1",
				Scenario:  "demo-scenario",
				Provider:  "mock",
				Region:    "us",
				Status:    StatusCompleted,
				Duration:  2 * time.Second,
				Selected:  true,
				StartTime: goldenFixedTime,
			}}
			m.currentPage = pageConversation
			m.initializeConversationData(&m.activeRuns[0])

			tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(sz.w, sz.h))
			tm.Send(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
			teatest.RequireEqualOutput(t, []byte(readFinal(t, tm)))
		})
	}
}

// NOTE: The file browser page is not snapshotted here. It is not reachable
// from the empty/seeded state through the model's key handling — it requires
// a result file on disk to open. Capturing it deterministically belongs to
// the later migration phase that touches that page; deferred per the plan.
