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

// Mock repository for testing
type mockRepository struct {
	supportsStreaming    bool
	saveResultsCalled    bool
	saveSummaryCalled    bool
	loadResultsCalled    bool
	saveResultCalled     bool
	shouldFailSave       bool
	shouldFailLoad       bool
	shouldFailSummary    bool
	shouldFailSingle     bool
	returnedResults      []engine.RunResult
	receivedResults      []engine.RunResult
	receivedSummary      *results.ResultSummary
	receivedSingleResult *engine.RunResult
}

func (m *mockRepository) SaveResults(res []engine.RunResult) error {
	m.saveResultsCalled = true
	m.receivedResults = res
	if m.shouldFailSave {
		return assert.AnError
	}
	return nil
}

func (m *mockRepository) SaveSummary(summary *results.ResultSummary) error {
	m.saveSummaryCalled = true
	m.receivedSummary = summary
	if m.shouldFailSummary {
		return assert.AnError
	}
	return nil
}

func (m *mockRepository) LoadResults() ([]engine.RunResult, error) {
	m.loadResultsCalled = true
	if m.shouldFailLoad {
		return nil, assert.AnError
	}
	if !m.supportsStreaming {
		return nil, results.NewUnsupportedOperationError("LoadResults", "mock doesn't support loading")
	}
	return m.returnedResults, nil
}

func (m *mockRepository) SupportsStreaming() bool {
	return m.supportsStreaming
}

func (m *mockRepository) SaveResult(result *engine.RunResult) error {
	m.saveResultCalled = true
	m.receivedSingleResult = result
	if !m.supportsStreaming {
		return results.NewUnsupportedOperationError("SaveResult", "mock doesn't support streaming")
	}
	if m.shouldFailSingle {
		return assert.AnError
	}
	return nil
}

// Test helper functions
func createTestResult(runID, scenario, provider string, hasError bool, cost float64, duration time.Duration) engine.RunResult {
	result := engine.RunResult{
		RunID:      runID,
		ScenarioID: scenario,
		ProviderID: provider,
		Region:     "us-east-1",
		Cost: types.CostInfo{
			InputTokens:  100,
			OutputTokens: 50,
			TotalCost:    cost,
		},
		Duration:  duration,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(duration),
	}

	if hasError {
		result.Error = "test error"
	}

	return result
}

func createTestResults() []engine.RunResult {
	return []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", false, 0.001, 2*time.Second),
		createTestResult("run-002", "scenario-1", "anthropic", true, 0.002, 3*time.Second),
		createTestResult("run-003", "scenario-2", "openai", false, 0.0015, 2500*time.Millisecond),
	}
}

func TestResultSummary_Structure(t *testing.T) {
	summary := &results.ResultSummary{
		TotalTests:    10,
		Passed:        8,
		Failed:        2,
		TotalCost:     0.05,
		AverageCost:   0.005,
		TotalTokens:   1500,
		TotalDuration: 30 * time.Second,
		ConfigFile:    "test-config.yaml",
		Timestamp:     time.Now(),
		RunIDs:        []string{"run-1", "run-2"},
		Scenarios:     []string{"scenario-1"},
		Providers:     []string{"openai", "anthropic"},
		Regions:       []string{"us-east-1"},
	}

	assert.Equal(t, 10, summary.TotalTests)
	assert.Equal(t, 8, summary.Passed)
	assert.Equal(t, 2, summary.Failed)
	assert.Equal(t, 0.05, summary.TotalCost)
	assert.Equal(t, 0.005, summary.AverageCost)
	assert.Equal(t, 1500, summary.TotalTokens)
	assert.Equal(t, 30*time.Second, summary.TotalDuration)
	assert.Equal(t, "test-config.yaml", summary.ConfigFile)
	assert.Len(t, summary.RunIDs, 2)
	assert.Len(t, summary.Scenarios, 1)
	assert.Len(t, summary.Providers, 2)
	assert.Len(t, summary.Regions, 1)
}

func TestCompositeRepository_NewCompositeRepository(t *testing.T) {
	repo1 := &mockRepository{}
	repo2 := &mockRepository{}

	composite := results.NewCompositeRepository(repo1, repo2)

	require.NotNil(t, composite)
	repos := composite.GetRepositories()
	assert.Len(t, repos, 2)
}

func TestCompositeRepository_AddRepository(t *testing.T) {
	composite := results.NewCompositeRepository()
	repo := &mockRepository{}

	composite.AddRepository(repo)

	repos := composite.GetRepositories()
	assert.Len(t, repos, 1)
}

func TestCompositeRepository_SaveResults_Success(t *testing.T) {
	repo1 := &mockRepository{}
	repo2 := &mockRepository{}
	composite := results.NewCompositeRepository(repo1, repo2)

	testResults := createTestResults()
	err := composite.SaveResults(testResults)

	assert.NoError(t, err)
	assert.True(t, repo1.saveResultsCalled)
	assert.True(t, repo2.saveResultsCalled)
	assert.Equal(t, testResults, repo1.receivedResults)
	assert.Equal(t, testResults, repo2.receivedResults)
}

func TestCompositeRepository_SaveResults_PartialFailure(t *testing.T) {
	repo1 := &mockRepository{shouldFailSave: true}
	repo2 := &mockRepository{}
	composite := results.NewCompositeRepository(repo1, repo2)

	testResults := createTestResults()
	err := composite.SaveResults(testResults)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repository 0 failed")
	assert.True(t, repo1.saveResultsCalled)
	assert.True(t, repo2.saveResultsCalled)

	// Verify it's a composite error
	var compositeErr *results.CompositeError
	require.ErrorAs(t, err, &compositeErr)
	assert.Equal(t, "SaveResults", compositeErr.Operation)
	assert.Len(t, compositeErr.Errors, 1)
}

func TestCompositeRepository_SaveSummary_Success(t *testing.T) {
	repo1 := &mockRepository{}
	repo2 := &mockRepository{}
	composite := results.NewCompositeRepository(repo1, repo2)

	summary := &results.ResultSummary{TotalTests: 5}
	err := composite.SaveSummary(summary)

	assert.NoError(t, err)
	assert.True(t, repo1.saveSummaryCalled)
	assert.True(t, repo2.saveSummaryCalled)
	assert.Equal(t, summary, repo1.receivedSummary)
	assert.Equal(t, summary, repo2.receivedSummary)
}

func TestCompositeRepository_LoadResults_Success(t *testing.T) {
	expectedResults := createTestResults()
	repo1 := &mockRepository{supportsStreaming: false} // Doesn't support loading
	repo2 := &mockRepository{supportsStreaming: true, returnedResults: expectedResults}
	composite := results.NewCompositeRepository(repo1, repo2)

	actualResults, err := composite.LoadResults()

	assert.NoError(t, err)
	assert.Equal(t, expectedResults, actualResults)
	assert.True(t, repo1.loadResultsCalled)
	assert.True(t, repo2.loadResultsCalled)
}

func TestCompositeRepository_LoadResults_NoSupportedRepositories(t *testing.T) {
	repo1 := &mockRepository{supportsStreaming: false}
	repo2 := &mockRepository{supportsStreaming: false}
	composite := results.NewCompositeRepository(repo1, repo2)

	loadResults, err := composite.LoadResults()

	assert.Nil(t, loadResults)
	assert.Error(t, err)
	assert.True(t, results.IsUnsupportedOperation(err))
}

func TestCompositeRepository_SupportsStreaming(t *testing.T) {
	t.Run("no repositories support streaming", func(t *testing.T) {
		repo1 := &mockRepository{supportsStreaming: false}
		repo2 := &mockRepository{supportsStreaming: false}
		composite := results.NewCompositeRepository(repo1, repo2)

		assert.False(t, composite.SupportsStreaming())
	})

	t.Run("one repository supports streaming", func(t *testing.T) {
		repo1 := &mockRepository{supportsStreaming: false}
		repo2 := &mockRepository{supportsStreaming: true}
		composite := results.NewCompositeRepository(repo1, repo2)

		assert.True(t, composite.SupportsStreaming())
	})
}

func TestCompositeRepository_SaveResult_Success(t *testing.T) {
	repo1 := &mockRepository{supportsStreaming: false}
	repo2 := &mockRepository{supportsStreaming: true}
	composite := results.NewCompositeRepository(repo1, repo2)

	testResult := createTestResult("test-001", "scenario", "provider", false, 0.001, time.Second)
	err := composite.SaveResult(&testResult)

	assert.NoError(t, err)
	assert.False(t, repo1.saveResultCalled) // Doesn't support streaming
	assert.True(t, repo2.saveResultCalled)
	assert.Equal(t, &testResult, repo2.receivedSingleResult)
}

func TestCompositeRepository_SaveResult_NoStreamingSupport(t *testing.T) {
	repo1 := &mockRepository{supportsStreaming: false}
	repo2 := &mockRepository{supportsStreaming: false}
	composite := results.NewCompositeRepository(repo1, repo2)

	testResult := createTestResult("test-001", "scenario", "provider", false, 0.001, time.Second)
	err := composite.SaveResult(&testResult)

	assert.Error(t, err)
	assert.True(t, results.IsUnsupportedOperation(err))
}