package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetup(t *testing.T) {
	t.Run("setup without log file", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		program := &tea.Program{}

		interceptor, err := Setup(logger, program, "", false)
		require.NoError(t, err)
		assert.NotNil(t, interceptor)
		assert.Nil(t, interceptor.logFile)

		// Test that logger still works
		logger.Info("test message")
		assert.Contains(t, buf.String(), "test message")

		// Cleanup
		err = interceptor.Close()
		assert.NoError(t, err)
	})

	t.Run("setup with log file", func(t *testing.T) {
		tmpDir := t.TempDir()
		logPath := filepath.Join(tmpDir, "test.log")

		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		program := &tea.Program{}

		interceptor, err := Setup(logger, program, logPath, false)
		require.NoError(t, err)
		assert.NotNil(t, interceptor)
		assert.NotNil(t, interceptor.logFile)

		// Test that logger still works
		logger.Info("test message")
		assert.Contains(t, buf.String(), "test message")

		// Test that file was created and written to
		err = interceptor.Close()
		assert.NoError(t, err)

		content, err := os.ReadFile(logPath)
		require.NoError(t, err)
		// Check for timestamp format (new slog format)
		assert.Contains(t, string(content), "level=INFO")
		assert.Contains(t, string(content), "test message")
	})

	t.Run("setup with invalid log file path", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		program := &tea.Program{}

		interceptor, err := Setup(logger, program, "/nonexistent/dir/test.log", false)
		assert.Error(t, err)
		assert.Nil(t, interceptor)
	})

	t.Run("logger with attributes", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		logger = logger.With("key", "value")
		program := &tea.Program{}

		interceptor, err := Setup(logger, program, "", false)
		require.NoError(t, err)
		defer interceptor.Close()

		logger.Info("test message")
		assert.Contains(t, buf.String(), "test message")
	})

	t.Run("logger with group", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		logger = logger.WithGroup("test-group")
		program := &tea.Program{}

		interceptor, err := Setup(logger, program, "", false)
		require.NoError(t, err)
		defer interceptor.Close()

		logger.Info("test message")
		assert.Contains(t, buf.String(), "test message")
	})

	t.Run("logger with different log levels", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
		program := &tea.Program{}

		interceptor, err := Setup(logger, program, "", false)
		require.NoError(t, err)
		defer interceptor.Close()

		ctx := context.Background()
		logger.DebugContext(ctx, "debug message")
		logger.InfoContext(ctx, "info message")
		logger.WarnContext(ctx, "warn message")
		logger.ErrorContext(ctx, "error message")

		output := buf.String()
		assert.Contains(t, output, "debug message")
		assert.Contains(t, output, "info message")
		assert.Contains(t, output, "warn message")
		assert.Contains(t, output, "error message")
	})
}
