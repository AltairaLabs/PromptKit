package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	logFilePermissions = 0600 // Read/write for owner only
)

// LogInterceptor wraps an slog.Handler to intercept log messages and send them to the TUI.
// It also optionally writes logs to a file in verbose mode.
type LogInterceptor struct {
	originalHandler slog.Handler
	program         *tea.Program
	logFile         *os.File
	mu              sync.Mutex
}

// NewLogInterceptor creates a log interceptor that sends logs to both the original handler and the TUI.
// If logFilePath is not empty, logs will also be written to that file.
func NewLogInterceptor(
	originalHandler slog.Handler, program *tea.Program, logFilePath string,
) (*LogInterceptor, error) {
	interceptor := &LogInterceptor{
		originalHandler: originalHandler,
		program:         program,
	}

	// Open log file if path provided
	if logFilePath != "" {
		//nolint:gosec // G304: logFilePath is controlled by the calling application, not user input
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePermissions)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		interceptor.logFile = f
	}

	return interceptor, nil
}

// Close closes the log file if one was opened.
func (l *LogInterceptor) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// Enabled reports whether the handler handles records at the given level.
func (l *LogInterceptor) Enabled(ctx context.Context, level slog.Level) bool {
	return l.originalHandler.Enabled(ctx, level)
}

// Handle processes a log record by sending it to the TUI and optionally writing to file.
//
//nolint:gocritic // hugeParam: slog.Record must be passed by value to satisfy slog.Handler interface
func (l *LogInterceptor) Handle(ctx context.Context, record slog.Record) error {
	// Send to original handler (stderr)
	if err := l.originalHandler.Handle(ctx, record); err != nil {
		return err
	}

	// Convert slog.Level to string
	levelStr := levelToString(record.Level)

	// Send to TUI (only if program is initialized and not nil)
	// Note: tea.Program.Send() panics if called on uninitialized program
	if l.program != nil {
		// Use recover to handle potential panics from bubbletea
		func() {
			defer func() {
				_ = recover() // Ignore panics from Send
			}()
			l.program.Send(LogMsg{
				Timestamp: record.Time,
				Level:     levelStr,
				Message:   record.Message,
			})
		}()
	}

	// Write to file if configured
	if l.logFile != nil {
		l.mu.Lock()
		defer l.mu.Unlock()

		// Format: 2006-01-02T15:04:05.000 [LEVEL] message
		logLine := fmt.Sprintf("%s [%s] %s\n",
			record.Time.Format("2006-01-02T15:04:05.000"),
			levelStr,
			record.Message)

		if _, err := l.logFile.WriteString(logLine); err != nil {
			return fmt.Errorf("failed to write to log file: %w", err)
		}
	}

	return nil
}

// WithAttrs returns a new handler with additional attributes.
func (l *LogInterceptor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogInterceptor{
		originalHandler: l.originalHandler.WithAttrs(attrs),
		program:         l.program,
		logFile:         l.logFile,
	}
}

// WithGroup returns a new handler with an additional group.
func (l *LogInterceptor) WithGroup(name string) slog.Handler {
	return &LogInterceptor{
		originalHandler: l.originalHandler.WithGroup(name),
		program:         l.program,
		logFile:         l.logFile,
	}
}

// LogMsg is a bubbletea message sent when a log entry is intercepted.
type LogMsg struct {
	Timestamp time.Time
	Level     string
	Message   string
}

// levelToString converts slog.Level to a readable string.
func levelToString(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "DEBUG"
	case slog.LevelInfo:
		return "INFO"
	case slog.LevelWarn:
		return "WARN"
	case slog.LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}
