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
	"sync"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
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
	mcpRegistry          *mcp.RegistryImpl // Registry for MCP servers
	stateStore           statestore.Store  // State store for conversation persistence (always enabled)
	scenarios            map[string]*config.Scenario
	providers            map[string]*config.Provider
	personas             map[string]*config.UserPersonaPack
	conversationExecutor ConversationExecutor
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

	// Build registries and executors from the config
	providerRegistry, promptRegistry, mcpRegistry, convExecutor, err := buildEngineComponents(cfg)
	if err != nil {
		return nil, err
	}

	// Create engine with all components
	return NewEngine(cfg, providerRegistry, promptRegistry, mcpRegistry, convExecutor)
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
//
// Returns an initialized Engine ready for test execution.
func NewEngine(
	cfg *config.Config,
	providerRegistry *providers.Registry,
	promptRegistry *prompt.Registry,
	mcpRegistry *mcp.RegistryImpl,
	convExecutor ConversationExecutor,
) (*Engine, error) {
	// Build state store from config if configured
	stateStore, err := buildStateStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build state store: %w", err)
	}

	return &Engine{
		config:               cfg,
		providerRegistry:     providerRegistry,
		promptRegistry:       promptRegistry,
		mcpRegistry:          mcpRegistry,
		stateStore:           stateStore,
		conversationExecutor: convExecutor,
		scenarios:            cfg.LoadedScenarios,
		providers:            cfg.LoadedProviders,
		personas:             cfg.LoadedPersonas,
	}, nil
}

// Close shuts down the engine and cleans up resources.
// This includes closing all MCP server connections and provider HTTP clients.
func (e *Engine) Close() error {
	if err := e.closeMCPRegistry(); err != nil {
		return err
	}

	if err := e.closeProviderRegistry(); err != nil {
		return err
	}

	if err := e.closeSelfPlayRegistry(); err != nil {
		return err
	}

	return nil
}

// EnableMockProviderMode replaces all providers in the registry with MockProvider instances.
// This is useful for CI/testing scenarios where deterministic responses are needed without
// making actual API calls. If mockConfigPath is provided, it will be used to configure
// scenario-specific mock responses (feature not yet implemented).
//
// Parameters:
//   - mockConfigPath: Optional path to YAML configuration file for mock responses
//
// Returns an error if mock providers cannot be created or if mock config is not yet supported.
func (e *Engine) EnableMockProviderMode(mockConfigPath string) error {
	// Mock configuration file support is not yet implemented
	// This will be added in a future enhancement to support scenario-specific responses
	if mockConfigPath != "" {
		return fmt.Errorf("mock configuration file support is not yet implemented (see #27)")
	}

	// Create a new provider registry with mock providers
	mockRegistry := providers.NewRegistry()

	// Replace each provider with a MockProvider using the same ID
	for providerID, provider := range e.providers {
		mockProvider := providers.NewMockProvider(
			providerID,
			provider.Model,
			provider.IncludeRawOutput,
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
