package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/inspect"
)

// TestInspectPage_TitleIsInspect verifies Title() returns "Inspect".
func TestInspectPage_TitleIsInspect(t *testing.T) {
	p := NewInspectPage(nil)
	if got := p.Title(); got != "Inspect" {
		t.Fatalf("Title() = %q, want %q", got, "Inspect")
	}
}

// TestInspectPage_NilCtxRendersPlaceholder verifies that a nil AppContext
// produces a non-empty View with a placeholder message.
func TestInspectPage_NilCtxRendersPlaceholder(t *testing.T) {
	p := NewInspectPage(nil)
	p.SetSize(80, 24) // chrome renders nothing until sized
	view := p.View()
	if view == "" {
		t.Fatal("View() returned empty string for nil ctx")
	}
	if !strings.Contains(view, "No configuration loaded") {
		t.Fatalf("expected placeholder text, got: %q", view)
	}
}

// TestInspectPage_SetSizeEnablesViewport verifies that SetSize causes View()
// to use the viewport path and return a non-empty string.
func TestInspectPage_SetSizeEnablesViewport(t *testing.T) {
	p := NewInspectPage(nil)
	p.SetSize(80, 24)

	if !p.ready {
		t.Fatal("ready should be true after SetSize")
	}
	view := p.View()
	if view == "" {
		t.Fatal("View() returned empty string after SetSize")
	}
}

// TestInspectPage_InitReturnsNil verifies Init() does not panic and returns nil.
func TestInspectPage_InitReturnsNil(t *testing.T) {
	p := NewInspectPage(nil)
	cmd := p.Init()
	if cmd != nil {
		t.Fatal("Init() should return nil cmd")
	}
}

// TestInspectPage_UpdateReturnsSelf verifies Update returns a non-nil page.
func TestInspectPage_UpdateReturnsSelf(t *testing.T) {
	p := NewInspectPage(nil)
	p.SetSize(80, 24)
	newPage, _ := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if newPage == nil {
		t.Fatal("Update returned nil page")
	}
}

// TestInspectPage_UpdateBeforeSetSize verifies Update before SetSize does not panic.
func TestInspectPage_UpdateBeforeSetSize(t *testing.T) {
	p := NewInspectPage(nil)
	newPage, cmd := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if newPage == nil {
		t.Fatal("Update before SetSize returned nil page")
	}
	if cmd != nil {
		t.Fatal("Update before SetSize should return nil cmd")
	}
}

// TestInspectPage_WithNilInCtx verifies NewInspectPage with a non-nil AppContext
// but nil Config falls back to placeholder.
func TestInspectPage_WithNilInCtx(t *testing.T) {
	ctx := &AppContext{
		Config:     nil, // config.Config is a pointer — nil means no config
		ConfigPath: "",
	}
	p := NewInspectPage(ctx)
	p.SetSize(120, 40)
	view := p.View()
	if view == "" {
		t.Fatal("View() returned empty string")
	}
}

// TestInspectPage_WithRealConfig verifies NewInspectPage with a real *config.Config
// exercises the collect+render path without panic.
func TestInspectPage_WithRealConfig(t *testing.T) {
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{},
		LoadedProviders:     map[string]*config.Provider{},
		LoadedScenarios:     map[string]*config.Scenario{},
		LoadedPersonas:      map[string]*config.UserPersonaPack{},
		LoadedJudges:        map[string]*config.JudgeTarget{},
		LoadedTools:         []config.ToolData{},
	}
	ctx := &AppContext{
		Config:     cfg,
		ConfigPath: "/tmp/test.arena.yaml",
	}
	p := NewInspectPage(ctx)
	p.SetSize(80, 24)
	view := p.View()
	if view == "" {
		t.Fatal("View() returned empty string for real config")
	}
}

// TestInspectPage_ScrollKeys verifies scroll key bindings do not panic.
func TestInspectPage_ScrollKeys(t *testing.T) {
	keys := []string{"up", "k", "down", "j", "pgup", "pgdown", "g", "G"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			p := NewInspectPage(nil)
			p.SetSize(80, 24)
			newPage, cmd := p.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune(key),
			})
			if newPage == nil {
				t.Fatalf("Update(%q) returned nil page", key)
			}
			if cmd != nil {
				t.Fatalf("Update(%q) returned non-nil cmd", key)
			}
		})
	}
}

// realConfigForInspect returns a minimal *AppContext suitable for render tests.
func realConfigForInspect() (*AppContext, string) {
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{
			"chat": {TaskType: "chat"},
		},
		LoadedProviders: map[string]*config.Provider{
			"default": {Type: "anthropic"},
		},
		LoadedScenarios: map[string]*config.Scenario{},
		LoadedPersonas:  map[string]*config.UserPersonaPack{},
		LoadedJudges:    map[string]*config.JudgeTarget{},
		LoadedTools:     []config.ToolData{},
	}
	ctx := &AppContext{Config: cfg, ConfigPath: "/tmp/test.arena.yaml"}
	return ctx, "/tmp/test.arena.yaml"
}

// TestInspectPage_WithOptionsThreaded verifies that NewInspectPageWithOptions
// threads the RenderOptions into the rendered content. It compares the page
// content to a direct inspect.RenderText call with the same options, confirming
// that opts are not silently dropped (I2 fix — options wired through to renderer).
func TestInspectPage_WithOptionsThreaded(t *testing.T) {
	ctx, _ := realConfigForInspect()

	// Build the page with Section="validation" so only the validation block renders.
	opts := inspect.RenderOptions{Section: "validation"}
	page := NewInspectPageWithOptions(ctx, opts)

	// Build expected content the same way the page constructor does.
	data := inspect.CollectInspectionData(ctx.Config, ctx.ConfigPath)
	inspect.PopulateValidation(data, ctx.Config, ctx.ConfigPath)
	want := inspect.RenderText(data, opts)

	if page.content != want {
		t.Fatalf("page content does not match direct RenderText call with same opts:\ngot:  %q\nwant: %q",
			page.content, want)
	}
}

// TestInspectPage_WithOptionsSectionFilters verifies that passing Section="validation"
// produces output that differs from (and is a subset of) the full render (I2 fix).
func TestInspectPage_WithOptionsSectionFilters(t *testing.T) {
	ctx, _ := realConfigForInspect()

	full := NewInspectPageWithOptions(ctx, inspect.RenderOptions{})
	sectioned := NewInspectPageWithOptions(ctx, inspect.RenderOptions{Section: "validation"})

	if full.content == sectioned.content {
		t.Fatal("expected Section=validation to produce different content than full render")
	}
	if !strings.Contains(sectioned.content, "Validation") {
		t.Fatalf("expected Section=validation output to contain 'Validation'; got:\n%s", sectioned.content)
	}
}

// TestInspectPage_NewInspectPageDefaultsToNoOptions verifies the convenience
// constructor is equivalent to WithOptions(..., RenderOptions{}).
func TestInspectPage_NewInspectPageDefaultsToNoOptions(t *testing.T) {
	ctx, _ := realConfigForInspect()

	plain := NewInspectPage(ctx)
	explicit := NewInspectPageWithOptions(ctx, inspect.RenderOptions{})

	if plain.content != explicit.content {
		t.Fatal("NewInspectPage and NewInspectPageWithOptions with zero opts must produce identical content")
	}
}
