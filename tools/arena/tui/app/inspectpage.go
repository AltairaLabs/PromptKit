package app

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/views"
)

// Shared footer key-hint labels.
const (
	keyHintScroll = "scroll"
	keyHintBack   = "back"
)

// InspectPage is a scrollable hub page that displays the configuration
// inspector output (the same content as `promptarena config-inspect`).
type InspectPage struct {
	vp   viewport.Model
	data *inspect.InspectionData
	opts inspect.RenderOptions
	// content is the rendered inspector text (or a placeholder when no config is
	// loaded). It is (re)rendered at the live terminal width on SetSize.
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
	p := &InspectPage{opts: opts}
	if ctx != nil && ctx.Config != nil {
		data := inspect.CollectInspectionData(ctx.Config, ctx.ConfigPath)
		// Run the validator so the view reports real status; without this
		// ValidationPassed defaults to false and every config reads as "errors".
		inspect.PopulateValidation(data, ctx.Config, ctx.ConfigPath)
		if opts.Stats {
			data.CacheStats = inspect.CollectCacheStats(ctx.Config, opts.Verbose)
		}
		p.data = data
		p.renderContent() // populate at default width; SetSize re-renders to fit
	} else {
		p.content = "No configuration loaded."
	}
	return p
}

// renderContent (re)renders the inspector text at the current terminal width so
// the boxes fill the available space. No-op when no config is loaded (the
// placeholder set in the constructor stands).
func (p *InspectPage) renderContent() {
	if p.data == nil {
		return
	}
	p.opts.Width = p.w
	p.content = inspect.RenderText(p.data, p.opts)
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

// View implements Page. Renders the scrollable content above a consistent
// footer key-hint bar (shared with the other hub pages).
func (p *InspectPage) View() string {
	return views.RenderWithChrome(
		views.ChromeConfig{
			Width:       p.w,
			Height:      p.h,
			Title:       titleInspect,
			KeyBindings: inspectBindings(),
		},
		func(contentHeight int) string {
			if !p.ready {
				return strings.TrimSpace(p.content)
			}
			p.vp.Height = contentHeight
			return p.vp.View()
		},
	)
}

// inspectBindings are the footer key hints for the Inspect page.
func inspectBindings() []views.KeyBinding {
	return []views.KeyBinding{
		{Keys: chatKeyLabelScrl, Description: keyHintScroll},
		{Keys: "g/G", Description: "top/bottom"},
		{Keys: chatKeyLabelEsc, Description: keyHintBack},
	}
}

// Title implements Page.
func (p *InspectPage) Title() string { return titleInspect }

// SetSize implements Page. Initializes (or re-initializes) the viewport.
func (p *InspectPage) SetSize(w, h int) {
	p.w, p.h = w, h
	p.renderContent()
	p.vp = viewport.New(w, h)
	p.vp.SetContent(p.content)
	p.ready = true
}
