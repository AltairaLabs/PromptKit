package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// Program is the slice of *tea.Program that Run depends on. It exists as a
// test seam so RunWithProgram can drive a fake program without a real
// terminal. A *tea.Program satisfies it.
type Program interface {
	Run() (tea.Model, error)
	Send(tea.Msg)
}

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
	return RunWithProgram(ctx, root, func(m tea.Model) Program {
		return tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	})
}

// RunWithProgram is the testable core of Run. It seeds the stack, builds the
// program via newProgram, installs the slog interceptor wired to the program's
// Send (so runtime logs are captured into the TUI on every alt-screen path
// instead of corrupting the screen), runs the program, then flushes buffered
// logs to stderr and restores the default logger.
func RunWithProgram(ctx *AppContext, root Page, newProgram func(tea.Model) Program) error {
	app := New(ctx, root)

	// Splash is appended directly (not via push): it owns Init() at startup via App.Init().
	// root's Init() will fire via initAndActivate when splash is popped (first reveal).
	splash := NewSplash(ctx)
	app.stack = append(app.stack, splash)

	p := newProgram(app)
	app.SetSend(p.Send)

	// Install the log interceptor before Run so engine/runtime logs are routed
	// into the TUI (and optionally a file) rather than written to stderr, which
	// would paint over the alt-screen.
	interceptor, err := installLogInterceptor(ctx, p.Send)
	if err != nil {
		return err
	}
	defer func() {
		if interceptor != nil {
			interceptor.FlushBuffer()
			_ = interceptor.Close()
			logger.SetLogger(nil)
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// installLogInterceptor wires a logging.Interceptor to send and sets it as the
// runtime logger. Verbose raises the level to debug and, when LogDir is set,
// tees to <LogDir>/promptarena.log.
func installLogInterceptor(ctx *AppContext, send func(tea.Msg)) (*logging.Interceptor, error) {
	level := slog.LevelInfo
	logFile := ""
	if ctx != nil && ctx.Verbose {
		level = slog.LevelDebug
		if ctx.LogDir != "" {
			logFile = filepath.Join(ctx.LogDir, "promptarena.log")
		}
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	ic, err := logging.NewInterceptor(handler, send, logFile, true)
	if err != nil {
		return nil, fmt.Errorf("install log interceptor: %w", err)
	}
	logger.SetLogger(slog.New(ic))
	return ic, nil
}
