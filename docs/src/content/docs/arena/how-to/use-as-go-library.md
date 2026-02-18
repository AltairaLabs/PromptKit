---
title: Use Arena as a Go Library
description: Integrate Arena into Go applications for programmatic LLM testing
---

This guide shows you how to integrate Arena into your Go applications for programmatic testing, custom tooling, and dynamic scenario generation.

## When to Use Arena as a Library

Use Arena programmatically when you need to:

- **Integrate with existing systems** - Embed testing into your application
- **Generate scenarios dynamically** - Create tests from data or user input
- **Custom reporting** - Process results with your own logic
- **Build testing tools** - Create specialized testing applications
- **Automate workflows** - Chain testing with other operations

## Installation

Add Arena to your Go project:

```bash
go get github.com/AltairaLabs/PromptKit/tools/arena/engine
go get github.com/AltairaLabs/PromptKit/pkg/config
go get github.com/AltairaLabs/PromptKit/runtime/prompt
```

## Basic Usage

### 1. Create Configuration

```go
import (
	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// Create prompt configuration
promptConfig := &prompt.Config{
	Spec: prompt.Spec{
		TaskType:       "assistant",
		SystemTemplate: "You are a helpful assistant.",
	},
}

// Create Arena configuration
cfg := &config.Config{
	LoadedProviders: map[string]*config.Provider{
		"openai": {
			ID:    "openai",
			Type:  "openai",
			Model: "gpt-4",
		},
	},
	LoadedPromptConfigs: map[string]*config.PromptConfigData{
		"assistant": {
			Config:   promptConfig,
			TaskType: "assistant",
		},
	},
	LoadedScenarios: map[string]*config.Scenario{
		"test-1": {
			ID:       "test-1",
			TaskType: "assistant",
			Turns: []config.TurnDefinition{
				{Role: "user", Content: "Hello"},
			},
		},
	},
	Defaults: config.Defaults{
		Temperature: 0.7,
		MaxTokens:   500,
	},
}
```

### 2. Build Engine Components

```go
// Build all required components
providerReg, promptReg, mcpReg, executor, err := engine.BuildEngineComponents(cfg)
if err != nil {
	return err
}
```

### 3. Create and Execute

```go
// Create engine
eng, err := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
if err != nil {
	return err
}
defer eng.Close()

// Generate plan
plan, err := eng.GenerateRunPlan(nil, nil, nil)
if err != nil {
	return err
}

// Execute
ctx := context.Background()
runIDs, err := eng.ExecuteRuns(ctx, plan, 4) // 4 concurrent workers
if err != nil {
	return err
}
```

### 4. Retrieve Results

```go
import "github.com/AltairaLabs/PromptKit/tools/arena/statestore"

arenaStore := eng.GetStateStore().(*statestore.ArenaStateStore)

for _, runID := range runIDs {
	result, err := arenaStore.GetRunResult(ctx, runID)
	if err != nil {
		continue
	}
	
	// Process result
	fmt.Printf("Scenario: %s, Status: %s\n", 
		result.ScenarioID,
		getStatus(result.Error))
}
```

## Advanced Patterns

### Dynamic Scenario Generation

Generate scenarios from external data:

```go
// Load test cases from database/API/file
testCases := loadTestCases()

scenarios := make(map[string]*config.Scenario)
for i, tc := range testCases {
	scenarios[tc.ID] = &config.Scenario{
		ID:          tc.ID,
		TaskType:    tc.TaskType,
		Description: tc.Description,
		Turns:       buildTurns(tc.Conversations),
	}
}

cfg.LoadedScenarios = scenarios
```

### Provider Comparison

Test the same scenario across multiple providers:

```go
providers := map[string]*config.Provider{
	"openai-gpt4": {
		ID:    "openai-gpt4",
		Type:  "openai",
		Model: "gpt-4",
	},
	"anthropic-claude": {
		ID:    "anthropic-claude",
		Type:  "anthropic",
		Model: "claude-3-5-sonnet-20241022",
	},
	"google-gemini": {
		ID:    "google-gemini",
		Type:  "gemini",
		Model: "gemini-2.0-flash-exp",
	},
}

cfg.LoadedProviders = providers

// Execute - all providers will be tested
plan, _ := eng.GenerateRunPlan(nil, nil, nil)
runIDs, _ := eng.ExecuteRuns(ctx, plan, 6)

// Compare results
compareProviders(runIDs, eng.GetStateStore())
```

### Custom Result Processing

Process results with your own logic:

```go
type TestMetrics struct {
	Provider    string
	Scenario    string
	Duration    time.Duration
	Cost        float64
	TokensUsed  int
	Success     bool
	ErrorMsg    string
}

func processResults(runIDs []string, store *statestore.ArenaStateStore) []TestMetrics {
	var metrics []TestMetrics
	
	for _, runID := range runIDs {
		result, _ := store.GetRunResult(context.Background(), runID)
		
		metrics = append(metrics, TestMetrics{
			Provider:   result.ProviderID,
			Scenario:   result.ScenarioID,
			Duration:   result.Duration,
			Cost:       result.Cost.TotalCost,
			TokensUsed: result.Cost.InputTokens + result.Cost.OutputTokens,
			Success:    result.Error == "",
			ErrorMsg:   result.Error,
		})
	}
	
	return metrics
}
```

### Filtered Execution

Run specific combinations only:

```go
// Only test specific scenarios with specific providers
plan, err := eng.GenerateRunPlan(
	nil,                           // all regions
	[]string{"openai", "claude"},  // only these providers
	[]string{"critical-test-1"},   // only this scenario
)
```

### Mock Provider for Testing

Use mock providers to test without API calls:

```go
cfg.LoadedProviders = map[string]*config.Provider{
	"mock": {
		ID:    "mock",
		Type:  "mock",
		Model: "test-model",
	},
}

// Optionally enable mock mode on existing engine
err := eng.EnableMockProviderMode("path/to/mock-config.yaml")
```

## Integration Examples

### Testing Framework Integration

```go
func TestLLMResponses(t *testing.T) {
	cfg := createTestConfig()
	
	providerReg, promptReg, mcpReg, executor, err := engine.BuildEngineComponents(cfg)
	require.NoError(t, err)
	
	eng, err := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
	require.NoError(t, err)
	defer eng.Close()
	
	plan, _ := eng.GenerateRunPlan(nil, nil, nil)
	runIDs, err := eng.ExecuteRuns(context.Background(), plan, 4)
	require.NoError(t, err)
	
	// Assert results
	store := eng.GetStateStore().(*statestore.ArenaStateStore)
	for _, runID := range runIDs {
		result, _ := store.GetRunResult(context.Background(), runID)
		assert.Empty(t, result.Error, "Test should pass")
	}
}
```

### CI/CD Integration

```go
func main() {
	// Load config from environment
	cfg := buildConfigFromEnv()
	
	// Execute tests
	eng, _ := setupEngine(cfg)
	defer eng.Close()
	
	plan, _ := eng.GenerateRunPlan(nil, nil, nil)
	runIDs, _ := eng.ExecuteRuns(context.Background(), plan, 10)
	
	// Process results and exit with appropriate code
	results := collectResults(runIDs, eng.GetStateStore())
	
	if hasFailures(results) {
		fmt.Println("❌ Tests failed")
		os.Exit(1)
	}
	
	fmt.Println("✅ All tests passed")
	os.Exit(0)
}
```

### Custom Test Runner

```go
type CustomRunner struct {
	engine *engine.Engine
	store  *statestore.ArenaStateStore
}

func (r *CustomRunner) RunWithRetry(scenario string, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		plan, _ := r.engine.GenerateRunPlan(nil, nil, []string{scenario})
		runIDs, _ := r.engine.ExecuteRuns(context.Background(), plan, 1)
		
		result, _ := r.store.GetRunResult(context.Background(), runIDs[0])
		if result.Error == "" {
			return nil
		}
		
		log.Printf("Attempt %d failed, retrying...", i+1)
		time.Sleep(time.Second * time.Duration(i+1))
	}
	
	return fmt.Errorf("failed after %d attempts", maxRetries)
}
```

## Best Practices

### 1. Always Close the Engine

```go
eng, _ := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
defer eng.Close() // Ensures proper cleanup
```

### 2. Handle Errors Gracefully

```go
result, err := store.GetRunResult(ctx, runID)
if err != nil {
	log.Printf("Failed to get result for %s: %v", runID, err)
	continue // Don't fail entire batch
}
```

### 3. Use Appropriate Concurrency

```go
// For rate-limited APIs
runIDs, _ := eng.ExecuteRuns(ctx, plan, 2)

// For mock testing
runIDs, _ := eng.ExecuteRuns(ctx, plan, 20)
```

### 4. Reuse Components

```go
// Build components once
providerReg, promptReg, mcpReg, executor, _ := engine.BuildEngineComponents(cfg)

// Reuse for multiple engines
eng1, _ := engine.NewEngine(cfg1, providerReg, promptReg, mcpReg, executor)
eng2, _ := engine.NewEngine(cfg2, providerReg, promptReg, mcpReg, executor)
```

## Troubleshooting

### Import Errors

If you see "package not found" errors:

```bash
go mod tidy
go get github.com/AltairaLabs/PromptKit/tools/arena/engine@latest
```

### Type Assertion Failures

Always check type assertions:

```go
arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
if !ok {
	log.Fatal("Expected ArenaStateStore")
}
```

### Memory Issues with Large Test Suites

Process results in batches:

```go
const batchSize = 100
for i := 0; i < len(runIDs); i += batchSize {
	end := i + batchSize
	if end > len(runIDs) {
		end = len(runIDs)
	}
	
	processBatch(runIDs[i:end], store)
}
```

## See Also

- [Tutorial: Programmatic Usage](../tutorials/07-programmatic-usage) - Step-by-step learning
- [Reference: API Documentation](../reference/api-reference) - Complete API reference
- [Example Code](https://github.com/AltairaLabs/PromptKit/tree/main/examples/programmatic-arena)
