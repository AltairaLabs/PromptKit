package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/spf13/cobra"
)

var promptDebugCmd = &cobra.Command{
	Use:   "prompt-debug",
	Short: "Debug and test prompt generation",
	Long: `Prompt debug command allows testing prompt generation with specific regions, 
task types, and contexts. Similar to the backend prompt-cli but for prompt-arena 
system prompt building and testing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPromptDebug(cmd)
	},
}

func init() {
	rootCmd.AddCommand(promptDebugCmd)

	// Configuration
	promptDebugCmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path")
	promptDebugCmd.Flags().StringP("scenario", "", "", "Scenario file path to load task_type and context from")

	// Prompt parameters
	promptDebugCmd.Flags().StringP("region", "r", "", "Region for prompt generation")
	promptDebugCmd.Flags().StringP("task-type", "t", "", "Task type for prompt generation")
	promptDebugCmd.Flags().StringP("persona", "", "", "Persona ID to test (e.g., 'us-hustler-v1')")
	promptDebugCmd.Flags().StringP("context", "", "", "Context slot content")
	promptDebugCmd.Flags().StringP("domain", "", "", "Domain hint (e.g., 'mobile development')")
	promptDebugCmd.Flags().StringP("user", "", "", "User context (e.g., 'iOS developer')")

	// Output control
	promptDebugCmd.Flags().BoolP("show-prompt", "p", true, "Show the full assembled prompt")
	promptDebugCmd.Flags().BoolP("show-meta", "m", true, "Show metadata and configuration info")
	promptDebugCmd.Flags().BoolP("show-stats", "s", true, "Show statistics (length, tokens, etc.)")
	promptDebugCmd.Flags().BoolP("json", "j", false, "Output as JSON")
	promptDebugCmd.Flags().BoolP("list", "l", false, "List available regions and task types")
	promptDebugCmd.Flags().BoolP("verbose", "v", false, "Verbose output with debug info")
}

func runPromptDebug(cmd *cobra.Command) error {
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to get config flag: %w", err)
	}
	scenarioFile, err := cmd.Flags().GetString("scenario")
	if err != nil {
		return fmt.Errorf("failed to get scenario flag: %w", err)
	}

	// If config path is a directory, append arena.yaml
	if info, err := os.Stat(configFile); err == nil && info.IsDir() {
		configFile = filepath.Join(configFile, "arena.yaml")
	}

	region, err := cmd.Flags().GetString("region")
	if err != nil {
		return fmt.Errorf("failed to get region flag: %w", err)
	}
	taskType, err := cmd.Flags().GetString("task-type")
	if err != nil {
		return fmt.Errorf("failed to get task-type flag: %w", err)
	}
	persona, err := cmd.Flags().GetString("persona")
	if err != nil {
		return fmt.Errorf("failed to get persona flag: %w", err)
	}
	context, err := cmd.Flags().GetString("context")
	if err != nil {
		return fmt.Errorf("failed to get context flag: %w", err)
	}
	domain, err := cmd.Flags().GetString("domain")
	if err != nil {
		return fmt.Errorf("failed to get domain flag: %w", err)
	}
	user, err := cmd.Flags().GetString("user")
	if err != nil {
		return fmt.Errorf("failed to get user flag: %w", err)
	}
	showPrompt, err := cmd.Flags().GetBool("show-prompt")
	if err != nil {
		return fmt.Errorf("failed to get show-prompt flag: %w", err)
	}
	showMeta, err := cmd.Flags().GetBool("show-meta")
	if err != nil {
		return fmt.Errorf("failed to get show-meta flag: %w", err)
	}
	showStats, err := cmd.Flags().GetBool("show-stats")
	if err != nil {
		return fmt.Errorf("failed to get show-stats flag: %w", err)
	}
	outputJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("failed to get json flag: %w", err)
	}
	listConfigs, err := cmd.Flags().GetBool("list")
	if err != nil {
		return fmt.Errorf("failed to get list flag: %w", err)
	}
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("failed to get verbose flag: %w", err)
	}

	// Load configuration
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// If scenario file is specified, load it and use its task_type and context
	if scenarioFile != "" {
		scenario, err := config.LoadScenario(scenarioFile)
		if err != nil {
			return fmt.Errorf("failed to load scenario file %s: %w", scenarioFile, err)
		}

		// Override task_type with scenario's task_type
		taskType = scenario.TaskType

		// Override context/domain/user with scenario data if not provided via flags
		if domain == "" && scenario.ContextMetadata != nil && scenario.ContextMetadata.Domain != "" {
			domain = scenario.ContextMetadata.Domain
		}
		if user == "" && scenario.ContextMetadata != nil && scenario.ContextMetadata.UserRole != "" {
			user = scenario.ContextMetadata.UserRole
		}

		// Try to get context from the general context map
		if context == "" && scenario.Context != nil {
			if contextValue, exists := scenario.Context["user_context"]; exists {
				if contextStr, ok := contextValue.(string); ok {
					context = contextStr
				}
			}
		}

		if verbose {
			fmt.Printf("📄 Loaded scenario from: %s\n", scenarioFile)
			fmt.Printf("   Task Type: %s\n", taskType)
			if scenario.ContextMetadata != nil {
				fmt.Printf("   Domain: %s\n", domain)
				fmt.Printf("   User Role: %s\n", user)
			}
			fmt.Printf("   Context: %s\n", context)
			fmt.Printf("\n")
		}
	}

	// If list mode, load config and show available options
	if listConfigs {
		fmt.Printf("📋 Available Prompt Configurations\n")
		fmt.Printf("===================================\n")

		if len(cfg.PromptConfigs) == 0 {
			fmt.Printf("No prompt configs loaded.\n")
			return nil
		}

		for _, promptConfig := range cfg.PromptConfigs {
			fmt.Printf("🎯 Prompt Config: %s (File: %s)\n", promptConfig.ID, promptConfig.File)
			fmt.Println()
		}

		// Collect unique task types from loaded prompt configs
		if len(cfg.LoadedPromptConfigs) > 0 {
			fmt.Printf("📝 Task Types:\n")
			for taskType := range cfg.LoadedPromptConfigs {
				fmt.Printf("   • %s\n", taskType)
			}
			fmt.Println()
		}

		// Show available personas
		if len(cfg.LoadedPersonas) > 0 {
			fmt.Printf("👤 Available Personas:\n")
			for id, persona := range cfg.LoadedPersonas {
				fmt.Printf("   • %s (activity: %s)\n", id, persona.PromptActivity)
			}
			fmt.Println()
		}

		fmt.Printf("💡 Example Usage:\n")
		fmt.Printf("   ./bin/promptarena prompt-debug --region=us --task-type=support\n")
		fmt.Printf("   ./bin/promptarena prompt-debug --region=uk --task-type=assistant --context=\"Building a mobile app\"\n")
		fmt.Printf("   ./bin/promptarena prompt-debug --persona=support-agent --context=\"Customer service scenario\"\n")
		fmt.Printf("   ./bin/promptarena prompt-debug --json --show-prompt=false\n")

		return nil
	}

	// Enable verbose debugging if requested
	if verbose {
		fmt.Printf("🔍 Prompt Debug Mode - Verbose Enabled\n")
		if persona != "" {
			fmt.Printf("Config: %s, Persona: %s\n\n", configFile, persona)
		} else {
			fmt.Printf("Config: %s, Region: %s, Task: %s\n\n", configFile, region, taskType)
		}
	}

	var systemPrompt string
	var promptType string
	variables := make(map[string]string)

	// Build the system prompt - either persona or region/task-type
	if persona != "" {
		// Use LoadedPersonas from config
		personaPack, exists := cfg.LoadedPersonas[persona]
		if !exists {
			availablePersonas := make([]string, 0, len(cfg.LoadedPersonas))
			for id := range cfg.LoadedPersonas {
				availablePersonas = append(availablePersonas, id)
			}
			return fmt.Errorf("persona '%s' not found. Available personas: %s", persona, strings.Join(availablePersonas, ", "))
		}

		// Build variables for persona prompt
		if context != "" {
			variables["context_slot"] = context
		} else {
			variables["context_slot"] = "General conversation context"
		}
		if domain != "" {
			variables["domain_hint"] = domain
		} else {
			variables["domain_hint"] = "general"
		}
		if user != "" {
			variables["user_context"] = user
		} else {
			variables["user_context"] = "general user"
		}

		// Extract region from persona ID (e.g., "us-hustler-v1" -> "us")
		personaRegion := "us" // default
		if strings.Contains(persona, "-uk-") {
			personaRegion = "uk"
		} else if strings.Contains(persona, "-au-") {
			personaRegion = "au"
		} else if strings.Contains(persona, "-us-") || strings.Contains(persona, "us-") {
			personaRegion = "us"
		}

		systemPrompt, err = personaPack.BuildSystemPrompt(personaRegion, variables)
		if err != nil {
			return fmt.Errorf("failed to build persona system prompt: %w", err)
		}
		promptType = fmt.Sprintf("persona '%s' (region: %s)", persona, personaRegion)
	} else {
		// Test regular region/task-type prompt
		if region == "" || taskType == "" {
			return fmt.Errorf("region and task-type are required when not using persona mode")
		}

		// Find matching prompt config data from LoadedPromptConfigs
		matchedConfig, exists := cfg.LoadedPromptConfigs[taskType]
		if !exists {
			availableTaskTypes := make([]string, 0, len(cfg.LoadedPromptConfigs))
			for tt := range cfg.LoadedPromptConfigs {
				availableTaskTypes = append(availableTaskTypes, tt)
			}
			return fmt.Errorf("no prompt config found for task-type '%s'. Available: %s", taskType, strings.Join(availableTaskTypes, ", "))
		}

		// Read the prompt config file
		promptFilePath := filepath.Join(cfg.ConfigDir, matchedConfig.FilePath)
		promptData, err := os.ReadFile(promptFilePath)
		if err != nil {
			return fmt.Errorf("failed to read prompt config %s: %w", promptFilePath, err)
		}

		// For simplicity, use the prompt content as-is
		// In a real implementation, you'd parse the prompt sections and filter by region
		systemPrompt = string(promptData)
		if systemPrompt == "" {
			return fmt.Errorf("failed to build system prompt for region=%s, task-type=%s", region, taskType)
		}
		promptType = fmt.Sprintf("region '%s', task-type '%s'", region, taskType)
	}

	// Add context variables
	if context != "" {
		variables["context"] = context
	}
	if domain != "" {
		variables["domain"] = domain
	}
	if user != "" {
		variables["user"] = user
	}

	// Load providers and scenarios for metadata
	providers := cfg.LoadedProviders
	scenarios := cfg.LoadedScenarios

	if outputJSON {
		return outputPromptJSON(systemPrompt, region, taskType, configFile, variables, 0, len(providers), len(scenarios))
	}

	// Human-readable output
	if showMeta {
		fmt.Printf("🎯 Prompt Generation Results\n")
		fmt.Printf("=============================\n")
		fmt.Printf("Prompt Type: %s\n", promptType)
		fmt.Printf("Config File: %s\n", configFile)

		if len(variables) > 0 {
			fmt.Printf("Variables:\n")
			for k, v := range variables {
				fmt.Printf("   %s: %s\n", k, truncateString(v, 80))
			}
		}

		fmt.Printf("Prompt Configs: %d loaded\n", len(cfg.PromptConfigs))
		fmt.Printf("Providers: %d loaded\n", len(providers))
		fmt.Printf("Scenarios: %d loaded\n", len(scenarios))
		fmt.Println()
	}

	if showStats {
		fmt.Printf("📊 Prompt Statistics\n")
		fmt.Printf("--------------------\n")
		fmt.Printf("Length: %d characters\n", len(systemPrompt))
		fmt.Printf("Lines: %d\n", strings.Count(systemPrompt, "\n")+1)
		fmt.Printf("Words (approx): %d\n", len(strings.Fields(systemPrompt)))
		fmt.Printf("Tokens (est): %d\n", estimateTokens(systemPrompt))
		fmt.Println()
	}

	if showPrompt {
		fmt.Printf("📄 Generated System Prompt\n")
		fmt.Printf("===========================\n")
		fmt.Println(systemPrompt)
		fmt.Printf("===========================\n")
	}

	if verbose {
		fmt.Printf("\n🔍 Debug Info\n")
		fmt.Printf("-------------\n")

		// Get available task types from loaded configs
		availableTaskTypes := make([]string, 0, len(cfg.LoadedPromptConfigs))
		for taskType := range cfg.LoadedPromptConfigs {
			availableTaskTypes = append(availableTaskTypes, taskType)
		}

		// Get available personas
		availablePersonas := make([]string, 0, len(cfg.LoadedPersonas))
		for personaID := range cfg.LoadedPersonas {
			availablePersonas = append(availablePersonas, personaID)
		}

		fmt.Printf("Available task types: %s\n", strings.Join(availableTaskTypes, ", "))
		fmt.Printf("Available personas: %s\n", strings.Join(availablePersonas, ", "))
	}

	return nil
}

func outputPromptJSON(systemPrompt, region, taskType, configFile string, variables map[string]string, promptPacksCount, providersCount, scenariosCount int) error {
	output := map[string]interface{}{
		"region":        region,
		"task_type":     taskType,
		"config_file":   configFile,
		"system_prompt": systemPrompt,
		"length":        len(systemPrompt),
		"lines":         strings.Count(systemPrompt, "\n") + 1,
		"words":         len(strings.Fields(systemPrompt)),
		"tokens_est":    estimateTokens(systemPrompt),
		"variables":     variables,
		"metadata": map[string]interface{}{
			"prompt_packs_count": promptPacksCount,
			"providers_count":    providersCount,
			"scenarios_count":    scenariosCount,
		},
	}

	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	fmt.Println(string(jsonData))
	return nil
}

func estimateTokens(text string) int {
	// Rough estimation: ~4 characters per token for English text
	return len(text) / 4
}
