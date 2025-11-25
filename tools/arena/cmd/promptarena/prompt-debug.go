package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/pkg/config"
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

// promptDebugOptions holds all command line options for prompt debugging
type promptDebugOptions struct {
	ConfigFile   string
	ScenarioFile string
	Region       string
	TaskType     string
	Persona      string
	Context      string
	Domain       string
	User         string
	ShowPrompt   bool
	ShowMeta     bool
	ShowStats    bool
	OutputJSON   bool
	ListConfigs  bool
	Verbose      bool
}

// parsePromptDebugFlags extracts all flags from the command
func parsePromptDebugFlags(cmd *cobra.Command) (*promptDebugOptions, error) {
	opts := &promptDebugOptions{}

	// Parse string flags
	if err := parseStringFlags(cmd, opts); err != nil {
		return nil, err
	}

	// Parse boolean flags
	if err := parseBoolFlags(cmd, opts); err != nil {
		return nil, err
	}

	// Normalize config file path
	normalizeConfigPath(opts)

	return opts, nil
}

// parseStringFlags extracts all string flags from the command
func parseStringFlags(cmd *cobra.Command, opts *promptDebugOptions) error {
	stringFlags := map[string]*string{
		"config":    &opts.ConfigFile,
		"scenario":  &opts.ScenarioFile,
		"region":    &opts.Region,
		"task-type": &opts.TaskType,
		"persona":   &opts.Persona,
		"context":   &opts.Context,
		"domain":    &opts.Domain,
		"user":      &opts.User,
	}

	for flagName, target := range stringFlags {
		value, err := cmd.Flags().GetString(flagName)
		if err != nil {
			return fmt.Errorf("failed to get %s flag: %w", flagName, err)
		}
		*target = value
	}

	return nil
}

// parseBoolFlags extracts all boolean flags from the command
func parseBoolFlags(cmd *cobra.Command, opts *promptDebugOptions) error {
	boolFlags := map[string]*bool{
		"show-prompt": &opts.ShowPrompt,
		"show-meta":   &opts.ShowMeta,
		"show-stats":  &opts.ShowStats,
		"json":        &opts.OutputJSON,
		"list":        &opts.ListConfigs,
		"verbose":     &opts.Verbose,
	}

	for flagName, target := range boolFlags {
		value, err := cmd.Flags().GetBool(flagName)
		if err != nil {
			return fmt.Errorf("failed to get %s flag: %w", flagName, err)
		}
		*target = value
	}

	return nil
}

// normalizeConfigPath adjusts config path if it points to a directory
func normalizeConfigPath(opts *promptDebugOptions) {
	if info, _ := os.Stat(opts.ConfigFile); info != nil && info.IsDir() {
		opts.ConfigFile = filepath.Join(opts.ConfigFile, "arena.yaml")
	}
}

func runPromptDebug(cmd *cobra.Command) error {
	opts, err := parsePromptDebugFlags(cmd)
	if err != nil {
		return err
	}

	// Load configuration
	cfg, err := config.LoadConfig(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Apply scenario overrides if specified
	err = applyScenarioOverrides(opts, cfg)
	if err != nil {
		return err
	}

	// Handle list mode
	if opts.ListConfigs {
		return showAvailableConfigurations(cfg)
	}

	// Generate and display the prompt
	return generateAndDisplayPrompt(opts, cfg)
}

// applyScenarioOverrides loads a scenario file and overrides options with scenario data
func applyScenarioOverrides(opts *promptDebugOptions, cfg *config.Config) error {
	if opts.ScenarioFile == "" {
		return nil
	}

	scenario, err := config.LoadScenario(opts.ScenarioFile)
	if err != nil {
		return fmt.Errorf("failed to load scenario file %s: %w", opts.ScenarioFile, err)
	}

	// Apply scenario overrides
	applyScenarioData(opts, scenario)

	// Show verbose output if requested
	displayScenarioInfo(opts, scenario)

	return nil
}

// applyScenarioData applies scenario data to options
func applyScenarioData(opts *promptDebugOptions, scenario *config.Scenario) {
	// Override task_type with scenario's task_type
	opts.TaskType = scenario.TaskType

	// Override context metadata if not provided via flags
	applyContextMetadata(opts, scenario)

	// Try to get context from the general context map
	applyContextFromMap(opts, scenario)
}

// applyContextMetadata applies context metadata from scenario if available
func applyContextMetadata(opts *promptDebugOptions, scenario *config.Scenario) {
	if scenario.ContextMetadata == nil {
		return
	}

	if opts.Domain == "" && scenario.ContextMetadata.Domain != "" {
		opts.Domain = scenario.ContextMetadata.Domain
	}
	if opts.User == "" && scenario.ContextMetadata.UserRole != "" {
		opts.User = scenario.ContextMetadata.UserRole
	}
}

// applyContextFromMap extracts context from scenario's context map
func applyContextFromMap(opts *promptDebugOptions, scenario *config.Scenario) {
	if opts.Context != "" || scenario.Context == nil {
		return
	}

	contextValue, exists := scenario.Context["user_context"]
	if !exists {
		return
	}

	if contextStr, ok := contextValue.(string); ok {
		opts.Context = contextStr
	}
}

// displayScenarioInfo shows scenario loading information if verbose mode is enabled
func displayScenarioInfo(opts *promptDebugOptions, scenario *config.Scenario) {
	if !opts.Verbose {
		return
	}

	fmt.Printf("📄 Loaded scenario from: %s\n", opts.ScenarioFile)
	fmt.Printf("   Task Type: %s\n", opts.TaskType)
	if scenario.ContextMetadata != nil {
		fmt.Printf("   Domain: %s\n", opts.Domain)
		fmt.Printf("   User Role: %s\n", opts.User)
	}
	fmt.Printf("   Context: %s\n", opts.Context)
	fmt.Printf("\n")
}

// showAvailableConfigurations displays all available configurations
func showAvailableConfigurations(cfg *config.Config) error {
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

// generateAndDisplayPrompt generates a prompt and displays it according to options
func generateAndDisplayPrompt(opts *promptDebugOptions, cfg *config.Config) error {
	// Enable verbose debugging if requested
	if opts.Verbose {
		fmt.Printf("🔍 Prompt Debug Mode - Verbose Enabled\n")
		if opts.Persona != "" {
			fmt.Printf("Config: %s, Persona: %s\n\n", opts.ConfigFile, opts.Persona)
		} else {
			fmt.Printf("Config: %s, Region: %s, Task: %s\n\n", opts.ConfigFile, opts.Region, opts.TaskType)
		}
	}

	// Build the system prompt
	systemPrompt, promptType, variables, err := buildSystemPrompt(opts, cfg)
	if err != nil {
		return err
	}

	// Display the results
	return displayPromptResults(opts, cfg, systemPrompt, promptType, variables)
}

// buildSystemPrompt constructs the system prompt based on options
func buildSystemPrompt(opts *promptDebugOptions, cfg *config.Config) (string, string, map[string]string, error) {
	variables := make(map[string]string)

	// Add context variables
	if opts.Context != "" {
		variables["context"] = opts.Context
	}
	if opts.Domain != "" {
		variables["domain"] = opts.Domain
	}
	if opts.User != "" {
		variables["user"] = opts.User
	}

	if opts.Persona != "" {
		return buildPersonaPrompt(opts, cfg, variables)
	}

	return buildRegionTaskPrompt(opts, cfg, variables)
}

// buildPersonaPrompt builds a system prompt using a persona
func buildPersonaPrompt(opts *promptDebugOptions, cfg *config.Config, variables map[string]string) (string, string, map[string]string, error) {
	personaPack, exists := cfg.LoadedPersonas[opts.Persona]
	if !exists {
		availablePersonas := make([]string, 0, len(cfg.LoadedPersonas))
		for id := range cfg.LoadedPersonas {
			availablePersonas = append(availablePersonas, id)
		}
		return "", "", nil, fmt.Errorf("persona '%s' not found. Available personas: %s", opts.Persona, strings.Join(availablePersonas, ", "))
	}

	// Build variables for persona prompt
	if opts.Context != "" {
		variables["context_slot"] = opts.Context
	} else {
		variables["context_slot"] = "General conversation context"
	}
	if opts.Domain != "" {
		variables["domain_hint"] = opts.Domain
	} else {
		variables["domain_hint"] = "general"
	}
	if opts.User != "" {
		variables["user_context"] = opts.User
	} else {
		variables["user_context"] = "general user"
	}

	// Extract region from persona ID (e.g., "us-hustler-v1" -> "us")
	personaRegion := extractRegionFromPersonaID(opts.Persona)

	systemPrompt, err := personaPack.BuildSystemPrompt(personaRegion, variables)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to build persona system prompt: %w", err)
	}

	promptType := fmt.Sprintf("persona '%s' (region: %s)", opts.Persona, personaRegion)
	return systemPrompt, promptType, variables, nil
}

// buildRegionTaskPrompt builds a system prompt using region and task type
func buildRegionTaskPrompt(opts *promptDebugOptions, cfg *config.Config, variables map[string]string) (string, string, map[string]string, error) {
	if opts.Region == "" || opts.TaskType == "" {
		return "", "", nil, fmt.Errorf("region and task-type are required when not using persona mode")
	}

	// Find matching prompt config data from LoadedPromptConfigs
	matchedConfig, exists := cfg.LoadedPromptConfigs[opts.TaskType]
	if !exists {
		availableTaskTypes := make([]string, 0, len(cfg.LoadedPromptConfigs))
		for tt := range cfg.LoadedPromptConfigs {
			availableTaskTypes = append(availableTaskTypes, tt)
		}
		return "", "", nil, fmt.Errorf("no prompt config found for task-type '%s'. Available: %s", opts.TaskType, strings.Join(availableTaskTypes, ", "))
	}

	// Read the prompt config file
	promptFilePath := filepath.Join(cfg.ConfigDir, matchedConfig.FilePath)
	promptData, err := os.ReadFile(promptFilePath)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to read prompt config %s: %w", promptFilePath, err)
	}

	// For simplicity, use the prompt content as-is
	// In a real implementation, you'd parse the prompt sections and filter by region
	systemPrompt := string(promptData)
	if systemPrompt == "" {
		return "", "", nil, fmt.Errorf("failed to build system prompt for region=%s, task-type=%s", opts.Region, opts.TaskType)
	}

	promptType := fmt.Sprintf("region '%s', task-type '%s'", opts.Region, opts.TaskType)
	return systemPrompt, promptType, variables, nil
}

// extractRegionFromPersonaID extracts region code from persona ID
func extractRegionFromPersonaID(persona string) string {
	if strings.Contains(persona, "-uk-") {
		return "uk"
	} else if strings.Contains(persona, "-au-") {
		return "au"
	} else if strings.Contains(persona, "-us-") || strings.Contains(persona, "us-") {
		return "us"
	}
	return "us" // default
}

// displayPromptResults shows the generated prompt according to display options
func displayPromptResults(opts *promptDebugOptions, cfg *config.Config, systemPrompt, promptType string, variables map[string]string) error {
	providers := cfg.LoadedProviders
	scenarios := cfg.LoadedScenarios

	if opts.OutputJSON {
		params := promptJSONParams{
			SystemPrompt:     systemPrompt,
			Region:           opts.Region,
			TaskType:         opts.TaskType,
			ConfigFile:       opts.ConfigFile,
			Variables:        variables,
			PromptPacksCount: 0,
			ProvidersCount:   len(providers),
			ScenariosCount:   len(scenarios),
		}
		return outputPromptJSON(params)
	}

	// Human-readable output
	if opts.ShowMeta {
		displayMetadata(opts, cfg, promptType, variables, len(providers), len(scenarios))
	}

	if opts.ShowStats {
		displayStatistics(systemPrompt)
	}

	if opts.ShowPrompt {
		displaySystemPrompt(systemPrompt)
	}

	if opts.Verbose {
		displayDebugInfo(cfg)
	}

	return nil
}

// displayMetadata shows metadata about the prompt generation
func displayMetadata(opts *promptDebugOptions, cfg *config.Config, promptType string, variables map[string]string, providerCount, scenarioCount int) {
	fmt.Printf("🎯 Prompt Generation Results\n")
	fmt.Printf("=============================\n")
	fmt.Printf("Prompt Type: %s\n", promptType)
	fmt.Printf("Config File: %s\n", opts.ConfigFile)

	if len(variables) > 0 {
		fmt.Printf("Variables:\n")
		for k, v := range variables {
			fmt.Printf("   %s: %s\n", k, truncateString(v, 80))
		}
	}

	fmt.Printf("Prompt Configs: %d loaded\n", len(cfg.PromptConfigs))
	fmt.Printf("Providers: %d loaded\n", providerCount)
	fmt.Printf("Scenarios: %d loaded\n", scenarioCount)
	fmt.Println()
}

// displayStatistics shows prompt statistics
func displayStatistics(systemPrompt string) {
	fmt.Printf("📊 Prompt Statistics\n")
	fmt.Printf("--------------------\n")
	fmt.Printf("Length: %d characters\n", len(systemPrompt))
	fmt.Printf("Lines: %d\n", strings.Count(systemPrompt, "\n")+1)
	fmt.Printf("Words (approx): %d\n", len(strings.Fields(systemPrompt)))
	fmt.Printf("Tokens (est): %d\n", estimateTokens(systemPrompt))
	fmt.Println()
}

// displaySystemPrompt shows the generated system prompt
func displaySystemPrompt(systemPrompt string) {
	fmt.Printf("📄 Generated System Prompt\n")
	fmt.Printf("===========================\n")
	fmt.Println(systemPrompt)
	fmt.Printf("===========================\n")
}

// displayDebugInfo shows debug information
func displayDebugInfo(cfg *config.Config) {
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

// promptJSONOutput represents the JSON output structure
type promptJSONOutput struct {
	Region       string                 `json:"region"`
	TaskType     string                 `json:"task_type"`
	ConfigFile   string                 `json:"config_file"`
	SystemPrompt string                 `json:"system_prompt"`
	Length       int                    `json:"length"`
	Lines        int                    `json:"lines"`
	Words        int                    `json:"words"`
	TokensEst    int                    `json:"tokens_est"`
	Variables    map[string]string      `json:"variables"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// promptJSONParams holds parameters for JSON output
type promptJSONParams struct {
	SystemPrompt     string
	Region           string
	TaskType         string
	ConfigFile       string
	Variables        map[string]string
	PromptPacksCount int
	ProvidersCount   int
	ScenariosCount   int
}

func outputPromptJSON(params promptJSONParams) error {
	output := promptJSONOutput{
		Region:       params.Region,
		TaskType:     params.TaskType,
		ConfigFile:   params.ConfigFile,
		SystemPrompt: params.SystemPrompt,
		Length:       len(params.SystemPrompt),
		Lines:        strings.Count(params.SystemPrompt, "\n") + 1,
		Words:        len(strings.Fields(params.SystemPrompt)),
		TokensEst:    estimateTokens(params.SystemPrompt),
		Variables:    params.Variables,
		Metadata: map[string]interface{}{
			"prompt_packs_count": params.PromptPacksCount,
			"providers_count":    params.ProvidersCount,
			"scenarios_count":    params.ScenariosCount,
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
