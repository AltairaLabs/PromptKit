package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the PromptArena TUI hub with root as the bottom page on the
// navigation stack. The splash screen is shown first — it is pushed on top of
// root so that dismissing it (via any key or the auto-dismiss timer) reveals
// root. App.Init() calls the top page's Init (once only), so the splash timer
// fires automatically under the bubbletea runtime.
//
// The stack is seeded as [root, splash] before tea.NewProgram is called, which
// means:
//   - a.Init() → splash.Init() (timer starts; root is NOT yet inited)
//   - splash dismiss → PopPageMsg → root becomes top → root.Init() fires once
//     (via initAndActivate in pop()) together with root.Activate() if applicable
//
// The once-only Init guarantee ensures deep-link pages like ChatPage load their
// agents/setup state when revealed by splash dismiss, not before.
// Esc/q at root (the only page remaining after splash dismiss) will quit.
func Run(ctx *AppContext, root Page) error {
	app := New(ctx, root)

	// Splash is appended directly (not via push): it owns Init() at startup via App.Init().
	// root's Init() will fire via initAndActivate when splash is popped (first reveal).
	splash := NewSplash(ctx)
	app.stack = append(app.stack, splash)

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	app.SetSend(p.Send)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
