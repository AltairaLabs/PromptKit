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
	rootCmd.AddCommand(chatCmd)
}
