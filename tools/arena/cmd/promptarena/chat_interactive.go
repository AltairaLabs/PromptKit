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
	useVoice, _ := cmd.Flags().GetBool("voice")
	voiceSTT, _ := cmd.Flags().GetString("voice-stt")
	voiceOutputVoice, _ := cmd.Flags().GetString("voice-output-voice")
	echoGuard, _ := cmd.Flags().GetBool("echo-guard")
	bargeIn, _ := cmd.Flags().GetBool("barge-in")
	verbose, _ := cmd.Flags().GetBool("verbose")

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
		// With -v, raise the TUI log interceptor to debug and tee to
		// <cwd>/promptarena.log so the realtime event timeline (otherwise hidden
		// behind the TUI) is inspectable. Without LogDir, debug logs would only
		// reach the in-TUI log panel.
		Verbose: verbose,
		LogDir:  ".",
	}

	// Auto-enable a live interactive session when the config describes a realtime
	// (duplex ASM/VAD) pipeline; --voice forces one on for any config.
	voiceOpts := app.DetectInteractiveSession(eng.GetConfig())
	if voiceOpts == nil && useVoice {
		voiceOpts = &app.VoiceOptions{}
	}
	if voiceOpts != nil {
		// CLI flags override / augment whatever the config implied.
		if voiceSTT != "" {
			voiceOpts.STTProviderID = voiceSTT
		}
		if voiceOutputVoice != "" {
			voiceOpts.OutputVoice = voiceOutputVoice
		}
		voiceOpts.EchoGuard = echoGuard
		voiceOpts.BargeIn = bargeIn
		appCtx.Voice = voiceOpts
	}

	return app.Run(appCtx, app.NewChatPage(appCtx))
}
