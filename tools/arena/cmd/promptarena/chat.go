package main

import "github.com/spf13/cobra"

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Chat interactively with an agent defined in an Arena config",
	RunE:  runChat,
}

func init() {
	chatCmd.Flags().String("config", "config.arena.yaml", "Path to the Arena config file")
	chatCmd.Flags().Bool("mock-provider", false, "Replace all providers with a generic mock")
	chatCmd.Flags().String("mock-config", "", "Mock response file (used with --mock-provider)")
	chatCmd.Flags().Bool("voice", false, "Hands-free voice mode (requires PortAudio installed)")
	chatCmd.Flags().String("voice-stt", "", "STT provider id for voice over text agents (VAD mode)")
	chatCmd.Flags().String("voice-output-voice", "", "TTS voice id the agent speaks in (VAD mode)")
	chatCmd.Flags().Bool("echo-guard", false,
		"Gate mic while the agent speaks (for laptop speakers; default off assumes headphones)")
	chatCmd.Flags().Bool("barge-in", false,
		"Allow interrupting the agent mid-reply (opt-in; needs headphones/AEC, off by default)")
	chatCmd.Flags().BoolP("verbose", "v", false, "Enable verbose debug logging (raw provider events, transcription)")
	rootCmd.AddCommand(chatCmd)
}
