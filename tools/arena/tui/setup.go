package tui

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
)

// SetupLogger configures the provided logger to intercept logs and send them to the TUI.
// If logFilePath is not empty, logs will also be written to that file.
// Returns the interceptor (to be closed when done) and an error if setup fails.
func SetupLogger(logger *slog.Logger, program *tea.Program, logFilePath string) (*LogInterceptor, error) {
	// Get the current handler
	handler := logger.Handler()

	// Create interceptor
	interceptor, err := NewLogInterceptor(handler, program, logFilePath)
	if err != nil {
		return nil, err
	}

	// Replace the logger's handler with the interceptor
	*logger = *slog.New(interceptor)

	return interceptor, nil
}
