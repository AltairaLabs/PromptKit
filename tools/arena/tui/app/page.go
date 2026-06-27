// Package app defines the core types for the PromptArena TUI hub shell: the
// Page interface that every screen implements, AppContext that carries shared
// runtime dependencies, and the navigation messages used to push/pop pages or
// signal quit and config-change events.
package app

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// Page is the interface that every screen in the TUI hub must implement.
// It mirrors the bubbletea Model interface but returns (Page, tea.Cmd) from
// Update so that the shell can swap the active page in response to navigation
// messages.
type Page interface {
	Init() tea.Cmd
	Update(tea.Msg) (Page, tea.Cmd)
	View() string
	Title() string
	SetSize(w, h int)
}

// Closeable is an optional interface a Page may implement to run cleanup logic
// when the App exits (e.g. canceling a background voice driver). App calls
// Close on every page in the stack when it processes a tea.Quit or QuitMsg.
type Closeable interface {
	Close()
}

// VoiceOptions carries the interactive-session parameters. A nil *VoiceOptions
// on AppContext means plain text chat; non-nil means a live mic/speaker session.
type VoiceOptions struct {
	STTProviderID     string // --voice-stt ("" = ASM/native realtime mode)
	OutputVoice       string // --voice-output-voice
	EchoGuard         bool   // --echo-guard
	BargeIn           bool   // --barge-in (interrupt the agent mid-reply; opt-in)
	TurnDetectionMode string // "asm" | "vad"; "" defaults to ASM
}

// DetectInteractiveSession inspects a loaded config and, if it describes a
// realtime pipeline, returns VoiceOptions configured to honor its turn-detection
// mode. Two signals count: a scenario with an explicit Duplex block (honors its
// ASM/VAD mode), or a native-realtime provider (additional_config.realtime:true,
// e.g. OpenAI Realtime — ASM). It returns nil for plain text-chat configs, so
// `chat` lights up a live mic/speaker session automatically whenever the config
// calls for one — no --voice flag needed.
func DetectInteractiveSession(cfg *config.Config) *VoiceOptions {
	if cfg == nil {
		return nil
	}
	// 1. A scenario with a duplex block → honor its declared turn-detection mode.
	for _, id := range sortedKeys(cfg.LoadedScenarios) {
		s := cfg.LoadedScenarios[id]
		if s == nil || s.Duplex == nil {
			continue
		}
		opts := &VoiceOptions{}
		if td := s.Duplex.TurnDetection; td != nil {
			opts.TurnDetectionMode = td.Mode
		}
		return opts
	}
	// 2. A native-realtime provider (server-side turn detection) → ASM. This
	// covers scenario-less voice-console configs whose realtime intent lives on
	// the provider rather than a scenario.
	for _, id := range sortedKeys(cfg.LoadedProviders) {
		if isRealtimeProvider(cfg.LoadedProviders[id]) {
			return &VoiceOptions{TurnDetectionMode: config.TurnDetectionModeASM}
		}
	}
	return nil
}

// isRealtimeProvider reports whether a provider declares native realtime audio
// (additional_config.realtime: true).
func isRealtimeProvider(p *config.Provider) bool {
	if p == nil {
		return false
	}
	switch v := p.AdditionalConfig["realtime"].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

// sortedKeys returns the keys of m in deterministic order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AppContext carries the shared runtime dependencies injected into every Page
// by the hub shell. Fields are set once at startup and then treated as
// read-only by pages.
//
//nolint:revive // AppContext is the intended public name; callers reference it as app.AppContext which is unambiguous.
type AppContext struct {
	Config     *config.Config
	ConfigPath string
	ResultsDir string
	StateStore statestore.Store
	Engine     *engine.Engine
	Version    string
	Voice      *VoiceOptions // nil => text chat

	// Verbose raises the hub's log interceptor to debug level and (with
	// LogDir set) tees logs to <LogDir>/promptarena.log. LogDir is the
	// directory for that file — usually the run's output dir.
	Verbose bool
	LogDir  string
}

// HasConfig reports whether a config has been loaded into this context.
func (c *AppContext) HasConfig() bool { return c.Config != nil }

// PushPageMsg instructs the hub shell to push a new page onto the navigation
// stack, making it the active page.
type PushPageMsg struct{ Page Page }

// PopPageMsg instructs the hub shell to pop the current page, returning to
// the previous one.
type PopPageMsg struct{}

// QuitMsg instructs the hub shell to exit the TUI.
type QuitMsg struct{}

// ConfigChangedMsg is emitted when the user loads or changes the arena config
// file. Path is the absolute path to the newly loaded file.
type ConfigChangedMsg struct{ Path string }
