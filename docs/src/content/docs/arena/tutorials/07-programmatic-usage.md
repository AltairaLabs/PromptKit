---
title: Using Arena as a Go Library
description: Learn how to integrate Arena into your Go applications for programmatic testing
sidebar:
  order: 7
---

In this tutorial, you'll learn how to use Arena as a Go library instead of a CLI tool. This is useful when you want to integrate LLM testing into your own applications, build custom testing tools, or generate test scenarios dynamically.

## What You'll Build

By the end of this tutorial, you'll have a working Go program that:
- Creates Arena configurations programmatically
- Executes test scenarios
- Retrieves and processes results
- All without touching YAML files

## Prerequisites

- Go 1.21 or later installed
- Basic understanding of Go programming
- Familiarity with Arena concepts (scenarios, providers)

## Step 1: Set Up Your Go Project

Create a new directory and initialize a Go module:

```bash
mkdir arena-lib-demo
cd arena-lib-demo
go mod init arena-lib-demo
```

## Step 2: Install Dependencies

Add PromptKit as a dependency:

```bash
go get github.com/AltairaLabs/PromptKit/pkg/config
go get github.com/AltairaLabs/PromptKit/runtime/prompt
go get github.com/AltairaLabs/PromptKit/tools/arena/engine
go get github.com/AltairaLabs/PromptKit/tools/arena/statestore
```

## Step 3: Create Your First Programmatic Test

Create a file named `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func main() {
	// Step 1: Create a prompt configuration
	promptConfig := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:    "assistant",
			Version:     "v1.0.0",
			Description: "A helpful AI assistant",
			SystemTemplate: `You are a helpful AI assistant.
Be concise and accurate in your responses.`,
		},
	}

	// Step 2: Create the Arena configuration
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-provider": {
				ID:    "mock-provider",
				Type:  "mock",
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
			"basic-test": {
				ID:          "basic-test",
				TaskType:    "assistant",
				Description: "Basic conversation test",
				Turns: []config.TurnDefinition{
					{Role: "user", Content: "What is 2+2?"},
					{Role: "user", Content: "What's the capital of France?"},
				},
			},
		},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   500,
			Output: config.OutputConfig{
				Dir: "out",
			},
		},
	}

	fmt.Println("Building Arena engine...")

	// Step 3: Build engine components
	providerReg, promptReg, mcpReg, executor, err := engine.BuildEngineComponents(cfg)
	if err != nil {
		log.Fatalf("Failed to build components: %v", err)
	}

	// Step 4: Create the engine
	eng, err := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor)
	if err != nil {
		log.Fatalf("Failed to create engine: %v", err)
	}
	defer eng.Close()

	// Step 5: Generate execution plan
	plan, err := eng.GenerateRunPlan(nil, nil, nil)
	if err != nil {
		log.Fatalf("Failed to generate plan: %v", err)
	}

	fmt.Printf("Generated %d test combinations\n", len(plan.Combinations))

	// Step 6: Execute tests
	ctx := context.Background()
	runIDs, err := eng.ExecuteRuns(ctx, plan, 2)
	if err != nil {
		log.Fatalf("Failed to execute: %v", err)
	}

	// Step 7: Retrieve results
	arenaStore := eng.GetStateStore().(*statestore.ArenaStateStore)
	
	for i, runID := range runIDs {
		result, err := arenaStore.GetRunResult(ctx, runID)
		if err != nil {
			log.Printf("Failed to get result: %v", err)
			continue
		}

		fmt.Printf("\nTest %d: %s\n", i+1, result.ScenarioID)
		fmt.Printf("Status: %s\n", getStatus(result.Error))
		fmt.Printf("Duration: %s\n", result.Duration)
		fmt.Printf("Turns: %d\n", len(result.Messages))
	}

	fmt.Println("\n✅ All tests completed!")
}

func getStatus(errMsg string) string {
	if errMsg == "" {
		return "✅ Success"
	}
	return "❌ Failed: " + errMsg
}
```

## Step 4: Run Your Program

```bash
go run main.go
```

You should see output like:

```
Building Arena engine...
Generated 1 test combinations
Test 1: basic-test
Status: ✅ Success
Duration: 2.5ms
Turns: 5

✅ All tests completed!
```

## Step 5: Add Real Provider Testing

Let's enhance the example to use a real provider (OpenAI):

```go
// Replace the mock provider with OpenAI
LoadedProviders: map[string]*config.Provider{
	"openai-gpt4": {
		ID:    "openai-gpt4",
		Type:  "openai",
		Model: "gpt-4",
		// API key is read from OPENAI_API_KEY env var by default
	},
},
```

Then run with your API key:

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## Step 6: Add Assertions

Let's add validation to our scenario:

```go
LoadedScenarios: map[string]*config.Scenario{
	"math-test": {
		ID:          "math-test",
		TaskType:    "assistant",
		Description: "Test math responses",
		Turns: []config.TurnDefinition{
			{
				Role:    "user",
				Content: "What is 2+2?",
				Assertions: []asrt.AssertionConfig{
					{
						Type:    "content_includes",
						Params:  map[string]interface{}{"patterns": []string{"4"}},
						Message: "Should contain the answer 4",
					},
				},
			},
		},
	},
},
```

Don't forget to import the assertions package:

```go
import (
	// ... other imports
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)
```

## What You've Learned

✅ How to create Arena configurations in Go code  
✅ How to build engine components programmatically  
✅ How to execute tests and retrieve results  
✅ How to switch between mock and real providers  
✅ How to add assertions to scenarios  

## Next Steps

- [How-To: Use Arena as a Go Library](../how-to/use-as-go-library) - Advanced integration patterns
- [Reference: API Documentation](../reference/api-reference) - Complete API reference
- [Example: Programmatic Arena](https://github.com/AltairaLabs/PromptKit/tree/main/examples/programmatic-arena) - Full working example

## Common Patterns

### Dynamic Scenario Generation

```go
// Generate scenarios from test data
scenarios := make(map[string]*config.Scenario)
for i, testCase := range testCases {
	scenarios[fmt.Sprintf("test-%d", i)] = &config.Scenario{
		ID:       fmt.Sprintf("test-%d", i),
		TaskType: "assistant",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: testCase.Input},
		},
	}
}

cfg.LoadedScenarios = scenarios
```

### Multiple Provider Comparison

```go
// Test against multiple providers
providers := map[string]*config.Provider{
	"openai":   {ID: "openai", Type: "openai", Model: "gpt-4"},
	"claude":   {ID: "claude", Type: "anthropic", Model: "claude-3-5-sonnet-20241022"},
	"gemini":   {ID: "gemini", Type: "gemini", Model: "gemini-2.0-flash-exp"},
}

cfg.LoadedProviders = providers
```

### Custom Result Processing

```go
// Process results with custom logic
for _, runID := range runIDs {
	result, _ := arenaStore.GetRunResult(ctx, runID)
	
	// Extract metrics
	metrics := map[string]interface{}{
		"provider":     result.ProviderID,
		"duration_ms":  result.Duration.Milliseconds(),
		"cost":         result.Cost.TotalCost,
		"tokens":       result.Cost.InputTokens + result.Cost.OutputTokens,
		"success":      result.Error == "",
	}
	
	// Send to your analytics system
	sendToAnalytics(metrics)
}
```
