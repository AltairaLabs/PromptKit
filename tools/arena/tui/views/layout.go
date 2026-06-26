package views

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ChromeConfig contains configuration for rendering page chrome.
type ChromeConfig struct {
	Width  int
	Height int

	// Title is the page-context line shown under the banner (e.g. "Home",
	// "Conversation · checkout / claude"). Used when ShowProgress is false.
	Title string

	// ShowProgress switches the header's second line to the run progress bar +
	// elapsed timer (ConfigFile / CompletedCount / TotalRuns / Elapsed). Only
	// run-type pages set this; everyone else shows Title instead.
	ShowProgress   bool
	ConfigFile     string
	CompletedCount int
	TotalRuns      int
	Elapsed        time.Duration

	KeyBindings []KeyBinding
}

// chromeOverhead is the vertical space the chrome consumes: banner+info (2) +
// footer (1) + a blank separator above and below the body (2).
const chromeOverhead = 5

// RenderWithChrome renders a page body with consistent banner, title/progress
// header, and footer. Every hub page (except the splash) routes through this so
// the shell looks like one app.
func RenderWithChrome(config ChromeConfig, renderBody func(contentHeight int) string) string {
	width := config.Width
	height := config.Height

	// Render nothing until sized — returning placeholder text here caused a
	// visible "Loading…"→full-frame snap on first paint.
	if width == 0 || height == 0 {
		return ""
	}

	contentHeight := height - chromeOverhead
	if contentHeight < 1 {
		contentHeight = 1
	}

	headerView := NewHeaderFooterView(width)
	var header string
	if config.ShowProgress {
		header = headerView.RenderHeader(config.ConfigFile, config.CompletedCount, config.TotalRuns, config.Elapsed)
	} else {
		header = headerView.RenderTitleHeader(config.Title)
	}
	body := renderBody(contentHeight)
	footer := headerView.RenderFooter(config.KeyBindings)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}
