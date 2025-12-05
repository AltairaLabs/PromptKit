package views

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ChromeConfig contains configuration for rendering page chrome
type ChromeConfig struct {
	Width          int
	Height         int
	ConfigFile     string
	CompletedCount int
	TotalRuns      int
	Elapsed        time.Duration
	KeyBindings    []KeyBinding
}

// RenderWithChrome renders a page body with consistent header and footer chrome
func RenderWithChrome(
	config ChromeConfig,
	renderBody func(contentHeight int) string,
) string {
	width := config.Width
	height := config.Height

	if width == 0 || height == 0 {
		return "Loading..."
	}

	// Calculate available height for content area
	// Subtract: header (2) + footer (1) + separators around body (2) = 5 lines
	contentHeight := height - 5
	if contentHeight < 15 {
		contentHeight = 15 // Minimum usable height
	}

	// Render header
	headerView := NewHeaderFooterView(width)
	header := headerView.RenderHeader(config.ConfigFile, config.CompletedCount, config.TotalRuns, config.Elapsed)

	// Render body using provided function
	body := renderBody(contentHeight)

	// Render footer
	footer := headerView.RenderFooter(config.KeyBindings)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}
