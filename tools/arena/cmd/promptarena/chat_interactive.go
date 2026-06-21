package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
)

func runChat(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	useMock, _ := cmd.Flags().GetBool("mock-provider")
	mockConfig, _ := cmd.Flags().GetString("mock-config")

	eng, err := engine.NewEngineFromConfigFile(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defer func() { _ = eng.Close() }()

	if useMock {
		if err := eng.EnableMockProviderMode(mockConfig); err != nil {
			return fmt.Errorf("enable mock provider: %w", err)
		}
	}

	page := pages.NewInteractiveChatPage(&engineChatAdapter{eng: eng})
	program := tea.NewProgram(&chatModel{page: page}, tea.WithAltScreen())
	_, runErr := program.Run()
	return runErr
}

// chatModel adapts the chat page to the tea.Model interface, routing window
// resize events to SetDimensions and forwarding all other messages to the page.
type chatModel struct {
	page *pages.InteractiveChatPage
}

func (m *chatModel) Init() tea.Cmd { return m.page.Init() }

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.page.SetDimensions(v.Width, v.Height)
		return m, nil
	case tea.KeyMsg:
		if v.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}
	return m, m.page.Update(msg)
}

func (m *chatModel) View() string { return m.page.View() }
