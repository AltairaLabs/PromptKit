package app

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	tea "github.com/charmbracelet/bubbletea"
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
		ctx := AppContext{Config: &config.Config{}}
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
