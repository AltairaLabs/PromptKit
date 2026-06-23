package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/app"
)

// runChat is the cobra RunE handler for the `chat` command.
// It builds an AppContext around the loaded engine and launches the TUI hub
// with ChatPage as the root, so the user arrives directly in the chat console.
// The hub's Esc/q binding exits back to the shell.
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

	appCtx := &app.AppContext{
		Config:     eng.GetConfig(),
		ConfigPath: configPath,
		ResultsDir: app.ResultsDirFromConfig(configPath),
		Engine:     eng,
		StateStore: eng.GetStateStore(),
		Version:    GetVersion(),
	}

	return app.Run(appCtx, app.NewChatPage(appCtx))
}
