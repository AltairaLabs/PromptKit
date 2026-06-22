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
	"os"
	"path/filepath"
	"sync"

	"go.opentelemetry.io/otel/trace"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/metrics"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/artifacts"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
	arenastore "github.com/AltairaLabs/PromptKit/tools/arena/statestore"
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
	toolRegistry         *tools.Registry              // Tool descriptors and executors (workflow driver)
	adapterRegistry      *adapters.Registry           // Registry for recording adapters (used for eval enumeration)
	eventBus             events.Bus                   // Optional event bus for runtime/TUI events
	eventStore           events.EventStore            // Optional event store for session recording
	otelListener         *telemetry.OTelEventListener // Optional OTel listener for distributed tracing
	recordingDir         string                       // Directory where session recordings are stored
	a2aCleanup           func()                       // Cleanup function for mock A2A servers
	evalOrchestrator     *EvalOrchestrator            // Orchestrates eval and assertion execution during runs
	workflowSpec         *workflow.Spec               // Optional workflow spec (set if config.Workflow != nil)
	workflowTransExec    *workflowTransitionExecutor  // Optional transition executor (set if config.Workflow != nil)
	skillExecutor        SkillFilterer                // Optional — set when skills are configured
	memoryStore          *memory.InMemoryStore        // Optional memory store (set if config.Memory != nil)
	recordingConfig      *stage.RecordingStageConfig  // Optional — enables RecordingStage in pipelines
	audioMonitorOpts     *arenaaudio.Options          // Optional — enables audio monitoring on duplex runs
	audioMonitorHooks    []AudioMonitorHook           // Subscribers fired when a per-run AudioRouter is built
	audioMonitorMu       sync.RWMutex                 // Guards audioMonitorHooks
	runCompletedHooks    []RunCompletedHook           // Subscribers fired when each run finishes
	runCompletedMu       sync.RWMutex                 // Guards runCompletedHooks
	sessionHooks         *hooks.Registry              // Optional — fires SessionHook lifecycle per run
	outputDir            string                       // Resolved report output dir; exposes per-run artifacts base
	artifactStore        artifacts.Store              // Persists per-run report artifacts (default: local backend)
	// mcpSourceScope manages source-backed MCP entries at run/scenario/session scopes.
	mcpSourceScope  *mcpSourceScope
	mcpConfig       []config.MCPServerConfig   // Source-backed MCP entries, re-read at each scope boundary
	mcpSkillSources []prompt.SkillSourceConfig // Skill sources to mount into source-backed MCP servers
}

// AudioMonitorHook is invoked when the engine constructs a per-run AudioRouter.
// Hooks can use the router to attach additional consumers (web SSE relay, TUI
// level meter). The router lifecycle is the run; hooks must not retain the
// pointer after the corresponding run completes and must not call Close (the
// engine owns lifecycle).
//
// runID is the unique identifier for this run; rate is the canonical sample
// rate of the router.
type AudioMonitorHook func(runID string, router *arenaaudio.AudioRouter, rate int)

// RegisterAudioMonitorHook registers a callback fired whenever the engine
// constructs an AudioRouter for a run. Multiple hooks can be registered;
// each fires in registration order. Nil hooks are ignored.
//
// Hooks should treat the router as borrowed — Subscribe / SubscribeRMS as
// needed for the run's duration, but never call Close. Hook panics propagate
// to the goroutine running the run; treat hook bodies as application code
// and recover internally if needed.
func (e *Engine) RegisterAudioMonitorHook(hook AudioMonitorHook) {
	if hook == nil {
		return
	}
	e.audioMonitorMu.Lock()
	e.audioMonitorHooks = append(e.audioMonitorHooks, hook)
	e.audioMonitorMu.Unlock()
}

// fireAudioMonitorHooks invokes all registered hooks in registration order.
// The slice is snapshotted under the lock so a hook that registers another
// hook does not race or deadlock with iteration.
func (e *Engine) fireAudioMonitorHooks(runID string, router *arenaaudio.AudioRouter, rate int) {
	e.audioMonitorMu.RLock()
	audioHooks := make([]AudioMonitorHook, len(e.audioMonitorHooks))
	copy(audioHooks, e.audioMonitorHooks)
	e.audioMonitorMu.RUnlock()
	for _, h := range audioHooks {
		h(runID, router, rate)
	}
}

// RunCompletedHook fires after each individual run finishes (success or
// failure). Used by the web server to persist completed runs to disk as
// soon as they're done, so killing the server mid-batch doesn't strand
// completed runs in the in-memory state store with no on-disk record.
//
// runID is the identifier of the run that just completed. err is the
// run's error, or nil on success. Hooks should not retain runID across
// calls — the next run uses a fresh ID.
type RunCompletedHook func(runID string, err error)

// RegisterRunCompletedHook registers a callback fired after each run
// in ExecuteRuns completes. Multiple hooks are supported and fire in
// registration order. Nil hooks are ignored. Hook panics propagate to
// the worker goroutine; treat hook bodies as application code.
func (e *Engine) RegisterRunCompletedHook(hook RunCompletedHook) {
	if hook == nil {
		return
	}
	e.runCompletedMu.Lock()
	e.runCompletedHooks = append(e.runCompletedHooks, hook)
	e.runCompletedMu.Unlock()
}

func (e *Engine) fireRunCompletedHooks(runID string, err error) {
	e.runCompletedMu.RLock()
	runHooks := make([]RunCompletedHook, len(e.runCompletedHooks))
	copy(runHooks, e.runCompletedHooks)
	e.runCompletedMu.RUnlock()
	for _, h := range runHooks {
		h(runID, err)
	}
}

// WithSessionHooks configures session lifecycle hooks on the Engine.
// When set, the engine fires OnSessionStart before the first turn,
// OnSessionUpdate after each turn, and OnSessionEnd after the conversation
// completes. A nil registry is a no-op — no hooks are fired.
func (e *Engine) WithSessionHooks(reg *hooks.Registry) {
	e.sessionHooks = reg
}

// outputDirPerm is the permission for engine-created output directories.
const outputDirPerm = 0o750

// WithOutputDir sets the resolved report output directory so the engine can
// expose each run's artifacts base (<outputDir>/artifacts/<runID>) to hooks
// via SessionEvent metadata. A nil/empty dir disables artifacts-base exposure.
func (e *Engine) WithOutputDir(dir string) {
	e.outputDir = dir
}

// WithArtifactStore sets the backend that persists per-run report artifacts.
// Defaults to the local backend (bytes left in place) when unset.
func (e *Engine) WithArtifactStore(s artifacts.Store) {
	e.artifactStore = s
}

// sessionEventMetadata returns the per-event metadata map injected into every
// SessionEvent fired by the engine. It includes the tool registry so that
// SessionHook implementations (e.g. tool_exec eval) can access registered tools.
func (e *Engine) sessionEventMetadata(runCtx context.Context, runID string) map[string]any {
	meta := map[string]any{
		"tool_registry": e.toolRegistry,
	}
	// Surface this run's open sandbox container IDs (keyed by server name) so
	// session hooks can reach the sandbox — e.g. a workspace-capture hook running
	// `docker cp`. The IDs come from the per-run MCP scope via the run context.
	if cids := sandboxContainerIDsFromContext(runCtx); len(cids) > 0 {
		meta["sandbox_containers"] = cids
	}
	// Expose this run's artifacts directory — the base path, owned by Arena from
	// config (<outDir>/artifacts/<runID>). Hooks write their outputs here and
	// record only relative filenames; the report links <base>/<filename>. Arena
	// creates the dir so hooks don't have to know the output layout.
	if e.outputDir != "" && runID != "" {
		dir := filepath.Join(e.outputDir, "artifacts", runID)
		if err := os.MkdirAll(dir, outputDirPerm); err == nil {
			meta["artifacts_dir"] = dir
		}
	}
	return meta
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
// providerFilter limits which providers have credentials resolved; nil or empty
// means all providers are initialized.
func NewEngineFromConfig(cfg *config.Config, providerFilter ...string) (*Engine, error) {
	// Build registries and executors from the config
	providerRegistry, promptRegistry, mcpRegistry, convExecutor,
		adapterRegistry, a2aCleanup, toolRegistry, skillExec, err := BuildEngineComponents(cfg, providerFilter)
	if err != nil {
		return nil, err
	}

	// Create engine with all components
	eng, err := NewEngine(cfg, providerRegistry, promptRegistry, mcpRegistry, convExecutor, adapterRegistry, toolRegistry)
	if err != nil {
		if a2aCleanup != nil {
			a2aCleanup()
		}
		return nil, err
	}
	eng.a2aCleanup = a2aCleanup
	eng.skillExecutor = skillExec

	// Initialize workflow state machine and register transition tool if configured
	if err := eng.initWorkflow(); err != nil {
		if a2aCleanup != nil {
			a2aCleanup()
		}
		return nil, fmt.Errorf("failed to initialize workflow: %w", err)
	}

	// Initialize memory subsystem if configured
	if err := eng.initMemory(); err != nil {
		if a2aCleanup != nil {
			a2aCleanup()
		}
		return nil, fmt.Errorf("failed to initialize memory: %w", err)
	}

	// Wire pass-through runtime config (cfg.Runtime): build hooks from
	// cfg.Runtime.Hooks using the same runtime builder the SDK uses.
	if err := eng.applyRuntimeHooks(); err != nil {
		if a2aCleanup != nil {
			a2aCleanup()
		}
		return nil, fmt.Errorf("failed to apply runtime hooks: %w", err)
	}

	// Use the eval orchestrator from the conversation executor — it already
	// has judge metadata, prompt_registry, and other config injected by
	// BuildEngineComponents. Creating a separate orchestrator here would
	// lose that metadata.
	eng.evalOrchestrator = evalOrchestratorFrom(convExecutor)
	return eng, nil
}

// evalOrchestratorFrom extracts the EvalOrchestrator out of whatever
// ConversationExecutor shape BuildEngineComponents returned.
//
// BuildEngineComponents wraps the DefaultConversationExecutor in a
// CompositeConversationExecutor for routing between default / duplex /
// eval modes, so the type assertion must reach through the composite.
// Without this, eng.evalOrchestrator stays nil for every run; workflow
// scenarios in particular silently lose their per-run metadata provider
// because prepareWorkflowScenario short-circuits on nil evalOrchestrator
// and returns nil. That manifests as `state_is` / `transitioned_to`
// assertions reporting "no workflow state in context" even when the
// state machine is running correctly (#1169).
//
// Returns nil if no orchestrator can be reached — callers must guard.
func evalOrchestratorFrom(executor ConversationExecutor) *EvalOrchestrator {
	switch ce := executor.(type) {
	case *DefaultConversationExecutor:
		return ce.evalOrchestrator
	case *CompositeConversationExecutor:
		if ce.defaultExecutor != nil {
			return ce.defaultExecutor.evalOrchestrator
		}
	}
	return nil
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
	toolRegistry *tools.Registry,
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

	eng := &Engine{
		config:               cfg,
		providerRegistry:     providerRegistry,
		promptRegistry:       promptRegistry,
		mcpRegistry:          mcpRegistry,
		stateStore:           stateStore,
		mediaStorage:         mediaStorage,
		conversationExecutor: convExecutor,
		adapterRegistry:      adapterRegistry,
		toolRegistry:         toolRegistry,
		scenarios:            cfg.LoadedScenarios,
		evals:                cfg.LoadedEvals,
		providers:            cfg.LoadedProviders,
		personas:             cfg.LoadedPersonas,
		mcpConfig:            cfg.MCPServers,
		mcpSkillSources:      cfg.LoadedSkillSources,
	}

	// Wire the source-backed MCP scope manager when a registry is available.
	// Run-scoped sources open at engine construction; they're typically fast.
	// If slow sources become common, thread ctx through NewEngine.
	if mcpRegistry != nil {
		eng.mcpSourceScope = newMCPSourceScopeWithTools(mcpRegistry, toolRegistry)
		if err := eng.mcpSourceScope.OpenAll(
			context.Background(), mcpsource.ScopeRun, "run", nil,
			eng.mcpSkillSources, eng.mcpConfig,
		); err != nil {
			return nil, fmt.Errorf("open run-scoped MCP sources: %w", err)
		}
	}

	return eng, nil
}

// Close shuts down the engine and cleans up resources.
// This includes closing all MCP server connections, provider HTTP clients,
// and the event store if session recording is enabled.
func (e *Engine) Close() error {
	if e.a2aCleanup != nil {
		e.a2aCleanup()
	}

	// Close run-scoped MCP sources before tearing down the underlying registry.
	if e.mcpSourceScope != nil {
		for _, cerr := range e.mcpSourceScope.CloseAll(mcpsource.ScopeRun, "run") {
			logger.Warn("mcp source close failed (run scope)", "error", cerr)
		}
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

	if e.otelListener != nil {
		e.otelListener.Close()
	}

	return e.closeEventStore()
}

// EventBusOption configures optional behavior when setting the event bus.
type EventBusOption func(*eventBusConfig)

type eventBusConfig struct {
	emitMessages bool
}

// WithMessageEvents enables RecordingStage in pipelines so message.created
// events are published to the event bus. This does NOT write session recordings
// to disk — use EnableSessionRecording for that.
func WithMessageEvents() EventBusOption {
	return func(c *eventBusConfig) { c.emitMessages = true }
}

// SetEventBus configures the shared event bus used for runtime and TUI observability.
// If session recording is enabled, the event store is subscribed to the bus.
func (e *Engine) SetEventBus(bus events.Bus, opts ...EventBusOption) {
	var cfg eventBusConfig
	for _, o := range opts {
		o(&cfg)
	}

	e.eventBus = bus
	// Subscribe event store to bus if both are configured. The subscription is
	// scoped to low-rate replay-essential events that don't flow through the
	// pipeline as StreamElement; high-frequency events (audio, messages, tools)
	// are picked up by the pipeline tap.
	if e.eventStore != nil && bus != nil {
		bus.Subscribe(events.EventConversationStarted, e.eventStore.OnEvent)
		bus.Subscribe(events.EventStreamInterrupted, e.eventStore.OnEvent)
		logger.Debug("Subscribed event store for session recording (targeted)")
	}
	// Wire event bus to eval orchestrator for judge provider telemetry
	if e.evalOrchestrator != nil {
		e.evalOrchestrator.SetEventBus(bus)
	}
	// Late-bind the bus on the instrumented media storage so storage
	// lifecycle events can flow through. Built before the bus exists,
	// so the wrapper started with a nil bus.
	if instrumented, ok := e.mediaStorage.(*storage.InstrumentedStorage); ok {
		instrumented.SetBus(bus)
	}
	// Enable RecordingStage so message.created events flow to the bus
	if cfg.emitMessages {
		e.EnableMessageEvents()
	}
}

// SetTracerProvider configures OpenTelemetry distributed tracing for the engine.
// When set, an OTelEventListener is created and subscribed to the event bus,
// converting provider call, tool call, and pipeline events into OTel spans.
// An event bus must be configured via SetEventBus before calling this method.
func (e *Engine) SetTracerProvider(tp trace.TracerProvider) {
	if tp == nil || e.eventBus == nil {
		return
	}
	tracer := telemetry.Tracer(tp)
	listener := telemetry.NewOTelEventListener(tracer)
	e.eventBus.SubscribeAll(listener.OnEvent)
	e.otelListener = listener
	logger.Debug("OTel tracing enabled for Arena engine")
}

// SetMetrics configures Prometheus metrics collection for the engine.
// When set, a MetricContext is created and subscribed to the event bus,
// recording provider call durations, token counts, costs, and tool call metrics.
// An event bus must be configured via SetEventBus before calling this method.
func (e *Engine) SetMetrics(collector *metrics.Collector, instanceLabels map[string]string) {
	if collector == nil || e.eventBus == nil {
		return
	}
	metricCtx := collector.Bind(instanceLabels)
	e.eventBus.SubscribeAll(metricCtx.OnEvent)
	logger.Debug("Metrics collection enabled for Arena engine")
}

// EnableMessageEvents enables RecordingStage in pipelines so message.created
// events are published to the event bus. This does NOT write session recordings
// to disk — use EnableSessionRecording for that. Requires an event bus to be
// configured via SetEventBus.
func (e *Engine) EnableMessageEvents() {
	if e.recordingConfig == nil {
		defaults := stage.DefaultRecordingStageConfig()
		e.recordingConfig = &defaults
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
	e.recordingDir = recordingDir
	if err := e.EnableSessionRecordingWithStore(store); err != nil {
		return err
	}
	logger.Info("Session recording enabled", "directory", recordingDir)
	return nil
}

// EnableSessionRecordingWithStore enables session recording using a caller-provided
// EventStore. Programmatic Arena consumers can inject any implementation: in-memory,
// S3-backed, sampled, ring-buffered, etc. The dir-based EnableSessionRecording is
// a thin wrapper that constructs a FileEventStore.
func (e *Engine) EnableSessionRecordingWithStore(store events.EventStore) error {
	if store == nil {
		return fmt.Errorf("event store is required")
	}
	e.eventStore = store

	// Subscribe store to existing event bus for low-rate replay-essential events
	// that don't flow through the pipeline as StreamElement.
	if e.eventBus != nil {
		e.eventBus.Subscribe(events.EventConversationStarted, store.OnEvent)
		e.eventBus.Subscribe(events.EventStreamInterrupted, store.OnEvent)
		logger.Debug("Subscribed event store for session recording (targeted)")
	}

	// Enable RecordingStage in pipelines so audio/messages/tools flow into the store
	// via the pipeline tap (synchronous, never drops under burst).
	if e.recordingConfig == nil {
		defaults := stage.DefaultRecordingStageConfig()
		e.recordingConfig = &defaults
	}

	logger.Debug("Session recording wired with custom store")
	return nil
}

// EnableAudioMonitor enables real-time audio monitoring for duplex scenarios.
//
// The monitor surfaces (LocalSink, SSE relay, TUI level meter) are added to
// per-run pipelines only when at least one consumer is wired and the
// scenario uses duplex streaming. CI / non-interactive runs (auto mode +
// no TTY) do not construct any audio infrastructure.
//
// Options.Rate must be one of audio.Rate16k, Rate24k, Rate48k.
//
// This method only stores configuration; the per-run wiring happens in the
// pipeline construction path (see duplex_executor_pipeline_integration.go).
func (e *Engine) EnableAudioMonitor(opts arenaaudio.Options) error {
	if !arenaaudio.IsValidRate(opts.Rate) {
		return fmt.Errorf("invalid audio rate %d (valid: 16000, 24000, 48000)", opts.Rate)
	}
	if opts.Mode == "" {
		opts.Mode = arenaaudio.ModeAuto
	}
	e.audioMonitorOpts = &opts
	logger.Info("Audio monitor configured",
		"mode", string(opts.Mode),
		"rate", opts.Rate,
		"local_sink", opts.LocalSink,
		"sse_playback", opts.SSEPlayback,
		"level_meter", opts.LevelMeter,
	)
	return nil
}

// AudioMonitorEnabled reports whether EnableAudioMonitor was called.
// Callers use this to decide whether to construct an arenaaudio.Monitor.
func (e *Engine) AudioMonitorEnabled() bool {
	return e.audioMonitorOpts != nil
}

// AudioMonitorOptions returns the configured monitor options. Zero value
// when monitoring is disabled — check AudioMonitorEnabled first.
func (e *Engine) AudioMonitorOptions() arenaaudio.Options {
	if e.audioMonitorOpts == nil {
		return arenaaudio.Options{}
	}
	return *e.audioMonitorOpts
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

	// workItem pairs a combination with its index in the results slice.
	type workItem struct {
		idx   int
		combo RunCombination
	}

	work := make(chan workItem, len(plan.Combinations))
	for i, combo := range plan.Combinations {
		work <- workItem{idx: i, combo: combo}
	}
	close(work)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range work {
				// Check cancellation before starting work.
				if ctx.Err() != nil {
					mu.Lock()
					runIDs[item.idx] = ""
					errors[item.idx] = ctx.Err()
					mu.Unlock()
					continue
				}

				runID, err := e.executeRun(ctx, item.combo)

				mu.Lock()
				runIDs[item.idx] = runID
				errors[item.idx] = err
				mu.Unlock()

				if runID != "" {
					e.fireRunCompletedHooks(runID, err)
				}
			}
		}()
	}

	wg.Wait()

	// Check if any executions failed to save metadata
	var executionErrors []error
	for _, err := range errors {
		if err != nil {
			executionErrors = append(executionErrors, err)
		}
	}

	// Aggregate trial results for scenarios with Trials > 1
	if arenaStore, ok := e.stateStore.(*arenastore.ArenaStateStore); ok {
		runIDs = AggregateTrialResults(arenaStore, runIDs, plan.Combinations)
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

// GetDuplexExecutor returns the DuplexConversationExecutor from the engine's
// composite executor, or nil if the engine was not built with a duplex executor.
// Used by the chat command's voice path to drive RunInteractiveVoice.
func (e *Engine) GetDuplexExecutor() *DuplexConversationExecutor {
	if ce, ok := e.conversationExecutor.(*CompositeConversationExecutor); ok {
		return ce.GetDuplexExecutor()
	}
	if de, ok := e.conversationExecutor.(*DuplexConversationExecutor); ok {
		return de
	}
	return nil
}

// GetEventBus returns the engine's event bus. May be nil if SetEventBus has not
// been called. Used by the chat voice path to subscribe the TUI adapter.
func (e *Engine) GetEventBus() events.Bus {
	return e.eventBus
}

// buildMediaStorage creates a media storage service for media externalization.
// It stores media files in the <output_dir>/media/ subdirectory using run-based organization.
// This enables automatic externalization of large media content to reduce memory usage and
// improve performance when processing media-heavy scenarios.
//
// The returned service is wrapped with InstrumentedStorage so every
// store/retrieve/delete emits a runtime event and OTel span. The
// event bus is bound later via Engine.SetEventBus → SetBus on the
// wrapper; until then events are no-ops but spans still emit.
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

	return storage.NewInstrumentedStorage(fileStore, nil, "local"), nil
}
