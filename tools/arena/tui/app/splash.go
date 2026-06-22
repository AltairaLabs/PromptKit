package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// splashDoneMsg is sent by the timer started in Init() to auto-dismiss the
// splash screen after ~1.5 seconds.
type splashDoneMsg struct{}

// pkLogo is the locked PromptKit ASCII art. Do not modify.
const pkLogo = `         ░▒░
          ████▓            ░█████████████████▓
          ▓█████▒          ▓██████████████████
           ▒█████▓         ▓██████████████████
             ▓█████▒       ▒██████████████████
              ▒█████▓        ░░░░░░░░░░░░░░░
                ▓█████▒     ▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒░
                 ▒█████▓   ▒██████████████████
                   █████▓  ▓██████████████████
                 ░██████   ▒██████████████████
                ▓█████▒     ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░
              ░██████░
             ▓█████▒       ▒██████████████████
           ░██████░        ▓██████████████████
          ▓█████▒          ▓██████████████████
          █████░           ▒██████████████████
           ▒▒░`

// splashDuration is the time before the splash auto-dismisses.
const splashDuration = 1500 * time.Millisecond

// Splash is the transient splash-screen Page shown at launch. It displays the
// locked PromptKit logo, a wordmark with the version, and a tagline. It
// dismisses on any key press or after splashDuration.
type Splash struct {
	ctx  *AppContext
	w, h int
}

// NewSplash creates a Splash page backed by the given AppContext. The version
// string from ctx is rendered on the screen; pass ctx.Version = "vTEST" (or
// similar fixed string) in golden tests to keep output byte-stable.
func NewSplash(ctx *AppContext) *Splash {
	return &Splash{ctx: ctx}
}

// Init implements Page. It starts a ~1.5-second timer that fires splashDoneMsg.
func (s *Splash) Init() tea.Cmd {
	return tea.Tick(splashDuration, func(time.Time) tea.Msg {
		return splashDoneMsg{}
	})
}

// Update implements Page. Any key press or the expiry timer dismisses the
// splash by returning a cmd that emits PopPageMsg{}.
func (s *Splash) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg, splashDoneMsg:
		return s, func() tea.Msg { return PopPageMsg{} }
	}
	return s, nil
}

// View implements Page. It renders the logo, wordmark, version, and tagline
// centered within the allocated terminal size.
func (s *Splash) View() string {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorIndigo)).
		Align(lipgloss.Left)

	logo := logoStyle.Render(pkLogo)

	wordmark := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(theme.ColorPrimary)).
		Render("P r o m p t K i t   ·   " + s.ctx.Version)

	tagline := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorLightGray)).
		Render("prompt testing, in your terminal")

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.ColorGray)).
		Render("press any key ▸")

	content := strings.Join([]string{logo, "", wordmark, tagline, "", hint}, "\n")

	return lipgloss.Place(s.w, s.h, lipgloss.Center, lipgloss.Center, content)
}

// Title implements Page. The splash has no title bar.
func (s *Splash) Title() string { return "" }

// SetSize implements Page.
func (s *Splash) SetSize(w, h int) {
	s.w, s.h = w, h
}
