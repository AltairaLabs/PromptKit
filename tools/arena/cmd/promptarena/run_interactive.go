package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui/app"
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

	// Execute the simulation runs.
	// Always process results even when some runs fail — the report shows
	// which scenarios passed and which failed. Only abort on setup errors
	// (nil results means we couldn't start at all).
	results, execErr := executeRuns(cfg, runParams)
	if results == nil && execErr != nil {
		return execErr
	}

	// Process and save results (reports are always generated)
	if reportErr := processResults(results, runParams, configFile); reportErr != nil {
		return reportErr
	}
	return execErr
}

// executeRuns creates the engine and executes all simulation runs.
// This function handles the decision between TUI and simple mode and manages the full execution lifecycle.
// It is excluded from coverage testing as it requires real engine initialization and user interaction.
func executeRuns(cfg *config.Config, params *RunParameters) ([]engine.RunResult, error) {
	// Create and configure engine
	eng, plan, err := setupEngine(cfg, params)
	if err != nil {
		return nil, err
	}

	params.TotalRuns = len(plan.Combinations)

	// Determine execution mode and run.
	// ExecuteRuns may return partial results with errors (e.g., some scenarios
	// fail assertions). We still need to convert and report all results.
	ctx := context.Background()
	runIDs, execErr := executeWithMode(ctx, eng, plan, params)

	// Convert results from statestore format — always attempt even with errors,
	// so that reports are generated for whatever did complete.
	results, convertErr := convertRunResults(ctx, eng, runIDs)
	if convertErr != nil && execErr != nil {
		return results, fmt.Errorf("execution failed: %w; conversion also failed: %w", execErr, convertErr)
	}
	if convertErr != nil {
		return results, convertErr
	}
	return results, execErr
}

// setupEngine creates and configures the engine with mock provider if needed.
// It accepts an already-loaded config to avoid re-reading and re-parsing the
// configuration file (the caller — executeRuns — receives cfg from loadConfiguration).
func setupEngine(cfg *config.Config, params *RunParameters) (*engine.Engine, *engine.RunPlan, error) {
	// Apply pack eval CLI overrides before building engine components
	cfg.SkipPackEvals = params.SkipPackEvals
	cfg.EvalTypeFilter = params.EvalTypes

	// Apply provider substitutions before building the engine, so the new specs
	// reach the candidate registry, self-play, and judges (which hold the cfg
	// provider pointer) alike.
	if err := applyProviderOverrides(cfg, params.ProviderOverrides); err != nil {
		return nil, nil, err
	}

	eng, err := engine.NewEngineFromConfig(cfg, params.Providers...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create engine: %w", err)
	}

	// Give the engine the resolved output dir so it can expose each run's
	// artifacts base (<outDir>/artifacts/<runID>) to hooks via SessionEvent metadata.
	eng.WithOutputDir(params.OutDir)

	if err := configureMockProvider(eng, params); err != nil {
		return nil, nil, err
	}

	// Enable session recording if configured
	if err = eng.ConfigureSessionRecordingFromConfig(); err != nil {
		return nil, nil, fmt.Errorf("failed to configure session recording: %w", err)
	}

	// Wire up audio monitor from CLI flags. Pre-configured Options enable all
	// surfaces (LocalSink, SSE relay, level meter); the engine itself decides
	// per-run whether to actually attach the MonitorTap based on Mode and TTY.
	if err = eng.EnableAudioMonitor(arenaaudio.Options{
		Mode:        arenaaudio.MonitorMode(params.AudioMonitorMode),
		Rate:        params.AudioMonitorRate,
		LocalSink:   true,
		SSEPlayback: true,
		LevelMeter:  true,
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to enable audio monitor: %w", err)
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

// executeWithTUI runs simulations through the PromptArena TUI hub.
//
// It builds an AppContext around the already-created engine/plan and launches
// the hub with a RunPage as the root page. The RunPage wires the event bus +
// adapter and starts ExecuteRuns on Activate; runtime events stream into the
// live 3-pane view, and selecting a run drills into its conversation via the
// hub's page stack. The run results persist to the engine's state store, so
// after the hub exits we collect the run IDs (from the RunPage, falling back to
// the state store) and return them for report generation.
//
// It is excluded from coverage testing as it requires real terminal
// interaction and cannot be easily tested in a CI environment.
func executeWithTUI(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	// Build the hub context around the already-constructed engine. EnsureEngine
	// short-circuits to this engine and derives the state store from it.
	appCtx := &app.AppContext{
		Config:     eng.GetConfig(),
		ConfigPath: params.ConfigFile,
		ResultsDir: params.OutDir,
		Engine:     eng,
		StateStore: eng.GetStateStore(),
		Version:    GetVersion(),
		Verbose:    params.Verbose,
		LogDir:     params.OutDir,
	}

	runPage := app.NewRunPage(appCtx, eng, plan, params.Concurrency, params.ConfigFile, params.TotalRuns)

	// Launch the hub (blocks until the user quits with 'q' or Ctrl+C).
	if runErr := app.Run(appCtx, runPage); runErr != nil {
		runPage.Cancel()
		return nil, fmt.Errorf("TUI error: %w", runErr)
	}

	// Cancel any still-running execution now that the hub has exited (early quit).
	runPage.Cancel()

	// Collect the run IDs produced by ExecuteRuns. If the user quit before the
	// run finished, fall back to whatever the state store recorded so reports
	// still cover the completed runs.
	runIDs, _, execErr := runPage.Results()
	if len(runIDs) == 0 {
		if arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore); ok {
			if ids, listErr := arenaStore.ListRunIDs(ctx); listErr == nil {
				runIDs = ids
			}
		}
	}

	// Build and print the summary after the hub exits.
	summary := runPage.Model().BuildSummary(params.OutDir, params.HTMLFile)
	fmt.Println()
	fmt.Println(tui.RenderSummary(summary, 80))

	if execErr != nil {
		return runIDs, execErr
	}
	return runIDs, nil
}

// executeSimple runs simulations with simple log output (CI mode).
// This function provides a headless execution mode without TUI interaction.
// It is excluded from coverage testing as it requires real engine execution.
func executeSimple(ctx context.Context, eng *engine.Engine, plan *engine.RunPlan, params *RunParameters) ([]string, error) {
	fmt.Printf("Generated %d run combinations\n", len(plan.Combinations))
	fmt.Println("Starting execution...")
	fmt.Println()

	eventBus := events.NewEventBus()
	eng.SetEventBus(eventBus)

	// In CI / headless mode we don't open an audio device, but if the
	// caller asked for a capture (AUDIO_CAPTURE_PATH) we still wire a
	// headless Monitor so the data path runs end-to-end and writes the
	// stereo bytes to disk. Lets bash invocations produce sample-accurate
	// audio captures without a TTY.
	capturePath := os.Getenv("AUDIO_CAPTURE_PATH")
	if capturePath != "" && eng.AudioMonitorEnabled() {
		opts := eng.AudioMonitorOptions()
		monitor, mErr := arenaaudio.NewMonitor(arenaaudio.MonitorConfig{
			Rate:            opts.Rate,
			EnableLocalSink: true,
			Headless:        true,
			CapturePath:     capturePath,
		})
		if mErr != nil {
			logger.Warn("audio monitor: headless capture disabled", "error", mErr)
		} else {
			defer monitor.Close()
			eng.RegisterAudioMonitorHook(func(runID string, router *arenaaudio.AudioRouter, _ int) {
				monitor.AttachRouter(runID, router)
			})
		}
	}

	runIDs, err := eng.ExecuteRuns(ctx, plan, params.Concurrency)

	// Close event bus to drain pending events and stop worker goroutines.
	eventBus.Close()

	// Return runIDs even on error — runs are in the state store and
	// reports should be generated for whatever completed.
	return runIDs, err
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
