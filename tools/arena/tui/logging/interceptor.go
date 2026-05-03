// Package logging provides log interception functionality for the TUI.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	logFilePermissions = 0600 // Read/write for owner only

	// tuiSendChanBuffer is the depth of the channel between the slog
	// interceptor and the goroutine that calls program.Send(). Sized
	// generously to absorb verbose-mode bursts (debug logs at hundreds
	// per second from the duplex pipeline). Frames overflow the
	// channel silently — losing a log line is preferable to deadlocking
	// the engine on the TUI's Update loop.
	tuiSendChanBuffer = 1024
)

// Interceptor wraps an slog.Handler to intercept log messages and send
// them to the TUI. It also optionally writes logs to a file in verbose
// mode.
//
// Concurrency model: the slog Handle path is hot — engine goroutines
// call it from many places. tea.Program.Send blocks until the message
// lands in BT's input channel; if the TUI's Update loop is busy, that
// blocks the calling goroutine. With debug logging on, the engine emits
// hundreds of log lines per second, and any backpressure from the TUI
// rapidly turns into a deadlock (engine goroutine stuck on Send → TUI
// has nothing new to render → engine never makes progress).
//
// To break that, the interceptor decouples Handle from Send via a
// buffered channel + worker goroutine. Handle pushes the Msg
// non-blocking (drops on full); the worker drains and calls Send.
type Interceptor struct {
	originalHandler slog.Handler
	program         *tea.Program
	logFile         *os.File
	suppressStderr  bool
	logBuffer       []slog.Record
	mu              sync.Mutex

	// Worker plumbing for the program.Send goroutine.
	tuiSendCh   chan Msg
	tuiSendDone chan struct{}

	// Closed flag protects against pushes after Close. Same pattern as
	// AudioRouter.Publish — closeMu RLock during Handle, closeMu Lock
	// during Close, prevents send-to-closed-channel panic.
	closeOnce sync.Once
	closeMu   sync.RWMutex
	closed    bool

	// drops counts log lines dropped because the TUI queue was full.
	// Surfaced on Close.
	drops atomic.Uint64
}

// NewInterceptor creates a log interceptor that sends logs to the TUI.
// If logFilePath is not empty, logs will also be written to that file.
// If suppressStderr is true, logs won't be sent to the original handler (useful for TUI mode).
func NewInterceptor(
	originalHandler slog.Handler, program *tea.Program, logFilePath string, suppressStderr bool,
) (*Interceptor, error) {
	interceptor := &Interceptor{
		originalHandler: originalHandler,
		program:         program,
		suppressStderr:  suppressStderr,
		tuiSendCh:       make(chan Msg, tuiSendChanBuffer),
		tuiSendDone:     make(chan struct{}),
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

	go interceptor.runTUIWorker()

	return interceptor, nil
}

// runTUIWorker drains the send channel and forwards messages to BT
// synchronously. Runs until tuiSendCh is closed by Close().
func (l *Interceptor) runTUIWorker() {
	defer close(l.tuiSendDone)
	for msg := range l.tuiSendCh {
		if l.program == nil {
			continue
		}
		// program.Send can panic if the program is in a bad state
		// (e.g. shutting down concurrently). Recover and discard.
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "panic sending log to TUI: %v\n", r)
				}
			}()
			l.program.Send(msg)
		}()
	}
}

// Close stops the worker goroutine, flushes pending file writes, and
// closes the log file. Safe to call multiple times.
func (l *Interceptor) Close() error {
	var fileErr error
	l.closeOnce.Do(func() {
		l.closeMu.Lock()
		l.closed = true
		close(l.tuiSendCh)
		l.closeMu.Unlock()
		<-l.tuiSendDone

		if dropped := l.drops.Load(); dropped > 0 {
			fmt.Fprintf(os.Stderr,
				"[logging.Interceptor] %d log line(s) dropped because TUI queue was full\n",
				dropped)
		}

		l.mu.Lock()
		defer l.mu.Unlock()
		if l.logFile != nil {
			fileErr = l.logFile.Close()
		}
	})
	return fileErr
}

// Enabled reports whether the handler handles records at the given level.
func (l *Interceptor) Enabled(ctx context.Context, level slog.Level) bool {
	return l.originalHandler.Enabled(ctx, level)
}

// Handle processes a log record by sending it to the TUI and optionally writing to file.
//
//nolint:gocritic // hugeParam: slog.Record must be passed by value to satisfy slog.Handler interface
func (l *Interceptor) Handle(ctx context.Context, record slog.Record) error {
	// If stderr suppressed, buffer the log for later flushing
	if l.suppressStderr {
		l.mu.Lock()
		l.logBuffer = append(l.logBuffer, record)
		l.mu.Unlock()
	} else {
		// Send to original handler (stderr) immediately
		if err := l.originalHandler.Handle(ctx, record); err != nil {
			return err
		}
	}

	// Send to TUI via the worker goroutine. NEVER block the engine on
	// the TUI — drop the log line if the buffer is full.
	if l.program != nil {
		l.closeMu.RLock()
		if !l.closed {
			msg := Msg{
				Timestamp: record.Time,
				Level:     levelToString(record.Level),
				Message:   record.Message,
			}
			select {
			case l.tuiSendCh <- msg:
			default:
				l.drops.Add(1)
			}
		}
		l.closeMu.RUnlock()
	}

	// Write to file if configured (use original handler to get full formatting)
	if l.logFile != nil {
		// Create a text handler that writes to the log file with full formatting
		fileHandler := slog.NewTextHandler(l.logFile, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
		if err := fileHandler.Handle(ctx, record); err != nil {
			return fmt.Errorf("failed to write to log file: %w", err)
		}
	}

	return nil
}

// WithAttrs returns a new handler with additional attributes.
//
// Subloggers share the parent's program/file/worker so attributed logs
// route through the same backpressure path.
func (l *Interceptor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &subInterceptor{parent: l, handler: l.originalHandler.WithAttrs(attrs)}
}

// WithGroup returns a new handler with an additional group.
func (l *Interceptor) WithGroup(name string) slog.Handler {
	return &subInterceptor{parent: l, handler: l.originalHandler.WithGroup(name)}
}

// subInterceptor is the WithAttrs/WithGroup view of an Interceptor.
// It applies the original handler's attribute scoping but shares the
// parent's TUI worker, log file, and drop counter so subloggers can't
// reintroduce the deadlock by going through their own synchronous Send.
type subInterceptor struct {
	parent  *Interceptor
	handler slog.Handler
}

// Enabled reports whether the underlying scoped slog.Handler accepts the level.
func (s *subInterceptor) Enabled(ctx context.Context, level slog.Level) bool {
	return s.handler.Enabled(ctx, level)
}

// Handle forwards a record through the parent's TUI worker / log file path
// while applying this sub-handler's scoped attributes for stderr/file output.
//
//nolint:gocritic // hugeParam: slog.Record must be value
func (s *subInterceptor) Handle(ctx context.Context, record slog.Record) error {
	// Same flow as parent's Handle, but using this sub-handler's
	// scoped attributes for stderr/file output.
	if s.parent.suppressStderr {
		s.parent.mu.Lock()
		s.parent.logBuffer = append(s.parent.logBuffer, record)
		s.parent.mu.Unlock()
	} else if err := s.handler.Handle(ctx, record); err != nil {
		return err
	}
	if s.parent.program != nil {
		s.parent.closeMu.RLock()
		if !s.parent.closed {
			msg := Msg{
				Timestamp: record.Time,
				Level:     levelToString(record.Level),
				Message:   record.Message,
			}
			select {
			case s.parent.tuiSendCh <- msg:
			default:
				s.parent.drops.Add(1)
			}
		}
		s.parent.closeMu.RUnlock()
	}
	if s.parent.logFile != nil {
		fileHandler := slog.NewTextHandler(s.parent.logFile, &slog.HandlerOptions{Level: slog.LevelDebug})
		if err := fileHandler.Handle(ctx, record); err != nil {
			return fmt.Errorf("failed to write to log file: %w", err)
		}
	}
	return nil
}

// WithAttrs returns a sub-handler with additional scoped attributes,
// sharing this sub-handler's parent worker / log file plumbing.
func (s *subInterceptor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &subInterceptor{parent: s.parent, handler: s.handler.WithAttrs(attrs)}
}

// WithGroup returns a sub-handler that nests subsequent attributes under
// the given group name, sharing the parent's worker / log file plumbing.
func (s *subInterceptor) WithGroup(name string) slog.Handler {
	return &subInterceptor{parent: s.parent, handler: s.handler.WithGroup(name)}
}

// Msg is a bubbletea message sent when a log entry is intercepted.
type Msg struct {
	Timestamp time.Time
	Level     string
	Message   string
}

// FlushBuffer writes all buffered logs to the original handler (stderr).
// Call this after the TUI exits to show any logs that occurred during execution.
func (l *Interceptor) FlushBuffer() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for i := range l.logBuffer {
		// Ignore errors during flush - best effort
		// Use background context since original context may be canceled
		_ = l.originalHandler.Handle(context.Background(), l.logBuffer[i])
	}

	// Clear the buffer
	l.logBuffer = nil
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
