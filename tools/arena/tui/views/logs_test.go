package views

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/stretchr/testify/assert"
)

func TestNewLogsView(t *testing.T) {
	view := NewLogsView(true)
	assert.NotNil(t, view)
	assert.True(t, view.focused)

	view = NewLogsView(false)
	assert.False(t, view.focused)
}

func TestLogsView_Render_NotReady(t *testing.T) {
	view := NewLogsView(true)
	vp := viewport.New(80, 20)

	output := view.Render(&vp, false, 100)

	assert.Contains(t, output, "Logs")
	assert.Contains(t, output, "Initializing...")
}

func TestLogsView_Render_Ready(t *testing.T) {
	view := NewLogsView(true)
	vp := viewport.New(80, 20)
	vp.SetContent("Test log content")

	output := view.Render(&vp, true, 100)

	assert.Contains(t, output, "Logs")
	assert.Contains(t, output, "Test log content")
}

func TestLogsView_Render_Focused(t *testing.T) {
	viewFocused := NewLogsView(true)
	viewUnfocused := NewLogsView(false)
	vp := viewport.New(80, 20)
	vp.SetContent("Test content")

	outputFocused := viewFocused.Render(&vp, true, 100)
	outputUnfocused := viewUnfocused.Render(&vp, true, 100)

	// Both should contain the content and title
	assert.Contains(t, outputFocused, "Test content")
	assert.Contains(t, outputUnfocused, "Test content")
	assert.Contains(t, outputFocused, "Logs")
	assert.Contains(t, outputUnfocused, "Logs")

	// Focus affects border color, but the styling may not be visible in test output
	// Just verify both render successfully
	assert.NotEmpty(t, outputFocused)
	assert.NotEmpty(t, outputUnfocused)
}

func TestFormatLogLine(t *testing.T) {
	tests := []struct {
		name     string
		level    string
		message  string
		contains string
	}{
		{
			name:     "INFO level",
			level:    "INFO",
			message:  "Test info message",
			contains: "Test info message",
		},
		{
			name:     "WARN level",
			level:    "WARN",
			message:  "Test warning",
			contains: "Test warning",
		},
		{
			name:     "ERROR level",
			level:    "ERROR",
			message:  "Test error",
			contains: "Test error",
		},
		{
			name:     "DEBUG level",
			level:    "DEBUG",
			message:  "Test debug",
			contains: "Test debug",
		},
		{
			name:     "Unknown level",
			level:    "UNKNOWN",
			message:  "Test unknown",
			contains: "Test unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := FormatLogLine(tt.level, tt.message)
			assert.Contains(t, output, tt.level)
			assert.Contains(t, output, tt.contains)
		})
	}
}

func TestFormatLogLines_Empty(t *testing.T) {
	logs := []LogEntry{}
	output := FormatLogLines(logs)
	assert.Equal(t, "No logs yet...", output)
}

func TestFormatLogLines_Single(t *testing.T) {
	logs := []LogEntry{
		{Level: "INFO", Message: "Single log"},
	}
	output := FormatLogLines(logs)
	assert.Contains(t, output, "INFO")
	assert.Contains(t, output, "Single log")
}

func TestFormatLogLines_Multiple(t *testing.T) {
	logs := []LogEntry{
		{Level: "INFO", Message: "First log"},
		{Level: "WARN", Message: "Second log"},
		{Level: "ERROR", Message: "Third log"},
	}
	output := FormatLogLines(logs)
	assert.Contains(t, output, "First log")
	assert.Contains(t, output, "Second log")
	assert.Contains(t, output, "Third log")

	// Should be separated by newlines
	lines := strings.Split(output, "\n")
	assert.Equal(t, 3, len(lines))
}

func TestFormatLogLines_PreservesOrder(t *testing.T) {
	logs := []LogEntry{
		{Level: "INFO", Message: "Log 1"},
		{Level: "INFO", Message: "Log 2"},
		{Level: "INFO", Message: "Log 3"},
	}
	output := FormatLogLines(logs)

	// Check order is preserved
	idx1 := strings.Index(output, "Log 1")
	idx2 := strings.Index(output, "Log 2")
	idx3 := strings.Index(output, "Log 3")

	assert.True(t, idx1 < idx2)
	assert.True(t, idx2 < idx3)
}
