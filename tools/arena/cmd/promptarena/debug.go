package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug configuration and prompt loading",
	Long: `Debug command shows loaded configuration, prompt packs, scenarios, 
and providers to help troubleshoot configuration issues.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDebug(cmd)
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)

	// Configuration file
	debugCmd.Flags().StringP("config", "c", "config.arena.yaml", "Configuration file path")
}

func runDebug(cmd *cobra.Command) error {
	configFile, err := getConfigFile(cmd)
	if err != nil {
		return err
	}

	printDebugHeader(configFile)

	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	printConfigOverview(cfg)
	printScenarios(cfg.LoadedScenarios)
	printProviders(cfg.LoadedProviders)

	fmt.Printf("\n✅ Debug complete!\n")
	return nil
}

// getConfigFile retrieves and validates the config file path
func getConfigFile(cmd *cobra.Command) (string, error) {
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", fmt.Errorf("failed to get config flag: %w", err)
	}

	// If config path is a directory, append arena.yaml
	if info, _ := os.Stat(configFile); info != nil && info.IsDir() {
		configFile = filepath.Join(configFile, "arena.yaml")
	}

	return configFile, nil
}

// printDebugHeader prints the debug mode header
func printDebugHeader(configFile string) {
	fmt.Printf("🔍 Altaira Prompt Arena - Debug Mode\n")
	fmt.Printf("=====================================\n")
	fmt.Printf("Config file: %s\n\n", configFile)
}

// printConfigOverview prints the configuration overview section
func printConfigOverview(cfg *config.Config) {
	fmt.Printf("📋 Configuration Overview\n")
	fmt.Printf("--------------------------\n")
	fmt.Printf("Prompt Configs: %d\n", len(cfg.PromptConfigs))
	fmt.Printf("Providers: %d\n", len(cfg.Providers))
	fmt.Printf("Scenarios: %d\n", len(cfg.Scenarios))
	fmt.Printf("Default Temperature: %.1f\n", cfg.Defaults.Temperature)
	fmt.Printf("Default Max Tokens: %d\n", cfg.Defaults.MaxTokens)
	fmt.Printf("Default Seed: %d\n", cfg.Defaults.Seed)
	fmt.Printf("Default Concurrency: %d\n", cfg.Defaults.Concurrency)
	fmt.Printf("\n")
}

// printScenarios prints all loaded scenarios
func printScenarios(scenarios map[string]*config.Scenario) {
	if len(scenarios) == 0 {
		return
	}

	fmt.Printf("🎬 Scenarios\n")
	fmt.Printf("-------------\n")
	for id, scenario := range scenarios {
		printScenarioDetails(id, *scenario)
	}
}

// printScenarioDetails prints details for a single scenario
func printScenarioDetails(id string, scenario config.Scenario) {
	fmt.Printf("ID: %s\n", id)
	fmt.Printf("Task Type: %s\n", scenario.TaskType)
	if scenario.Mode != "" {
		fmt.Printf("Mode: %s\n", scenario.Mode)
	}
	fmt.Printf("Description: %s\n", scenario.Description)
	fmt.Printf("Turns: %d\n", len(scenario.Turns))

	if len(scenario.Constraints) > 0 {
		fmt.Printf("Constraints: %v\n", getConstraintKeys(scenario.Constraints))
	}

	fmt.Printf("\n")
}

// printProviders prints all loaded providers
func printProviders(providers map[string]*config.Provider) {
	if len(providers) == 0 {
		return
	}

	fmt.Printf("🔌 Providers\n")
	fmt.Printf("-------------\n")
	for id, provider := range providers {
		printProviderDetails(id, *provider)
	}
}

// printProviderDetails prints details for a single provider
func printProviderDetails(id string, provider config.Provider) {
	fmt.Printf("ID: %s\n", id)
	fmt.Printf("Type: %s\n", provider.Type)
	fmt.Printf("Model: %s\n", provider.Model)
	fmt.Printf("Base URL: %s\n", provider.BaseURL)
	fmt.Printf("Rate Limit: %d rps, %d burst\n", provider.RateLimit.RPS, provider.RateLimit.Burst)
	fmt.Printf("Defaults: temp=%.1f, top_p=%.1f, max_tokens=%d\n",
		provider.Defaults.Temperature, provider.Defaults.TopP, provider.Defaults.MaxTokens)
	fmt.Printf("\n")
}

func getWrapperKeys(wrappers map[string]interface{}) []string {
	keys := make([]string, 0, len(wrappers))
	for k := range wrappers {
		keys = append(keys, k)
	}
	return keys
}

func getConstraintKeys(constraints map[string]interface{}) []string {
	keys := make([]string, 0, len(constraints))
	for k := range constraints {
		keys = append(keys, k)
	}
	return keys
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
