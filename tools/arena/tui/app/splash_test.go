package app

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// ansiPattern matches ANSI/VT escape sequences.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// goldenSplashSizes is the size matrix for splash golden snapshots.
var goldenSplashSizes = []struct {
	name string
	w, h int
}{
	{"80x24", 80, 24},
	{"120x40", 120, 40},
}

func TestSplash_DismissOnKey(t *testing.T) {
	s := NewSplash(&AppContext{Version: "vTEST"})
	s.SetSize(80, 24)
	// Init must return a non-nil timer cmd.
	initCmd := s.Init()
	if initCmd == nil {
		t.Fatal("Init() returned nil cmd, expected a timer cmd")
	}

	// Any key message should return a cmd that resolves to PopPageMsg{}.
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Update(KeyMsg) returned nil cmd, expected cmd emitting PopPageMsg{}")
	}
	msg := cmd()
	if _, ok := msg.(PopPageMsg); !ok {
		t.Fatalf("expected PopPageMsg{}, got %T %v", msg, msg)
	}
}

func TestSplash_DismissOnSplashDone(t *testing.T) {
	s := NewSplash(&AppContext{Version: "vTEST"})
	s.SetSize(80, 24)

	_, cmd := s.Update(splashDoneMsg{})
	if cmd == nil {
		t.Fatal("Update(splashDoneMsg) returned nil cmd, expected cmd emitting PopPageMsg{}")
	}
	msg := cmd()
	if _, ok := msg.(PopPageMsg); !ok {
		t.Fatalf("expected PopPageMsg{}, got %T %v", msg, msg)
	}
}

func TestSplash_View_ContainsLogo(t *testing.T) {
	s := NewSplash(&AppContext{Version: "vTEST"})
	s.SetSize(80, 24)
	view := stripANSI(s.View())

	// A recognizable substring from the locked logo art (second line of art).
	const logoRow = "████▓"
	if !strings.Contains(view, logoRow) {
		t.Errorf("View does not contain expected logo row %q", logoRow)
	}

	// Tagline must be present.
	const tagline = "prompt testing, in your terminal"
	if !strings.Contains(view, tagline) {
		t.Errorf("View does not contain tagline %q", tagline)
	}

	// Version must be present.
	if !strings.Contains(view, "vTEST") {
		t.Errorf("View does not contain version %q", "vTEST")
	}

	// Press any key hint must be present.
	if !strings.Contains(view, "press any key") {
		t.Errorf("View does not contain press-any-key hint")
	}
}

func TestSplash_Title(t *testing.T) {
	s := NewSplash(&AppContext{Version: "vTEST"})
	if got := s.Title(); got != "" {
		t.Fatalf("Title() = %q, want empty string", got)
	}
}

func TestSplash_SetSize(t *testing.T) {
	s := NewSplash(&AppContext{Version: "vTEST"})
	s.SetSize(120, 40)
	if s.w != 120 || s.h != 40 {
		t.Fatalf("SetSize(120,40): got w=%d h=%d", s.w, s.h)
	}
}

func TestGoldenSplash(t *testing.T) {
	for _, sz := range goldenSplashSizes {
		t.Run(sz.name, func(t *testing.T) {
			s := NewSplash(&AppContext{Version: "vTEST"})
			s.SetSize(sz.w, sz.h)
			out := stripANSI(s.View())
			teatest.RequireEqualOutput(t, []byte(out))
		})
	}
}
