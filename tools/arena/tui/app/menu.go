package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// DefaultMenu returns the canonical menu items for the Home page.
//
// View is always enabled (it only needs a results directory).
// Run, Chat, and Inspect require a loaded config.
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
				p, err := NewRunPageFromContext(c)
				if err != nil {
					return placeholderPage("Run", err.Error())
				}
				return p
			},
		},
		{
			label:       "Chat     — interactive multi-turn session",
			needsConfig: true,
			make: func(c *AppContext) Page {
				return NewChatPage(c)
			},
		},
		{
			label:       "Inspect  — explore config and state",
			needsConfig: true,
			make: func(c *AppContext) Page {
				return NewInspectPage(c)
			},
		},
	}
}

// ---------------------------------------------------------------------------
// placeholderPage
// ---------------------------------------------------------------------------

// placeholder is a minimal Page that renders a centered notice and exits on
// any keypress. It is used as a fallback error page when a menu factory fails
// (e.g. EnsureEngine returns an error), displaying the error message so the
// user knows why the page could not be built.
type placeholder struct {
	title string
	issue string
	w, h  int
}

// placeholderPage returns a Page that renders title and message centered, then
// pops back to Home on any keypress.
func placeholderPage(title, message string) Page {
	return &placeholder{title: title, issue: message}
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
		Render(p.issue)

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
