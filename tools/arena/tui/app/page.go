// Package app defines the core types for the PromptArena TUI hub shell: the
// Page interface that every screen implements, AppContext that carries shared
// runtime dependencies, and the navigation messages used to push/pop pages or
// signal quit and config-change events.
package app

import (
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

// VoiceOptions carries the voice-mode parameters parsed from CLI flags.
// A nil *VoiceOptions on AppContext means text-chat mode.
type VoiceOptions struct {
	STTProviderID string // --voice-stt ("" = ASM/native realtime mode)
	OutputVoice   string // --voice-output-voice
	EchoGuard     bool   // --echo-guard
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
