package engine

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestGenerateRunPlan_AllCombinations(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
		// No prompt configs needed for this test
	}

	eng := newTestEngine(t, tmpDir, cfg)

	// Load multiple scenarios
	eng.scenarios = map[string]*config.Scenario{
		"s1": {ID: "s1", TaskType: "support"},
		"s2": {ID: "s2", TaskType: "consulting"},
	}

	// For providers, we need to specify in the filters since provider registry requires initialization
	// Test with filtered providers instead
	providerFilter := []string{"p1", "p2"}
	scenarioFilter := []string{"s1", "s2"}
	regionFilter := []string{"us", "uk"}

	plan, err := eng.GenerateRunPlan(regionFilter, providerFilter, scenarioFilter)
	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Should have 2 regions * 2 providers * 2 scenarios = 8 combinations
	expected := 2 * 2 * 2
	if len(plan.Combinations) != expected {
		t.Errorf("Expected %d combinations, got %d", expected, len(plan.Combinations))
	}

	// Verify combinations have correct structure
	for i, combo := range plan.Combinations {
		if combo.Region == "" {
			t.Errorf("Combination %d has empty region", i)
		}
		if combo.ProviderID == "" {
			t.Errorf("Combination %d has empty provider ID", i)
		}
		if combo.ScenarioID == "" {
			t.Errorf("Combination %d has empty scenario ID", i)
		}
	}
}

func TestGenerateRunPlan_NoRegions(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
		// No prompt configs needed for this test
	}

	eng := newTestEngine(t, tmpDir, cfg)

	// Load scenario
	eng.scenarios = map[string]*config.Scenario{
		"support": {ID: "support", TaskType: "support"},
	}

	// Test with filtered providers
	providerFilter := []string{"p1", "p2", "p3"}
	scenarioFilter := []string{} // Empty - should use all scenarios
	regionFilter := []string{}   // Empty - should use default region

	plan, err := eng.GenerateRunPlan(regionFilter, providerFilter, scenarioFilter)
	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Should have 1 default region * 3 providers * 1 scenario = 3 combinations
	expected := 3
	if len(plan.Combinations) != expected {
		t.Errorf("Expected %d combinations (1 region * 3 providers * 1 scenario), got %d", expected, len(plan.Combinations))
	}

	// Verify all combinations have the default region
	for i, combo := range plan.Combinations {
		if combo.Region != "default" {
			t.Errorf("Combination %d: expected region 'default', got '%s'", i, combo.Region)
		}
		if combo.ProviderID == "" {
			t.Errorf("Combination %d has empty provider ID", i)
		}
		if combo.ScenarioID == "" {
			t.Errorf("Combination %d has empty scenario ID", i)
		}
	}
}

func TestGenerateRunPlan_WithRegions(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
		// No prompt configs needed for this test
	}

	eng := newTestEngine(t, tmpDir, cfg)

	// Load scenario
	eng.scenarios = map[string]*config.Scenario{
		"support": {ID: "support", TaskType: "support"},
	}

	// Test with no region filter - should default to "default" region
	providerFilter := []string{"p1", "p2"}
	scenarioFilter := []string{} // Empty - should use all scenarios
	regionFilter := []string{}   // Empty - should default to "default" region

	plan, err := eng.GenerateRunPlan(regionFilter, providerFilter, scenarioFilter)
	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Should have 1 default region * 2 providers * 1 scenario = 2 combinations
	expected := 2
	if len(plan.Combinations) != expected {
		t.Errorf("Expected %d combinations (1 default region * 2 providers * 1 scenario), got %d", expected, len(plan.Combinations))
	}

	// Verify all combinations use default region
	for _, combo := range plan.Combinations {
		if combo.Region != "default" {
			t.Errorf("Expected region 'default', got '%s'", combo.Region)
		}
	}

	// Test with explicit region filter
	regionFilter = []string{"us", "eu"}
	plan, err = eng.GenerateRunPlan(regionFilter, providerFilter, scenarioFilter)
	if err != nil {
		t.Errorf("GenerateRunPlan with region filter failed: %v", err)
	}

	// Should have 2 regions * 2 providers * 1 scenario = 4 combinations
	expected = 4
	if len(plan.Combinations) != expected {
		t.Errorf("Expected %d combinations (2 regions * 2 providers * 1 scenario), got %d", expected, len(plan.Combinations))
	}

	// Verify combinations have correct regions
	regionCounts := make(map[string]int)
	for _, combo := range plan.Combinations {
		regionCounts[combo.Region]++
	}

	if regionCounts["us"] != 2 {
		t.Errorf("Expected 2 combinations with 'us' region, got %d", regionCounts["us"])
	}
	if regionCounts["eu"] != 2 {
		t.Errorf("Expected 2 combinations with 'eu' region, got %d", regionCounts["eu"])
	}
}

func TestExecuteRuns_MultipleCombinations(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	ctx := context.Background()

	// Add scenario but execution will fail due to missing provider
	eng.scenarios = map[string]*config.Scenario{
		"s1": {ID: "s1", TaskType: "support"},
	}

	plan := &RunPlan{
		Combinations: []RunCombination{
			{Region: "us", ScenarioID: "s1", ProviderID: "p1"},
			{Region: "uk", ScenarioID: "s1", ProviderID: "p1"},
			{Region: "au", ScenarioID: "s1", ProviderID: "p1"},
		},
	}

	runIDs, err := eng.ExecuteRuns(ctx, plan, 2)
	if err != nil {
		t.Fatalf("ExecuteRuns failed: %v", err)
	}

	if len(runIDs) != 3 {
		t.Errorf("Expected 3 runIDs, got %d", len(runIDs))
	}

	// All should have errors since provider doesn't exist - retrieve from statestore
	arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		t.Fatal("StateStore is not ArenaStateStore")
	}

	for i, runID := range runIDs {
		result, err := arenaStore.GetRunResult(ctx, runID)
		if err != nil {
			t.Fatalf("Failed to get result %d: %v", i, err)
		}
		if result.Error == "" {
			t.Errorf("Result %d: Expected error for missing provider", i)
		}
	}
}

func TestRunResult_TimeTracking(t *testing.T) {
	startTime := time.Now()
	endTime := startTime.Add(5 * time.Second)

	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		StartTime:  startTime,
		EndTime:    endTime,
		Duration:   endTime.Sub(startTime),
	}

	if result.Duration != 5*time.Second {
		t.Errorf("Expected duration 5s, got %v", result.Duration)
	}

	if !result.StartTime.Before(result.EndTime) {
		t.Error("StartTime should be before EndTime")
	}
}

func TestRunResult_EmptyError(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		Error:      "",
	}

	if result.Error != "" {
		t.Error("Expected empty error string")
	}
}

func TestRunResult_WithCommit(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		Commit: map[string]interface{}{
			"decision": "approve",
			"reason":   "meets requirements",
		},
	}

	if len(result.Commit) != 2 {
		t.Error("Commit map size mismatch")
	}

	if result.Commit["decision"] != "approve" {
		t.Error("Commit decision mismatch")
	}
}

func TestRunCombination_DifferentRegions(t *testing.T) {
	regions := []string{"us", "uk", "au", "ca", "eu"}

	for _, region := range regions {
		combo := RunCombination{
			Region:     region,
			ScenarioID: "test",
			ProviderID: "test",
		}

		if combo.Region != region {
			t.Errorf("Region mismatch for %s", region)
		}
	}
}

func TestValidationError_Types(t *testing.T) {
	errTypes := []string{"args_invalid", "result_invalid", "policy_violation"}

	for _, errType := range errTypes {
		err := types.ValidationError{
			Type:   errType,
			Tool:   "test",
			Detail: "test detail",
		}

		if err.Type != errType {
			t.Errorf("Type mismatch for %s", errType)
		}
	}
}

func TestToolStats_EmptyMap(t *testing.T) {
	stats := types.ToolStats{
		TotalCalls: 0,
		ByTool:     map[string]int{},
	}

	if stats.TotalCalls != 0 {
		t.Error("Expected 0 total calls")
	}

	if len(stats.ByTool) != 0 {
		t.Error("Expected empty ByTool map")
	}
}

func TestToolStats_SingleTool(t *testing.T) {
	stats := types.ToolStats{
		TotalCalls: 5,
		ByTool: map[string]int{
			"search": 5,
		},
	}

	if stats.TotalCalls != 5 {
		t.Error("TotalCalls mismatch")
	}

	if len(stats.ByTool) != 1 {
		t.Error("Expected 1 tool in map")
	}

	if stats.ByTool["search"] != 5 {
		t.Error("Search count mismatch")
	}
}

func TestCostInfo_ZeroCosts(t *testing.T) {
	cost := types.CostInfo{
		InputTokens:   0,
		OutputTokens:  0,
		InputCostUSD:  0.0,
		OutputCostUSD: 0.0,
		TotalCost:     0.0,
	}

	if cost.TotalCost != 0.0 {
		t.Error("Expected zero cost")
	}

	if cost.InputTokens != 0 {
		t.Error("Expected zero input tokens")
	}

	if cost.OutputTokens != 0 {
		t.Error("Expected zero output tokens")
	}
}

func TestSelfPlayRoleInfo_DifferentProviders(t *testing.T) {
	providers := []struct {
		provider string
		model    string
	}{
		{"openai", "gpt-4"},
		{"anthropic", "claude-3"},
		{"gemini", "gemini-1.5-pro"},
	}

	for _, p := range providers {
		role := SelfPlayRoleInfo{
			Provider: p.provider,
			Model:    p.model,
			Region:   "us",
		}

		if role.Provider != p.provider {
			t.Errorf("Provider mismatch for %s", p.provider)
		}

		if role.Model != p.model {
			t.Errorf("Model mismatch for %s", p.model)
		}
	}
}

func TestRunResult_MultipleViolations(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		Violations: []types.ValidationError{
			{Type: "args_invalid", Tool: "search", Detail: "missing param"},
			{Type: "result_invalid", Tool: "calc", Detail: "invalid output"},
			{Type: "policy_violation", Tool: "api", Detail: "rate limit"},
		},
	}

	if len(result.Violations) != 3 {
		t.Errorf("Expected 3 violations, got %d", len(result.Violations))
	}

	// Verify each violation type
	expectedTypes := []string{"args_invalid", "result_invalid", "policy_violation"}
	for i, violation := range result.Violations {
		if violation.Type != expectedTypes[i] {
			t.Errorf("Violation %d type mismatch", i)
		}
	}
}

func TestRunResult_Params(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		Params: map[string]interface{}{
			"temperature": 0.7,
			"max_tokens":  1000,
			"top_p":       0.9,
		},
	}

	if len(result.Params) != 3 {
		t.Errorf("Expected 3 params, got %d", len(result.Params))
	}

	if result.Params["temperature"] != 0.7 {
		t.Error("Temperature param mismatch")
	}

	if result.Params["max_tokens"] != 1000 {
		t.Error("Max tokens param mismatch")
	}
}

func TestRunResult_GeneralProperties(t *testing.T) {
	// Test needs to be rewritten for flat message model - deferred until message model is finalized
	t.Skip("Test needs to be rewritten for flat message model")
}

func TestRunPlan_LargePlan(t *testing.T) {
	// Create a large plan with many combinations
	combinations := make([]RunCombination, 100)
	for i := 0; i < 100; i++ {
		combinations[i] = RunCombination{
			Region:     "us",
			ScenarioID: "test",
			ProviderID: "test",
		}
	}

	plan := &RunPlan{
		Combinations: combinations,
	}

	if len(plan.Combinations) != 100 {
		t.Errorf("Expected 100 combinations, got %d", len(plan.Combinations))
	}
}
