package app

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// composerMode selects how a Composer presents the "send a turn" affordance.
type composerMode int

const (
	composerText   composerMode = iota // bordered text input box
	composerSpeech                     // mic indicator (voice mode)
)

// composerSpeechLabel is the voice-mode prompt shown in place of a text box.
const composerSpeechLabel = "🎤 mic active — speak to send a message"

// composerBorderChars is the horizontal space a rounded border adds (one column
// each side), subtracted so the box spans the available width.
const composerBorderChars = 2

// Composer renders the input affordance for an interactive conversation: a
// bordered text box (text mode) or a mic indicator (speech mode). It owns the
// presentation only — the host owns the underlying text-input model and its key
// handling — so the same component serves typed chat and voice chat.
type Composer struct {
	mode  composerMode
	width int
}

// NewTextComposer returns a Composer that renders a bordered text input box.
func NewTextComposer() *Composer { return &Composer{mode: composerText} }

// NewSpeechComposer returns a Composer that renders a mic indicator.
func NewSpeechComposer() *Composer { return &Composer{mode: composerSpeech} }

// SetWidth records the available width for the text box.
func (c *Composer) SetWidth(w int) { c.width = w }

// SetSpeech switches the composer between speech (true) and text (false) modes.
// Hosts call this so the affordance tracks their current voice state.
func (c *Composer) SetSpeech(speech bool) {
	if speech {
		c.mode = composerSpeech
	} else {
		c.mode = composerText
	}
}

// IsSpeech reports whether this composer is in voice mode.
func (c *Composer) IsSpeech() bool { return c.mode == composerSpeech }

// borderColor is the text box's border color: highlighted when the input holds
// focus, dimmed when the conversation panel does.
func (c *Composer) borderColor(focused bool) lipgloss.Color {
	if focused {
		return theme.BorderColorFocused()
	}
	return theme.BorderColorUnfocused()
}

// View renders the composer. In text mode it wraps inputView in a bordered box
// whose border reflects focus; in speech mode it renders the mic indicator
// (inputView and focused are ignored).
func (c *Composer) View(inputView string, focused bool) string {
	if c.mode == composerSpeech {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7dd3fc")). // sky-300 — matches theme
			Render(composerSpeechLabel)
	}
	width := max(c.width-composerBorderChars, 0)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c.borderColor(focused)).
		Width(width).
		Render(inputView)
}
