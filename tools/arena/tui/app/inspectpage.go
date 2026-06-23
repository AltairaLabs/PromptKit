package app

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
)

// InspectPage is a scrollable hub page that displays the configuration
// inspector output (the same content as `promptarena config-inspect`).
type InspectPage struct {
	vp      viewport.Model
	content string
	ready   bool
	w, h    int
}

// NewInspectPage creates an InspectPage with default render options.
// If ctx is nil or has no config loaded, a placeholder message is shown instead.
// This is the entry point used by the Home menu.
func NewInspectPage(ctx *AppContext) *InspectPage {
	return NewInspectPageWithOptions(ctx, inspect.RenderOptions{})
}

// NewInspectPageWithOptions creates an InspectPage with the given render options,
// allowing callers to thread --verbose/--section/--stats/--short flags through
// from the CLI. If ctx is nil or has no config loaded, a placeholder is shown.
func NewInspectPageWithOptions(ctx *AppContext, opts inspect.RenderOptions) *InspectPage {
	p := &InspectPage{}
	if ctx != nil && ctx.Config != nil {
		data := inspect.CollectInspectionData(ctx.Config, ctx.ConfigPath)
		if opts.Stats {
			data.CacheStats = inspect.CollectCacheStats(ctx.Config, opts.Verbose)
		}
		p.content = inspect.RenderText(data, opts)
	} else {
		p.content = "No configuration loaded."
	}
	return p
}

// Init implements Page.
func (p *InspectPage) Init() tea.Cmd { return nil }

// Update implements Page. Handles scroll key bindings.
func (p *InspectPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	if !p.ready {
		return p, nil
	}
	if m, ok := msg.(tea.KeyMsg); ok {
		switch m.String() {
		case "up", "k":
			p.vp.ScrollUp(1)
		case "down", "j":
			p.vp.ScrollDown(1)
		case "pgup":
			p.vp.HalfPageUp()
		case "pgdown":
			p.vp.HalfPageDown()
		case "g":
			p.vp.GotoTop()
		case "G":
			p.vp.GotoBottom()
		}
	}
	return p, nil
}

// View implements Page.
func (p *InspectPage) View() string {
	if !p.ready {
		return strings.TrimSpace(p.content)
	}
	return p.vp.View()
}

// Title implements Page.
func (p *InspectPage) Title() string { return "Inspect" }

// SetSize implements Page. Initializes (or re-initializes) the viewport.
func (p *InspectPage) SetSize(w, h int) {
	p.w, p.h = w, h
	p.vp = viewport.New(w, h)
	p.vp.SetContent(p.content)
	p.ready = true
}
