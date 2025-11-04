package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/render"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Flag name constants to avoid duplication
const (
	flagMaxTokens = "max-tokens"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run conversation simulations",
	Long: `Run multi-turn conversation simulations across multiple LLM providers, 
voice profiles, and system prompts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSimulations(cmd)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	// NOSONAR
	// Configuration file
	runCmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path") // NOSONAR

	// Override flags
	runCmd.Flags().StringSlice("region", []string{}, "Regions to run (e.g., us,uk,au)")
	runCmd.Flags().StringSlice("provider", []string{}, "Providers to use")
	runCmd.Flags().StringSlice("scenario", []string{}, "Scenarios to run")

	// Execution settings
	runCmd.Flags().IntP("concurrency", "j", 6, "Number of concurrent workers")
	runCmd.Flags().StringP("out", "o", "out", "Output directory")
	runCmd.Flags().Bool("ci", false, "CI mode (headless)")
	runCmd.Flags().Bool("html", false, "Generate HTML report")
	runCmd.Flags().Float32("temperature", 0.6, "Override temperature")
	runCmd.Flags().Int(flagMaxTokens, 0, "Override max tokens for all scenarios")
	runCmd.Flags().IntP("seed", "s", 42, "Random seed")
	runCmd.Flags().BoolP("verbose", "v", false, "Enable verbose debug logging for API calls")

	// Mock provider settings
	runCmd.Flags().Bool("mock-provider", false, "Replace all providers with MockProvider (for CI/testing)")
	runCmd.Flags().String("mock-config", "", "Path to mock provider configuration file (YAML)")

	// Self-play settings
	runCmd.Flags().Bool("selfplay", false, "Enable self-play mode")
	runCmd.Flags().StringSlice("roles", []string{}, "Self-play role configurations to use")

	// Bind flags to viper
	_ = viper.BindPFlag("concurrency", runCmd.Flags().Lookup("concurrency"))
	_ = viper.BindPFlag("out_dir", runCmd.Flags().Lookup("out"))
	_ = viper.BindPFlag("ci_mode", runCmd.Flags().Lookup("ci"))
	_ = viper.BindPFlag("html_report", runCmd.Flags().Lookup("html"))
	_ = viper.BindPFlag("temperature", runCmd.Flags().Lookup("temperature"))
	_ = viper.BindPFlag(flagMaxTokens, runCmd.Flags().Lookup(flagMaxTokens))
	_ = viper.BindPFlag("seed", runCmd.Flags().Lookup("seed"))
	_ = viper.BindPFlag("selfplay", runCmd.Flags().Lookup("selfplay"))
	_ = viper.BindPFlag("roles", runCmd.Flags().Lookup("roles"))
}

func runSimulations(cmd *cobra.Command) error {
	// Parse configuration and flags
	configFile, cfg, err := loadConfiguration(cmd)
	if err != nil {
		return err
	}

	// Extract run parameters from flags
	runParams, err := extractRunParameters(cmd, cfg)
	if err != nil {
		return err
	}

	// Display run information if not in CI mode
	displayRunInfo(runParams, configFile)

	// Execute the simulation runs
	results, err := executeRuns(configFile, runParams)
	if err != nil {
		return err
	}

	// Process and save results
	return processResults(results, runParams, configFile)
}

// RunParameters holds all the parameters for running simulations
type RunParameters struct {
	Regions        []string
	Providers      []string
	Scenarios      []string
	Concurrency    int
	OutDir         string
	CIMode         bool
	Verbose        bool
	GenerateHTML   bool
	HTMLReportPath string
}

// loadConfiguration loads the configuration file and sets up viper
func loadConfiguration(cmd *cobra.Command) (string, *config.Config, error) {
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", nil, fmt.Errorf("failed to get config flag: %w", err)
	}

	// If config path is a directory, append arena.yaml
	if info, statErr := os.Stat(configFile); statErr == nil && info.IsDir() {
		configFile = filepath.Join(configFile, "arena.yaml")
	}

	// Load configuration
	viper.SetConfigFile(configFile)
	if readErr := viper.ReadInConfig(); readErr != nil {
		log.Printf("Warning: Could not read config file %s: %v", configFile, readErr)
	}

	// Load main config
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to load config: %w", err)
	}

	return configFile, cfg, nil
}

// extractRunParameters extracts all run parameters from command flags
func extractRunParameters(cmd *cobra.Command, cfg *config.Config) (*RunParameters, error) {
	params := &RunParameters{}
	var err error

	// Extract override flags
	if params.Regions, err = cmd.Flags().GetStringSlice("region"); err != nil {
		return nil, fmt.Errorf("failed to get region flag: %w", err)
	}
	if params.Providers, err = cmd.Flags().GetStringSlice("provider"); err != nil {
		return nil, fmt.Errorf("failed to get provider flag: %w", err)
	}
	if params.Scenarios, err = cmd.Flags().GetStringSlice("scenario"); err != nil {
		return nil, fmt.Errorf("failed to get scenario flag: %w", err)
	}
	if params.Concurrency, err = cmd.Flags().GetInt("concurrency"); err != nil {
		return nil, fmt.Errorf("failed to get concurrency flag: %w", err)
	}
	if params.OutDir, err = cmd.Flags().GetString("out"); err != nil {
		return nil, fmt.Errorf("failed to get out flag: %w", err)
	}
	if params.CIMode, err = cmd.Flags().GetBool("ci"); err != nil {
		return nil, fmt.Errorf("failed to get ci flag: %w", err)
	}
	if params.Verbose, err = cmd.Flags().GetBool("verbose"); err != nil {
		return nil, fmt.Errorf("failed to get verbose flag: %w", err)
	}

	// Process HTML report settings
	if err := processHTMLSettings(cmd, cfg, params); err != nil {
		return nil, err
	}

	// Apply configuration overrides
	applyConfigurationOverrides(cmd, cfg, params)

	return params, nil
}

// processHTMLSettings determines HTML report generation settings
func processHTMLSettings(cmd *cobra.Command, cfg *config.Config, params *RunParameters) error {
	// HTML report generation: use CLI flag if set, otherwise check config
	if cmd.Flags().Changed("html") {
		// CLI flag takes precedence
		var err error
		params.GenerateHTML, err = cmd.Flags().GetBool("html")
		if err != nil {
			return fmt.Errorf("failed to get html flag: %w", err)
		}
	} else if cfg.Defaults.HTMLReport != "" {
		// Use config file setting
		params.GenerateHTML = true
		params.HTMLReportPath = cfg.Defaults.HTMLReport
	}
	return nil
}

// applyConfigurationOverrides applies command line overrides to configuration
func applyConfigurationOverrides(cmd *cobra.Command, cfg *config.Config, params *RunParameters) {
	// Apply verbose override to config
	if params.Verbose {
		cfg.Defaults.Verbose = true
	}

	// Apply max_tokens override if provided
	if cmd.Flags().Changed(flagMaxTokens) {
		if maxTokens, err := cmd.Flags().GetInt(flagMaxTokens); err == nil {
			cfg.Defaults.MaxTokens = maxTokens
		}
	}
}

// displayRunInfo displays run information when not in CI mode
func displayRunInfo(params *RunParameters, configFile string) {
	if params.CIMode {
		return
	}

	fmt.Printf("Running Altaira Prompt Arena\n")
	fmt.Printf("Config: %s\n", configFile)
	fmt.Printf("Regions: %s\n", strings.Join(params.Regions, ", "))
	fmt.Printf("Providers: %s\n", strings.Join(params.Providers, ", "))
	fmt.Printf("Scenarios: %s\n", strings.Join(params.Scenarios, ", "))
	fmt.Printf("Concurrency: %d\n", params.Concurrency)
	fmt.Printf("Output: %s\n", params.OutDir)
	if params.GenerateHTML {
		fmt.Printf("HTML Report: enabled\n")
	}
	fmt.Println()
}

// executeRuns creates the engine and executes all simulation runs
func executeRuns(configFile string, params *RunParameters) ([]engine.RunResult, error) {
	// Create engine and load all resources
	eng, err := engine.NewEngineFromConfigFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine: %w", err)
	}

	// Generate run plan
	plan, err := eng.GenerateRunPlan(params.Regions, params.Providers, params.Scenarios)
	if err != nil {
		return nil, fmt.Errorf("failed to generate run plan: %w", err)
	}

	if !params.CIMode {
		fmt.Printf("Generated %d run combinations\n", len(plan.Combinations))
		fmt.Println("Starting execution...")
		fmt.Println()
	}

	// Execute runs
	ctx := context.Background()
	runIDs, err := eng.ExecuteRuns(ctx, plan, params.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to execute runs: %w", err)
	}

	// Convert results from statestore format
	return convertRunResults(ctx, eng, runIDs)
}

// convertRunResults retrieves and converts run results from the statestore
func convertRunResults(ctx context.Context, eng *engine.Engine, runIDs []string) ([]engine.RunResult, error) {
	arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		return nil, fmt.Errorf("failed to get ArenaStateStore")
	}

	results := make([]engine.RunResult, 0, len(runIDs))
	for _, runID := range runIDs {
		storeResult, err := arenaStore.GetResult(ctx, runID)
		if err != nil {
			log.Printf("Warning: failed to get run result for %s: %v", runID, err)
			continue
		}
		results = append(results, convertToEngineRunResult(storeResult))
	}

	return results, nil
}

// processResults processes, saves, and reports execution results
func processResults(results []engine.RunResult, params *RunParameters, configFile string) error {
	// Create output directory
	if err := os.MkdirAll(params.OutDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save individual results and count status
	successCount, errorCount := saveIndividualResults(results, params.OutDir)

	// Create and save index summary
	if err := saveIndexSummary(results, successCount, errorCount, configFile, params.OutDir); err != nil {
		return err
	}

	// Generate HTML report if requested
	generateHTMLReport(results, params)

	// Display final summary and handle CI mode errors
	return displayFinalSummary(params, results, successCount, errorCount)
}

// saveIndividualResults saves each result to individual JSON files
func saveIndividualResults(results []engine.RunResult, outDir string) (successCount, errorCount int) {
	successCount, errorCount = countResultsByStatus(results)

	for i := range results {
		filename := filepath.Join(outDir, results[i].RunID+".json")
		if err := saveResult(&results[i], filename); err != nil {
			log.Printf("Warning: failed to save result %s: %v", results[i].RunID, err)
		}
	}

	return successCount, errorCount
}

// saveIndexSummary creates and saves the index summary file
func saveIndexSummary(results []engine.RunResult, successCount, errorCount int, configFile, outDir string) error {
	summary := createSummary(results, successCount, errorCount, configFile)

	indexFile := filepath.Join(outDir, "index.json")
	if err := saveJSON(summary, indexFile); err != nil {
		log.Printf("Warning: failed to save index: %v", err)
		return err
	}

	return nil
}

// generateHTMLReport generates HTML report if requested
func generateHTMLReport(results []engine.RunResult, params *RunParameters) {
	if !params.GenerateHTML {
		return
	}

	var htmlFile string
	if params.HTMLReportPath != "" {
		htmlFile = config.ResolveOutputPath(params.OutDir, params.HTMLReportPath)
	} else {
		htmlFile = resolveHTMLReportPath(params.OutDir, "")
	}

	if err := render.GenerateHTMLReport(results, htmlFile); err != nil {
		log.Printf("Warning: failed to generate HTML report: %v", err)
	} else if !params.CIMode {
		fmt.Printf("HTML report generated: %s\n", htmlFile)
	}
}

// displayFinalSummary displays execution summary and handles CI mode errors
func displayFinalSummary(params *RunParameters, results []engine.RunResult, successCount, errorCount int) error {
	// Print summary
	if !params.CIMode {
		fmt.Printf("Execution complete!\n")
		fmt.Printf("Total runs: %d\n", len(results))
		fmt.Printf("Successful: %d\n", successCount)
		fmt.Printf("Errors: %d\n", errorCount)
		fmt.Printf("Results saved to: %s\n", params.OutDir)
	}

	// Exit with error code if any runs failed and CI mode
	if errorCount > 0 && params.CIMode {
		return fmt.Errorf("execution failed: %d runs had errors", errorCount)
	}

	return nil
}

func saveResult(result *engine.RunResult, filename string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0600)
}

func saveJSON(data interface{}, filename string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, jsonData, 0600)
}

func extractRunIDs(results []engine.RunResult) []string {
	ids := make([]string, len(results))
	for i := range results {
		ids[i] = results[i].RunID
	}
	return ids
}

// convertToEngineRunResult converts statestore.RunResult to engine.RunResult
func convertToEngineRunResult(sr *statestore.RunResult) engine.RunResult {
	result := engine.RunResult{
		RunID:        sr.RunID,
		PromptPack:   sr.PromptPack,
		Region:       sr.Region,
		ScenarioID:   sr.ScenarioID,
		ProviderID:   sr.ProviderID,
		Params:       sr.Params,
		Messages:     sr.Messages,
		Commit:       sr.Commit,
		Cost:         sr.Cost,
		ToolStats:    sr.ToolStats,
		Violations:   sr.Violations,
		StartTime:    sr.StartTime,
		EndTime:      sr.EndTime,
		Duration:     sr.Duration,
		Error:        sr.Error,
		SelfPlay:     sr.SelfPlay,
		PersonaID:    sr.PersonaID,
		UserFeedback: sr.UserFeedback,
		SessionTags:  sr.SessionTags,
	}

	// Convert AssistantRole from interface{} to *engine.SelfPlayRoleInfo
	if sr.AssistantRole != nil {
		if roleMap, ok := sr.AssistantRole.(map[string]interface{}); ok {
			result.AssistantRole = &engine.SelfPlayRoleInfo{
				Provider: getStringFromMap(roleMap, "Provider"),
				Model:    getStringFromMap(roleMap, "Model"),
				Region:   getStringFromMap(roleMap, "Region"),
			}
		}
	}

	// Convert UserRole from interface{} to *engine.SelfPlayRoleInfo
	if sr.UserRole != nil {
		if roleMap, ok := sr.UserRole.(map[string]interface{}); ok {
			result.UserRole = &engine.SelfPlayRoleInfo{
				Provider: getStringFromMap(roleMap, "Provider"),
				Model:    getStringFromMap(roleMap, "Model"),
				Region:   getStringFromMap(roleMap, "Region"),
			}
		}
	}

	return result
}

// getStringFromMap safely extracts a string value from a map
func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// countResultsByStatus counts successful and error results
func countResultsByStatus(results []engine.RunResult) (successCount, errorCount int) {
	for i := range results {
		if results[i].Error != "" {
			errorCount++
		} else {
			successCount++
		}
	}

	return successCount, errorCount
}

// createSummary creates a summary data structure
func createSummary(results []engine.RunResult, successCount, errorCount int, configFile string) map[string]interface{} {
	return map[string]interface{}{
		"total_runs":  len(results),
		"successful":  successCount,
		"errors":      errorCount,
		"timestamp":   time.Now(),
		"config_file": configFile,
		"run_ids":     extractRunIDs(results),
	}
}

// resolveHTMLReportPath determines the final HTML report path
func resolveHTMLReportPath(outDir, htmlReportPath string) string {
	if htmlReportPath == "" {
		timestamp := time.Now().Format("2006-01-02T15-04Z")
		return filepath.Join(outDir, fmt.Sprintf("report-%s.html", timestamp))
	}
	return config.ResolveOutputPath(outDir, htmlReportPath)
}
