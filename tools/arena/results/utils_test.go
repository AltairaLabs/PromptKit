package results_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
)

func TestSummaryBuilder_NewSummaryBuilder(t *testing.T) {
	builder := results.NewSummaryBuilder("test-config.yaml")

	assert.NotNil(t, builder)
}

func TestSummaryBuilder_SetTimestamp(t *testing.T) {
	builder := results.NewSummaryBuilder("test-config.yaml")
	testTime := time.Date(2025, 11, 4, 12, 0, 0, 0, time.UTC)

	result := builder.SetTimestamp(testTime)

	assert.Equal(t, builder, result) // Should return self for chaining
}

func TestSummaryBuilder_BuildSummary_EmptyResults(t *testing.T) {
	builder := results.NewSummaryBuilder("test-config.yaml")

	summary := builder.BuildSummary([]engine.RunResult{})

	assert.NotNil(t, summary)
	assert.Equal(t, 0, summary.TotalTests)
	assert.Equal(t, 0, summary.Passed)
	assert.Equal(t, 0, summary.Failed)
	assert.Equal(t, "test-config.yaml", summary.ConfigFile)
	assert.Empty(t, summary.RunIDs)
}

func TestSummaryBuilder_BuildSummary_WithResults(t *testing.T) {
	testResults := []engine.RunResult{
		{
			RunID:      "run-001",
			PromptPack: "pack-1",
			ScenarioID: "scenario-1",
			ProviderID: "openai",
			Region:     "us-east-1",
			Cost:       types.CostInfo{TotalCost: 0.001, InputTokens: 100, OutputTokens: 50},
			Duration:   2 * time.Second,
			Error:      "", // Success
		},
		{
			RunID:      "run-002",
			PromptPack: "pack-1",
			ScenarioID: "scenario-1",
			ProviderID: "anthropic",
			Region:     "us-west-2",
			Cost:       types.CostInfo{TotalCost: 0.002, InputTokens: 120, OutputTokens: 60},
			Duration:   3 * time.Second,
			Error:      "test error", // Failure
		},
		{
			RunID:      "run-003",
			PromptPack: "pack-2",
			ScenarioID: "scenario-2",
			ProviderID: "openai",
			Region:     "eu-west-1",
			Cost:       types.CostInfo{TotalCost: 0.0015, InputTokens: 110, OutputTokens: 55},
			Duration:   2500 * time.Millisecond,
			Error:      "",                                                                                      // Success
			Violations: []types.ValidationError{{Type: "test", Tool: "validator", Detail: "validation failed"}}, // But has violations
		},
	}

	builder := results.NewSummaryBuilder("test-config.yaml")
	summary := builder.BuildSummary(testResults)

	// Test counts
	assert.Equal(t, 3, summary.TotalTests)
	assert.Equal(t, 1, summary.Passed)  // Only run-001 is truly successful
	assert.Equal(t, 2, summary.Failed)  // run-002 has error, run-003 has violations
	assert.Equal(t, 0, summary.Errors)  // Arena doesn't distinguish errors from failures
	assert.Equal(t, 0, summary.Skipped) // Arena doesn't have skipped tests

	// Test performance metrics
	expectedTotalCost := 0.001 + 0.002 + 0.0015
	assert.InDelta(t, expectedTotalCost, summary.TotalCost, 0.0001)
	assert.InDelta(t, expectedTotalCost/3, summary.AverageCost, 0.0001)
	expectedTotalTokens := (100 + 50) + (120 + 60) + (110 + 55)
	assert.Equal(t, expectedTotalTokens, summary.TotalTokens)
	expectedTotalDuration := 2*time.Second + 3*time.Second + 2500*time.Millisecond
	assert.Equal(t, expectedTotalDuration, summary.TotalDuration)

	// Test metadata extraction
	assert.Equal(t, []string{"run-001", "run-002", "run-003"}, summary.RunIDs)
	assert.ElementsMatch(t, []string{"pack-1", "pack-2"}, summary.PromptPacks)
	assert.ElementsMatch(t, []string{"scenario-1", "scenario-2"}, summary.Scenarios)
	assert.ElementsMatch(t, []string{"openai", "anthropic"}, summary.Providers)
	assert.ElementsMatch(t, []string{"us-east-1", "us-west-2", "eu-west-1"}, summary.Regions)
}

func TestCountResultsByStatus(t *testing.T) {
	testResults := []engine.RunResult{
		{Error: "", Violations: []types.ValidationError{}},                            // Success
		{Error: "test error", Violations: []types.ValidationError{}},                  // Failure - error
		{Error: "", Violations: []types.ValidationError{{Type: "test"}}},              // Failure - violations
		{Error: "another error", Violations: []types.ValidationError{{Type: "test"}}}, // Failure - both
		{Error: "", Violations: []types.ValidationError{}},                            // Success
	}

	passed, failed := results.CountResultsByStatus(testResults)

	assert.Equal(t, 2, passed)
	assert.Equal(t, 3, failed)
}

func TestCalculatePerformanceMetrics(t *testing.T) {
	testResults := []engine.RunResult{
		{
			Cost:     types.CostInfo{TotalCost: 0.001, InputTokens: 100, OutputTokens: 50},
			Duration: 2 * time.Second,
		},
		{
			Cost:     types.CostInfo{TotalCost: 0.002, InputTokens: 120, OutputTokens: 60},
			Duration: 3 * time.Second,
		},
		{
			Cost:     types.CostInfo{TotalCost: 0.0015, InputTokens: 110, OutputTokens: 55},
			Duration: 2500 * time.Millisecond,
		},
	}

	totalCost, totalTokens, totalDuration := results.CalculatePerformanceMetrics(testResults)

	expectedCost := 0.001 + 0.002 + 0.0015
	expectedTokens := (100 + 50) + (120 + 60) + (110 + 55)
	expectedDuration := 2*time.Second + 3*time.Second + 2500*time.Millisecond

	assert.InDelta(t, expectedCost, totalCost, 0.0001)
	assert.Equal(t, expectedTokens, totalTokens)
	assert.Equal(t, expectedDuration, totalDuration)
}

func TestExtractRunIDs(t *testing.T) {
	testResults := []engine.RunResult{
		{RunID: "run-001"},
		{RunID: "run-002"},
		{RunID: "run-003"},
	}

	runIDs := results.ExtractRunIDs(testResults)

	expected := []string{"run-001", "run-002", "run-003"}
	assert.Equal(t, expected, runIDs)
}

func TestExtractUniqueValues(t *testing.T) {
	testResults := []engine.RunResult{
		{ScenarioID: "scenario-1"},
		{ScenarioID: "scenario-2"},
		{ScenarioID: "scenario-1"}, // Duplicate
		{ScenarioID: "scenario-3"},
		{ScenarioID: ""}, // Empty should be filtered out
	}

	extractor := func(r engine.RunResult) string { return r.ScenarioID }
	scenarios := results.ExtractUniqueValues(testResults, extractor)

	expected := []string{"scenario-1", "scenario-2", "scenario-3"}
	assert.ElementsMatch(t, expected, scenarios)
}

func TestValidateResults(t *testing.T) {
	t.Run("nil results", func(t *testing.T) {
		err := results.ValidateResults(nil)

		assert.Error(t, err)
		var validationErr *results.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "results", validationErr.Field)
	})

	t.Run("valid results", func(t *testing.T) {
		testResults := []engine.RunResult{
			{RunID: "run-001", ScenarioID: "scenario-1", ProviderID: "openai"},
			{RunID: "run-002", ScenarioID: "scenario-2", ProviderID: "anthropic"},
		}

		err := results.ValidateResults(testResults)
		assert.NoError(t, err)
	})

	t.Run("empty RunID", func(t *testing.T) {
		testResults := []engine.RunResult{
			{RunID: "", ScenarioID: "scenario-1", ProviderID: "openai"},
		}

		err := results.ValidateResults(testResults)

		assert.Error(t, err)
		var validationErr *results.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "RunID", validationErr.Field)
		assert.Contains(t, validationErr.Message, "result 0")
	})

	t.Run("empty ScenarioID", func(t *testing.T) {
		testResults := []engine.RunResult{
			{RunID: "run-001", ScenarioID: "", ProviderID: "openai"},
		}

		err := results.ValidateResults(testResults)

		assert.Error(t, err)
		var validationErr *results.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "ScenarioID", validationErr.Field)
	})

	t.Run("empty ProviderID", func(t *testing.T) {
		testResults := []engine.RunResult{
			{RunID: "run-001", ScenarioID: "scenario-1", ProviderID: ""},
		}

		err := results.ValidateResults(testResults)

		assert.Error(t, err)
		var validationErr *results.ValidationError
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "ProviderID", validationErr.Field)
	})
}
