package tui

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

func TestNewLogInterceptor(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	t.Run("without log file", func(t *testing.T) {
		interceptor, err := NewLogInterceptor(handler, program, "", false)
		require.NoError(t, err)
		assert.NotNil(t, interceptor)
		assert.Nil(t, interceptor.logFile)
		assert.Equal(t, program, interceptor.program)
	})

	t.Run("with log file", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		interceptor, err := NewLogInterceptor(handler, program, logPath, false)
		require.NoError(t, err)
		assert.NotNil(t, interceptor)
		assert.NotNil(t, interceptor.logFile)

		// Cleanup
		err = interceptor.Close()
		assert.NoError(t, err)
	})

	t.Run("invalid log file path", func(t *testing.T) {
		interceptor, err := NewLogInterceptor(handler, program, "/nonexistent/dir/test.log", false)
		assert.Error(t, err)
		assert.Nil(t, interceptor)
	})
}

func TestLogInterceptor_Close(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	t.Run("with no log file", func(t *testing.T) {
		interceptor, err := NewLogInterceptor(handler, program, "", false)
		require.NoError(t, err)

		err = interceptor.Close()
		assert.NoError(t, err)
	})

	t.Run("with log file", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		interceptor, err := NewLogInterceptor(handler, program, logPath, false)
		require.NoError(t, err)

		err = interceptor.Close()
		assert.NoError(t, err)
	})
}

func TestLogInterceptor_Enabled(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	program := &tea.Program{}

	interceptor, err := NewLogInterceptor(handler, program, "", false)
	require.NoError(t, err)

	ctx := context.Background()

	// Should be enabled for INFO and above
	assert.True(t, interceptor.Enabled(ctx, slog.LevelInfo))
	assert.True(t, interceptor.Enabled(ctx, slog.LevelWarn))
	assert.True(t, interceptor.Enabled(ctx, slog.LevelError))

	// Should be disabled for DEBUG (below INFO)
	assert.False(t, interceptor.Enabled(ctx, slog.LevelDebug))
}

func TestLogInterceptor_Handle(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	handler := slog.NewTextHandler(os.Stderr, nil)

	// Don't pass a program for this test - we're testing file writing
	interceptor, err := NewLogInterceptor(handler, nil, logPath, false)
	require.NoError(t, err)
	defer interceptor.Close()

	ctx := context.Background()
	now := time.Now()

	// Create a log record
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

func TestLogInterceptor_WithAttrs(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	interceptor, err := NewLogInterceptor(handler, program, "", false)
	require.NoError(t, err)

	attrs := []slog.Attr{slog.String("key", "value")}
	newHandler := interceptor.WithAttrs(attrs)

	assert.NotNil(t, newHandler)
	// Type assertion
	_, ok := newHandler.(*LogInterceptor)
	assert.True(t, ok)
}

func TestLogInterceptor_WithGroup(t *testing.T) {
	handler := slog.NewTextHandler(os.Stderr, nil)
	program := &tea.Program{}

	interceptor, err := NewLogInterceptor(handler, program, "", false)
	require.NoError(t, err)

	newHandler := interceptor.WithGroup("test-group")

	assert.NotNil(t, newHandler)
	// Type assertion
	_, ok := newHandler.(*LogInterceptor)
	assert.True(t, ok)
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

func TestLogMsg(t *testing.T) {
	now := time.Now()
	msg := LogMsg{
		Timestamp: now,
		Level:     "INFO",
		Message:   "test message",
	}

	assert.Equal(t, now, msg.Timestamp)
	assert.Equal(t, "INFO", msg.Level)
	assert.Equal(t, "test message", msg.Message)
}

func TestModel_handleLogMsg(t *testing.T) {
	m := NewModel("test-pack", 1)
	now := time.Now()

	msg := &LogMsg{
		Timestamp: now,
		Level:     "INFO",
		Message:   "test log message",
	}

	m.mu.Lock()
	m.handleLogMsg(msg)
	m.mu.Unlock()

	assert.Len(t, m.logs, 1)
	assert.Equal(t, now, m.logs[0].Timestamp)
	assert.Equal(t, "INFO", m.logs[0].Level)
	assert.Equal(t, "test log message", m.logs[0].Message)
}

func TestModel_handleLogMsg_trimming(t *testing.T) {
	m := NewModel("test-pack", 1)

	// Add more logs than maxLogBufferSize
	for i := 0; i < maxLogBufferSize+50; i++ {
		msg := &LogMsg{
			Timestamp: time.Now(),
			Level:     "INFO",
			Message:   "test",
		}
		m.mu.Lock()
		m.handleLogMsg(msg)
		m.mu.Unlock()
	}

	// Should be trimmed to maxLogBufferSize
	assert.Equal(t, maxLogBufferSize, len(m.logs))
}
