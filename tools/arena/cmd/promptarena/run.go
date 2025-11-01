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

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/render"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

	// Configuration file
	runCmd.Flags().StringP("config", "c", "arena.yaml", "Configuration file path")

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
	runCmd.Flags().Int("max-tokens", 400, "Override max tokens")
	runCmd.Flags().IntP("seed", "s", 42, "Random seed")
	runCmd.Flags().BoolP("verbose", "v", false, "Enable verbose debug logging for API calls")

	// Self-play settings
	runCmd.Flags().Bool("selfplay", false, "Enable self-play mode")
	runCmd.Flags().StringSlice("roles", []string{}, "Self-play role configurations to use")

	// Bind flags to viper
	_ = viper.BindPFlag("concurrency", runCmd.Flags().Lookup("concurrency"))
	_ = viper.BindPFlag("out_dir", runCmd.Flags().Lookup("out"))
	_ = viper.BindPFlag("ci_mode", runCmd.Flags().Lookup("ci"))
	viper.BindPFlag("html_report", runCmd.Flags().Lookup("html"))
	viper.BindPFlag("temperature", runCmd.Flags().Lookup("temperature"))
	viper.BindPFlag("max_tokens", runCmd.Flags().Lookup("max-tokens"))
	viper.BindPFlag("seed", runCmd.Flags().Lookup("seed"))
	viper.BindPFlag("selfplay", runCmd.Flags().Lookup("selfplay"))
	viper.BindPFlag("roles", runCmd.Flags().Lookup("roles"))
}

func runSimulations(cmd *cobra.Command) error {
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to get config flag: %w", err)
	}

	// If config path is a directory, append arena.yaml
	if info, err := os.Stat(configFile); err == nil && info.IsDir() {
		configFile = filepath.Join(configFile, "arena.yaml")
	}

	// Load configuration
	viper.SetConfigFile(configFile)
	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Warning: Could not read config file %s: %v", configFile, err)
	}

	// Load main config
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get override flags
	regions, err := cmd.Flags().GetStringSlice("region")
	if err != nil {
		return fmt.Errorf("failed to get region flag: %w", err)
	}
	providers, err := cmd.Flags().GetStringSlice("provider")
	if err != nil {
		return fmt.Errorf("failed to get provider flag: %w", err)
	}
	scenarios, err := cmd.Flags().GetStringSlice("scenario")
	if err != nil {
		return fmt.Errorf("failed to get scenario flag: %w", err)
	}
	concurrency, err := cmd.Flags().GetInt("concurrency")
	if err != nil {
		return fmt.Errorf("failed to get concurrency flag: %w", err)
	}
	outDir, err := cmd.Flags().GetString("out")
	if err != nil {
		return fmt.Errorf("failed to get out flag: %w", err)
	}
	ciMode, err := cmd.Flags().GetBool("ci")
	if err != nil {
		return fmt.Errorf("failed to get ci flag: %w", err)
	}
	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return fmt.Errorf("failed to get verbose flag: %w", err)
	}

	// HTML report generation: use CLI flag if set, otherwise check config
	generateHTML := false
	htmlReportPath := ""
	if cmd.Flags().Changed("html") {
		// CLI flag takes precedence
		var err error
		generateHTML, err = cmd.Flags().GetBool("html")
		if err != nil {
			return fmt.Errorf("failed to get html flag: %w", err)
		}
	} else if cfg.Defaults.HTMLReport != "" {
		// Use config file setting
		generateHTML = true
		htmlReportPath = cfg.Defaults.HTMLReport
	}

	// Apply verbose override to config
	if verbose {
		cfg.Defaults.Verbose = true
	}

	// Note: Logger verbosity is configured in main.go's PersistentPreRun
	// based on the --verbose flag, not here.

	// Apply max_tokens override if provided
	if cmd.Flags().Changed("max-tokens") {
		maxTokens, err := cmd.Flags().GetInt("max-tokens")
		if err != nil {
			return fmt.Errorf("failed to get max-tokens flag: %w", err)
		}
		cfg.Defaults.MaxTokens = maxTokens
	}

	if !ciMode {
		fmt.Printf("Running Altaira Prompt Arena\n")
		fmt.Printf("Config: %s\n", configFile)
		fmt.Printf("Regions: %s\n", strings.Join(regions, ", "))
		fmt.Printf("Providers: %s\n", strings.Join(providers, ", "))
		fmt.Printf("Scenarios: %s\n", strings.Join(scenarios, ", "))
		fmt.Printf("Concurrency: %d\n", concurrency)
		fmt.Printf("Output: %s\n", outDir)
		if generateHTML {
			fmt.Printf("HTML Report: enabled\n")
		}
		fmt.Println()
	}

	// Create engine and load all resources
	eng, err := engine.NewEngineFromConfigFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to create engine: %w", err)
	}

	// Generate run plan
	plan, err := eng.GenerateRunPlan(regions, providers, scenarios)
	if err != nil {
		return fmt.Errorf("failed to generate run plan: %w", err)
	}

	if !ciMode {
		fmt.Printf("Generated %d run combinations\n", len(plan.Combinations))
		fmt.Println("Starting execution...")
		fmt.Println()
	}

	// Execute runs
	ctx := context.Background()
	runIDs, err := eng.ExecuteRuns(ctx, plan, concurrency)
	if err != nil {
		return fmt.Errorf("failed to execute runs: %w", err)
	}

	// Get the results from statestore
	arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		return fmt.Errorf("failed to get ArenaStateStore")
	}

	// Retrieve all run results and convert them to engine.RunResult
	results := make([]engine.RunResult, 0, len(runIDs))
	for _, runID := range runIDs {
		storeResult, err := arenaStore.GetResult(ctx, runID)
		if err != nil {
			log.Printf("Warning: failed to get run result for %s: %v", runID, err)
			continue
		}
		// Convert statestore.RunResult to engine.RunResult
		results = append(results, convertToEngineRunResult(storeResult))
	}

	// Create output directory
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save individual run results
	var successCount, errorCount int
	for _, result := range results {
		if result.Error != "" {
			errorCount++
		} else {
			successCount++
		}

		// Save individual result
		filename := filepath.Join(outDir, result.RunID+".json")
		if err := saveResult(result, filename); err != nil {
			log.Printf("Warning: failed to save result %s: %v", result.RunID, err)
		}
	}

	// Create index summary
	summary := map[string]interface{}{
		"total_runs":  len(results),
		"successful":  successCount,
		"errors":      errorCount,
		"timestamp":   time.Now(),
		"config_file": configFile,
		"run_ids":     extractRunIDs(results),
	}

	indexFile := filepath.Join(outDir, "index.json")
	if err := saveJSON(summary, indexFile); err != nil {
		log.Printf("Warning: failed to save index: %v", err)
	}

	// Generate HTML report if requested
	if generateHTML {
		// Determine HTML file path using the new helper
		var htmlFile string
		if htmlReportPath != "" {
			// Resolve path relative to outDir
			htmlFile = config.ResolveOutputPath(outDir, htmlReportPath)
		} else {
			// Generate timestamped filename in outDir
			timestamp := time.Now().Format("2006-01-02T15-04Z")
			htmlFile = filepath.Join(outDir, fmt.Sprintf("report-%s.html", timestamp))
		}

		if err := render.GenerateHTMLReport(results, htmlFile); err != nil {
			log.Printf("Warning: failed to generate HTML report: %v", err)
		} else if !ciMode {
			fmt.Printf("HTML report generated: %s\n", htmlFile)
		}
	}

	// Print summary
	if !ciMode {
		fmt.Printf("Execution complete!\n")
		fmt.Printf("Total runs: %d\n", len(results))
		fmt.Printf("Successful: %d\n", successCount)
		fmt.Printf("Errors: %d\n", errorCount)
		fmt.Printf("Results saved to: %s\n", outDir)
	}

	// Exit with error code if any runs failed and CI mode
	if errorCount > 0 && ciMode {
		return fmt.Errorf("execution failed: %d runs had errors", errorCount)
	}

	return nil
}

func saveResult(result engine.RunResult, filename string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func saveJSON(data interface{}, filename string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, jsonData, 0644)
}

func extractRunIDs(results []engine.RunResult) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.RunID
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
