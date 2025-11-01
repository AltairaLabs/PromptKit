package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

func TestCostInfo_Structure(t *testing.T) {
	cost := types.CostInfo{
		InputTokens:   100,
		OutputTokens:  50,
		CachedTokens:  10,
		InputCostUSD:  0.003,
		OutputCostUSD: 0.006,
		CachedCostUSD: 0.0003,
		TotalCost:     0.0093,
	}

	if cost.InputTokens != 100 {
		t.Error("InputTokens mismatch")
	}

	if cost.OutputTokens != 50 {
		t.Error("OutputTokens mismatch")
	}

	if cost.CachedTokens != 10 {
		t.Error("CachedTokens mismatch")
	}

	if cost.TotalCost != 0.0093 {
		t.Error("USDEstimate mismatch")
	}
}

func TestToolStats_Structure(t *testing.T) {
	stats := types.ToolStats{
		TotalCalls: 5,
		ByTool: map[string]int{
			"search":     2,
			"calculator": 3,
		},
	}

	if stats.TotalCalls != 5 {
		t.Error("TotalCalls mismatch")
	}

	if len(stats.ByTool) != 2 {
		t.Error("ByTool map size mismatch")
	}

	if stats.ByTool["search"] != 2 {
		t.Error("search tool count mismatch")
	}

	if stats.ByTool["calculator"] != 3 {
		t.Error("calculator tool count mismatch")
	}
}

func TestValidationError_Structure(t *testing.T) {
	err := types.ValidationError{
		Type:   "args_invalid",
		Tool:   "search",
		Detail: "missing required parameter",
	}

	if err.Type != "args_invalid" {
		t.Error("Type mismatch")
	}

	if err.Tool != "search" {
		t.Error("Tool mismatch")
	}

	if err.Detail != "missing required parameter" {
		t.Error("Detail mismatch")
	}
}

func TestSelfPlayRoleInfo_Structure(t *testing.T) {
	role := SelfPlayRoleInfo{
		Provider: "openai",
		Model:    "gpt-4",
		Region:   "us",
	}

	if role.Provider != "openai" {
		t.Error("Provider mismatch")
	}

	if role.Model != "gpt-4" {
		t.Error("Model mismatch")
	}

	if role.Region != "us" {
		t.Error("Region mismatch")
	}
}

func TestMessageToolCall_Structure(t *testing.T) {
	call := types.MessageToolCall{
		ID:   "call-123",
		Name: "search",
		Args: []byte(`{"query": "test"}`),
	}

	if call.ID != "call-123" {
		t.Error("ID mismatch")
	}

	if call.Name != "search" {
		t.Error("Name mismatch")
	}

	if len(call.Args) == 0 {
		t.Error("Args should not be empty")
	}
}

func TestEngine_EmptyScenarios(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)

	if len(eng.scenarios) != 0 {
		t.Errorf("Expected 0 scenarios initially, got %d", len(eng.scenarios))
	}
}

func TestEngine_EmptyProviders(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)

	if len(eng.providers) != 0 {
		t.Errorf("Expected 0 providers initially, got %d", len(eng.providers))
	}
}

func TestEngine_EmptyPersonas(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)

	if len(eng.personas) != 0 {
		t.Errorf("Expected 0 personas initially, got %d", len(eng.personas))
	}
}

func TestGenerateRunPlan_MultipleFilters(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)

	// Load test data
	eng.scenarios = map[string]*config.Scenario{
		"s1": {ID: "s1", TaskType: "support"},
		"s2": {ID: "s2", TaskType: "consulting"},
	}

	eng.providers = map[string]*config.Provider{
		"p1": {ID: "p1", Type: "openai"},
		"p2": {ID: "p2", Type: "anthropic"},
	}

	// Test with multiple filters
	plan, err := eng.GenerateRunPlan(
		[]string{"us", "uk"},
		[]string{"p1"},
		[]string{"s1"},
	)

	if err != nil {
		t.Errorf("GenerateRunPlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Plan is nil")
	}

	// Verify all combinations match filters
	for _, combo := range plan.Combinations {
		if combo.ScenarioID != "s1" {
			t.Errorf("Expected scenario s1, got %s", combo.ScenarioID)
		}

		if combo.ProviderID != "p1" {
			t.Errorf("Expected provider p1, got %s", combo.ProviderID)
		}

		if combo.Region != "us" && combo.Region != "uk" {
			t.Errorf("Expected region us or uk, got %s", combo.Region)
		}
	}
}

func TestExecuteRuns_InvalidProvider(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Defaults: config.Defaults{
			Verbose: false,
		},
	}

	eng := newTestEngine(t, tmpDir, cfg)
	ctx := context.Background()

	// Add scenario but no provider
	eng.scenarios = map[string]*config.Scenario{
		"test-scenario": {ID: "test-scenario", TaskType: "support"},
	}

	plan := &RunPlan{
		Combinations: []RunCombination{
			{
				Region:     "us",
				ScenarioID: "test-scenario",
				ProviderID: "nonexistent-provider",
			},
		},
	}

	runIDs, err := eng.ExecuteRuns(ctx, plan, 1)
	if err != nil {
		t.Fatalf("ExecuteRuns failed: %v", err)
	}

	if len(runIDs) != 1 {
		t.Fatalf("Expected 1 runID, got %d", len(runIDs))
	}

	// Get the result from statestore
	arenaStore, ok := eng.GetStateStore().(*statestore.ArenaStateStore)
	if !ok {
		t.Fatal("Failed to get ArenaStateStore")
	}

	result, err := arenaStore.GetRunResult(ctx, runIDs[0])
	if err != nil {
		t.Fatalf("Failed to get run result: %v", err)
	}

	// Result should have an error
	if result.Error == "" {
		t.Error("Expected error for nonexistent provider, got none")
	}
}

func TestRunResult_WithViolations(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		Violations: []types.ValidationError{
			{
				Type:   "policy_violation",
				Tool:   "search",
				Detail: "exceeded rate limit",
			},
		},
	}

	if len(result.Violations) != 1 {
		t.Error("Violations count mismatch")
	}

	if result.Violations[0].Type != "policy_violation" {
		t.Error("Violation type mismatch")
	}

	if result.Violations[0].Tool != "search" {
		t.Error("Violation tool mismatch")
	}

	if result.Violations[0].Detail != "exceeded rate limit" {
		t.Error("Violation detail mismatch")
	}
}

func TestRunResult_WithToolStats(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		ToolStats: &types.ToolStats{
			TotalCalls: 3,
			ByTool: map[string]int{
				"search": 2,
				"calc":   1,
			},
		},
	}

	if result.ToolStats == nil {
		t.Fatal("ToolStats is nil")
	}

	if result.ToolStats.TotalCalls != 3 {
		t.Error("TotalCalls mismatch")
	}

	if len(result.ToolStats.ByTool) != 2 {
		t.Error("ByTool map size mismatch")
	}
}

func TestRunResult_SelfPlay(t *testing.T) {
	result := RunResult{
		ScenarioID: "test",
		ProviderID: "test",
		Region:     "us",
		SelfPlay:   true,
		PersonaID:  "test-persona",
		AssistantRole: &SelfPlayRoleInfo{
			Provider: "openai",
			Model:    "gpt-4",
			Region:   "us",
		},
		UserRole: &SelfPlayRoleInfo{
			Provider: "anthropic",
			Model:    "claude-3",
			Region:   "us",
		},
	}

	if !result.SelfPlay {
		t.Error("Expected SelfPlay to be true")
	}

	if result.PersonaID != "test-persona" {
		t.Error("PersonaID mismatch")
	}

	if result.AssistantRole == nil {
		t.Fatal("AssistantRole is nil")
	}

	if result.UserRole == nil {
		t.Fatal("UserRole is nil")
	}

	if result.AssistantRole.Provider != "openai" {
		t.Error("AssistantRole provider mismatch")
	}

	if result.UserRole.Provider != "anthropic" {
		t.Error("UserRole provider mismatch")
	}
}
