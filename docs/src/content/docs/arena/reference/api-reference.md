---
title: Go API Reference
description: Complete API reference for using Arena as a Go library
sidebar:
  order: 9
---

Reference documentation for the Arena Go API, covering all exported types, functions, and methods for programmatic usage.

## Core Packages

### `github.com/AltairaLabs/PromptKit/tools/arena/engine`

The main package for programmatic Arena usage.

#### `BuildEngineComponents`

```go
func BuildEngineComponents(cfg *config.Config) (
    providerRegistry *providers.Registry,
    promptRegistry *prompt.Registry,
    mcpRegistry *mcp.RegistryImpl,
    convExecutor ConversationExecutor,
    err error,
)
```

Builds all engine components from a loaded Config object.

**Parameters:**
- `cfg` - Fully constructed `*config.Config` with loaded providers, scenarios, and prompts

**Returns:**
- `providerRegistry` - Registry containing all configured LLM providers
- `promptRegistry` - Registry containing all prompt configurations
- `mcpRegistry` - Registry for Model Context Protocol servers
- `convExecutor` - Executor for running conversations
- `err` - Error if component building fails

**Example:**
```go
cfg := &config.Config{
    LoadedProviders: map[string]*config.Provider{...},
    LoadedScenarios: map[string]*config.Scenario{...},
}

providerReg, promptReg, mcpReg, executor, err := engine.BuildEngineComponents(cfg)
if err != nil {
    log.Fatal(err)
}
```

---

#### `NewEngine`

```go
func NewEngine(
    cfg *config.Config,
    providerRegistry *providers.Registry,
    promptRegistry *prompt.Registry,
    mcpRegistry *mcp.RegistryImpl,
    convExecutor ConversationExecutor,
) (*Engine, error)
```

Creates a new Engine from pre-built components.

**Parameters:**
- `cfg` - Configuration object
- `providerRegistry` - Provider registry from `BuildEngineComponents`
- `promptRegistry` - Prompt registry from `BuildEngineComponents`
- `mcpRegistry` - MCP registry from `BuildEngineComponents`
- `convExecutor` - Conversation executor from `BuildEngineComponents`

**Returns:**
- `*Engine` - Initialized engine ready for execution
- `error` - Error if engine creation fails

---

#### `NewEngineFromConfigFile`

```go
func NewEngineFromConfigFile(configPath string) (*Engine, error)
```

Creates a new Engine by loading configuration from a file.

**Parameters:**
- `configPath` - Path to arena.yaml configuration file

**Returns:**
- `*Engine` - Initialized engine
- `error` - Error if loading or initialization fails

**Example:**
```go
eng, err := engine.NewEngineFromConfigFile("config.arena.yaml")
if err != nil {
    log.Fatal(err)
}
defer eng.Close()
```

---

### Engine Methods

#### `GenerateRunPlan`

```go
func (e *Engine) GenerateRunPlan(
    regionFilter, 
    providerFilter, 
    scenarioFilter []string,
) (*RunPlan, error)
```

Creates a test execution plan from filter criteria.

**Parameters:**
- `regionFilter` - Regions to include (nil = all)
- `providerFilter` - Providers to include (nil = all)
- `scenarioFilter` - Scenarios to include (nil = all)

**Returns:**
- `*RunPlan` - Execution plan with all matching combinations
- `error` - Error if plan generation fails

**Example:**
```go
// Test only OpenAI and Claude providers
plan, err := eng.GenerateRunPlan(
    nil,                           // all regions
    []string{"openai", "claude"},  // specific providers
    nil,                           // all scenarios
)
```

---

#### `ExecuteRuns`

```go
func (e *Engine) ExecuteRuns(
    ctx context.Context,
    plan *RunPlan,
    concurrency int,
) ([]string, error)
```

Executes all runs in the plan concurrently.

**Parameters:**
- `ctx` - Context for cancellation
- `plan` - RunPlan from `GenerateRunPlan`
- `concurrency` - Maximum concurrent executions

**Returns:**
- `[]string` - Run IDs in same order as plan combinations
- `error` - Error if execution setup fails

**Example:**
```go
ctx := context.Background()
runIDs, err := eng.ExecuteRuns(ctx, plan, 4)
if err != nil {
    log.Fatal(err)
}
```

---

#### `GetStateStore`

```go
func (e *Engine) GetStateStore() statestore.Store
```

Returns the engine's state store for accessing results.

**Returns:**
- `statestore.Store` - State store interface

**Example:**
```go
arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
if !ok {
    log.Fatal("Expected ArenaStateStore")
}
```

---

#### `Close`

```go
func (e *Engine) Close() error
```

Shuts down the engine and cleans up resources.

**Returns:**
- `error` - Error if cleanup fails

**Example:**
```go
defer eng.Close()
```

---

#### `EnableMockProviderMode`

```go
func (e *Engine) EnableMockProviderMode(mockConfigPath string) error
```

Replaces all providers with mock providers for testing.

**Parameters:**
- `mockConfigPath` - Path to mock config YAML (empty string for default responses)

**Returns:**
- `error` - Error if mock configuration is invalid

**Example:**
```go
err := eng.EnableMockProviderMode("mock-responses.yaml")
```

---

#### `EnableSessionRecording`

```go
func (e *Engine) EnableSessionRecording(recordingDir string) error
```

Enables session recording for all runs.

**Parameters:**
- `recordingDir` - Directory to store recording files

**Returns:**
- `error` - Error if directory cannot be created

---

### Types

#### `RunPlan`

```go
type RunPlan struct {
    Combinations []RunCombination
}
```

Represents a test execution plan.

**Fields:**
- `Combinations` - All test combinations to execute

---

#### `RunCombination`

```go
type RunCombination struct {
    Region     string
    ScenarioID string
    ProviderID string
}
```

Represents a single test execution.

**Fields:**
- `Region` - Region identifier
- `ScenarioID` - Scenario to execute
- `ProviderID` - Provider to use

---

#### `RunResult`

```go
type RunResult struct {
    RunID      string
    ProviderID string
    ScenarioID string
    Messages   []types.Message
    Cost       types.CostInfo
    Duration   time.Duration
    Error      string
    // ... additional fields
}
```

Contains complete results of a test execution.

**Key Fields:**
- `RunID` - Unique identifier for this run
- `ProviderID` - Provider that executed the test
- `ScenarioID` - Scenario that was executed
- `Messages` - Full conversation history
- `Cost` - Token usage and cost information
- `Duration` - Execution time
- `Error` - Error message if test failed

---

## Configuration Package

### `github.com/AltairaLabs/PromptKit/pkg/config`

#### `Config`

```go
type Config struct {
    LoadedProviders     map[string]*Provider
    LoadedScenarios     map[string]*Scenario
    LoadedPromptConfigs map[string]*PromptConfigData
    Defaults            Defaults
    // ... additional fields
}
```

Main Arena configuration structure.

---

#### `Provider`

```go
type Provider struct {
    ID    string
    Type  string  // "openai", "anthropic", "gemini", "mock"
    Model string
    // ... additional fields
}
```

Provider configuration.

---

#### `Scenario`

```go
type Scenario struct {
    ID          string
    TaskType    string
    Description string
    Turns       []TurnDefinition
    // ... additional fields
}
```

Test scenario definition.

---

#### `TurnDefinition`

```go
type TurnDefinition struct {
    Role    string  // "user" or "assistant"
    Content string
    // ... additional fields
}
```

Single conversation turn.

---

#### `Defaults`

```go
type Defaults struct {
    Temperature float32
    MaxTokens   int
    Output      OutputConfig
    // ... additional fields
}
```

Default execution parameters.

---

## State Store Package

### `github.com/AltairaLabs/PromptKit/tools/arena/statestore`

#### `ArenaStateStore`

```go
type ArenaStateStore struct {
    // ... internal fields
}
```

State store implementation for Arena results.

---

#### `GetRunResult`

```go
func (s *ArenaStateStore) GetRunResult(
    ctx context.Context,
    runID string,
) (*RunResult, error)
```

Retrieves detailed results for a specific run.

**Parameters:**
- `ctx` - Context for cancellation
- `runID` - Run identifier from `ExecuteRuns`

**Returns:**
- `*RunResult` - Complete run results
- `error` - Error if run not found or retrieval fails

**Example:**
```go
result, err := arenaStore.GetRunResult(ctx, runID)
if err != nil {
    log.Printf("Failed to get result: %v", err)
    continue
}

fmt.Printf("Scenario: %s\n", result.ScenarioID)
fmt.Printf("Duration: %s\n", result.Duration)
fmt.Printf("Cost: $%.6f\n", result.Cost.TotalCost)
```

---

## Prompt Package

### `github.com/AltairaLabs/PromptKit/runtime/prompt`

#### `Config`

```go
type Config struct {
    Spec Spec
}
```

Prompt configuration wrapper.

---

#### `Spec`

```go
type Spec struct {
    TaskType       string
    Version        string
    Description    string
    SystemTemplate string
    AllowedTools   []string
    // ... additional fields
}
```

Prompt specification.

**Example:**
```go
promptConfig := &prompt.Config{
    Spec: prompt.Spec{
        TaskType:       "assistant",
        Version:        "v1.0.0",
        Description:    "Helpful assistant",
        SystemTemplate: "You are a helpful AI assistant.",
        AllowedTools:   []string{"calculator", "search"},
    },
}
```

---

## Common Patterns

### Complete Workflow

```go
// 1. Create configuration
cfg := &config.Config{
    LoadedProviders: map[string]*config.Provider{...},
    LoadedScenarios: map[string]*config.Scenario{...},
}

// 2. Build components
providerReg, promptReg, mcpReg, executor, err := engine.BuildEngineComponents(cfg)
if err != nil {
    return err
}

// 3. Create engine
eng, err := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
if err != nil {
    return err
}
defer eng.Close()

// 4. Generate plan
plan, err := eng.GenerateRunPlan(nil, nil, nil)
if err != nil {
    return err
}

// 5. Execute
ctx := context.Background()
runIDs, err := eng.ExecuteRuns(ctx, plan, 4)
if err != nil {
    return err
}

// 6. Get results
store := eng.GetStateStore().(*statestore.ArenaStateStore)
for _, runID := range runIDs {
    result, _ := store.GetRunResult(ctx, runID)
    // Process result
}
```

---

## See Also

- [Tutorial: Programmatic Usage](../tutorials/07-programmatic-usage)
- [How-To: Use as Go Library](../how-to/use-as-go-library)
- [Example Code](https://github.com/AltairaLabs/PromptKit/tree/main/examples/programmatic-arena)
