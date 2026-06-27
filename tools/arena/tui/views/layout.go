package views

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// ChromeConfig contains configuration for rendering page chrome.
type ChromeConfig struct {
	Width  int
	Height int

	// Title is the page-context line shown under the banner (e.g. "Home",
	// "Conversation · checkout / claude"). Used when ShowProgress is false.
	Title string

	// ShowProgress switches the header's second line to the run progress bar
	// (ConfigFile / CompletedCount / TotalRuns). Only run-type pages set this;
	// everyone else shows Title instead.
	ShowProgress   bool
	ConfigFile     string
	CompletedCount int
	TotalRuns      int

	KeyBindings []KeyBinding
}

// bodySeparators is the number of blank lines the chrome inserts around the
// body (one above, one below).
const bodySeparators = 2

// RenderWithChrome renders a page body with consistent banner, title/progress
// header, and footer. Every hub page (except the splash) routes through this so
// the shell looks like one app.
//
// Heights are MEASURED, not assumed: the header can be 1 or 2 lines, so we
// render it and the footer first, then give the body exactly the remaining rows
// (padded and capped to that exact height). This keeps the header pinned to the
// top and the footer to the bottom regardless of body content — no brittle
// magic-number math.
func RenderWithChrome(config ChromeConfig, renderBody func(contentHeight int) string) string {
	width := config.Width
	height := config.Height

	// Render nothing until sized — a placeholder here caused a visible
	// "Loading…"→full-frame snap on first paint.
	if width == 0 || height == 0 {
		return ""
	}

	headerView := NewHeaderFooterView(width)
	var header string
	if config.ShowProgress {
		header = headerView.RenderHeader(config.ConfigFile, config.CompletedCount, config.TotalRuns)
	} else {
		header = headerView.RenderTitleHeader(config.Title)
	}
	footer := headerView.RenderFooter(config.KeyBindings)

	// Exact remaining rows for the body after the (measured) header, footer,
	// and the two blank separators.
	bodyHeight := height - lipgloss.Height(header) - lipgloss.Height(footer) - bodySeparators
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// Height + MaxHeight forces the body to exactly bodyHeight rows (padding if
	// short, clipping if tall); MaxWidth stops any over-wide line from wrapping.
	body := lipgloss.NewStyle().
		MaxWidth(width).
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Render(renderBody(bodyHeight))

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}

// RenderCenteredNotice renders a title + hint centered within width×height. Used
// for empty-state messages (e.g. a run config with no scenarios).
func RenderCenteredNotice(width, height int, title, hint string) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorWarning)).
		Bold(true)
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorGray)).
		Italic(true)
	body := lipgloss.JoinVertical(lipgloss.Center, titleStyle.Render(title), "", hintStyle.Render(hint))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}
