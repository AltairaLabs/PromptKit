package app

import (
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

// fakeProgram is a test double for *tea.Program satisfying the Program seam.
type fakeProgram struct {
	send  func(tea.Msg)
	onRun func()
}

func (f *fakeProgram) Run() (tea.Model, error) {
	if f.onRun != nil {
		f.onRun()
	}
	return nil, nil
}

func (f *fakeProgram) Send(m tea.Msg) {
	if f.send != nil {
		f.send(m)
	}
}

// TestRunWithProgram_InstallsInterceptor_CapturesLogs verifies that logs
// emitted while the program runs are captured as logging.Msg via the
// program's Send (not written to stderr), and that the logger is restored
// after Run returns.
func TestRunWithProgram_InstallsInterceptor_CapturesLogs(t *testing.T) {
	var mu sync.Mutex
	var sent []tea.Msg

	fake := &fakeProgram{}
	fake.send = func(m tea.Msg) {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
	}
	// Emit a runtime log while the "program" is running; the interceptor
	// (installed before Run) should capture it.
	fake.onRun = func() { logger.Warn("during-run-log") }

	ctx := &AppContext{Version: "vTEST"}
	home := NewHome(ctx, DefaultMenu(ctx))

	err := RunWithProgram(ctx, home, func(tea.Model) Program { return fake })
	require.NoError(t, err)

	// Logger restored after Run; emitting again must not panic.
	require.NotPanics(t, func() { logger.Info("after-run") })

	// At least one logging.Msg was delivered through Send during the run.
	mu.Lock()
	defer mu.Unlock()
	logs := 0
	for _, m := range sent {
		if _, ok := m.(logging.Msg); ok {
			logs++
		}
	}
	require.GreaterOrEqual(t, logs, 1, "expected at least one captured log msg")
}
