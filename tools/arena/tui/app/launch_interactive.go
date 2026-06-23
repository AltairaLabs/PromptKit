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

	// Push the splash on top so it is visible at startup and its Init fires.
	splash := NewSplash(ctx)
	app.stack = append(app.stack, splash)

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	app.SetSend(p.Send)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
