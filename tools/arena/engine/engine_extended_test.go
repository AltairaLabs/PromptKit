package engine

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
)

func TestEngine_BuildSystemPrompt(t *testing.T) {
	t.Skip("BuildSystemPrompt requires prompt registry initialization - tested in integration tests")
}

func TestEngine_BuildSystemPrompt_DifferentRegions(t *testing.T) {
	t.Skip("BuildSystemPrompt requires prompt registry initialization - tested in integration tests")
}

func TestEngine_BuildSystemPrompt_DifferentTaskTypes(t *testing.T) {
	t.Skip("BuildSystemPrompt requires prompt registry initialization - tested in integration tests")
}

func TestEngine_GetPromptRegistry(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Verify engine has a prompt registry (may be nil if no configs loaded)
	// This just tests that the field exists and is accessible
	_ = eng.promptRegistry
}

func TestEngine_GetSelfPlayCacheStats(t *testing.T) {
	t.Skip("Self-play cache stats require prompt registry and self-play initialization - tested in integration tests")
}

func TestRunPlan_Structure(t *testing.T) {
	plan := &RunPlan{
		Combinations: []RunCombination{
			{
				Region:     "us",
				ScenarioID: "test-scenario",
				ProviderID: "test-provider",
			},
		},
	}

	if len(plan.Combinations) != 1 {
		t.Errorf("Expected 1 combination, got %d", len(plan.Combinations))
	}

	combo := plan.Combinations[0]
	if combo.Region != "us" {
		t.Error("Region mismatch")
	}

	if combo.ScenarioID != "test-scenario" {
		t.Error("ScenarioID mismatch")
	}

	if combo.ProviderID != "test-provider" {
		t.Error("ProviderID mismatch")
	}
}

func TestRunResult_Structure(t *testing.T) {
	result := RunResult{
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
		Region:     "us",
		Cost: types.CostInfo{
			InputTokens:   100,
			OutputTokens:  50,
			InputCostUSD:  0.005,
			OutputCostUSD: 0.005,
			TotalCost:     0.01,
		},
		Duration: 100 * time.Millisecond,
		Error:    "",
	}

	if result.ScenarioID != "test-scenario" {
		t.Error("ScenarioID mismatch")
	}

	if result.ProviderID != "test-provider" {
		t.Error("ProviderID mismatch")
	}

	if result.Region != "us" {
		t.Error("Region mismatch")
	}

	if result.Cost.InputTokens != 100 {
		t.Error("InputTokens mismatch")
	}

	if result.Cost.OutputTokens != 50 {
		t.Error("OutputTokens mismatch")
	}

	if result.Cost.TotalCost != 0.01 {
		t.Error("USDEstimate mismatch")
	}

	if result.Duration != 100*time.Millisecond {
		t.Error("Duration mismatch")
	}

	if result.Error != "" {
		t.Error("Expected no error")
	}
}

func TestRunResult_WithError(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		Error:      "test error",
	}

	if result.Error != "test error" {
		t.Error("Error message mismatch")
	}
}

func TestCacheStats_Structure(t *testing.T) {
	stats := &selfplay.CacheStats{
		Hits:   10,
		Misses: 5,
		Size:   100,
	}

	if stats.Hits != 10 {
		t.Error("Hits mismatch")
	}

	if stats.Misses != 5 {
		t.Error("Misses mismatch")
	}

	if stats.Size != 100 {
		t.Error("Size mismatch")
	}
}

func TestExecuteRuns_Concurrency(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose:     false,
			Concurrency: 2,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)
	ctx := context.Background()

	// Execute with empty plan but test concurrency parameter
	plan := &RunPlan{
		Combinations: []RunCombination{},
	}

	// Test different concurrency values
	concurrencyLevels := []int{1, 2, 5, 10}
	for _, concurrency := range concurrencyLevels {
		results, err := eng.ExecuteRuns(ctx, plan, concurrency)
		if err != nil {
			t.Errorf("ExecuteRuns failed with concurrency %d: %v", concurrency, err)
		}

		if results == nil {
			t.Errorf("Results is nil for concurrency %d", concurrency)
		}
	}
}

func TestExecuteRuns_ContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	plan := &RunPlan{
		Combinations: []RunCombination{},
	}

	results, err := eng.ExecuteRuns(ctx, plan, 1)

	// Should still complete successfully with empty plan
	if err != nil {
		t.Errorf("ExecuteRuns failed: %v", err)
	}

	if results == nil {
		t.Error("Results should not be nil")
	}
}

func TestGenerateRunPlan_WithFilters(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Load some test data into the engine
	eng.scenarios = map[string]*config.Scenario{
		"scenario1": {ID: "scenario1", TaskType: "support"},
		"scenario2": {ID: "scenario2", TaskType: "consulting"},
	}

	eng.providers = map[string]*config.Provider{
		"provider1": {ID: "provider1", Type: "openai"},
		"provider2": {ID: "provider2", Type: "anthropic"},
	}

	// Test with scenario filter
	plan, err := eng.GenerateRunPlan(nil, nil, []string{"scenario1"})
	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Should only include scenario1
	for _, combo := range plan.Combinations {
		if combo.ScenarioID != "scenario1" {
			t.Errorf("Expected only scenario1, got %s", combo.ScenarioID)
		}
	}
}

func TestGenerateRunPlan_RegionFilter(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Load test data
	eng.scenarios = map[string]*config.Scenario{
		"scenario1": {ID: "scenario1", TaskType: "support"},
	}

	eng.providers = map[string]*config.Provider{
		"provider1": {ID: "provider1", Type: "openai"},
	}

	// Test with region filter
	plan, err := eng.GenerateRunPlan([]string{"us"}, nil, nil)
	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Should only include us region
	for _, combo := range plan.Combinations {
		if combo.Region != "us" {
			t.Errorf("Expected only 'us' region, got %s", combo.Region)
		}
	}
}

func TestGenerateRunPlan_ProviderFilter(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Load test data
	eng.scenarios = map[string]*config.Scenario{
		"scenario1": {ID: "scenario1", TaskType: "support"},
	}

	eng.providers = map[string]*config.Provider{
		"provider1": {ID: "provider1", Type: "openai"},
		"provider2": {ID: "provider2", Type: "anthropic"},
	}

	// Test with provider filter
	plan, err := eng.GenerateRunPlan(nil, []string{"provider1"}, nil)
	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Should only include provider1
	for _, combo := range plan.Combinations {
		if combo.ProviderID != "provider1" {
			t.Errorf("Expected only 'provider1', got %s", combo.ProviderID)
		}
	}
}

func TestEngine_MultipleScenarios(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Load multiple scenarios
	eng.scenarios = map[string]*config.Scenario{
		"s1": {ID: "s1", TaskType: "support"},
		"s2": {ID: "s2", TaskType: "consulting"},
		"s3": {ID: "s3", TaskType: "pivot"},
	}

	if len(eng.scenarios) != 3 {
		t.Errorf("Expected 3 scenarios, got %d", len(eng.scenarios))
	}
}

func TestEngine_MultipleProviders(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)

	// Load multiple providers
	eng.providers = map[string]*config.Provider{
		"p1": {ID: "p1", Type: "openai"},
		"p2": {ID: "p2", Type: "anthropic"},
		"p3": {ID: "p3", Type: "gemini"},
	}

	if len(eng.providers) != 3 {
		t.Errorf("Expected 3 providers, got %d", len(eng.providers))
	}
}

func TestRunCombination_AllFields(t *testing.T) {
	combo := RunCombination{
		Region:     "uk",
		ScenarioID: "test-scenario",
		ProviderID: "test-provider",
	}

	if combo.Region != "uk" {
		t.Error("Region not set correctly")
	}

	if combo.ScenarioID != "test-scenario" {
		t.Error("ScenarioID not set correctly")
	}

	if combo.ProviderID != "test-provider" {
		t.Error("ProviderID not set correctly")
	}
}

func TestEngine_VerboseMode(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with verbose enabled
	cfgVerbose := &config.Config{
		Defaults: config.Defaults{
			Verbose: true,
		},
	}

	engVerbose := newTestEngine(t, tmpDir, cfgVerbose)
	if engVerbose == nil {
		t.Error("Engine creation failed with verbose mode")
	}

	// Test with verbose disabled
	cfgQuiet := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	engQuiet := newTestEngine(t, tmpDir, cfgQuiet)
	if engQuiet == nil {
		t.Error("Engine creation failed with quiet mode")
	}
}

func TestEngine_DefaultConcurrency(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose:     false,
			Concurrency: 0, // Default
		},
	}

	tmpDir := t.TempDir()
	eng := newTestEngine(t, tmpDir, cfg)
	if eng == nil {
		t.Error("Engine creation failed with default concurrency")
	}

	cfg2 := &config.Config{
		Defaults: config.Defaults{
			Verbose:     false,
			Concurrency: 10,
		},
	}

	eng2 := newTestEngine(t, tmpDir, cfg2)
	if eng2 == nil {
		t.Error("Engine creation failed with concurrency 10")
	}

	if eng2.config.Defaults.Concurrency != 10 {
		t.Error("Concurrency not set correctly")
	}
}

func TestEngine_Close(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	if eng == nil {
		t.Fatal("Engine creation failed")
	}

	// Close should not error even if registries are nil
	err := eng.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Should be safe to call Close() multiple times
	err = eng.Close()
	if err != nil {
		t.Errorf("Second Close() returned error: %v", err)
	}
}
