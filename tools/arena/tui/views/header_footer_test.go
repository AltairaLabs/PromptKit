package views

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewHeaderFooterView(t *testing.T) {
	view := NewHeaderFooterView(120)
	assert.NotNil(t, view)
	assert.Equal(t, 120, view.width)
}

func TestHeaderFooterView_RenderHeader(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderHeader("test-config.yaml", 5, 10, 30*time.Second)

	// Verify banner
	assert.Contains(t, output, "✨ PromptArena ✨")

	// Verify config file name
	assert.Contains(t, output, "test-config.yaml")

	// Verify progress
	assert.Contains(t, output, "5/10")

	// Verify elapsed time
	assert.Contains(t, output, "30s")
}

func TestHeaderFooterView_RenderHeader_MockMode(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderHeader("mock-config.yaml", 2, 5, 10*time.Second)

	// Should show MOCK MODE tag
	assert.Contains(t, output, "MOCK MODE")
	assert.Contains(t, output, "mock-config.yaml")
}

func TestHeaderFooterView_RenderHeader_NoMockMode(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderHeader("production-config.yaml", 2, 5, 10*time.Second)

	// Should NOT show MOCK MODE tag
	assert.NotContains(t, output, "MOCK MODE")
	assert.Contains(t, output, "production-config.yaml")
}

func TestHeaderFooterView_RenderHeader_ZeroProgress(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderHeader("config.yaml", 0, 10, 0*time.Second)

	assert.Contains(t, output, "0/10")
	assert.Contains(t, output, "⏱")
}

func TestHeaderFooterView_RenderHeader_CompleteProgress(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderHeader("config.yaml", 10, 10, 45*time.Second)

	assert.Contains(t, output, "10/10")
	assert.Contains(t, output, "45s")
}

func TestHeaderFooterView_RenderFooter_MainPage(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderFooter(false)

	// Main page footer
	assert.Contains(t, output, "q: quit")
	assert.Contains(t, output, "tab: focus runs/logs")
	assert.Contains(t, output, "enter: open conversation")
	assert.Contains(t, output, "enter: select")

	// Should NOT have conversation-specific help
	assert.NotContains(t, output, "esc: back")
	assert.NotContains(t, output, "tab: focus turns/detail")
}

func TestHeaderFooterView_RenderFooter_ConversationPage(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderFooter(true)

	// Conversation page footer
	assert.Contains(t, output, "q: quit")
	assert.Contains(t, output, "esc: back")
	assert.Contains(t, output, "tab: focus turns/detail")
	assert.Contains(t, output, "↑/↓: navigate")
	assert.Contains(t, output, "enter: select")

	// Should NOT have main page help
	assert.NotContains(t, output, "tab: focus runs/logs")
	assert.NotContains(t, output, "enter: open conversation")
}

func TestBuildProgressBar(t *testing.T) {
	tests := []struct {
		name      string
		completed int
		total     int
		width     int
		expected  string
	}{
		{
			name:      "Empty",
			completed: 0,
			total:     10,
			width:     10,
			expected:  "░░░░░░░░░░",
		},
		{
			name:      "Half",
			completed: 5,
			total:     10,
			width:     10,
			expected:  "█████░░░░░",
		},
		{
			name:      "Full",
			completed: 10,
			total:     10,
			width:     10,
			expected:  "██████████",
		},
		{
			name:      "OverComplete",
			completed: 15,
			total:     10,
			width:     10,
			expected:  "██████████",
		},
		{
			name:      "ZeroTotal",
			completed: 0,
			total:     0,
			width:     10,
			expected:  "░░░░░░░░░░",
		},
		{
			name:      "OneThird",
			completed: 1,
			total:     3,
			width:     12,
			expected:  "████░░░░░░░░",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildProgressBar(tt.completed, tt.total, tt.width)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, tt.width, len([]rune(result)))
		})
	}
}

func TestHeaderFooterView_RenderHeader_LongDuration(t *testing.T) {
	view := NewHeaderFooterView(100)
	output := view.RenderHeader("config.yaml", 50, 100, 2*time.Hour+30*time.Minute+15*time.Second)

	// Should format long duration properly
	assert.Contains(t, output, "50/100")
	// Duration should be present (exact format depends on theme.FormatDuration)
	assert.Contains(t, output, "⏱")
}

func TestHeaderFooterView_RenderHeader_DifferentWidths(t *testing.T) {
	widths := []int{80, 100, 120, 150}

	for _, width := range widths {
		t.Run(fmt.Sprintf("Width%d", width), func(t *testing.T) {
			view := NewHeaderFooterView(width)
			output := view.RenderHeader("config.yaml", 3, 7, 15*time.Second)

			// Should always contain essential info
			assert.Contains(t, output, "PromptArena")
			assert.Contains(t, output, "3/7")
			assert.NotEmpty(t, output)
		})
	}
}
