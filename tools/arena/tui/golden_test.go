// Golden snapshot tests for the Arena TUI.
//
// These render whole pages at a fixed terminal-size matrix and compare against
// committed testdata/*.golden files — the safety net for the layout engine
// migration.
//
// To stay byte-stable across environments (local vs CI) the snapshot is a
// single warmed View() of the final model with ANSI escape sequences stripped,
// NOT teatest's raw PTY output stream. See renderGolden for why.
//
// Regenerate after an intentional layout change:
//
//	go -C tools/arena test ./tui/ -update
//
// CI runs them without -update; a diff means the layout changed.
package tui

import (
	"regexp"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// ansiPattern matches ANSI/VT escape sequences (CSI). Glamour renders the
// conversation markdown with 256-color SGR codes whose exact profile is
// environment-dependent (it varies between a local terminal and CI); stripping
// them keeps the goldens validating layout and text while staying byte-stable
// across machines.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

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

// renderGolden drives the model to the given terminal size and returns its
// final, warmed View() — a single deterministic frame.
//
// We deliberately do NOT snapshot teatest's PTY output stream: that stream
// contains every intermediate frame the program emits (e.g. the initial 80x24
// frame before the WindowSizeMsg resize) plus cursor-movement and teardown
// escape sequences, and exactly which frames land in the buffer is
// timing-dependent — it differs between a local run and CI. Capturing a single
// View() of the final model state is environment-independent.
//
// The first View() warms up lazy panel initialization (e.g. the logs viewport's
// "Waiting for logs..." placeholder becomes its real content); the second is the
// stable frame we assert on.
func renderGolden(m *Model, w, h int) string {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	_ = m.View()
	return stripANSI(m.View())
}

// goldenFixedTime is a constant timestamp used for every seeded run and
// message so any time-derived rendering stays byte-stable.
var goldenFixedTime = time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)

func TestGoldenMainPage(t *testing.T) {
	for _, sz := range goldenSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newGoldenModel()
			teatest.RequireEqualOutput(t, []byte(renderGolden(m, sz.w, sz.h)))
		})
	}
}

// TestGoldenMainPageLogsCollapsed locks the resize/collapse layout: with the
// logs pane focused and collapsed via 'z', the result pane should fill the
// bottom row. Seeded with a completed run so the runs table has no live clock.
func TestGoldenMainPageLogsCollapsed(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"100x30", 100, 30},
		{"120x40", 120, 40},
	}
	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newGoldenModel()
			m.activeRuns = []RunInfo{{
				RunID:     "run-1",
				Scenario:  "demo-scenario",
				Provider:  "mock",
				Region:    "us",
				Status:    StatusCompleted,
				Duration:  2 * time.Second,
				StartTime: goldenFixedTime,
			}}
			m.setFocusToLogsPane()

			m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}}) // collapse logs
			_ = m.View()                                                 // warm up lazy panel init
			teatest.RequireEqualOutput(t, []byte(stripANSI(m.View())))
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

			teatest.RequireEqualOutput(t, []byte(renderGolden(m, sz.w, sz.h)))
		})
	}
}

// NOTE: The file browser page (reachable via 'f' on the main page) is
// intentionally NOT covered by golden snapshots: its body is a bubbles
// filepicker that renders live filesystem contents, so its output is not
// deterministic across machines/CI. It is covered instead by unit tests in
// tui_filebrowser_test.go (open/close, file selection, error handling) and
// pages/file_browser_test.go (render + navigation).
