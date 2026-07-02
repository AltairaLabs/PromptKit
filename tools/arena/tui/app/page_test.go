package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

// fakePage is a minimal Page implementation used to verify the contract.
type fakePage struct{}

func (f fakePage) Init() tea.Cmd                  { return nil }
func (f fakePage) Update(tea.Msg) (Page, tea.Cmd) { return f, nil }
func (f fakePage) View() string                   { return "" }
func (f fakePage) Title() string                  { return "fake" }
func (f fakePage) SetSize(_, _ int)               {}

func TestPageContract(t *testing.T) {
	t.Run("AppContext HasConfig false when nil", func(t *testing.T) {
		ctx := AppContext{}
		if ctx.HasConfig() {
			t.Fatal("expected HasConfig() == false for zero AppContext")
		}
	})

	t.Run("AppContext HasConfig true when set", func(t *testing.T) {
		// We only need a non-nil pointer; the concrete value does not matter here.
		ctx := AppContext{Config: &arenaconfig.Config{}}
		if !ctx.HasConfig() {
			t.Fatal("expected HasConfig() == true after setting Config")
		}
	})

	t.Run("PushPageMsg carries the page", func(t *testing.T) {
		page := fakePage{}
		msg := PushPageMsg{Page: page}
		if msg.Page == nil {
			t.Fatal("expected PushPageMsg.Page to be non-nil")
		}
		if _, ok := msg.Page.(fakePage); !ok {
			t.Fatalf("expected PushPageMsg.Page to be fakePage, got %T", msg.Page)
		}
	})
}

func TestDetectInteractiveSession(t *testing.T) {
	if got := DetectInteractiveSession(nil); got != nil {
		t.Fatalf("nil config should yield no session, got %+v", got)
	}

	textOnly := &arenaconfig.Config{LoadedScenarios: map[string]*arenaconfig.Scenario{
		"plain": {ID: "plain"},
	}}
	if got := DetectInteractiveSession(textOnly); got != nil {
		t.Fatalf("text-only config should yield no session, got %+v", got)
	}

	realtime := &arenaconfig.Config{LoadedScenarios: map[string]*arenaconfig.Scenario{
		"voice": {ID: "voice", Duplex: &arenaconfig.DuplexConfig{
			TurnDetection: &arenaconfig.TurnDetectionConfig{Mode: arenaconfig.TurnDetectionModeVAD},
		}},
	}}
	got := DetectInteractiveSession(realtime)
	if got == nil {
		t.Fatal("realtime config should enable an interactive session")
	}
	if got.TurnDetectionMode != arenaconfig.TurnDetectionModeVAD {
		t.Fatalf("TurnDetectionMode = %q, want %q", got.TurnDetectionMode, arenaconfig.TurnDetectionModeVAD)
	}
}

func TestDetectInteractiveSession_RealtimeProvider(t *testing.T) {
	// A scenario-less config whose realtime intent lives on the provider.
	cfg := &arenaconfig.Config{LoadedProviders: map[string]*config.Provider{
		"openai-realtime": {Type: "openai", AdditionalConfig: map[string]interface{}{"realtime": true}},
	}}
	got := DetectInteractiveSession(cfg)
	if got == nil {
		t.Fatal("realtime provider should enable an interactive session")
	}
	if got.TurnDetectionMode != arenaconfig.TurnDetectionModeASM {
		t.Fatalf("TurnDetectionMode = %q, want %q", got.TurnDetectionMode, arenaconfig.TurnDetectionModeASM)
	}

	// A plain provider must not trigger one.
	plain := &arenaconfig.Config{LoadedProviders: map[string]*config.Provider{
		"openai": {Type: "openai"},
	}}
	if DetectInteractiveSession(plain) != nil {
		t.Fatal("non-realtime provider should stay text chat")
	}
}
