package main

import (
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "promptarena",
	Short: "PromptKit Arena - Multi-turn conversation simulation and testing tool",
	Long: `PromptKit Arena is a testing framework for running multi-turn conversation 
simulations across multiple LLM providers and system prompts.

It evaluates tone consistency, content quality, prompt adherence, and validates
conversation flows for various task types (customer support, code assistance, etc.).`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize logger based on verbose flag if present
		// This runs before all subcommands
		if cmd.Flags().Changed("verbose") {
			verbose, err := cmd.Flags().GetBool("verbose")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error getting verbose flag: %v\n", err)
				return
			}
			logger.SetVerbose(verbose)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
