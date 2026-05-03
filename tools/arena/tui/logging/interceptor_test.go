package logging

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInterceptor(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	t.Run("without log file", func(t *testing.T) {
		interceptor, err := NewInterceptor(handler, program, "", false)
		require.NoError(t, err)
		assert.NotNil(t, interceptor)
		assert.Nil(t, interceptor.logFile)
		assert.Equal(t, program, interceptor.program)
	})

	t.Run("with log file", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		interceptor, err := NewInterceptor(handler, program, logPath, false)
		require.NoError(t, err)
		assert.NotNil(t, interceptor)
		assert.NotNil(t, interceptor.logFile)

		// Cleanup
		err = interceptor.Close()
		assert.NoError(t, err)
	})

	t.Run("invalid log file path", func(t *testing.T) {
		interceptor, err := NewInterceptor(handler, program, "/nonexistent/dir/test.log", false)
		assert.Error(t, err)
		assert.Nil(t, interceptor)
	})
}

func TestInterceptor_Close(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	t.Run("with no log file", func(t *testing.T) {
		interceptor, err := NewInterceptor(handler, program, "", false)
		require.NoError(t, err)

		err = interceptor.Close()
		assert.NoError(t, err)
	})

	t.Run("with log file", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		interceptor, err := NewInterceptor(handler, program, logPath, false)
		require.NoError(t, err)

		err = interceptor.Close()
		assert.NoError(t, err)
	})
}

func TestInterceptor_Enabled(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	program := &tea.Program{}

	interceptor, err := NewInterceptor(handler, program, "", false)
	require.NoError(t, err)

	ctx := context.Background()

	// Should be enabled for INFO and above
	assert.True(t, interceptor.Enabled(ctx, slog.LevelInfo))
	assert.True(t, interceptor.Enabled(ctx, slog.LevelWarn))
	assert.True(t, interceptor.Enabled(ctx, slog.LevelError))

	// Should be disabled for DEBUG (below INFO)
	assert.False(t, interceptor.Enabled(ctx, slog.LevelDebug))
}

func TestInterceptor_Handle(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	handler := slog.NewTextHandler(os.Stderr, nil)
	// Don't pass a program for this test - we're testing file writing
	interceptor, err := NewInterceptor(handler, nil, logPath, false)
	require.NoError(t, err)
	defer interceptor.Close()

	ctx := context.Background()

	// Create a log record
	now := time.Now()
	record := slog.Record{
		Time:    now,
		Message: "test message",
		Level:   slog.LevelInfo,
	}

	// Handle the record
	err = interceptor.Handle(ctx, record)
	assert.NoError(t, err)

	// Check log file was written
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)

	// Check for timestamp format (new slog format)
	assert.Contains(t, string(content), "level=INFO")
	assert.Contains(t, string(content), "test message")
}

func TestInterceptor_WithAttrs(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	interceptor, err := NewInterceptor(handler, program, "", false)
	require.NoError(t, err)

	attrs := []slog.Attr{slog.String("key", "value")}
	newHandler := interceptor.WithAttrs(attrs)

	assert.NotNil(t, newHandler)
	// WithAttrs returns a sub-handler that shares the parent's TUI worker
	// and file output but applies its own attribute scoping.
	sub, ok := newHandler.(*subInterceptor)
	require.True(t, ok)
	assert.Same(t, interceptor, sub.parent)
}

func TestInterceptor_WithGroup(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	interceptor, err := NewInterceptor(handler, program, "", false)
	require.NoError(t, err)

	newHandler := interceptor.WithGroup("test-group")

	assert.NotNil(t, newHandler)
	sub, ok := newHandler.(*subInterceptor)
	require.True(t, ok)
	assert.Same(t, interceptor, sub.parent)
}

func TestLevelToString(t *testing.T) {
	tests := []struct {
		level    slog.Level
		expected string
	}{
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelWarn, "WARN"},
		{slog.LevelError, "ERROR"},
		{slog.Level(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := levelToString(tt.level)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMsg(t *testing.T) {
	now := time.Now()

	msg := Msg{
		Timestamp: now,
		Level:     "INFO",
		Message:   "test message",
	}

	assert.Equal(t, now, msg.Timestamp)
	assert.Equal(t, "INFO", msg.Level)
	assert.Equal(t, "test message", msg.Message)
}

type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *capturingHandler) WithGroup(string) slog.Handler { return h }

func TestInterceptor_FlushBuffer(t *testing.T) {
	handler := &capturingHandler{}

	interceptor, err := NewInterceptor(handler, nil, "", true)
	require.NoError(t, err)

	record := slog.Record{
		Time:    time.Now(),
		Message: "buffered",
		Level:   slog.LevelDebug,
	}

	err = interceptor.Handle(context.Background(), record)
	require.NoError(t, err)

	// Should be buffered, not forwarded yet
	assert.Len(t, handler.records, 0)

	interceptor.FlushBuffer()

	// Second flush should be no-op
	assert.Len(t, handler.records, 1)
	assert.Equal(t, "buffered", handler.records[0].Message)

	interceptor.FlushBuffer()
	assert.Len(t, handler.records, 1)
}

// TestSubInterceptor exercises the WithAttrs / WithGroup paths on the
// parent Interceptor and the resulting subInterceptor's Enabled / Handle /
// WithAttrs / WithGroup methods. Subinterceptors share the parent's TUI
// worker + log file but apply scoped slog attributes.
func TestSubInterceptor(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	interceptor, err := NewInterceptor(handler, nil, "", false)
	require.NoError(t, err)
	defer interceptor.Close()

	t.Run("WithAttrs returns subInterceptor", func(t *testing.T) {
		sub := interceptor.WithAttrs([]slog.Attr{slog.String("k", "v")})
		assert.NotNil(t, sub)
		assert.True(t, sub.Enabled(context.Background(), slog.LevelInfo))
	})

	t.Run("WithGroup returns subInterceptor", func(t *testing.T) {
		sub := interceptor.WithGroup("ns")
		assert.NotNil(t, sub)
	})

	t.Run("subInterceptor.Handle writes through", func(t *testing.T) {
		sub := interceptor.WithAttrs([]slog.Attr{slog.String("scope", "test")})
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "scoped log", 0)
		err := sub.Handle(context.Background(), rec)
		assert.NoError(t, err)
	})

	t.Run("subInterceptor.Handle buffers when suppressStderr", func(t *testing.T) {
		// New interceptor with suppressStderr=true to exercise the
		// buffering branch of subInterceptor.Handle.
		sup, err := NewInterceptor(handler, nil, "", true)
		require.NoError(t, err)
		defer sup.Close()
		sub := sup.WithAttrs([]slog.Attr{slog.String("k", "v")})
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "buffered", 0)
		require.NoError(t, sub.Handle(context.Background(), rec))
		assert.Len(t, sup.logBuffer, 1, "suppressed log should land in parent buffer")
	})

	t.Run("subInterceptor.Handle writes to log file when present", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "sub.log")
		withFile, err := NewInterceptor(handler, nil, logPath, false)
		require.NoError(t, err)
		defer withFile.Close()
		sub := withFile.WithAttrs([]slog.Attr{slog.String("k", "v")})
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "file-bound", 0)
		require.NoError(t, sub.Handle(context.Background(), rec))
		// Force flush/close to ensure buffered file writes hit disk.
		withFile.Close()
		data, err := os.ReadFile(logPath)
		require.NoError(t, err)
		assert.Contains(t, string(data), "file-bound")
	})

	t.Run("subInterceptor.WithAttrs / WithGroup chain", func(t *testing.T) {
		sub := interceptor.WithAttrs([]slog.Attr{slog.String("a", "1")})
		nested := sub.WithAttrs([]slog.Attr{slog.String("b", "2")})
		assert.NotNil(t, nested)
		grouped := sub.WithGroup("inner")
		assert.NotNil(t, grouped)
	})
}
