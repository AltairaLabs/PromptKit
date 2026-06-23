package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// DefaultMenu returns the canonical menu items for the Home page.
//
// View is always enabled (it only needs a results directory).
// Run, Chat, and Inspect are phase-2 stubs that require a loaded config.
//
//nolint:revive // menuItem is intentionally unexported; callers pass the slice to NewHome unchanged.
func DefaultMenu(ctx *AppContext) []menuItem {
	return []menuItem{
		{
			label:       "View     — browse past test results",
			needsConfig: false,
			make: func(c *AppContext) Page {
				return NewViewPage(c.ResultsDir)
			},
		},
		{
			label:       "Run      — run scenarios against providers",
			needsConfig: true,
			make: func(c *AppContext) Page {
				return placeholderPage("Run", "#1455")
			},
		},
		{
			label:       "Chat     — interactive multi-turn session",
			needsConfig: true,
			make: func(c *AppContext) Page {
				return placeholderPage("Chat", "#1455")
			},
		},
		{
			label:       "Inspect  — explore config and state",
			needsConfig: true,
			make: func(c *AppContext) Page {
				return placeholderPage("Inspect", "#1455")
			},
		},
	}
}

// ---------------------------------------------------------------------------
// placeholderPage
// ---------------------------------------------------------------------------

// placeholder is a minimal Page used as a stub for hub menu items that are not
// yet implemented. It renders a centered notice and exits on any keypress.
type placeholder struct {
	title string
	issue string
	w, h  int
}

// placeholderPage returns a stub Page that displays the item title and the
// tracking issue for its implementation.
func placeholderPage(title, issue string) Page {
	return &placeholder{title: title, issue: issue}
}

// Init implements Page. No background command needed.
func (p *placeholder) Init() tea.Cmd { return nil }

// Update implements Page. Any key dismisses the placeholder by emitting
// PopPageMsg so the user returns to Home.
func (p *placeholder) Update(msg tea.Msg) (Page, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return p, func() tea.Msg { return PopPageMsg{} }
	}
	return p, nil
}

// View implements Page. It renders a centered notice using theme colors.
func (p *placeholder) View() string {
	notice := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.ColorPrimary)).
		Render(p.title)

	sub := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorGray)).
		Render(fmt.Sprintf("coming in a later phase (%s)", p.issue))

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightGray)).
		Render("press any key to return")

	content := notice + "\n\n" + sub + "\n\n" + hint
	return lipgloss.Place(p.w, p.h, lipgloss.Center, lipgloss.Center, content)
}

// Title implements Page.
func (p *placeholder) Title() string { return p.title }

// SetSize implements Page.
func (p *placeholder) SetSize(w, h int) { p.w, p.h = w, h }
