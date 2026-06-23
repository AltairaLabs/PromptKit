package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// menuItem describes a single entry in the Home page menu.
// label is the display string; needsConfig indicates the item requires a loaded
// arena config to be enabled; make constructs the target Page when selected.
type menuItem struct {
	label       string
	needsConfig bool
	make        func(*AppContext) Page
}

// Home is the top-level navigation page for the PromptArena TUI hub shell.
// It is generic over its menu items so that the concrete Run/View/Chat/Inspect
// entries can be injected by the top-level wiring (Task 8) without baking them
// in here.
type Home struct {
	ctx    *AppContext
	items  []menuItem
	cursor int
	w, h   int
}

// NewHome creates a Home page backed by ctx and populated with items. Items
// whose needsConfig field is true are greyed and non-selectable when
// ctx.HasConfig() returns false.
func NewHome(ctx *AppContext, items []menuItem) *Home {
	h := &Home{ctx: ctx, items: items}
	// Start cursor on the first enabled item.
	h.cursor = h.firstEnabled()
	return h
}

// Init implements Page. Home has no background commands to start.
func (h *Home) Init() tea.Cmd { return nil }

// Update implements Page. It handles cursor movement (up/down/j/k) and
// selection (Enter). Disabled items are skipped during navigation and silently
// ignored on Enter.
//
// 'c' config-switcher is wired in Task 6 (#1454).
func (h *Home) Update(msg tea.Msg) (Page, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return h, nil
	}

	//nolint:exhaustive // Only cursor and select keys are handled; all others are intentionally dropped.
	switch km.Type {
	case tea.KeyUp:
		h.cursor = h.prevEnabled(h.cursor)
	case tea.KeyDown:
		h.cursor = h.nextEnabled(h.cursor)
	case tea.KeyEnter:
		if h.cursor >= 0 && h.cursor < len(h.items) && h.isEnabled(h.items[h.cursor]) {
			item := h.items[h.cursor]
			p := item.make(h.ctx)
			return h, func() tea.Msg { return PushPageMsg{Page: p} }
		}
	case tea.KeyRunes:
		switch {
		case len(km.Runes) == 1 && km.Runes[0] == 'k':
			h.cursor = h.prevEnabled(h.cursor)
		case len(km.Runes) == 1 && km.Runes[0] == 'j':
			h.cursor = h.nextEnabled(h.cursor)
		}
	}
	return h, nil
}

// View implements Page. It renders a small heading, a config indicator line,
// and the menu with disabled items rendered faint/greyed.
func (h *Home) View() string {
	heading := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.ColorPrimary)).
		Render("PromptArena")

	var configLine string
	if h.ctx.HasConfig() {
		name := configName(h.ctx.ConfigPath)
		configLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorSuccess)).
			Render("config: " + name)
	} else {
		configLine = lipgloss.NewStyle().
			Foreground(lipgloss.Color(theme.ColorGray)).
			// 'c' switcher wired in Task 6 (#1454).
			Render("no config — press c to pick, or run `promptarena init`")
	}

	var menuLines []string
	for i, item := range h.items {
		enabled := h.isEnabled(item)
		cursor := "  "
		if i == h.cursor {
			cursor = "> "
		}
		label := cursor + item.label
		if !enabled {
			menuLines = append(menuLines, lipgloss.NewStyle().
				Faint(true).
				Foreground(lipgloss.Color(theme.ColorGray)).
				Render(label))
		} else if i == h.cursor {
			menuLines = append(menuLines, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(theme.ColorViolet)).
				Render(label))
		} else {
			menuLines = append(menuLines, lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.ColorWhite)).
				Render(label))
		}
	}

	content := strings.Join([]string{
		heading,
		"",
		configLine,
		"",
		strings.Join(menuLines, "\n"),
	}, "\n")

	return lipgloss.Place(h.w, h.h, lipgloss.Left, lipgloss.Top, content)
}

// Title implements Page.
func (h *Home) Title() string { return "Home" }

// SetSize implements Page.
func (h *Home) SetSize(width, height int) {
	h.w, h.h = width, height
}

// isEnabled reports whether item is currently selectable.
func (h *Home) isEnabled(item menuItem) bool {
	return !item.needsConfig || h.ctx.HasConfig()
}

// firstEnabled returns the index of the first enabled item, or 0 if none.
func (h *Home) firstEnabled() int {
	for i, item := range h.items {
		if h.isEnabled(item) {
			return i
		}
	}
	return 0
}

// nextEnabled returns the index of the next enabled item after current,
// wrapping around if necessary. Returns current if there is only one enabled.
func (h *Home) nextEnabled(current int) int {
	n := len(h.items)
	for delta := 1; delta < n; delta++ {
		next := (current + delta) % n
		if h.isEnabled(h.items[next]) {
			return next
		}
	}
	return current
}

// prevEnabled returns the index of the previous enabled item before current,
// wrapping around if necessary. Returns current if there is only one enabled.
func (h *Home) prevEnabled(current int) int {
	n := len(h.items)
	for delta := 1; delta < n; delta++ {
		prev := (current - delta + n) % n
		if h.isEnabled(h.items[prev]) {
			return prev
		}
	}
	return current
}

// configName derives a short display name from a config file path.
// It returns the parent directory name when it differs from "." or "/",
// otherwise falls back to the base filename.
func configName(path string) string {
	if path == "" {
		return "(unknown)"
	}
	dir := filepath.Base(filepath.Dir(path))
	if dir == "." || dir == "/" || dir == "" {
		return filepath.Base(path)
	}
	return dir
}
