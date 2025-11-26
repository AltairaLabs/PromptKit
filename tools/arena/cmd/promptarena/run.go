package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/html"
	jsonrepo "github.com/AltairaLabs/PromptKit/tools/arena/results/json"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/junit"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/markdown"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
)

// Flag name constants to avoid duplication
const (
	flagMaxTokens = "max-tokens"
)

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// createResultRepository creates a composite repository based on output formats
func createResultRepository(params *RunParameters, configFile string) (results.ResultRepository, error) {
	composite := results.NewCompositeRepository()

	for _, format := range params.OutputFormats {
		switch format {
		case "json":
			jsonRepo := jsonrepo.NewJSONResultRepository(params.OutDir)
			composite.AddRepository(jsonRepo)
		case "junit":
			junitRepo := junit.NewJUnitResultRepository(params.JUnitFile)
			composite.AddRepository(junitRepo)
		case "html":
			htmlRepo := html.NewHTMLResultRepository(params.HTMLFile)
			composite.AddRepository(htmlRepo)
		case "markdown":
			// Get markdown configuration from arena defaults
			var markdownConfig *markdown.MarkdownConfig
			if configFile != "" {
				// Load config to get markdown defaults
				cfg, err := config.LoadConfig(configFile)
				if err == nil && cfg != nil {
					markdownConfig = markdown.CreateMarkdownConfigFromDefaults(&cfg.Defaults)
				}
			}
			if markdownConfig == nil {
				markdownConfig = markdown.CreateMarkdownConfigFromDefaults(nil)
			}

			// Create markdown repository with configuration and custom output file
			markdownRepo := markdown.NewMarkdownResultRepositoryWithConfig(filepath.Dir(params.MarkdownFile), markdownConfig)
			// Override the output file path
			markdownRepo.SetOutputFile(params.MarkdownFile)
			composite.AddRepository(markdownRepo)
		default:
			return nil, fmt.Errorf("unsupported output format: %s", format)
		}
	}

	return composite, nil
}

// createResultSummary creates a summary using the results builder
func createResultSummary(runResults []engine.RunResult, successCount, errorCount int, configFile string) *results.ResultSummary {
	builder := results.NewSummaryBuilder(configFile)
	return builder.BuildSummary(runResults)
}

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
	runCmd.Flags().StringP("config", "c", "config.arena.yaml", "Configuration file path") // NOSONAR

	// Override flags
	runCmd.Flags().StringSlice("region", []string{}, "Regions to run (e.g., us,uk,au)")
	runCmd.Flags().StringSlice("provider", []string{}, "Providers to use")
	runCmd.Flags().StringSlice("scenario", []string{}, "Scenarios to run")

	// Execution settings
	runCmd.Flags().IntP("concurrency", "j", 6, "Number of concurrent workers")
	runCmd.Flags().StringP("out", "o", "out", "Output directory")
	runCmd.Flags().Bool("ci", false, "CI mode (disable TUI, simple logs)")
	runCmd.Flags().Bool("simple", false, "Simple mode (alias for --ci)")
	runCmd.Flags().Bool("html", false, "Generate HTML report (deprecated: use --format)")
	runCmd.Flags().StringSlice("format", []string{}, "Output formats (json, junit, html, markdown) - defaults from config")
	runCmd.Flags().StringSlice("formats", []string{}, "Output formats (json, junit, html, markdown) - alias for --format")
	runCmd.Flags().String("junit-file", "", "JUnit XML output file (default: out/junit.xml)")
	runCmd.Flags().String("html-file", "", "HTML report output file (default: out/report-[timestamp].html)")
	runCmd.Flags().String("markdown-file", "", "Markdown report output file (default: out/results.md)")
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
	SimpleMode     bool // Alias for CIMode
	Verbose        bool
	GenerateHTML   bool     // Deprecated: use OutputFormats
	HTMLReportPath string   // Deprecated: use HTMLFile
	OutputFormats  []string // New: output formats (json, junit, html, markdown)
	JUnitFile      string   // New: JUnit XML output file
	HTMLFile       string   // New: HTML output file
	MarkdownFile   string   // New: Markdown output file
	MockProvider   bool     // Enable mock provider mode
	MockConfig     string   // Path to mock provider configuration
	ConfigFile     string   // Configuration file name for TUI display
	TotalRuns      int      // Total number of runs for TUI progress
}

// loadConfiguration loads the configuration file and sets up viper
func loadConfiguration(cmd *cobra.Command) (string, *config.Config, error) {
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", nil, fmt.Errorf("failed to get config flag: %w", err)
	}

	// If config path is a directory, append config.arena.yaml
	if info, statErr := os.Stat(configFile); statErr == nil && info.IsDir() {
		configFile = filepath.Join(configFile, "config.arena.yaml")
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

	// Extract basic flags
	if err := extractBasicFlags(cmd, params); err != nil {
		return nil, err
	}

	// Extract mock provider flags
	if err := extractMockFlags(cmd, params); err != nil {
		return nil, err
	}

	// Extract output format flags
	if err := extractOutputFormatFlags(cmd, cfg, params); err != nil {
		return nil, err
	}

	// Process HTML report settings (maintain backward compatibility)
	if err := processHTMLSettings(cmd, cfg, params); err != nil {
		return nil, err
	}

	// Apply configuration overrides
	applyConfigurationOverrides(cmd, cfg, params)

	return params, nil
}

// extractBasicFlags extracts basic command flags
func extractBasicFlags(cmd *cobra.Command, params *RunParameters) error {
	var err error
	if params.Regions, err = cmd.Flags().GetStringSlice("region"); err != nil {
		return fmt.Errorf("failed to get region flag: %w", err)
	}
	if params.Providers, err = cmd.Flags().GetStringSlice("provider"); err != nil {
		return fmt.Errorf("failed to get provider flag: %w", err)
	}
	if params.Scenarios, err = cmd.Flags().GetStringSlice("scenario"); err != nil {
		return fmt.Errorf("failed to get scenario flag: %w", err)
	}
	if params.Concurrency, err = cmd.Flags().GetInt("concurrency"); err != nil {
		return fmt.Errorf("failed to get concurrency flag: %w", err)
	}
	if params.OutDir, err = cmd.Flags().GetString("out"); err != nil {
		return fmt.Errorf("failed to get out flag: %w", err)
	}
	if params.CIMode, err = cmd.Flags().GetBool("ci"); err != nil {
		return fmt.Errorf("failed to get ci flag: %w", err)
	}
	if params.SimpleMode, err = cmd.Flags().GetBool("simple"); err != nil {
		return fmt.Errorf("failed to get simple flag: %w", err)
	}
	// If either --ci or --simple is set, disable TUI
	if params.SimpleMode {
		params.CIMode = true
	}
	if params.Verbose, err = cmd.Flags().GetBool("verbose"); err != nil {
		return fmt.Errorf("failed to get verbose flag: %w", err)
	}
	return nil
}

// extractMockFlags extracts mock provider configuration flags
func extractMockFlags(cmd *cobra.Command, params *RunParameters) error {
	var err error
	if params.MockProvider, err = cmd.Flags().GetBool("mock-provider"); err != nil {
		return fmt.Errorf("failed to get mock-provider flag: %w", err)
	}
	if params.MockConfig, err = cmd.Flags().GetString("mock-config"); err != nil {
		return fmt.Errorf("failed to get mock-config flag: %w", err)
	}
	return nil
}

// extractOutputFormatFlags extracts output format flags and applies config defaults
func extractOutputFormatFlags(cmd *cobra.Command, cfg *config.Config, params *RunParameters) error {
	var err error

	// Check if --formats (plural) flag was used
	if cmd.Flags().Changed("formats") {
		if params.OutputFormats, err = cmd.Flags().GetStringSlice("formats"); err != nil {
			return fmt.Errorf("failed to get formats flag: %w", err)
		}
	} else {
		// Use --format (singular) flag
		if params.OutputFormats, err = cmd.Flags().GetStringSlice("format"); err != nil {
			return fmt.Errorf("failed to get format flag: %w", err)
		}
		// If format flag wasn't changed, use config defaults, otherwise fallback to json
		if !cmd.Flags().Changed("format") {
			outputConfig := cfg.Defaults.GetOutputConfig()
			if len(outputConfig.Formats) > 0 {
				params.OutputFormats = outputConfig.Formats
			} else {
				params.OutputFormats = []string{"json"} // Default fallback
			}
		}
	}

	if params.JUnitFile, err = cmd.Flags().GetString("junit-file"); err != nil {
		return fmt.Errorf("failed to get junit-file flag: %w", err)
	}
	if params.HTMLFile, err = cmd.Flags().GetString("html-file"); err != nil {
		return fmt.Errorf("failed to get html-file flag: %w", err)
	}
	if params.MarkdownFile, err = cmd.Flags().GetString("markdown-file"); err != nil {
		return fmt.Errorf("failed to get markdown-file flag: %w", err)
	}
	return nil
}

// processHTMLSettings determines HTML report generation settings
// Maintains backward compatibility while transitioning to new format system
func processHTMLSettings(cmd *cobra.Command, cfg *config.Config, params *RunParameters) error {
	// Handle HTML flag and config settings
	if err := processHTMLFlags(cmd, cfg, params); err != nil {
		return err
	}

	// Set default output file paths
	setDefaultFilePaths(cfg, params)

	return nil
}

// processHTMLFlags handles the deprecated --html flag and config HTML settings
func processHTMLFlags(cmd *cobra.Command, cfg *config.Config, params *RunParameters) error {
	if cmd.Flags().Changed("html") {
		return processDeprecatedHTMLFlag(cmd, params)
	}
	if cfg.Defaults.HTMLReport != "" {
		processConfigHTMLSetting(cfg, params)
	}
	return nil
}

// processDeprecatedHTMLFlag handles the deprecated --html flag for backward compatibility
func processDeprecatedHTMLFlag(cmd *cobra.Command, params *RunParameters) error {
	var err error
	params.GenerateHTML, err = cmd.Flags().GetBool("html")
	if err != nil {
		return fmt.Errorf("failed to get html flag: %w", err)
	}
	// Add html to output formats if not already present
	if params.GenerateHTML && !contains(params.OutputFormats, "html") {
		params.OutputFormats = append(params.OutputFormats, "html")
	}
	return nil
}

// processConfigHTMLSetting processes HTML settings from configuration file
func processConfigHTMLSetting(cfg *config.Config, params *RunParameters) {
	params.GenerateHTML = true
	params.HTMLReportPath = cfg.Defaults.HTMLReport
	// Add html to output formats if not already present
	if !contains(params.OutputFormats, "html") {
		params.OutputFormats = append(params.OutputFormats, "html")
	}
}

// setDefaultFilePaths sets default file paths for output files if not specified
func setDefaultFilePaths(cfg *config.Config, params *RunParameters) {
	// Set default JUnit file path
	if params.JUnitFile == "" {
		params.JUnitFile = filepath.Join(params.OutDir, "junit.xml")
	}

	// Set default HTML file path if HTML generation is enabled
	if params.HTMLFile == "" && (params.GenerateHTML || contains(params.OutputFormats, "html")) {
		// First priority: deprecated HTMLReportPath for backward compatibility
		if params.HTMLReportPath != "" {
			params.HTMLFile = config.ResolveOutputPath(params.OutDir, params.HTMLReportPath)
		} else if cfg.Defaults.Output.HTML != nil && cfg.Defaults.Output.HTML.File != "" {
			// Second priority: new Output.HTML.File configuration
			params.HTMLFile = config.ResolveOutputPath(params.OutDir, cfg.Defaults.Output.HTML.File)
		} else {
			// Default: timestamped report file
			timestamp := time.Now().Format("2006-01-02T15-04-05")
			params.HTMLFile = filepath.Join(params.OutDir, fmt.Sprintf("report-%s.html", timestamp))
		}
	}

	// Set default Markdown file path if markdown generation is enabled
	if params.MarkdownFile == "" && contains(params.OutputFormats, "markdown") {
		params.MarkdownFile = filepath.Join(params.OutDir, "results.md")
	}
}

// applyConfigurationOverrides applies command line overrides to configuration
func applyConfigurationOverrides(cmd *cobra.Command, cfg *config.Config, params *RunParameters) {
	// Apply verbose override to config
	if params.Verbose {
		cfg.Defaults.Verbose = true
	}

	// Apply max_tokens override if provided
	if cmd.Flags().Changed(flagMaxTokens) {
		maxTokens, _ := cmd.Flags().GetInt(flagMaxTokens)
		cfg.Defaults.MaxTokens = maxTokens
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
	fmt.Printf("Formats: %s\n", strings.Join(params.OutputFormats, ", "))
	if contains(params.OutputFormats, "junit") {
		fmt.Printf("JUnit XML: %s\n", params.JUnitFile)
	}
	if contains(params.OutputFormats, "html") || params.GenerateHTML {
		fmt.Printf("HTML Report: %s\n", params.HTMLFile)
	}
	if contains(params.OutputFormats, "markdown") {
		fmt.Printf("Markdown Report: %s\n", params.MarkdownFile)
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

	// Apply mock provider override if requested
	if params.MockProvider {
		if err := eng.EnableMockProviderMode(params.MockConfig); err != nil {
			return nil, fmt.Errorf("failed to enable mock provider mode: %w", err)
		}
		if !params.CIMode {
			fmt.Println("Mock Provider Mode: ENABLED")
			if params.MockConfig != "" {
				fmt.Printf("Mock Config: %s\n", params.MockConfig)
			}
		}
	}

	// Generate run plan
	plan, err := eng.GenerateRunPlan(params.Regions, params.Providers, params.Scenarios)
	if err != nil {
		return nil, fmt.Errorf("failed to generate run plan: %w", err)
	}

	params.TotalRuns = len(plan.Combinations)

	// Check if TUI should be used (not CI mode, terminal large enough)
	useTUI := !params.CIMode
	var tuiReason string
	if useTUI {
		w, h, supported, reason := tui.CheckTerminalSize()
		if !supported {
			useTUI = false
			tuiReason = reason
		}
		if params.Verbose {
			if supported {
				log.Printf("TUI mode enabled (terminal size: %dx%d)", w, h)
			} else {
				log.Printf("TUI disabled: %s (terminal size: %dx%d)", reason, w, h)
			}
		}
	}

	// Execute with TUI or simple mode
	var runIDs []string
	ctx := context.Background()

	if useTUI {
		if params.Verbose {
			log.Printf("Starting TUI mode...")
		}
		runIDs, err = executeWithTUI(ctx, eng, plan, params)
	} else {
		if tuiReason != "" && !params.CIMode {
			fmt.Printf("TUI disabled: %s\n", tuiReason)
		}
		runIDs, err = executeSimple(ctx, eng, plan, params)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to execute runs: %w", err)
	}

	// Convert results from statestore format
	return convertRunResults(ctx, eng, runIDs)
}

// executeWithTUI runs simulations with the TUI interface
func executeWithTUI(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	// Create TUI model
	model := tui.NewModel(params.ConfigFile, params.TotalRuns)

	// Create bubbletea program (needed for observer and log interceptor)
	program := bubbletea.NewProgram(
		model,
		bubbletea.WithAltScreen(),
		bubbletea.WithMouseCellMotion(), // Enable mouse support for scrolling
	)

	// Create observer that bridges engine callbacks to bubbletea messages
	observer := tui.NewObserver(program)
	eng.SetObserver(observer)

	// Setup log interceptor to capture logs in TUI
	var logInterceptor *tui.LogInterceptor
	var logFilePath string
	if params.Verbose {
		logFilePath = filepath.Join(params.OutDir, "promptarena.log")
	}

	// Get the current default logger handler and set level based on verbose flag
	logLevel := slog.LevelInfo
	if params.Verbose {
		logLevel = slog.LevelDebug
	}
	currentHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})

	// Create interceptor - buffer stderr output to write after TUI exits
	var err error
	logInterceptor, err = tui.NewLogInterceptor(currentHandler, program, logFilePath, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create log interceptor: %w", err)
	}
	// Install the interceptor as the default logger
	interceptedLogger := slog.New(logInterceptor)
	slog.SetDefault(interceptedLogger)

	// CRITICAL: Also replace runtime logger which is used by providers
	// This is what actually captures the logs we're seeing
	logger.DefaultLogger = interceptedLogger

	// Start execution in background
	var runIDs []string
	var execErr error
	doneChan := make(chan struct{})

	go func() {
		defer close(doneChan)
		runIDs, execErr = eng.ExecuteRuns(ctx, plan, params.Concurrency)
		// Execution complete - user can quit TUI to see summary
	}()

	// Run the TUI (blocks until user quits with 'q' or Ctrl+C)
	if _, tuiErr := program.Run(); tuiErr != nil {
		return nil, fmt.Errorf("TUI error: %w", tuiErr)
	}

	// Check if execution completed, but don't block - user might have quit early
	select {
	case <-doneChan:
		// Execution finished
	default:
		// Still running, that's OK - user quit early
	}

	if execErr != nil {
		return nil, execErr
	}

	// Flush buffered logs to stderr and close log file (if verbose mode)
	if params.Verbose {
		fmt.Fprintf(os.Stderr, "\n=== Execution Logs ===\n\n")

		// Flush synchronously to ensure logs appear before summary
		logInterceptor.FlushBuffer()
		_ = logInterceptor.Close()

		fmt.Fprintf(os.Stderr, "\nLogs also saved to: %s\n\n", logFilePath)
	}

	// Print summary to terminal after TUI exits (use CI mode for clean output)
	if len(runIDs) > 0 {
		summary := model.BuildSummary(params.OutDir, params.HTMLFile)
		fmt.Println(tui.RenderSummaryCIMode(summary))
	}

	return runIDs, nil
}

// executeSimple runs simulations with simple log output (CI mode)
func executeSimple(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	fmt.Printf("Generated %d run combinations\n", len(plan.Combinations))
	fmt.Println("Starting execution...")
	fmt.Println()

	return eng.ExecuteRuns(ctx, plan, params.Concurrency)
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

// processResults processes, saves, and reports execution results using repository pattern
func processResults(results []engine.RunResult, params *RunParameters, configFile string) error {
	// Create output directory
	if err := os.MkdirAll(params.OutDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create composite repository for multiple output formats
	repository, err := createResultRepository(params, configFile)
	if err != nil {
		return fmt.Errorf("failed to create result repository: %w", err)
	}

	// Save results using repository pattern
	if err := repository.SaveResults(results); err != nil {
		return fmt.Errorf("failed to save results: %w", err)
	}

	// Create and save summary
	successCount, errorCount := countResultsByStatus(results)
	summary := createResultSummary(results, successCount, errorCount, configFile)
	if err := repository.SaveSummary(summary); err != nil {
		log.Printf("Warning: failed to save summary: %v", err)
	}

	// Display final summary and handle CI mode errors
	return displayFinalSummary(params, results, successCount, errorCount)
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

		// Show specific output files generated
		if contains(params.OutputFormats, "junit") {
			fmt.Printf("JUnit XML: %s\n", params.JUnitFile)
		}
		if contains(params.OutputFormats, "html") || params.GenerateHTML {
			fmt.Printf("HTML Report: %s\n", params.HTMLFile)
		}
		if contains(params.OutputFormats, "markdown") {
			fmt.Printf("Markdown Report: %s\n", params.MarkdownFile)
		}
	}

	// Exit with error code if any runs failed and CI mode
	if errorCount > 0 && params.CIMode {
		return fmt.Errorf("execution failed: %d runs had errors", errorCount)
	}

	return nil
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
