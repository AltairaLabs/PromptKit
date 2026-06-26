package logging

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
)

// Setup configures the provided logger to intercept logs and send them to the TUI.
// If logFilePath is not empty, logs will also be written to that file.
// If suppressStderr is true, logs won't be sent to stderr (useful for TUI mode).
// Returns the interceptor (to be closed when done) and an error if setup fails.
func Setup(logger *slog.Logger, program *tea.Program, logFilePath string, suppressStderr bool) (*Interceptor, error) {
	// Get the current handler
	handler := logger.Handler()

	// A *tea.Program's Send satisfies func(tea.Msg); guard nil so a missing
	// program degrades to file/stderr-only interception instead of panicking.
	var send func(tea.Msg)
	if program != nil {
		send = program.Send
	}

	// Create interceptor
	interceptor, err := NewInterceptor(handler, send, logFilePath, suppressStderr)
	if err != nil {
		return nil, err
	}

	// Replace the logger's handler with the interceptor
	*logger = *slog.New(interceptor)

	return interceptor, nil
}
