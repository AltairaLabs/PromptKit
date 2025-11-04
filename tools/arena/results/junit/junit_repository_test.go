package junit_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/junit"
)

// Test helpers
func createTestResult(runID, scenario, provider, region string, hasError bool, cost float64, duration time.Duration) engine.RunResult {
	result := engine.RunResult{
		RunID:      runID,
		ScenarioID: scenario,
		ProviderID: provider,
		Region:     region,
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
		result.Error = "test error message"
	}

	return result
}

func createResultWithViolations(runID, scenario, provider string) engine.RunResult {
	result := createTestResult(runID, scenario, provider, "us-east-1", false, 0.001, 2*time.Second)
	result.Violations = []types.ValidationError{
		{Type: "banned_words", Tool: "validator", Detail: "Found banned word 'test'"},
		{Type: "length_limit", Tool: "validator", Detail: "Response too long"},
	}
	return result
}

func TestNewJUnitResultRepository(t *testing.T) {
	repo := junit.NewJUnitResultRepository("/tmp/junit.xml")

	assert.NotNil(t, repo)
	assert.Equal(t, "/tmp/junit.xml", repo.GetOutputPath())
}

func TestNewJUnitResultRepositoryWithOptions(t *testing.T) {
	options := &junit.JUnitOptions{
		IncludeSystemOut:  false,
		IncludeSystemErr:  true,
		IncludeMetrics:    false,
		SuiteNameTemplate: "{{.ScenarioID}}-custom",
		TestNameTemplate:  "{{.RunID}}",
	}

	repo := junit.NewJUnitResultRepositoryWithOptions("/tmp/junit.xml", options)

	assert.NotNil(t, repo)
	assert.Equal(t, "/tmp/junit.xml", repo.GetOutputPath())
}

func TestDefaultJUnitOptions(t *testing.T) {
	options := junit.DefaultJUnitOptions()

	assert.True(t, options.IncludeSystemOut)
	assert.False(t, options.IncludeSystemErr)
	assert.True(t, options.IncludeMetrics)
	assert.Equal(t, "{{.ScenarioID}}", options.SuiteNameTemplate)
	assert.Equal(t, "{{.ProviderID}}.{{.Region}}.{{.RunID}}", options.TestNameTemplate)
}

func TestJUnitResultRepository_SaveResults_Success(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")
	repo := junit.NewJUnitResultRepository(junitFile)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
		createTestResult("run-002", "scenario-1", "anthropic", "us-west-2", true, 0.002, 3*time.Second),
		createTestResult("run-003", "scenario-2", "openai", "eu-west-1", false, 0.0015, 2500*time.Millisecond),
		createResultWithViolations("run-004", "scenario-1", "claude"),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify XML file was created
	assert.FileExists(t, junitFile)

	// Parse and verify XML structure
	data, err := os.ReadFile(junitFile)
	require.NoError(t, err)

	var testSuites junit.JUnitTestSuites
	err = xml.Unmarshal(data, &testSuites)
	require.NoError(t, err)

	// Verify top-level structure
	assert.Equal(t, "Arena Test Results", testSuites.Name)
	assert.Equal(t, 4, testSuites.Tests)
	assert.Equal(t, 1, testSuites.Failures) // run-004 has violations
	assert.Equal(t, 1, testSuites.Errors)   // run-002 has error
	assert.Greater(t, testSuites.Time, 0.0)

	// Verify test suites (grouped by scenario)
	assert.Len(t, testSuites.TestSuites, 2) // scenario-1 and scenario-2

	// Find specific test suites
	var scenario1Suite, scenario2Suite *junit.JUnitTestSuite
	for _, suite := range testSuites.TestSuites {
		switch suite.Name {
		case "scenario-1":
			scenario1Suite = suite
		case "scenario-2":
			scenario2Suite = suite
		}
	}

	require.NotNil(t, scenario1Suite)
	require.NotNil(t, scenario2Suite)

	// Verify scenario-1 suite
	assert.Equal(t, 3, scenario1Suite.Tests)
	assert.Equal(t, 1, scenario1Suite.Failures)
	assert.Equal(t, 1, scenario1Suite.Errors)

	// Verify scenario-2 suite
	assert.Equal(t, 1, scenario2Suite.Tests)
	assert.Equal(t, 0, scenario2Suite.Failures)
	assert.Equal(t, 0, scenario2Suite.Errors)
}

func TestJUnitResultRepository_SaveResults_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")
	repo := junit.NewJUnitResultRepository(junitFile)

	// Test with nil results
	err := repo.SaveResults(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")

	// Test with empty RunID
	invalidResults := []engine.RunResult{
		{RunID: "", ScenarioID: "test", ProviderID: "test"},
	}

	err = repo.SaveResults(invalidResults)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestJUnitResultRepository_XMLValidation(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")
	repo := junit.NewJUnitResultRepository(junitFile)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Read and verify XML is valid
	data, err := os.ReadFile(junitFile)
	require.NoError(t, err)

	// Should start with XML declaration
	assert.True(t, strings.HasPrefix(string(data), "<?xml version=\"1.0\" encoding=\"UTF-8\"?>"))

	// Should be parseable XML
	var testSuites junit.JUnitTestSuites
	err = xml.Unmarshal(data, &testSuites)
	require.NoError(t, err)
}

func TestJUnitResultRepository_TestCaseStructure(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")
	repo := junit.NewJUnitResultRepository(junitFile)

	// Test with different types of results
	testResults := []engine.RunResult{
		// Successful test
		createTestResult("run-success", "scenario-test", "openai", "us-east-1", false, 0.001, 2*time.Second),
		// Failed test (execution error)
		createTestResult("run-error", "scenario-test", "anthropic", "us-west-2", true, 0.002, 3*time.Second),
		// Failed test (validation violations)
		createResultWithViolations("run-violations", "scenario-test", "claude"),
	}

	// Add some metadata to the successful test
	testResults[0].ToolStats = &types.ToolStats{TotalCalls: 5}
	testResults[0].SelfPlay = true
	testResults[0].PersonaID = "test-persona"

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Parse XML and examine test cases
	data, err := os.ReadFile(junitFile)
	require.NoError(t, err)

	var testSuites junit.JUnitTestSuites
	err = xml.Unmarshal(data, &testSuites)
	require.NoError(t, err)

	require.Len(t, testSuites.TestSuites, 1)
	suite := testSuites.TestSuites[0]
	require.Len(t, suite.TestCases, 3)

	// Find test cases by name
	var successCase, errorCase, violationCase *junit.JUnitTestCase
	for i := range suite.TestCases {
		testCase := &suite.TestCases[i]
		if strings.Contains(testCase.Name, "run-success") {
			successCase = testCase
		} else if strings.Contains(testCase.Name, "run-error") {
			errorCase = testCase
		} else if strings.Contains(testCase.Name, "run-violations") {
			violationCase = testCase
		}
	}

	require.NotNil(t, successCase)
	require.NotNil(t, errorCase)
	require.NotNil(t, violationCase)

	// Verify successful test case
	assert.Equal(t, "openai.us-east-1.run-success", successCase.Name)
	assert.Equal(t, "scenario-test", successCase.Classname)
	assert.Equal(t, 2.0, successCase.Time)
	assert.Nil(t, successCase.Failure)
	assert.Nil(t, successCase.Error)
	assert.NotNil(t, successCase.SystemOut)
	assert.Contains(t, successCase.SystemOut.Content, "Cost: $0.001000")
	assert.Contains(t, successCase.SystemOut.Content, "Tool Calls: 5")
	assert.Contains(t, successCase.SystemOut.Content, "Self-Play: true")
	assert.Contains(t, successCase.SystemOut.Content, "Persona: test-persona")

	// Verify error test case
	assert.Equal(t, "anthropic.us-west-2.run-error", errorCase.Name)
	assert.NotNil(t, errorCase.Error)
	assert.Equal(t, "test error message", errorCase.Error.Message)
	assert.Equal(t, "ExecutionError", errorCase.Error.Type)
	assert.Contains(t, errorCase.Error.Content, "Error: test error message")

	// Verify validation failure test case
	assert.Equal(t, "claude.us-east-1.run-violations", violationCase.Name)
	assert.NotNil(t, violationCase.Failure)
	assert.Contains(t, violationCase.Failure.Message, "Validation failed: 2 violation(s)")
	assert.Equal(t, "ValidationError", violationCase.Failure.Type)
	assert.Contains(t, violationCase.Failure.Content, "banned_words")
	assert.Contains(t, violationCase.Failure.Content, "length_limit")
}

func TestJUnitResultRepository_SaveSummary(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")
	repo := junit.NewJUnitResultRepository(junitFile)

	summary := &results.ResultSummary{
		TotalTests: 5,
		Passed:     3,
		Failed:     2,
	}

	// SaveSummary should be a no-op for JUnit (summary is in XML structure)
	err := repo.SaveSummary(summary)
	assert.NoError(t, err)

	// File should not be created by SaveSummary alone
	assert.NoFileExists(t, junitFile)
}

func TestJUnitResultRepository_UnsupportedOperations(t *testing.T) {
	repo := junit.NewJUnitResultRepository("/tmp/junit.xml")

	t.Run("LoadResults not supported", func(t *testing.T) {
		runResults, err := repo.LoadResults()
		assert.Nil(t, runResults)
		assert.Error(t, err)
		assert.True(t, results.IsUnsupportedOperation(err))
	})

	t.Run("SupportsStreaming is false", func(t *testing.T) {
		assert.False(t, repo.SupportsStreaming())
	})

	t.Run("SaveResult not supported", func(t *testing.T) {
		result := createTestResult("test", "scenario", "provider", "region", false, 0.001, time.Second)
		err := repo.SaveResult(&result)
		assert.Error(t, err)
		assert.True(t, results.IsUnsupportedOperation(err))
	})
}

func TestJUnitResultRepository_CustomOptions(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")

	options := &junit.JUnitOptions{
		IncludeSystemOut: false, // Disable system-out
		IncludeSystemErr: false,
		IncludeMetrics:   false, // Disable metrics
	}

	repo := junit.NewJUnitResultRepositoryWithOptions(junitFile, options)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
	}

	// Add metadata that should be excluded
	testResults[0].ToolStats = &types.ToolStats{TotalCalls: 5}
	testResults[0].Cost = types.CostInfo{TotalCost: 0.001, InputTokens: 100, OutputTokens: 50}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Parse XML and verify system-out is not included
	data, err := os.ReadFile(junitFile)
	require.NoError(t, err)

	var testSuites junit.JUnitTestSuites
	err = xml.Unmarshal(data, &testSuites)
	require.NoError(t, err)

	require.Len(t, testSuites.TestSuites, 1)
	suite := testSuites.TestSuites[0]
	require.Len(t, suite.TestCases, 1)

	testCase := suite.TestCases[0]
	assert.Nil(t, testCase.SystemOut) // Should be nil when IncludeSystemOut=false
}

func TestJUnitResultRepository_EmptyResults(t *testing.T) {
	tmpDir := t.TempDir()
	junitFile := filepath.Join(tmpDir, "junit.xml")
	repo := junit.NewJUnitResultRepository(junitFile)

	err := repo.SaveResults([]engine.RunResult{})
	require.NoError(t, err)

	// Verify XML file was created with empty structure
	data, err := os.ReadFile(junitFile)
	require.NoError(t, err)

	var testSuites junit.JUnitTestSuites
	err = xml.Unmarshal(data, &testSuites)
	require.NoError(t, err)

	assert.Equal(t, 0, testSuites.Tests)
	assert.Equal(t, 0, testSuites.Failures)
	assert.Equal(t, 0, testSuites.Errors)
	assert.Empty(t, testSuites.TestSuites)
}

func TestJUnitResultRepository_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "directory", "junit.xml")
	repo := junit.NewJUnitResultRepository(nestedPath)

	testResults := []engine.RunResult{
		createTestResult("run-001", "scenario-1", "openai", "us-east-1", false, 0.001, 2*time.Second),
	}

	err := repo.SaveResults(testResults)
	require.NoError(t, err)

	// Verify nested directory was created
	assert.FileExists(t, nestedPath)
}
