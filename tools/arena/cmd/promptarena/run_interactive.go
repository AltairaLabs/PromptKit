package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/logging"
)

const (
	outputDirPerm = 0750 // Directory permissions for output directories
)

// loadConfiguration loads and parses the arena configuration file
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

// convertRunResults converts run IDs to RunResults by fetching from state store
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
	if err := os.MkdirAll(params.OutDir, outputDirPerm); err != nil {
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

	// Handle CI mode errors and failures
	if params.CIMode {
		failedAssertions := countFailedAssertions(results)
		if errorCount > 0 && failedAssertions > 0 {
			return fmt.Errorf("execution failed: %d runs had errors, %d assertions failed", errorCount, failedAssertions)
		}
		if errorCount > 0 {
			return fmt.Errorf("execution failed: %d runs had errors", errorCount)
		}
		if failedAssertions > 0 {
			return fmt.Errorf("test failures: %d assertions failed", failedAssertions)
		}
	}

	return nil
}

// runSimulations is the main entry point for the run command.
// It orchestrates configuration loading, parameter extraction, execution, and result processing.
// This is an interactive/integration function excluded from coverage testing.
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

// executeRuns creates the engine and executes all simulation runs.
// This function handles the decision between TUI and simple mode and manages the full execution lifecycle.
// It is excluded from coverage testing as it requires real engine initialization and user interaction.
func executeRuns(configFile string, params *RunParameters) ([]engine.RunResult, error) {
	// Create and configure engine
	eng, plan, err := setupEngine(configFile, params)
	if err != nil {
		return nil, err
	}

	params.TotalRuns = len(plan.Combinations)

	// Determine execution mode and run
	ctx := context.Background()
	runIDs, err := executeWithMode(ctx, eng, plan, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute runs: %w", err)
	}

	// Convert results from statestore format
	return convertRunResults(ctx, eng, runIDs)
}

// setupEngine creates and configures the engine with mock provider if needed.
func setupEngine(configFile string, params *RunParameters) (*engine.Engine, *engine.RunPlan, error) {
	// Load config, apply CLI overrides, then build engine
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Apply pack eval CLI overrides before building engine components
	cfg.SkipPackEvals = params.SkipPackEvals
	cfg.EvalTypeFilter = params.EvalTypes

	eng, err := engine.NewEngineFromConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create engine: %w", err)
	}

	if err := configureMockProvider(eng, params); err != nil {
		return nil, nil, err
	}

	// Enable session recording if configured
	if err = eng.ConfigureSessionRecordingFromConfig(); err != nil {
		return nil, nil, fmt.Errorf("failed to configure session recording: %w", err)
	}

	plan, err := eng.GenerateRunPlan(params.Regions, params.Providers, params.Scenarios, params.Evals)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate run plan: %w", err)
	}

	return eng, plan, nil
}

// configureMockProvider enables mock provider mode if requested.
func configureMockProvider(eng *engine.Engine, params *RunParameters) error {
	if !params.MockProvider {
		return nil
	}

	if err := eng.EnableMockProviderMode(params.MockConfig); err != nil {
		return fmt.Errorf("failed to enable mock provider mode: %w", err)
	}

	if !params.CIMode {
		fmt.Println("Mock Provider Mode: ENABLED")
		if params.MockConfig != "" {
			fmt.Printf("Mock Config: %s\n", params.MockConfig)
		}
	}

	return nil
}

// executeWithMode determines whether to use TUI or simple mode and executes accordingly.
func executeWithMode(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	useTUI, tuiReason := shouldUseTUI(params)

	if useTUI {
		if params.Verbose {
			log.Printf("Starting TUI mode...")
		}
		return executeWithTUI(ctx, eng, plan, params)
	}

	if tuiReason != "" && !params.CIMode {
		fmt.Printf("TUI disabled: %s\n", tuiReason)
	}
	return executeSimple(ctx, eng, plan, params)
}

// shouldUseTUI determines if TUI mode should be used based on CI mode and terminal size.
func shouldUseTUI(params *RunParameters) (useTUI bool, reason string) {
	if params.CIMode {
		return false, ""
	}

	w, h, supported, reason := tui.CheckTerminalSize()
	if !supported {
		return false, reason
	}

	if params.Verbose {
		log.Printf("TUI mode enabled (terminal size: %dx%d)", w, h)
	}

	return true, ""
}

// executeWithTUI runs simulations with the TUI interface.
// This function manages the full TUI lifecycle including log interception, observer setup,
// and background execution. It is excluded from coverage testing as it requires real
// terminal interaction and cannot be easily tested in a CI environment.
func executeWithTUI(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	// Create TUI model
	model := tui.NewModel(params.ConfigFile, params.TotalRuns)
	if arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore); ok {
		model.SetStateStore(arenaStore)
	}

	// Create bubbletea program (needed for observer and log interceptor)
	program := bubbletea.NewProgram(
		model,
		bubbletea.WithAltScreen(),
		bubbletea.WithMouseCellMotion(), // Enable mouse support for scrolling
	)

	// Create event bus and adapter for TUI updates
	eventBus := events.NewEventBus()
	eng.SetEventBus(eventBus)
	adapter := tui.NewEventAdapter(program)
	adapter.Subscribe(eventBus)

	// Setup log interceptor to capture logs in TUI
	var logInterceptor *logging.Interceptor
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
	logInterceptor, err = logging.NewInterceptor(currentHandler, program, logFilePath, true)
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
	if logInterceptor != nil {
		logInterceptor.FlushBuffer()
		_ = logInterceptor.Close()
	}

	// Build and print summary after TUI exits
	summary := model.BuildSummary(params.OutDir, params.HTMLFile)
	fmt.Println()
	fmt.Println(tui.RenderSummary(summary, 80))

	return runIDs, nil
}

// executeSimple runs simulations with simple log output (CI mode).
// This function provides a headless execution mode without TUI interaction.
// It is excluded from coverage testing as it requires real engine execution.
func executeSimple(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	fmt.Printf("Generated %d run combinations\n", len(plan.Combinations))
	fmt.Println("Starting execution...")
	fmt.Println()

	// Create TUI model to track execution (without displaying TUI)
	model := tui.NewModel(params.ConfigFile, params.TotalRuns)
	if arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore); ok {
		model.SetStateStore(arenaStore)
	}

	eventBus := events.NewEventBus()
	eng.SetEventBus(eventBus)
	adapter := tui.NewEventAdapterWithModel(model)
	adapter.Subscribe(eventBus)

	runIDs, err := eng.ExecuteRuns(ctx, plan, params.Concurrency)
	if err != nil {
		return nil, err
	}

	// Build and print summary after execution
	summary := model.BuildSummary(params.OutDir, params.HTMLFile)
	fmt.Println()
	fmt.Println(tui.RenderSummaryCIMode(summary))

	return runIDs, nil
}

// displayRunInfo prints configuration and execution parameters.
// This is UI/display code excluded from coverage requirements.
func displayRunInfo(params *RunParameters, configFile string) {
	if params.CIMode {
		return
	}

	fmt.Printf("Running Altaira Prompt Arena\n")
	fmt.Printf("Config: %s\n", configFile)
	if len(params.Regions) > 0 {
		fmt.Printf("Regions: %s\n", strings.Join(params.Regions, ", "))
	}
	if len(params.Providers) > 0 {
		fmt.Printf("Providers: %s\n", strings.Join(params.Providers, ", "))
	}
	if len(params.Scenarios) > 0 {
		fmt.Printf("Scenarios: %s\n", strings.Join(params.Scenarios, ", "))
	}
	if len(params.Evals) > 0 {
		fmt.Printf("Evaluations: %s\n", strings.Join(params.Evals, ", "))
	}
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
