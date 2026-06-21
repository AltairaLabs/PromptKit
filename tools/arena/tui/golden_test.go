package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
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
