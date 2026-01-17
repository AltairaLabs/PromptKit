# Programmatic Arena Usage

This example demonstrates how to use Arena as a Go package without file-based configuration.

## Overview

Instead of loading YAML configuration files, you can create Arena configurations programmatically in Go code. This is useful for:

- Integrating Arena into custom testing tools
- Generating test scenarios dynamically
- Building Arena-based applications
- Testing without file system dependencies

## What This Example Shows

1. **Creating Config programmatically** - Build `*config.Config` in code
2. **Using BuildEngineComponents** - Get all required registries from config
3. **Executing tests** - Run scenarios and get results
4. **Processing results** - Access run results from the state store

## Running the Example

```bash
# Set your OpenAI API key
export OPENAI_API_KEY=sk-...

# Run the example
go run main.go
```

## Output

The example will:
- Create two test scenarios programmatically
- Execute them against GPT-4
- Display conversation turns and results
- Show cost information

Example output:
```
Building Arena engine components...
Creating Arena engine...
Generating execution plan...
Generated plan with 2 run combinations

Executing test runs...

Retrieving results...

--- Run 1: greeting-test ---
Provider: openai-gpt4
Duration: 1.23s
Turns: 4
✅ Success
  Turn 1 [user]: Hello! How are you today?
  Turn 2 [assistant]: Hello! I'm doing well, thank you for asking...
  Turn 3 [user]: What's the capital of France?
  Turn 4 [assistant]: The capital of France is Paris...
Cost: $0.000345 (input: 45 tokens, output: 89 tokens)

--- Run 2: factual-test ---
Provider: openai-gpt4
Duration: 0.87s
Turns: 2
✅ Success
  Turn 1 [user]: What is the speed of light?
  Turn 2 [assistant]: The speed of light in vacuum is approximately 299,792,458 meters per second...
Cost: $0.000156 (input: 23 tokens, output: 42 tokens)

=== Summary ===
Total runs: 2
Success: 2
Errors: 0
```

## Key APIs Used

### BuildEngineComponents
```go
providerReg, promptReg, mcpReg, executor, err := engine.BuildEngineComponents(cfg)
```
Converts a programmatic config into all required engine components.

### NewEngine
```go
eng, err := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
```
Creates an engine from components.

### GenerateRunPlan
```go
plan, err := eng.GenerateRunPlan(nil, nil, nil) // all regions, providers, scenarios
```
Creates execution plan from filters.

### ExecuteRuns
```go
runIDs, err := eng.ExecuteRuns(ctx, plan, concurrency)
```
Executes all runs in the plan.

### GetRunResult
```go
result, err := store.GetRunResult(ctx, runID)
```
Retrieves detailed result for a run.

## Extending This Example

### Adding Assertions

```go
LoadedScenarios: map[string]*config.Scenario{
    "test-with-assertions": {
        ID: "test-with-assertions",
        Turns: []config.Turn{
            {
                User: "What is 2+2?",
                Assertions: []config.TurnAssertion{
                    {
                        Type:  "contains",
                        Value: "4",
                    },
                },
            },
        },
    },
},
```

### Multiple Providers

```go
LoadedProviders: map[string]*config.Provider{
    "openai": {...},
    "claude": {
        ID:     "claude",
        Type:   "anthropic",
        Model:  "claude-3-5-sonnet-20241022",
        APIKey: os.Getenv("ANTHROPIC_API_KEY"),
    },
},
```

### Filter Execution

```go
// Only run specific scenarios with specific providers
plan, err := eng.GenerateRunPlan(
    nil,                      // all regions
    []string{"openai"},       // only OpenAI
    []string{"greeting-test"}, // only greeting test
)
```

## See Also

- [Arena CLI Documentation](../../docs/src/content/docs/arena/)
- [Config Package](../../pkg/config/)
- [Engine Package](../../tools/arena/engine/)
