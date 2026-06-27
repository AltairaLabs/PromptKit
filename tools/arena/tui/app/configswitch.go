package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// ConfigSwitchPage wraps the file browser to let the user pick an arena config
// at runtime. On a successful selection it calls ctx.LoadConfig then emits both
// ConfigChangedMsg and PopPageMsg. On error it stays open and surfaces the error
// in its View.
type ConfigSwitchPage struct {
	ctx     *AppContext
	browser *pages.FileBrowserPage
	err     error
	w, h    int
}

// NewConfigSwitchPage creates a ConfigSwitchPage rooted at startDir.
// startDir is the initial directory shown in the file browser; a sensible
// default (dir of the current config, or ".") is chosen by callers.
func NewConfigSwitchPage(ctx *AppContext, startDir string) *ConfigSwitchPage {
	return &ConfigSwitchPage{
		ctx:     ctx,
		browser: pages.NewFileBrowserPage(startDir),
	}
}

// Init implements Page. Delegates to the underlying file browser.
func (p *ConfigSwitchPage) Init() tea.Cmd {
	return p.browser.Init()
}

// Update implements Page. It intercepts FileSelectedMsg from the browser and
// drives the load-config flow; all other messages are forwarded to the browser.
//
// Filtering note: FileBrowserPage uses AllowedTypes (extension list) which does
// not support glob patterns like "*.arena.yaml". The browser is therefore left
// unrestricted; invalid selections (non-YAML, non-arena files) are rejected via
// the LoadConfig error path and surfaced in the view.
func (p *ConfigSwitchPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	if sel, ok := msg.(pages.FileSelectedMsg); ok {
		if err := p.ctx.LoadConfig(sel.Path); err != nil {
			p.err = fmt.Errorf("cannot load config: %w", err)
			return p, nil
		}
		// Load succeeded — clear any prior error, pop and notify.
		p.err = nil
		path := sel.Path
		return p, tea.Batch(
			func() tea.Msg { return ConfigChangedMsg{Path: path} },
			func() tea.Msg { return PopPageMsg{} },
		)
	}

	_, cmd := p.browser.Update(msg)
	return p, cmd
}

// View implements Page. Shows the file browser, or an error banner when the
// last selection failed.
func (p *ConfigSwitchPage) View() string {
	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:  p.w,
			Height: p.h,
			Title:  titleChooseConfig,
			KeyBindings: []views.KeyBinding{
				{Keys: chatKeyLabelScrl, Description: keyHintNavigate},
				{Keys: keyEnter, Description: chatKeyLabelSel},
				{Keys: keyHintArrowH, Description: keyHintParentDir},
				{Keys: chatKeyLabelEsc, Description: keyHintBack},
			},
		},
		func(contentHeight int) string {
			p.browser.SetDimensions(p.w, contentHeight)
			if p.err != nil {
				errorStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(theme.ColorError)).
					Bold(true)
				helpStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(theme.ColorGray)).
					Italic(true)
				return lipgloss.JoinVertical(lipgloss.Left,
					errorStyle.Render("error: "+p.err.Error()),
					helpStyle.Render("select a valid .arena.yaml file, or press Esc to go back"),
					"",
					p.browser.Render(),
				)
			}
			return p.browser.Render()
		},
	)
}

// Title implements Page.
func (p *ConfigSwitchPage) Title() string { return "Config Switcher" }

// SetSize implements Page. Stores dimensions for View; does NOT call
// SetDimensions on the browser here because Render() is called from View()
// where we apply dimensions immediately before rendering.
func (p *ConfigSwitchPage) SetSize(w, h int) {
	p.w, p.h = w, h
}
