package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// goldenAppSizes is the size matrix for app-level golden snapshots.
var goldenAppSizes = []struct {
	name string
	w, h int
}{
	{"80x24", 80, 24},
	{"120x40", 120, 40},
}

// TestGoldenApp_HomeFirstFrame builds the App as Run does (splash on top),
// dismisses the splash by sending a splashDoneMsg, then goldens the revealed
// Home frame at each size in the matrix.
func TestGoldenApp_HomeFirstFrame(t *testing.T) {
	ctx := &AppContext{Version: "vTEST"}
	items := DefaultMenu(ctx)

	for _, sz := range goldenAppSizes {
		t.Run(sz.name, func(t *testing.T) {
			home := NewHome(ctx, items)

			// Seed the stack as Run does: [home, splash] so splash is on top.
			a := New(ctx, home)
			splash := NewSplash(ctx)
			splash.SetSize(sz.w, sz.h)
			a.stack = append(a.stack, splash)

			// Apply window size so home gets correct dimensions when splash pops.
			a.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})

			// Dismiss the splash by delivering splashDoneMsg to the App.
			// The splash produces PopPageMsg from Update; App handles PopPageMsg on next Update.
			_, popCmd := a.Update(splashDoneMsg{})
			if popCmd != nil {
				msg := popCmd()
				a.Update(msg)
			}

			// Home should now be on top.
			out := stripANSI(a.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
