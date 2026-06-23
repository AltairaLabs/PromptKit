package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the PromptArena TUI hub with root as the bottom page on the
// navigation stack. The splash screen is shown first — it is pushed on top of
// root so that dismissing it (via any key or the auto-dismiss timer) reveals
// root. App.Init() calls the top page's Init, so the splash timer fires
// automatically under the bubbletea runtime.
//
// The stack is seeded as [root, splash] before tea.NewProgram is called, which
// means:
//   - a.Init() → splash.Init() (timer starts)
//   - splash dismiss → PopPageMsg → root becomes top
//
// Esc/q at root (the only page remaining after splash dismiss) will quit.
func Run(ctx *AppContext, root Page) error {
	app := New(ctx, root)

	// Splash is appended directly (not via push): it owns Init() at startup via App.Init().
	// If a future top-of-stack startup page needs Activatable wiring, route it through push instead.
	splash := NewSplash(ctx)
	app.stack = append(app.stack, splash)

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	app.SetSend(p.Send)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
