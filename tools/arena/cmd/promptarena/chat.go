package main

import (
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/pages"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Chat interactively with an agent defined in an Arena config",
	RunE:  runChat,
}

func init() {
	chatCmd.Flags().String("config", "config.arena.yaml", "Path to the Arena config file")
	chatCmd.Flags().Bool("mock-provider", false, "Replace all providers with a generic mock")
	chatCmd.Flags().String("mock-config", "", "Mock response file (used with --mock-provider)")
	rootCmd.AddCommand(chatCmd)
}

// engineChatAdapter bridges *engine.Engine to the pages.ChatEngine interface.
// cmd/promptarena can import both engine and pages; the pages package cannot
// import engine directly (import cycle via engine's test dependencies).
type engineChatAdapter struct {
	eng *engine.Engine
}

func (a *engineChatAdapter) Agents() []pages.AgentOption {
	infos := a.eng.Agents()
	out := make([]pages.AgentOption, len(infos))
	for i, info := range infos {
		out[i] = pages.AgentOption{TaskType: info.TaskType, Description: info.Description}
	}
	return out
}

func (a *engineChatAdapter) ProviderIDs() []string {
	return a.eng.ProviderIDs()
}

func (a *engineChatAdapter) MissingRequiredVars(taskType string, provided map[string]string) ([]string, error) {
	return a.eng.MissingRequiredVars(taskType, provided)
}

func (a *engineChatAdapter) HasConfigEvals() bool {
	return a.eng.HasConfigEvals()
}

func (a *engineChatAdapter) NewChatSession(opts pages.ChatSessionOptions) (pages.ChatSession, error) {
	sess, err := a.eng.NewInteractiveSession(engine.InteractiveSessionOptions{
		ProviderID: opts.ProviderID,
		TaskType:   opts.TaskType,
		Variables:  opts.Variables,
		RunEvals:   opts.RunEvals,
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}
