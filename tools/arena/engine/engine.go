// Package engine orchestrates test execution across scenarios, providers, and configurations.
//
// The engine package is the core execution layer of the Arena testing tool. It manages:
//   - Conversation lifecycle and message flow
//   - Provider and model configuration
//   - Telemetry and metrics collection
//   - Result aggregation and validation
//   - Concurrent test execution across multiple scenarios
//
// Key types:
//   - Engine: Main orchestration struct that executes test runs
//   - RunResult: Contains execution results, metrics, and conversation history
//   - RunFilters: Filters for selective test execution
//
// Example usage:
//
//	eng, _ := engine.NewEngine(cfg, providerRegistry, promptRegistry)
//	results, _ := eng.Execute(ctx, filters)
//	for _, result := range results {
//	    fmt.Printf("Scenario %s: %s\n", result.ScenarioID, result.Status)
//	}
package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
)

// Default directory names for output.
const (
	defaultOutputDir     = "out"
	defaultRecordingsDir = "recordings"
)

// Engine manages the execution of prompt testing scenarios across multiple
// providers, regions, and configurations. It coordinates conversation execution,
// tool calling, validation, and result collection.
//
// The engine supports both scripted conversations and self-play mode where
// an LLM simulates user behavior. It handles provider initialization, concurrent
// execution, and comprehensive result tracking including costs and tool usage.
type Engine struct {
	config               *config.Config
	providerRegistry     *providers.Registry // Registry for looking up providers by ID
	promptRegistry       *prompt.Registry
	mcpRegistry          *mcp.RegistryImpl           // Registry for MCP servers
	stateStore           statestore.Store            // State store for conversation persistence (always enabled)
	mediaStorage         storage.MediaStorageService // Media storage for externalization (always enabled)
	scenarios            map[string]*config.Scenario
	evals                map[string]*config.Eval
	providers            map[string]*config.Provider
	personas             map[string]*config.UserPersonaPack
	conversationExecutor ConversationExecutor
	toolRegistry         *tools.Registry    // Registry for tool descriptors and executors (used by workflow driver)
	adapterRegistry      *adapters.Registry // Registry for recording adapters (used for eval enumeration)
	eventBus             *events.EventBus   // Optional event bus for runtime/TUI events
	eventStore           events.EventStore // Optional event store for session recording
	recordingDir         string            // Directory where session recordings are stored
	a2aCleanup           func()            // Cleanup function for mock A2A servers
	packEvalHook         *PackEvalHook     // Pack eval hook for running evals during execution
}

// NewEngineFromConfigFile creates a new simulation engine from a configuration file.
// It loads the configuration, validates it, initializes all registries,
// and sets up the execution pipeline for conversation testing.
//
// The configuration file is loaded along with all referenced resources
// (scenarios, providers, tools, personas), making the Config object
// fully self-contained.
//
// This constructor performs all necessary initialization steps in the correct order:
// 1. Load and validate configuration from file (including all referenced resources)
// 2. Build registries from loaded resources
// 3. Initialize executors (turn, conversation, self-play if enabled)
// 4. Create Engine with all components
//
// Note: Logger verbosity should be configured at application startup, not here.
// This function does not modify global logger settings.
//
// Parameters:
//   - configPath: Path to the arena.yaml configuration file
//
// Returns an initialized Engine ready for test execution, or an error if:
//   - Configuration file cannot be read or parsed
//   - Configuration validation fails
//   - Any resource file cannot be loaded
//   - Provider type is unsupported
func NewEngineFromConfigFile(configPath string) (*Engine, error) {
	// Load configuration from file (including all referenced resources)
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return NewEngineFromConfig(cfg)
}

// NewEngineFromConfig creates a new Engine from a pre-loaded configuration.
// This allows CLI or programmatic callers to modify the config before engine creation.
func NewEngineFromConfig(cfg *config.Config) (*Engine, error) {
	// Build registries and executors from the config
	providerRegistry, promptRegistry, mcpRegistry, convExecutor,
		adapterRegistry, a2aCleanup, toolRegistry, err := BuildEngineComponents(cfg)
	if err != nil {
		return nil, err
	}

	// Create engine with all components
	eng, err := NewEngine(cfg, providerRegistry, promptRegistry, mcpRegistry, convExecutor, adapterRegistry)
	if err != nil {
		if a2aCleanup != nil {
			a2aCleanup()
		}
		return nil, err
	}
	eng.a2aCleanup = a2aCleanup
	eng.toolRegistry = toolRegistry
	// Build pack eval hook for workflow executor (conversation executors get their own copy).
	// Eval type validation already passed in BuildEngineComponents above, so this call
	// cannot fail — unknown types were already rejected.
	if cfg.LoadedPack != nil {
		eng.packEvalHook, _ = buildPackEvalHook(cfg, cfg.SkipPackEvals, cfg.EvalTypeFilter)
	}
	return eng, nil
}

// NewEngine creates a new simulation engine from pre-built components.
// This is the primary constructor for the Engine and is preferred for testing
// where components can be created and configured independently.
//
// This constructor uses dependency injection, accepting all required registries
// and executors as parameters. This makes testing easier and follows better
// architectural practices.
//
// Parameters:
//   - cfg: Fully loaded and validated Config object
//   - providerRegistry: Registry for looking up providers by ID
//   - promptRegistry: Registry for system prompts and task types
//   - convExecutor: Executor for full conversations
//   - adapterRegistry: Registry for recording adapters (used for eval enumeration)
//
// Returns an initialized Engine ready for test execution.
func NewEngine(
	cfg *config.Config,
	providerRegistry *providers.Registry,
	promptRegistry *prompt.Registry,
	mcpRegistry *mcp.RegistryImpl,
	convExecutor ConversationExecutor,
	adapterRegistry *adapters.Registry,
) (*Engine, error) {
	// Build state store from config if configured
	stateStore, err := buildStateStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build state store: %w", err)
	}

	// Build media storage service for externalization
	mediaStorage, err := buildMediaStorage(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build media storage: %w", err)
	}

	return &Engine{
		config:               cfg,
		providerRegistry:     providerRegistry,
		promptRegistry:       promptRegistry,
		mcpRegistry:          mcpRegistry,
		stateStore:           stateStore,
		mediaStorage:         mediaStorage,
		conversationExecutor: convExecutor,
		adapterRegistry:      adapterRegistry,
		scenarios:            cfg.LoadedScenarios,
		evals:                cfg.LoadedEvals,
		providers:            cfg.LoadedProviders,
		personas:             cfg.LoadedPersonas,
	}, nil
}

// Close shuts down the engine and cleans up resources.
// This includes closing all MCP server connections, provider HTTP clients,
// and the event store if session recording is enabled.
func (e *Engine) Close() error {
	if e.a2aCleanup != nil {
		e.a2aCleanup()
	}

	if err := e.closeMCPRegistry(); err != nil {
		return err
	}

	if err := e.closeProviderRegistry(); err != nil {
		return err
	}

	if err := e.closeSelfPlayRegistry(); err != nil {
		return err
	}

	return e.closeEventStore()
}

// SetEventBus configures the shared event bus used for runtime and TUI observability.
// If session recording is enabled, the event bus will be wired to the event store.
func (e *Engine) SetEventBus(bus *events.EventBus) {
	e.eventBus = bus
	// Wire event store to bus if both are configured
	if e.eventStore != nil && bus != nil {
		bus.WithStore(e.eventStore)
		logger.Debug("Wired event store to event bus for session recording")
	}
}

// EnableSessionRecording enables session recording for all runs.
// Recordings are stored in the specified directory as JSONL files,
// one file per session (using RunID as session ID).
// Returns an error if the directory cannot be created.
func (e *Engine) EnableSessionRecording(recordingDir string) error {
	store, err := events.NewFileEventStore(recordingDir)
	if err != nil {
		return fmt.Errorf("failed to create event store: %w", err)
	}
	e.eventStore = store
	e.recordingDir = recordingDir

	// Wire to existing event bus if present
	if e.eventBus != nil {
		e.eventBus.WithStore(store)
		logger.Debug("Wired event store to event bus for session recording")
	}

	logger.Info("Session recording enabled", "directory", recordingDir)
	return nil
}

// GetRecordingDir returns the directory where session recordings are stored.
// Returns empty string if recording is not enabled.
func (e *Engine) GetRecordingDir() string {
	return e.recordingDir
}

// GetRecordingPath returns the path to the recording file for a given run ID.
// Returns empty string if recording is not enabled.
func (e *Engine) GetRecordingPath(runID string) string {
	if e.recordingDir == "" {
		return ""
	}
	return filepath.Join(e.recordingDir, runID+".jsonl")
}

// GetConfig returns the engine's configuration.
func (e *Engine) GetConfig() *config.Config {
	return e.config
}

// ConfigureSessionRecordingFromConfig enables session recording if configured.
// It reads the recording configuration from the engine's config and enables
// session recording with the appropriate directory path.
// Returns nil if recording is not enabled in the config.
func (e *Engine) ConfigureSessionRecordingFromConfig() error {
	rec := e.config.Defaults.Output.Recording
	if rec == nil || !rec.Enabled {
		return nil
	}

	// Determine recording directory
	outDir := e.config.Defaults.Output.Dir
	if outDir == "" {
		outDir = defaultOutputDir
	}
	recDir := rec.Dir
	if recDir == "" {
		recDir = defaultRecordingsDir
	}
	recordingPath := filepath.Join(outDir, recDir)

	return e.EnableSessionRecording(recordingPath)
}

// EnableMockProviderMode replaces all providers in the registry with mock providers.
// This enables testing of scenario behavior without making real API calls.
// Mock providers can use either file-based configuration for scenario-specific
// responses or default in-memory responses.
//
// Parameters:
//   - mockConfigPath: Optional path to YAML configuration file for mock responses
//
// Returns an error if the mock configuration file cannot be loaded or parsed.
func (e *Engine) EnableMockProviderMode(mockConfigPath string) error {
	var repository mock.ResponseRepository

	// Create appropriate repository based on whether config file is provided
	if mockConfigPath != "" {
		fileRepo, err := mock.NewFileMockRepository(mockConfigPath)
		if err != nil {
			return fmt.Errorf("failed to load mock configuration from %s: %w", mockConfigPath, err)
		}
		repository = fileRepo
	} else {
		// Use default in-memory repository with generic responses
		repository = mock.NewInMemoryMockRepository("Mock response from provider")
	}

	// Create a new provider registry with mock providers
	mockRegistry := providers.NewRegistry()

	// Replace each provider with a MockToolProvider using the same ID for tool call simulation
	for providerID, provider := range e.providers {
		mockProvider := mock.NewToolProviderWithRepository(
			providerID,
			provider.Model,
			provider.IncludeRawOutput,
			repository,
		)
		mockRegistry.Register(mockProvider)
	}

	// Replace the engine's provider registry
	e.providerRegistry = mockRegistry

	return nil
}

// closeMCPRegistry closes the MCP registry if initialized
func (e *Engine) closeMCPRegistry() error {
	if e.mcpRegistry != nil {
		if err := e.mcpRegistry.Close(); err != nil {
			return fmt.Errorf("failed to close MCP registry: %w", err)
		}
	}
	return nil
}

// closeProviderRegistry closes the provider registry
func (e *Engine) closeProviderRegistry() error {
	if e.providerRegistry != nil {
		if err := e.providerRegistry.Close(); err != nil {
			return fmt.Errorf("failed to close provider registry: %w", err)
		}
	}
	return nil
}

// closeSelfPlayRegistry closes the self-play registry if it exists
func (e *Engine) closeSelfPlayRegistry() error {
	if e.conversationExecutor == nil {
		return nil
	}

	executor, ok := e.conversationExecutor.(*DefaultConversationExecutor)
	if !ok || executor.selfPlayRegistry == nil {
		return nil
	}

	if err := executor.selfPlayRegistry.Close(); err != nil {
		return fmt.Errorf("failed to close self-play registry: %w", err)
	}

	return nil
}

// closeEventStore closes the event store if session recording is enabled
func (e *Engine) closeEventStore() error {
	if e.eventStore == nil {
		return nil
	}

	if err := e.eventStore.Close(); err != nil {
		return fmt.Errorf("failed to close event store: %w", err)
	}

	return nil
}

// GetStateStore returns the engine's state store for accessing run results
// Runs are executed concurrently up to the specified concurrency limit, with run IDs
// collected in order matching the input plan.
//
// Each run executes independently:
// - Loads scenario and provider
// - Executes conversation turns (with self-play if configured)
// - Runs validators on the results
// - Tracks costs, timing, and tool calls
// - Saves results to StateStore
//
// The context can be used to cancel all in-flight executions. Run IDs are returned
// for all combinations, with errors captured in individual RunResult (accessible via StateStore).
//
// Parameters:
//   - ctx: Context for cancellation
//   - plan: RunPlan containing combinations to execute
//   - concurrency: Maximum number of simultaneous executions
//
// Returns a slice of RunIDs in the same order as plan.Combinations, or an error
// if execution setup fails. Individual run errors are stored in StateStore, not returned here.
func (e *Engine) ExecuteRuns(ctx context.Context, plan *RunPlan, concurrency int) ([]string, error) {
	runIDs := make([]string, len(plan.Combinations))
	errors := make([]error, len(plan.Combinations))
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, combination := range plan.Combinations {
		wg.Add(1)
		go func(idx int, combo RunCombination) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			runID, err := e.executeRun(ctx, combo)

			mu.Lock()
			runIDs[idx] = runID
			errors[idx] = err
			mu.Unlock()
		}(i, combination)
	}

	wg.Wait()

	// Check if any executions failed to save metadata
	var executionErrors []error
	for _, err := range errors {
		if err != nil {
			executionErrors = append(executionErrors, err)
		}
	}

	if len(executionErrors) > 0 {
		return runIDs, fmt.Errorf("some runs failed to save: %v", executionErrors)
	}

	return runIDs, nil
}

// GetStateStore returns the engine's state store for accessing run results
func (e *Engine) GetStateStore() statestore.Store {
	return e.stateStore
}

// buildMediaStorage creates a media storage service for media externalization.
// It stores media files in the <output_dir>/media/ subdirectory using run-based organization.
// This enables automatic externalization of large media content to reduce memory usage and
// improve performance when processing media-heavy scenarios.
func buildMediaStorage(cfg *config.Config) (storage.MediaStorageService, error) {
	// Determine the media storage directory
	outDir := cfg.Defaults.Output.Dir
	if outDir == "" {
		outDir = defaultOutputDir
	}

	mediaDir := filepath.Join(outDir, "media")

	// Create local file store with run-based organization
	fileStore, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:             mediaDir,
		Organization:        storage.OrganizationByRun,
		EnableDeduplication: true,
		DefaultPolicy:       "retain",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create media file store: %w", err)
	}

	return fileStore, nil
}
