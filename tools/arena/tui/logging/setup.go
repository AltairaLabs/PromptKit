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

	// Create interceptor
	interceptor, err := NewInterceptor(handler, program, logFilePath, suppressStderr)
	if err != nil {
		return nil, err
	}

	// Replace the logger's handler with the interceptor
	*logger = *slog.New(interceptor)

	return interceptor, nil
}
