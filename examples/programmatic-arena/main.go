// Package main demonstrates programmatic usage of Arena without file-based configs.
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
	fmt.Println("=== Programmatic Arena Example ===\n")

	// Create a simple prompt config programmatically
	promptConfig := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:    "assistant",
			Version:     "v1.0.0",
			Description: "A helpful AI assistant",
			SystemTemplate: `You are a helpful AI assistant.
Be concise, accurate, and friendly in your responses.`,
		},
	}

	// Create config programmatically - using mock provider for demonstration
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-gpt4": {
				ID:    "mock-gpt4",
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
			"greeting-test": {
				ID:          "greeting-test",
				TaskType:    "assistant",
				Description: "Test basic greeting response",
				Turns: []config.TurnDefinition{
					{Role: "user", Content: "Hello! How are you today?"},
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

	// Build engine components from programmatic config
	fmt.Println("Building Arena engine components...")
	providerReg, promptReg, mcpReg, executor, adapterReg, _, _, err := engine.BuildEngineComponents(cfg)
	if err != nil {
		log.Fatalf("Failed to build engine components: %v", err)
	}

	// Create engine
	fmt.Println("Creating Arena engine...")
	eng, err := engine.NewEngine(cfg, providerReg, promptReg, mcpReg, executor, adapterReg)
	if err != nil {
		log.Fatalf("Failed to create engine: %v", err)
	}
	defer eng.Close()

	// Generate execution plan
	fmt.Println("Generating execution plan...")
	plan, err := eng.GenerateRunPlan(nil, nil, nil)
	if err != nil {
		log.Fatalf("Failed to generate run plan: %v", err)
	}

	fmt.Printf("Generated plan with %d run combinations\n", len(plan.Combinations))

	// Execute runs
	fmt.Println("\nExecuting test runs...")
	ctx := context.Background()
	runIDs, err := eng.ExecuteRuns(ctx, plan, 2)
	if err != nil {
		log.Fatalf("Failed to execute runs: %v", err)
	}

	// Get results
	fmt.Println("\nRetrieving results...")
	arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		log.Fatal("Failed to get ArenaStateStore")
	}

	for i, runID := range runIDs {
		result, err := arenaStore.GetRunResult(ctx, runID)
		if err != nil {
			log.Printf("Failed to get result for run %s: %v", runID, err)
			continue
		}

		fmt.Printf("\n--- Run %d: %s ---\n", i+1, result.ScenarioID)
		fmt.Printf("Provider: %s\n", result.ProviderID)
		fmt.Printf("Duration: %s\n", result.Duration)
		fmt.Printf("Turns: %d\n", len(result.Messages))

		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else {
			fmt.Println("Success!")
		}
	}
}
