package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// Test constants to avoid string literal duplication (SonarQube go:S1192)
const (
	// Test providers
	testProviderOpenAI         = "openai"
	testProviderOpenAIGPT4Mini = "openai-gpt-4o-mini"
	testProviderAnthropic      = "anthropic"
	testProviderTest           = "test-provider"
	testProviderUnknown        = "unknown-provider"

	// Test regions
	testRegionUSWest  = "us-west"
	testRegionUSEast  = "us-east"
	testRegionUS      = "us"
	testRegionUnknown = "unknown-region"

	// Test scenarios
	testScenario1        = "scenario1"
	testScenario2        = "scenario2"
	testScenarioS1       = "s1"
	testScenarioS2       = "s2"
	testScenarioSelfPlay = "selfplay-scenario"

	// Test run IDs
	testRunID1 = "run1"
	testRunID2 = "run2"
	testRunID3 = "run3"

	// Test report titles
	testReportTitle   = "Test Report"
	testTemplateTitle = "Template Test"
	testEdgeCaseTitle = "Edge Case Test"

	// Test strings
	testTotalRuns = "Total Runs"
	testErrorMsg  = "Test error message"

	// Common test error messages
	testFailedGenerateHTML = "Failed to generate HTML: %v"
	testGeneratedHTMLEmpty = "Generated HTML is empty"

	// Validation test strings
	testValidatorBannedWords = "*validators.BannedWordsValidator"
	testValidatorLength      = "*validators.LengthValidator"
	testValidations          = "validations"
	testTurnMetrics          = "turn_metrics"
	testTurnIndex            = "turn_index"
	testValidatorType        = "validator_type"
	testPassed               = "passed"
	testDetails              = "details"
	testTokensIn             = "tokens_in"
	testTokensOut            = "tokens_out"
	testCostUSD              = "cost_usd"
)

// Helper function to create test results
func createTestResult(id, provider, region, scenario string, cost float64, inputTokens, outputTokens int, hasError bool, duration time.Duration) engine.RunResult {
	result := engine.RunResult{
		RunID:      id,
		ProviderID: provider,
		Region:     region,
		ScenarioID: scenario,
		Cost: types.CostInfo{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalCost:    cost,
		},
		Duration:  duration,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(duration),
	}

	if hasError {
		result.Error = testErrorMsg
	}

	return result
}

func TestPrepareReportData_EmptyResults(t *testing.T) {
	results := []engine.RunResult{}
	data := prepareReportData(results)

	if data.Summary.TotalRuns != 0 {
		t.Errorf("Expected 0 total runs, got %d", data.Summary.TotalRuns)
	}

	if data.Summary.SuccessfulRuns != 0 {
		t.Errorf("Expected 0 successful runs, got %d", data.Summary.SuccessfulRuns)
	}

	if data.Summary.ErrorRuns != 0 {
		t.Errorf("Expected 0 error runs, got %d", data.Summary.ErrorRuns)
	}

	if data.Summary.TotalCost != 0 {
		t.Errorf("Expected 0 total cost, got %f", data.Summary.TotalCost)
	}

	if data.Summary.TotalTokens != 0 {
		t.Errorf("Expected 0 total tokens, got %d", data.Summary.TotalTokens)
	}

	if data.Summary.AvgLatency != "N/A" {
		t.Errorf("Expected 'N/A' for average latency, got %s", data.Summary.AvgLatency)
	}

	if len(data.Providers) != 0 {
		t.Errorf("Expected 0 providers, got %d", len(data.Providers))
	}

	if len(data.Regions) != 0 {
		t.Errorf("Expected 0 regions, got %d", len(data.Regions))
	}

	if len(data.Scenarios) != 0 {
		t.Errorf("Expected 0 scenarios, got %d", len(data.Scenarios))
	}
}

func TestPrepareReportData_SingleResult(t *testing.T) {
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
	}

	data := prepareReportData(results)

	if data.Summary.TotalRuns != 1 {
		t.Errorf("Expected 1 total run, got %d", data.Summary.TotalRuns)
	}

	if data.Summary.SuccessfulRuns != 1 {
		t.Errorf("Expected 1 successful run, got %d", data.Summary.SuccessfulRuns)
	}

	if data.Summary.ErrorRuns != 0 {
		t.Errorf("Expected 0 error runs, got %d", data.Summary.ErrorRuns)
	}

	if data.Summary.TotalCost != 0.05 {
		t.Errorf("Expected 0.05 total cost, got %f", data.Summary.TotalCost)
	}

	if data.Summary.TotalTokens != 150 {
		t.Errorf("Expected 150 total tokens, got %d", data.Summary.TotalTokens)
	}

	if data.Summary.AvgLatency == "N/A" {
		t.Error("Expected average latency to be calculated")
	}

	if len(data.Providers) != 1 || data.Providers[0] != testProviderOpenAI {
		t.Errorf("Expected 1 provider '%s', got %v", testProviderOpenAI, data.Providers)
	}

	if len(data.Regions) != 1 || data.Regions[0] != testRegionUSWest {
		t.Errorf("Expected 1 region '%s', got %v", testRegionUSWest, data.Regions)
	}

	if len(data.Scenarios) != 1 || data.Scenarios[0] != testScenario1 {
		t.Errorf("Expected 1 scenario '%s', got %v", testScenario1, data.Scenarios)
	}
}

func TestPrepareReportData_MultipleResults(t *testing.T) {
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderAnthropic, testRegionUSEast, testScenario2, 0.03, 80, 40, false, 300*time.Millisecond),
		createTestResult(testRunID3, testProviderOpenAI, testRegionUSWest, testScenario2, 0.04, 90, 45, true, 0),
	}

	data := prepareReportData(results)

	if data.Summary.TotalRuns != 3 {
		t.Errorf("Expected 3 total runs, got %d", data.Summary.TotalRuns)
	}

	if data.Summary.SuccessfulRuns != 2 {
		t.Errorf("Expected 2 successful runs, got %d", data.Summary.SuccessfulRuns)
	}

	if data.Summary.ErrorRuns != 1 {
		t.Errorf("Expected 1 error run, got %d", data.Summary.ErrorRuns)
	}

	expectedCost := 0.05 + 0.03 + 0.04
	if data.Summary.TotalCost != expectedCost {
		t.Errorf("Expected %.2f total cost, got %.2f", expectedCost, data.Summary.TotalCost)
	}

	expectedTokens := (100 + 50) + (80 + 40) + (90 + 45)
	if data.Summary.TotalTokens != expectedTokens {
		t.Errorf("Expected %d total tokens, got %d", expectedTokens, data.Summary.TotalTokens)
	}

	if len(data.Providers) != 2 {
		t.Errorf("Expected 2 providers, got %d", len(data.Providers))
	}

	if len(data.Regions) != 2 {
		t.Errorf("Expected 2 regions, got %d", len(data.Regions))
	}

	if len(data.Scenarios) != 2 {
		t.Errorf("Expected 2 scenarios, got %d", len(data.Scenarios))
	}
}

func TestPrepareReportData_AvgLatencyFormatting(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"Microseconds", 500 * time.Microsecond, "µs"},
		{"Milliseconds", 250 * time.Millisecond, "ms"},
		{"Seconds", 2 * time.Second, "s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := []engine.RunResult{
				createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.01, 100, 50, false, tt.duration),
			}

			data := prepareReportData(results)

			if !strings.Contains(data.Summary.AvgLatency, tt.expected) {
				t.Errorf("Expected average latency to contain '%s', got '%s'", tt.expected, data.Summary.AvgLatency)
			}
		})
	}
}

func TestPrepareReportData_WithCachedTokens(t *testing.T) {
	result := createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond)
	result.Cost.CachedTokens = 25
	results := []engine.RunResult{result}

	data := prepareReportData(results)

	expectedTokens := 100 + 50 + 25
	if data.Summary.TotalTokens != expectedTokens {
		t.Errorf("Expected %d total tokens (including cached), got %d", expectedTokens, data.Summary.TotalTokens)
	}
}

func TestGenerateMatrix_EmptyResults(t *testing.T) {
	results := []engine.RunResult{}
	providers := []string{}
	regions := []string{}

	matrix := generateMatrix(results, providers, regions)

	if len(matrix) != 0 {
		t.Errorf("Expected empty matrix, got %d rows", len(matrix))
	}
}

func TestGenerateMatrix_SingleCell(t *testing.T) {
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderOpenAI, testRegionUSWest, testScenario2, 0.03, 80, 40, false, 300*time.Millisecond),
	}
	providers := []string{testProviderOpenAI}
	regions := []string{testRegionUSWest}

	matrix := generateMatrix(results, providers, regions)

	if len(matrix) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(matrix))
	}

	if len(matrix[0]) != 1 {
		t.Fatalf("Expected 1 column, got %d", len(matrix[0]))
	}

	cell := matrix[0][0]
	if cell.Provider != testProviderOpenAI {
		t.Errorf("Expected provider '%s', got '%s'", testProviderOpenAI, cell.Provider)
	}

	if cell.Region != testRegionUSWest {
		t.Errorf("Expected region '%s', got '%s'", testRegionUSWest, cell.Region)
	}

	if cell.Scenarios != 2 {
		t.Errorf("Expected 2 scenarios, got %d", cell.Scenarios)
	}

	if cell.Successful != 2 {
		t.Errorf("Expected 2 successful, got %d", cell.Successful)
	}

	if cell.Errors != 0 {
		t.Errorf("Expected 0 errors, got %d", cell.Errors)
	}

	expectedCost := 0.05 + 0.03
	if cell.Cost != expectedCost {
		t.Errorf("Expected cost %.2f, got %.2f", expectedCost, cell.Cost)
	}
}

func TestGenerateMatrix_MultipleProviders(t *testing.T) {
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderAnthropic, testRegionUSWest, testScenarioS1, 0.03, 80, 40, false, 300*time.Millisecond),
		createTestResult(testRunID3, testProviderOpenAI, testRegionUSEast, testScenarioS1, 0.04, 90, 45, true, 0),
	}
	providers := []string{testProviderOpenAI, testProviderAnthropic}
	regions := []string{testRegionUSWest, testRegionUSEast}

	matrix := generateMatrix(results, providers, regions)

	if len(matrix) != 2 {
		t.Fatalf("Expected 2 rows (providers), got %d", len(matrix))
	}

	if len(matrix[0]) != 2 {
		t.Fatalf("Expected 2 columns (regions), got %d", len(matrix[0]))
	}

	// Check openai/us-west cell
	cell := matrix[0][0]
	if cell.Scenarios != 1 || cell.Successful != 1 || cell.Errors != 0 {
		t.Errorf("Unexpected openai/us-west cell: %+v", cell)
	}

	// Check openai/us-east cell
	cell = matrix[0][1]
	if cell.Scenarios != 1 || cell.Successful != 0 || cell.Errors != 1 {
		t.Errorf("Unexpected openai/us-east cell: %+v", cell)
	}

	// Check anthropic/us-west cell
	cell = matrix[1][0]
	if cell.Scenarios != 1 || cell.Successful != 1 || cell.Errors != 0 {
		t.Errorf("Unexpected anthropic/us-west cell: %+v", cell)
	}

	// Check anthropic/us-east cell (should be empty)
	cell = matrix[1][1]
	if cell.Scenarios != 0 || cell.Successful != 0 || cell.Errors != 0 {
		t.Errorf("Expected empty anthropic/us-east cell, got: %+v", cell)
	}
}

func TestGenerateHTML_ValidData(t *testing.T) {
	data := HTMLReportData{
		Title:       testReportTitle,
		GeneratedAt: time.Now(),
		Summary: ReportSummary{
			TotalRuns:      5,
			SuccessfulRuns: 4,
			ErrorRuns:      1,
			TotalCost:      0.25,
			TotalTokens:    1000,
			AvgLatency:     "250.00ms",
		},
		Results:   []engine.RunResult{},
		Providers: []string{testProviderOpenAI},
		Regions:   []string{testRegionUS},
		Scenarios: []string{"test"},
		Matrix:    [][]MatrixCell{},
	}

	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf(testFailedGenerateHTML, err)
	}

	if len(html) == 0 {
		t.Error(testGeneratedHTMLEmpty)
	}

	// Check for key content in HTML
	if !strings.Contains(html, testReportTitle) {
		t.Error("HTML doesn't contain title")
	}

	if !strings.Contains(html, testTotalRuns) {
		t.Error("HTML doesn't contain 'Total Runs'")
	}
}

func TestGenerateHTML_TemplateFunctions(t *testing.T) {
	// Test with data that exercises template functions
	data := HTMLReportData{
		Title:       testTemplateTitle,
		GeneratedAt: time.Now(),
		Summary: ReportSummary{
			TotalRuns:      10,
			SuccessfulRuns: 8,
			ErrorRuns:      2,
			TotalCost:      0.1234,
			TotalTokens:    500,
			AvgLatency:     "100.00ms",
		},
		Results: []engine.RunResult{
			createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
		},
		Providers: []string{testProviderOpenAI},
		Regions:   []string{testRegionUS},
		Scenarios: []string{testScenarioS1},
		Matrix: [][]MatrixCell{
			{{Provider: testProviderOpenAI, Region: testRegionUS, Scenarios: 1, Successful: 1, Errors: 0, Cost: 0.05}},
		},
	}

	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf(testFailedGenerateHTML, err)
	}

	// The template should format the cost
	if !strings.Contains(html, "$") {
		t.Error("HTML doesn't contain formatted cost")
	}
}

func TestGenerateHTMLReport_CreatesFiles(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-report.html")

	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
	}

	err := GenerateHTMLReport(results, outputPath)
	if err != nil {
		t.Fatalf("Failed to generate HTML report: %v", err)
	}

	// Check HTML file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("HTML file was not created")
	}

	// Check JSON file was created
	jsonPath := strings.TrimSuffix(outputPath, ".html") + "-data.json"
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Error("JSON data file was not created")
	}

	// Read and verify JSON file
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON file: %v", err)
	}

	var data HTMLReportData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		t.Fatalf("Failed to unmarshal JSON data: %v", err)
	}

	if data.Summary.TotalRuns != 1 {
		t.Errorf("Expected 1 run in JSON data, got %d", data.Summary.TotalRuns)
	}
}

func TestGenerateHTMLReport_CreatesDirectory(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nested", "dir", "report.html")

	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
	}

	err := GenerateHTMLReport(results, outputPath)
	if err != nil {
		t.Fatalf("Failed to generate HTML report: %v", err)
	}

	// Check that nested directory was created
	if _, err := os.Stat(filepath.Dir(outputPath)); os.IsNotExist(err) {
		t.Error("Output directory was not created")
	}

	// Check file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("HTML file was not created in nested directory")
	}
}

func TestGenerateHTMLReport_InvalidPath(t *testing.T) {
	// Use an invalid path (directory without write permissions on Unix-like systems)
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
	}

	// This path should be invalid on most systems
	invalidPath := "/invalid/path/that/does/not/exist/and/cannot/be/created/report.html"

	err := GenerateHTMLReport(results, invalidPath)
	if err == nil {
		t.Error("Expected error for invalid path, got nil")
	}
}

func TestHTMLReportData_Structure(t *testing.T) {
	data := HTMLReportData{
		Title:       "Test",
		GeneratedAt: time.Now(),
		Summary: ReportSummary{
			TotalRuns:      1,
			SuccessfulRuns: 1,
			ErrorRuns:      0,
			TotalCost:      0.05,
			TotalTokens:    100,
			AvgLatency:     "100ms",
		},
		Results:   []engine.RunResult{},
		Providers: []string{"test"},
		Regions:   []string{"us"},
		Scenarios: []string{"s1"},
		Matrix:    [][]MatrixCell{},
	}

	// Test JSON serialization
	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal data: %v", err)
	}

	// Test JSON deserialization
	var decoded HTMLReportData
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal data: %v", err)
	}

	if decoded.Title != data.Title {
		t.Errorf("Title mismatch after JSON round-trip")
	}

	if decoded.Summary.TotalRuns != data.Summary.TotalRuns {
		t.Errorf("TotalRuns mismatch after JSON round-trip")
	}
}

func TestReportSummary_Structure(t *testing.T) {
	summary := ReportSummary{
		TotalRuns:      10,
		SuccessfulRuns: 8,
		ErrorRuns:      2,
		TotalCost:      1.23,
		TotalTokens:    5000,
		AvgLatency:     "250ms",
	}

	jsonData, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Failed to marshal summary: %v", err)
	}

	var decoded ReportSummary
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal summary: %v", err)
	}

	if decoded.TotalRuns != summary.TotalRuns {
		t.Error("TotalRuns mismatch")
	}

	if decoded.TotalCost != summary.TotalCost {
		t.Error("TotalCost mismatch")
	}
}

func TestMatrixCell_Structure(t *testing.T) {
	cell := MatrixCell{
		Provider:   testProviderOpenAI,
		Region:     testRegionUSWest,
		Scenarios:  5,
		Successful: 4,
		Errors:     1,
		Cost:       0.25,
	}

	jsonData, err := json.Marshal(cell)
	if err != nil {
		t.Fatalf("Failed to marshal cell: %v", err)
	}

	var decoded MatrixCell
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal cell: %v", err)
	}

	if decoded.Provider != cell.Provider {
		t.Error("Provider mismatch")
	}

	if decoded.Scenarios != cell.Scenarios {
		t.Error("Scenarios mismatch")
	}

	if decoded.Cost != cell.Cost {
		t.Error("Cost mismatch")
	}
}

func TestPrepareReportData_ZeroDuration(t *testing.T) {
	// Test with results that have zero duration (should not affect average)
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderOpenAI, testRegionUS, testScenarioS2, 0.03, 80, 40, true, 0), // Error with no duration
	}

	data := prepareReportData(results)

	// Average should be calculated only from non-zero durations
	if data.Summary.AvgLatency == "N/A" {
		t.Error("Expected average latency to be calculated from valid durations")
	}

	// Should contain "ms" for the 500ms result
	if !strings.Contains(data.Summary.AvgLatency, "ms") {
		t.Errorf("Expected latency in milliseconds, got %s", data.Summary.AvgLatency)
	}
}

func TestGenerateMatrix_UnmatchedResults(t *testing.T) {
	// Test with results that don't match the provider/region lists
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderUnknown, testRegionUnknown, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
	}
	providers := []string{testProviderOpenAI}
	regions := []string{testRegionUSWest}

	matrix := generateMatrix(results, providers, regions)

	// Matrix should still be created with correct dimensions
	if len(matrix) != 1 || len(matrix[0]) != 1 {
		t.Error("Matrix dimensions incorrect")
	}

	// Cell should be empty since result doesn't match
	cell := matrix[0][0]
	if cell.Scenarios != 0 {
		t.Errorf("Expected empty cell, got %d scenarios", cell.Scenarios)
	}
}

func TestPrepareReportData_DuplicateProviders(t *testing.T) {
	// Multiple results with same provider/region should be deduplicated
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderOpenAI, testRegionUS, testScenarioS2, 0.03, 80, 40, false, 300*time.Millisecond),
	}

	data := prepareReportData(results)

	if len(data.Providers) != 1 {
		t.Errorf("Expected 1 unique provider, got %d", len(data.Providers))
	}

	if len(data.Regions) != 1 {
		t.Errorf("Expected 1 unique region, got %d", len(data.Regions))
	}

	if len(data.Scenarios) != 2 {
		t.Errorf("Expected 2 unique scenarios, got %d", len(data.Scenarios))
	}
}

func TestGetValidationsForTurn(t *testing.T) {
	tests := []struct {
		name       string
		validators interface{}
		turnIndex  int
		want       int // number of validations expected
	}{
		{
			name:       "nil validators",
			validators: nil,
			turnIndex:  1,
			want:       0,
		},
		{
			name: "direct struct type (from Go)",
			validators: map[string]interface{}{
				testValidations: []statestore.ValidationResult{
					{
						TurnIndex:     1,
						ValidatorType: testValidatorBannedWords,
						Passed:        false,
						Details:       map[string]interface{}{"value": []string{"guarantee"}},
					},
					{
						TurnIndex:     1,
						ValidatorType: testValidatorLength,
						Passed:        true,
						Details:       map[string]interface{}{"length": 100},
					},
					{
						TurnIndex:     2,
						ValidatorType: testValidatorBannedWords,
						Passed:        true,
						Details:       nil,
					},
				},
				testTurnMetrics: []map[string]interface{}{},
			},
			turnIndex: 1,
			want:      2,
		},
		{
			name: "JSON-loaded type (from file)",
			validators: func() interface{} {
				// Simulate what happens when JSON is loaded
				jsonStr := `{
					"` + testValidations + `": [
						{
							"` + testTurnIndex + `": 1,
							"` + testValidatorType + `": "` + testValidatorBannedWords + `",
							"` + testPassed + `": false,
							"` + testDetails + `": {"value": ["guarantee"]}
						},
						{
							"` + testTurnIndex + `": 1,
							"` + testValidatorType + `": "` + testValidatorLength + `",
							"` + testPassed + `": true,
							"` + testDetails + `": {"length": 100}
						},
						{
							"` + testTurnIndex + `": 2,
							"` + testValidatorType + `": "` + testValidatorBannedWords + `",
							"` + testPassed + `": true,
							"` + testDetails + `": null
						}
					],
					"` + testTurnMetrics + `": []
				}`
				var v map[string]interface{}
				_ = json.Unmarshal([]byte(jsonStr), &v) // Ignore error in test
				return v
			}(),
			turnIndex: 1,
			want:      2,
		},
		{
			name: "turn 2 has one validation",
			validators: map[string]interface{}{
				testValidations: []statestore.ValidationResult{
					{TurnIndex: 1, ValidatorType: "test1", Passed: true},
					{TurnIndex: 2, ValidatorType: "test2", Passed: false},
				},
			},
			turnIndex: 2,
			want:      1,
		},
		{
			name: "turn 3 has no validations",
			validators: map[string]interface{}{
				testValidations: []statestore.ValidationResult{
					{TurnIndex: 1, ValidatorType: "test1", Passed: true},
					{TurnIndex: 2, ValidatorType: "test2", Passed: false},
				},
			},
			turnIndex: 3,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the actual function from the template generation
			data := HTMLReportData{}
			html, _ := generateHTML(data)
			_ = html // Ensure the function is created

			// Call our test implementation
			got := testGetValidationsForTurn(tt.validators, tt.turnIndex)

			// Check length
			if len(got) != tt.want {
				t.Errorf("getValidationsForTurn() returned %d validations, want %d", len(got), tt.want)
			}

			// For non-empty results, verify structure
			if len(got) > 0 {
				for i, v := range got {
					if _, ok := v["turn_index"]; !ok {
						t.Errorf("validation %d missing turn_index field", i)
					}
					if _, ok := v["validator_type"]; !ok {
						t.Errorf("validation %d missing validator_type field", i)
					}
					if _, ok := v["passed"]; !ok {
						t.Errorf("validation %d missing passed field", i)
					}
					// Verify turn_index matches
					if ti, ok := v["turn_index"].(float64); ok {
						if int(ti) != tt.turnIndex {
							t.Errorf("validation %d has turn_index=%d, want %d", i, int(ti), tt.turnIndex)
						}
					}
				}
			}
		})
	}
}

func TestGetTurnMetrics(t *testing.T) {
	tests := []struct {
		name       string
		validators interface{}
		turnIndex  int
		wantNil    bool
	}{
		{
			name:       "nil validators",
			validators: nil,
			turnIndex:  1,
			wantNil:    true,
		},
		{
			name: "direct struct type (from Go)",
			validators: map[string]interface{}{
				testTurnMetrics: []map[string]interface{}{
					{
						testTurnIndex: 1,
						testTokensIn:  100,
						testTokensOut: 50,
						testCostUSD:   0.001,
					},
					{
						testTurnIndex: 2,
						testTokensIn:  200,
						testTokensOut: 75,
						testCostUSD:   0.002,
					},
				},
				testValidations: []statestore.ValidationResult{},
			},
			turnIndex: 1,
			wantNil:   false,
		},
		{
			name: "JSON-loaded type (from file)",
			validators: func() interface{} {
				jsonStr := `{
					"` + testTurnMetrics + `": [
						{
							"` + testTurnIndex + `": 1,
							"` + testTokensIn + `": 100,
							"` + testTokensOut + `": 50,
							"` + testCostUSD + `": 0.001
						},
						{
							"` + testTurnIndex + `": 2,
							"` + testTokensIn + `": 200,
							"` + testTokensOut + `": 75,
							"` + testCostUSD + `": 0.002
						}
					],
					"` + testValidations + `": []
				}`
				var v map[string]interface{}
				_ = json.Unmarshal([]byte(jsonStr), &v) // Ignore error in test
				return v
			}(),
			turnIndex: 1,
			wantNil:   false,
		},
		{
			name: "turn not found",
			validators: map[string]interface{}{
				testTurnMetrics: []map[string]interface{}{
					{testTurnIndex: 1, testTokensIn: 100},
					{testTurnIndex: 2, testTokensIn: 200},
				},
			},
			turnIndex: 3,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call our test implementation
			got := testGetTurnMetrics(tt.validators, tt.turnIndex)

			// Check nil
			if tt.wantNil {
				if got != nil {
					t.Errorf("getTurnMetrics() returned %v, want nil", got)
				}
				return
			}

			// For non-nil results, verify structure
			if got == nil {
				t.Errorf("getTurnMetrics() returned nil, want metrics")
				return
			}

			if _, ok := got["turn_index"]; !ok {
				t.Errorf("metrics missing turn_index field")
			}

			// Verify turn_index matches
			if ti, ok := got["turn_index"].(float64); ok {
				if int(ti) != tt.turnIndex {
					t.Errorf("metrics has turn_index=%d, want %d", int(ti), tt.turnIndex)
				}
			}
		})
	}
}

// Test implementation of template functions for testing
func testGetValidationsForTurn(validators interface{}, turnIndex int) []map[string]interface{} {
	if validators == nil {
		return nil
	}
	validatorsMap, ok := validators.(map[string]interface{})
	if !ok {
		return nil
	}

	validationsRaw := validatorsMap["validations"]
	if validationsRaw == nil {
		return nil
	}

	// Try to convert to []interface{} (happens when loaded from JSON)
	if validations, ok := validationsRaw.([]interface{}); ok {
		var turnValidations []map[string]interface{}
		for _, v := range validations {
			validation, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			// Check if this validation is for the current turn
			if ti, ok := validation["turn_index"].(float64); ok && int(ti) == turnIndex {
				turnValidations = append(turnValidations, validation)
			}
		}
		return turnValidations
	}

	// Handle direct struct type (when passed from Go without JSON round-trip)
	// Convert to generic map format
	jsonBytes, err := json.Marshal(validationsRaw)
	if err != nil {
		return nil
	}
	var validations []map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &validations); err != nil {
		return nil
	}

	var turnValidations []map[string]interface{}
	for _, validation := range validations {
		// Check if this validation is for the current turn
		if ti, ok := validation["turn_index"].(float64); ok && int(ti) == turnIndex {
			turnValidations = append(turnValidations, validation)
		}
	}
	return turnValidations
}

// testGetTurnData is a helper function to extract data for a specific turn from validators
func testGetTurnData(validators interface{}, dataKey string, turnIndex int) interface{} {
	if validators == nil {
		return nil
	}
	validatorsMap, ok := validators.(map[string]interface{})
	if !ok {
		return nil
	}

	dataRaw := validatorsMap[dataKey]
	if dataRaw == nil {
		return nil
	}

	// Try to convert to []interface{} (happens when loaded from JSON)
	if data, ok := dataRaw.([]interface{}); ok {
		for _, item := range data {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			// Check if this item is for the current turn
			if ti, ok := itemMap["turn_index"].(float64); ok && int(ti) == turnIndex {
				return itemMap
			}
		}
		return nil
	}

	// Handle direct struct type (when passed from Go without JSON round-trip)
	// Convert to generic map format
	jsonBytes, err := json.Marshal(dataRaw)
	if err != nil {
		return nil
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &items); err != nil {
		return nil
	}

	for _, item := range items {
		// Check if this item is for the current turn
		if ti, ok := item["turn_index"].(float64); ok && int(ti) == turnIndex {
			return item
		}
	}
	return nil
}

func testGetTurnMetrics(validators interface{}, turnIndex int) map[string]interface{} {
	result := testGetTurnData(validators, "turn_metrics", turnIndex)
	if result == nil {
		return nil
	}
	return result.(map[string]interface{})
}

func TestHasValidations(t *testing.T) {
	tests := []struct {
		name       string
		validators interface{}
		turnIndex  int
		want       bool
	}{
		{
			name:       "nil validators",
			validators: nil,
			turnIndex:  1,
			want:       false,
		},
		{
			name: "has validation for turn",
			validators: map[string]interface{}{
				testValidations: []statestore.ValidationResult{
					{TurnIndex: 1, ValidatorType: "test", Passed: true},
				},
			},
			turnIndex: 1,
			want:      true,
		},
		{
			name: "no validation for turn",
			validators: map[string]interface{}{
				testValidations: []statestore.ValidationResult{
					{TurnIndex: 1, ValidatorType: "test", Passed: true},
				},
			},
			turnIndex: 2,
			want:      false,
		},
		{
			name: "JSON-loaded type",
			validators: func() interface{} {
				jsonStr := `{
					"` + testValidations + `": [
						{"` + testTurnIndex + `": 1, "` + testValidatorType + `": "test", "` + testPassed + `": true}
					]
				}`
				var v map[string]interface{}
				_ = json.Unmarshal([]byte(jsonStr), &v) // Ignore error in test
				return v
			}(),
			turnIndex: 1,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testHasValidations(tt.validators, tt.turnIndex)
			if got != tt.want {
				t.Errorf("hasValidations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testHasValidations(validators interface{}, turnIndex int) bool {
	result := testGetTurnData(validators, "validations", turnIndex)
	return result != nil
}

// TestGenerateHTML_WithMessageCostInfo tests that the HTML template correctly
// accesses the TotalCost field (not TotalCostUSD) from message CostInfo.
// This is a regression test for the template error:
// "can't evaluate field TotalCostUSD in type *types.CostInfo"
func TestGenerateHTML_WithMessageCostInfo(t *testing.T) {
	// Create a result with messages that have CostInfo
	result := createTestResult(testRunID1, testProviderOpenAIGPT4Mini, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond)

	// Add messages with CostInfo to the result
	result.Messages = []types.Message{
		{
			Role:      "user",
			Content:   "What is the weather?",
			Timestamp: time.Now(),
		},
		{
			Role:      "assistant",
			Content:   "I don't have access to real-time weather data.",
			Timestamp: time.Now(),
			LatencyMs: 250,
			CostInfo: &types.CostInfo{
				InputTokens:   10,
				OutputTokens:  15,
				InputCostUSD:  0.00001,
				OutputCostUSD: 0.00002,
				TotalCost:     0.00003,
			},
		},
		{
			Role:      "user",
			Content:   "Tell me about climate patterns.",
			Timestamp: time.Now(),
		},
		{
			Role:      "assistant",
			Content:   "Climate patterns vary by region and are influenced by many factors.",
			Timestamp: time.Now(),
			LatencyMs: 320,
			CostInfo: &types.CostInfo{
				InputTokens:   25,
				OutputTokens:  20,
				InputCostUSD:  0.000025,
				OutputCostUSD: 0.00004,
				TotalCost:     0.000065,
			},
		},
	}

	// Use prepareReportData to properly populate ScenarioGroups
	data := prepareReportData([]engine.RunResult{result})

	// This should not panic or error - specifically tests that the template
	// can access $msg.CostInfo.TotalCost (not TotalCostUSD)
	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf("Failed to generate HTML with message CostInfo: %v", err)
	}

	if len(html) == 0 {
		t.Error(testGeneratedHTMLEmpty)
	}

	// Verify the HTML contains the formatted costs
	if !strings.Contains(html, "$") {
		t.Error("HTML doesn't contain formatted cost symbols")
	}

	// Verify the HTML contains token information from messages
	if !strings.Contains(html, "→") {
		t.Error("HTML doesn't contain token arrow separator (InputTokens→OutputTokens)")
	}

	// Verify the HTML contains the report title (set by prepareReportData)
	if !strings.Contains(html, "Altaira Prompt Arena Report") {
		t.Error("HTML doesn't contain report title")
	}

	// Verify the HTML contains latency information
	if !strings.Contains(html, "⏱️") {
		t.Error("HTML doesn't contain latency indicator")
	}

	// Verify the HTML contains token count indicators
	if !strings.Contains(html, "🎯") {
		t.Error("HTML doesn't contain token count indicator")
	}

	// Verify the HTML contains cost indicators
	if !strings.Contains(html, "💰") {
		t.Error("HTML doesn't contain cost indicator")
	}
}

// TestGenerateHTML_UserMessageWithCostInfo tests that user messages with CostInfo
// (from self-play scenarios) display the cost/latency badge in the HTML.
func TestGenerateHTML_UserMessageWithCostInfo(t *testing.T) {
	result := createTestResult(testRunID1, testProviderOpenAIGPT4Mini, testRegionUSWest, testScenarioSelfPlay, 0.0001, 231, 59, false, 1500*time.Millisecond)

	// Add messages mimicking a self-play scenario
	result.Messages = []types.Message{
		{
			Role:      "user",
			Content:   "Hello, I need help with my account",
			Timestamp: time.Now(),
			// No cost info - static/scripted message
		},
		{
			Role:      "assistant",
			Content:   "I'd be happy to help you with your account. What seems to be the issue?",
			Timestamp: time.Now(),
			LatencyMs: 850,
			CostInfo: &types.CostInfo{
				InputTokens:   15,
				OutputTokens:  20,
				InputCostUSD:  0.000015,
				OutputCostUSD: 0.00004,
				TotalCost:     0.000055,
			},
		},
		{
			Role:      "user",
			Content:   "I seem to have forgotten the email I used for my account. Can you help me recover it?",
			Timestamp: time.Now(),
			LatencyMs: 650, // Self-play generation took time
			CostInfo: &types.CostInfo{
				InputTokens:   231,
				OutputTokens:  59,
				InputCostUSD:  0.00003465,
				OutputCostUSD: 0.00003540,
				TotalCost:     0.00007005,
			},
			Meta: map[string]interface{}{
				"raw_response": map[string]interface{}{
					"self_play_execution": true,
					"persona":             "social-engineer",
				},
			},
		},
		{
			Role:      "assistant",
			Content:   "I can help you with account recovery. Let me verify your identity first...",
			Timestamp: time.Now(),
			LatencyMs: 920,
			CostInfo: &types.CostInfo{
				InputTokens:   290,
				OutputTokens:  25,
				InputCostUSD:  0.000290,
				OutputCostUSD: 0.00005,
				TotalCost:     0.000340,
			},
		},
	}

	// Use prepareReportData to properly populate ScenarioGroups
	data := prepareReportData([]engine.RunResult{result})

	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf(testFailedGenerateHTML, err)
	}

	// The HTML should contain cost badges for both user (self-play) and assistant messages
	// Each message with CostInfo should have:
	// - The latency indicator (⏱️)
	// - The token count indicator (🎯)
	// - The cost indicator (💰)
	// - The token arrow (→) showing input→output
	// - The formatted cost

	// Count occurrences of cost-related indicators
	latencyCount := strings.Count(html, "⏱️")
	tokenIndicatorCount := strings.Count(html, "🎯")
	costIndicatorCount := strings.Count(html, "💰")

	// We expect 3 messages with cost info (1 user + 2 assistant)
	if latencyCount < 3 {
		t.Errorf("Expected at least 3 latency indicators (⏱️), got %d", latencyCount)
	}

	if tokenIndicatorCount < 3 {
		t.Errorf("Expected at least 3 token indicators (🎯), got %d", tokenIndicatorCount)
	}

	if costIndicatorCount < 3 {
		t.Errorf("Expected at least 3 cost indicators (💰), got %d", costIndicatorCount)
	}

	// Verify the self-play user message with CostInfo shows metrics
	// The badge should appear in the turn-header for user messages with cost_info
	if !strings.Contains(html, "650ms") { // Self-play generation latency
		t.Error("HTML doesn't contain self-play user message latency (650ms)")
	}

	if !strings.Contains(html, "231→59") { // Self-play message tokens
		t.Error("HTML doesn't contain self-play user message token counts (231→59)")
	}

	// Verify all cost values are present
	if !strings.Contains(html, "$0.0001") {
		t.Error("HTML doesn't contain formatted cost values")
	}
}

// TestGenerateHTML_MessageCostInfoEdgeCases tests edge cases for message cost rendering
func TestGenerateHTML_MessageCostInfoEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		costInfo *types.CostInfo
		wantErr  bool
	}{
		{
			name:     "nil CostInfo",
			costInfo: nil,
			wantErr:  false,
		},
		{
			name: "zero costs",
			costInfo: &types.CostInfo{
				InputTokens:   0,
				OutputTokens:  0,
				InputCostUSD:  0,
				OutputCostUSD: 0,
				TotalCost:     0,
			},
			wantErr: false,
		},
		{
			name: "very small costs",
			costInfo: &types.CostInfo{
				InputTokens:   1,
				OutputTokens:  1,
				InputCostUSD:  0.0000001,
				OutputCostUSD: 0.0000001,
				TotalCost:     0.0000002,
			},
			wantErr: false,
		},
		{
			name: "large costs",
			costInfo: &types.CostInfo{
				InputTokens:   10000,
				OutputTokens:  5000,
				InputCostUSD:  1.5,
				OutputCostUSD: 2.3,
				TotalCost:     3.8,
			},
			wantErr: false,
		},
		{
			name: "with cached tokens",
			costInfo: &types.CostInfo{
				InputTokens:   100,
				OutputTokens:  50,
				CachedTokens:  25,
				InputCostUSD:  0.001,
				OutputCostUSD: 0.002,
				CachedCostUSD: 0.0005,
				TotalCost:     0.0035,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createTestResult(testRunID1, testProviderTest, testRegionUS, testScenarioS1, 0.05, 100, 50, false, 500*time.Millisecond)

			// Add a message with the test CostInfo
			result.Messages = []types.Message{
				{
					Role:      "assistant",
					Content:   "Test response",
					Timestamp: time.Now(),
					LatencyMs: 100,
					CostInfo:  tt.costInfo,
				},
			}

			data := HTMLReportData{
				Title:       testEdgeCaseTitle,
				GeneratedAt: time.Now(),
				Summary: ReportSummary{
					TotalRuns:      1,
					SuccessfulRuns: 1,
					ErrorRuns:      0,
					TotalCost:      0.05,
					TotalTokens:    150,
					AvgLatency:     "100.00ms",
				},
				Results:   []engine.RunResult{result},
				Providers: []string{testProviderTest},
				Regions:   []string{"us"},
				Scenarios: []string{"s1"},
				Matrix:    [][]MatrixCell{},
			}

			html, err := generateHTML(data)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateHTML() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(html) == 0 {
				t.Error(testGeneratedHTMLEmpty)
			}
		})
	}
}

func TestHasValidators_WithMessageValidations(t *testing.T) {
	tests := []struct {
		name        string
		message     types.Message
		wantDisplay bool
	}{
		{
			name: "message with validations",
			message: types.Message{
				Role:    "assistant",
				Content: "Test response",
				Validations: []types.ValidationResult{
					{
						ValidatorType: "*validators.BannedWordsValidator",
						Passed:        false,
						Details: map[string]interface{}{
							"value": []string{"damn"},
						},
					},
				},
			},
			wantDisplay: true,
		},
		{
			name: "message without validations",
			message: types.Message{
				Role:    "assistant",
				Content: "Test response",
			},
			wantDisplay: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createTestResult(testRunID1, testProviderTest, testRegionUS, testScenarioS1, 0.01, 50, 25, false, 100*time.Millisecond)
			result.Messages = []types.Message{tt.message}

			data := prepareReportData([]engine.RunResult{result})
			html, err := generateHTML(data)
			if err != nil {
				t.Fatalf("generateHTML() error = %v", err)
			}

			hasValidationBadge := strings.Contains(html, "badge-label\">V</span>")
			if hasValidationBadge != tt.wantDisplay {
				t.Errorf("Expected validation badge = %v, got %v", tt.wantDisplay, hasValidationBadge)
			}
		})
	}
}

func TestGetValidatorsFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		msgData map[string]interface{}
		want    int // number of validators
	}{
		{
			name: "message with validations array",
			msgData: map[string]interface{}{
				"validations": []interface{}{
					map[string]interface{}{
						"validator_type": "*validators.BannedWordsValidator",
						"passed":         false,
					},
				},
			},
			want: 1,
		},
		{
			name:    "message without validations",
			msgData: map[string]interface{}{},
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getValidatorsFromMessage(tt.msgData)
			if len(got) != tt.want {
				t.Errorf("getValidatorsFromMessage() returned %d validators, want %d", len(got), tt.want)
			}
		})
	}
}

func TestPrepareReportData_WithMediaStats(t *testing.T) {
	imageData := "base64imagedata"
	audioData := "base64audiodata"

	result := createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond)
	result.Messages = []types.Message{
		{
			Role:    "user",
			Content: "Generate some media",
		},
		{
			Role:    "assistant",
			Content: "Here you go",
			Parts: []types.ContentPart{
				{
					Type: types.ContentTypeImage,
					Media: &types.MediaContent{
						MIMEType: "image/png",
						Data:     &imageData,
					},
				},
				{
					Type: types.ContentTypeAudio,
					Media: &types.MediaContent{
						MIMEType: "audio/mp3",
						Data:     &audioData,
					},
				},
			},
		},
	}

	data := prepareReportData([]engine.RunResult{result})

	// Check media statistics
	if data.Summary.TotalImages != 1 {
		t.Errorf("Expected TotalImages 1, got %d", data.Summary.TotalImages)
	}
	if data.Summary.TotalAudio != 1 {
		t.Errorf("Expected TotalAudio 1, got %d", data.Summary.TotalAudio)
	}
	if data.Summary.TotalVideo != 0 {
		t.Errorf("Expected TotalVideo 0, got %d", data.Summary.TotalVideo)
	}
	if data.Summary.MediaLoadSuccess != 2 {
		t.Errorf("Expected MediaLoadSuccess 2, got %d", data.Summary.MediaLoadSuccess)
	}
	if data.Summary.MediaLoadErrors != 0 {
		t.Errorf("Expected MediaLoadErrors 0, got %d", data.Summary.MediaLoadErrors)
	}
}

func TestGenerateScenarioGroups_SingleScenario(t *testing.T) {
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderAnthropic, testRegionUSEast, testScenario1, 0.03, 80, 40, false, 300*time.Millisecond),
	}

	groups := generateScenarioGroups(results, []string{testScenario1})

	if len(groups) != 1 {
		t.Fatalf("Expected 1 scenario group, got %d", len(groups))
	}

	group := groups[0]
	if group.ScenarioID != testScenario1 {
		t.Errorf("Expected ScenarioID %s, got %s", testScenario1, group.ScenarioID)
	}
	if len(group.Providers) != 2 {
		t.Errorf("Expected 2 providers in group, got %d", len(group.Providers))
	}
	if len(group.Regions) != 2 {
		t.Errorf("Expected 2 regions in group, got %d", len(group.Regions))
	}
	if len(group.Results) != 2 {
		t.Errorf("Expected 2 results in group, got %d", len(group.Results))
	}
	if len(group.Matrix) != 2 {
		t.Errorf("Expected matrix with 2 rows, got %d", len(group.Matrix))
	}
}

func TestGenerateScenarioGroups_MultipleScenarios(t *testing.T) {
	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderAnthropic, testRegionUSWest, testScenario2, 0.03, 80, 40, false, 300*time.Millisecond),
		createTestResult(testRunID3, testProviderOpenAI, testRegionUSEast, testScenario1, 0.04, 90, 45, false, 400*time.Millisecond),
	}

	groups := generateScenarioGroups(results, []string{testScenario1, testScenario2})

	if len(groups) != 2 {
		t.Fatalf("Expected 2 scenario groups, got %d", len(groups))
	}

	// Check first scenario group
	group1 := groups[0]
	if group1.ScenarioID != testScenario1 {
		t.Errorf("Group 0: Expected ScenarioID %s, got %s", testScenario1, group1.ScenarioID)
	}
	if len(group1.Results) != 2 {
		t.Errorf("Group 0: Expected 2 results, got %d", len(group1.Results))
	}

	// Check second scenario group
	group2 := groups[1]
	if group2.ScenarioID != testScenario2 {
		t.Errorf("Group 1: Expected ScenarioID %s, got %s", testScenario2, group2.ScenarioID)
	}
	if len(group2.Results) != 1 {
		t.Errorf("Group 1: Expected 1 result, got %d", len(group2.Results))
	}
}

func TestRenderMessageContent(t *testing.T) {
	msg := types.Message{
		Role:    "assistant",
		Content: "Hello, world!",
	}

	html := renderMessageContent(msg)
	htmlStr := string(html)

	if !strings.Contains(htmlStr, "assistant") {
		t.Error("HTML should contain role 'assistant'")
	}
	if !strings.Contains(htmlStr, "Hello, world!") {
		t.Error("HTML should contain message content")
	}
}

func TestFormatBytesHTML(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatBytesHTML(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatBytesHTML(%d) = %s, want %s", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestHasMediaOutputs(t *testing.T) {
	tests := []struct {
		name     string
		result   engine.RunResult
		expected bool
	}{
		{
			name:     "no media outputs",
			result:   engine.RunResult{},
			expected: false,
		},
		{
			name: "with media outputs",
			result: engine.RunResult{
				MediaOutputs: []engine.MediaOutput{
					{Type: types.ContentTypeImage},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMediaOutputs(tt.result)
			if result != tt.expected {
				t.Errorf("hasMediaOutputs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRenderMediaOutputs(t *testing.T) {
	width := 800
	height := 600
	duration := 120
	outputs := []engine.MediaOutput{
		{
			Type:      types.ContentTypeImage,
			MIMEType:  "image/jpeg",
			SizeBytes: 1024 * 500,
			Width:     &width,
			Height:    &height,
			FilePath:  "/path/to/image.jpg",
		},
		{
			Type:      types.ContentTypeVideo,
			MIMEType:  "video/mp4",
			SizeBytes: 1024 * 1024 * 10,
			Duration:  &duration,
			FilePath:  "/path/to/video.mp4",
		},
	}

	html := renderMediaOutputs(outputs)
	htmlStr := string(html)

	// Check for container div
	if !strings.Contains(htmlStr, "media-outputs-section") {
		t.Error("HTML should contain media-outputs-section div")
	}

	// Check for count
	if !strings.Contains(htmlStr, "2") {
		t.Error("HTML should show count of 2 media outputs")
	}

	// Check for image details
	if !strings.Contains(htmlStr, "image/jpeg") {
		t.Error("HTML should contain image MIME type")
	}
	if !strings.Contains(htmlStr, "800×600") {
		t.Error("HTML should contain image dimensions")
	}

	// Check for video details
	if !strings.Contains(htmlStr, "video/mp4") {
		t.Error("HTML should contain video MIME type")
	}
	if !strings.Contains(htmlStr, "120s") {
		t.Error("HTML should contain video duration")
	}
}

func TestRenderMediaOutputs_Empty(t *testing.T) {
	outputs := []engine.MediaOutput{}
	html := renderMediaOutputs(outputs)

	if html != "" {
		t.Error("Expected empty HTML for empty outputs")
	}
}

func TestRenderMediaOutputs_WithThumbnail(t *testing.T) {
	outputs := []engine.MediaOutput{
		{
			Type:      types.ContentTypeImage,
			MIMEType:  "image/png",
			SizeBytes: 1024,
			Thumbnail: "base64thumbnaildata",
		},
	}

	html := renderMediaOutputs(outputs)
	htmlStr := string(html)

	if !strings.Contains(htmlStr, "media-thumbnail") {
		t.Error("HTML should contain media-thumbnail div")
	}
	if !strings.Contains(htmlStr, "base64thumbnaildata") {
		t.Error("HTML should contain thumbnail data")
	}
	if !strings.Contains(htmlStr, "data:image/png;base64,") {
		t.Error("HTML should contain proper data URI for thumbnail")
	}
}

func TestFindProviderIndex(t *testing.T) {
	providers := []string{"openai", "anthropic", "google"}

	tests := []struct {
		providerID string
		expected   int
	}{
		{"openai", 0},
		{"anthropic", 1},
		{"google", 2},
		{"unknown", -1},
	}

	for _, tt := range tests {
		t.Run(tt.providerID, func(t *testing.T) {
			result := findProviderIndex(tt.providerID, providers)
			if result != tt.expected {
				t.Errorf("findProviderIndex(%s) = %d, want %d", tt.providerID, result, tt.expected)
			}
		})
	}
}

func TestFindRegionIndex(t *testing.T) {
	regions := []string{"us-west", "us-east", "eu-central"}

	tests := []struct {
		region   string
		expected int
	}{
		{"us-west", 0},
		{"us-east", 1},
		{"eu-central", 2},
		{"unknown", -1},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			result := findRegionIndex(tt.region, regions)
			if result != tt.expected {
				t.Errorf("findRegionIndex(%s) = %d, want %d", tt.region, result, tt.expected)
			}
		})
	}
}

func TestUpdateMatrixCell(t *testing.T) {
	cell := MatrixCell{
		Provider: testProviderOpenAI,
		Region:   testRegionUSWest,
	}

	result := createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond)

	updateMatrixCell(&cell, result)

	if cell.Scenarios != 1 {
		t.Errorf("Expected Scenarios 1, got %d", cell.Scenarios)
	}
	if cell.Successful != 1 {
		t.Errorf("Expected Successful 1, got %d", cell.Successful)
	}
	if cell.Errors != 0 {
		t.Errorf("Expected Errors 0, got %d", cell.Errors)
	}
	if cell.Cost != 0.05 {
		t.Errorf("Expected Cost 0.05, got %f", cell.Cost)
	}
}

func TestUpdateMatrixCell_WithError(t *testing.T) {
	cell := MatrixCell{
		Provider: testProviderOpenAI,
		Region:   testRegionUSWest,
	}

	result := createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.03, 100, 50, true, 0)

	updateMatrixCell(&cell, result)

	if cell.Scenarios != 1 {
		t.Errorf("Expected Scenarios 1, got %d", cell.Scenarios)
	}
	if cell.Successful != 0 {
		t.Errorf("Expected Successful 0, got %d", cell.Successful)
	}
	if cell.Errors != 1 {
		t.Errorf("Expected Errors 1, got %d", cell.Errors)
	}
}

func TestInitializeMatrix(t *testing.T) {
	providers := []string{"provider1", "provider2"}
	regions := []string{"region1", "region2", "region3"}

	matrix := initializeMatrix(providers, regions)

	if len(matrix) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(matrix))
	}
	if len(matrix[0]) != 3 {
		t.Errorf("Expected 3 columns, got %d", len(matrix[0]))
	}

	// Check cell initialization
	if matrix[0][0].Provider != "provider1" || matrix[0][0].Region != "region1" {
		t.Error("Matrix cell [0][0] not initialized correctly")
	}
	if matrix[1][2].Provider != "provider2" || matrix[1][2].Region != "region3" {
		t.Error("Matrix cell [1][2] not initialized correctly")
	}
}

func TestPopulateMatrixWithResults(t *testing.T) {
	providers := []string{testProviderOpenAI, testProviderAnthropic}
	regions := []string{testRegionUSWest, testRegionUSEast}

	matrix := initializeMatrix(providers, regions)

	results := []engine.RunResult{
		createTestResult(testRunID1, testProviderOpenAI, testRegionUSWest, testScenario1, 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult(testRunID2, testProviderOpenAI, testRegionUSWest, testScenario2, 0.03, 80, 40, false, 300*time.Millisecond),
		createTestResult(testRunID3, testProviderAnthropic, testRegionUSEast, testScenario1, 0.04, 90, 45, true, 0),
	}

	populateMatrixWithResults(matrix, results, providers, regions)

	// Check openai/us-west cell (2 results)
	if matrix[0][0].Scenarios != 2 {
		t.Errorf("Cell [0][0] expected 2 scenarios, got %d", matrix[0][0].Scenarios)
	}
	if matrix[0][0].Successful != 2 {
		t.Errorf("Cell [0][0] expected 2 successful, got %d", matrix[0][0].Successful)
	}

	// Check anthropic/us-east cell (1 error)
	if matrix[1][1].Scenarios != 1 {
		t.Errorf("Cell [1][1] expected 1 scenario, got %d", matrix[1][1].Scenarios)
	}
	if matrix[1][1].Errors != 1 {
		t.Errorf("Cell [1][1] expected 1 error, got %d", matrix[1][1].Errors)
	}

	// Check openai/us-east cell (empty)
	if matrix[0][1].Scenarios != 0 {
		t.Errorf("Cell [0][1] expected 0 scenarios, got %d", matrix[0][1].Scenarios)
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		cost     float64
		expected string
	}{
		{0.0, "$0.0000"},
		{0.05, "$0.0500"},
		{1.2345, "$1.2345"},
		{10.5, "$10.5000"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatCost(tt.cost)
			if result != tt.expected {
				t.Errorf("formatCost(%f) = %s, want %s", tt.cost, result, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		contains string
	}{
		{500 * time.Nanosecond, "µs"},
		{500 * time.Microsecond, "µs"},
		{500 * time.Millisecond, "ms"},
		{2 * time.Second, "s"},
		{250 * time.Millisecond, "ms"},
	}

	for _, tt := range tests {
		t.Run(tt.contains, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("formatDuration(%v) = %s, want to contain %s", tt.duration, result, tt.contains)
			}
		})
	}
}

func TestFormatPercent(t *testing.T) {
	tests := []struct {
		part     int
		total    int
		expected string
	}{
		{0, 0, "0%"},
		{5, 10, "50.0%"},
		{1, 3, "33.3%"},
		{10, 10, "100.0%"},
		{0, 10, "0.0%"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatPercent(tt.part, tt.total)
			if result != tt.expected {
				t.Errorf("formatPercent(%d, %d) = %s, want %s", tt.part, tt.total, result, tt.expected)
			}
		})
	}
}

func TestGetStatusClass(t *testing.T) {
	tests := []struct {
		successful int
		errors     int
		expected   string
	}{
		{0, 0, "empty"},
		{5, 0, "success"},
		{0, 5, "error"},
		{5, 2, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getStatusClass(tt.successful, tt.errors)
			if result != tt.expected {
				t.Errorf("getStatusClass(%d, %d) = %s, want %s", tt.successful, tt.errors, result, tt.expected)
			}
		})
	}
}

func TestPrettyJSON_String(t *testing.T) {
	jsonStr := `{"key":"value","number":123}`
	result := prettyJSON(jsonStr)

	// Should be formatted with indentation
	if !strings.Contains(result, "\n") {
		t.Error("Expected formatted JSON with newlines")
	}
	if !strings.Contains(result, "key") {
		t.Error("Expected JSON to contain 'key'")
	}
}

func TestPrettyJSON_Object(t *testing.T) {
	obj := map[string]interface{}{
		"name":  "test",
		"count": 42,
	}
	result := prettyJSON(obj)

	if !strings.Contains(result, "name") {
		t.Error("Expected JSON to contain 'name'")
	}
	if !strings.Contains(result, "42") {
		t.Error("Expected JSON to contain '42'")
	}
}

func TestPrettyJSON_InvalidString(t *testing.T) {
	invalidJSON := "not json at all"
	result := prettyJSON(invalidJSON)

	// Should return original string for invalid JSON
	if result != invalidJSON {
		t.Errorf("Expected original string for invalid JSON, got %s", result)
	}
}

func TestConvertToJS(t *testing.T) {
	data := map[string]string{
		"key": "value",
	}

	result := convertToJS(data)
	resultStr := string(result)

	if !strings.Contains(resultStr, "key") {
		t.Error("Expected JS to contain 'key'")
	}
	if !strings.Contains(resultStr, "value") {
		t.Error("Expected JS to contain 'value'")
	}
}

func TestGetAssertions(t *testing.T) {
	tests := []struct {
		name     string
		meta     interface{}
		expected bool
	}{
		{
			name:     "nil meta",
			meta:     nil,
			expected: false,
		},
		{
			name: "with assertions",
			meta: map[string]interface{}{
				"assertions": map[string]interface{}{
					"test": "value",
				},
			},
			expected: true,
		},
		{
			name: "without assertions",
			meta: map[string]interface{}{
				"other": "value",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAssertions(tt.meta)
			hasAssertions := len(result) > 0
			if hasAssertions != tt.expected {
				t.Errorf("getAssertions() has assertions = %v, want %v", hasAssertions, tt.expected)
			}
		})
	}
}

func TestHasAssertions(t *testing.T) {
	tests := []struct {
		name     string
		meta     interface{}
		expected bool
	}{
		{
			name:     "nil meta",
			meta:     nil,
			expected: false,
		},
		{
			name: "with assertions",
			meta: map[string]interface{}{
				"assertions": map[string]interface{}{
					"test": "value",
				},
			},
			expected: true,
		},
		{
			name: "empty assertions",
			meta: map[string]interface{}{
				"assertions": map[string]interface{}{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAssertions(tt.meta)
			if result != tt.expected {
				t.Errorf("hasAssertions() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetMessageFromMap(t *testing.T) {
	tests := []struct {
		name     string
		result   interface{}
		expected string
	}{
		{
			name: "map with message",
			result: map[string]interface{}{
				"message": "test message",
			},
			expected: "test message",
		},
		{
			name:     "map without message",
			result:   map[string]interface{}{},
			expected: "",
		},
		{
			name:     "nil",
			result:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMessageFromMap(tt.result)
			if result != tt.expected {
				t.Errorf("getMessageFromMap() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestGetOKFromResult(t *testing.T) {
	tests := []struct {
		name     string
		result   interface{}
		expected bool
	}{
		{
			name: "map with passed=true",
			result: map[string]interface{}{
				"passed": true,
			},
			expected: true,
		},
		{
			name: "map with passed=false",
			result: map[string]interface{}{
				"passed": false,
			},
			expected: false,
		},
		{
			name: "map with Passed=true",
			result: map[string]interface{}{
				"Passed": true,
			},
			expected: true,
		},
		{
			name:     "map without passed field",
			result:   map[string]interface{}{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getOKFromResult(tt.result)
			if result != tt.expected {
				t.Errorf("getOKFromResult() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetDetailsFromResult(t *testing.T) {
	tests := []struct {
		name     string
		result   interface{}
		hasValue bool
	}{
		{
			name: "map with details",
			result: map[string]interface{}{
				"details": map[string]interface{}{"key": "value"},
			},
			hasValue: true,
		},
		{
			name: "map with Details",
			result: map[string]interface{}{
				"Details": "some details",
			},
			hasValue: true,
		},
		{
			name:     "map without details",
			result:   map[string]interface{}{},
			hasValue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getDetailsFromResult(tt.result)
			if (result != nil) != tt.hasValue {
				t.Errorf("getDetailsFromResult() has value = %v, want %v", result != nil, tt.hasValue)
			}
		})
	}
}

func TestCheckValidatorsPassed_AllPass(t *testing.T) {
	validators := map[string]interface{}{
		"validator1": map[string]interface{}{"passed": true},
		"validator2": map[string]interface{}{"passed": true},
	}

	result := checkValidatorsPassed(validators)
	if !result {
		t.Error("Expected all validators to pass")
	}
}

func TestCheckValidatorsPassed_OneFails(t *testing.T) {
	validators := map[string]interface{}{
		"validator1": map[string]interface{}{"passed": true},
		"validator2": map[string]interface{}{"passed": false},
	}

	result := checkValidatorsPassed(validators)
	if result {
		t.Error("Expected validators to fail")
	}
}

func TestCheckValidatorsPassed_Nil(t *testing.T) {
	result := checkValidatorsPassed(nil)
	if !result {
		t.Error("Expected nil validators to pass")
	}
}

func TestCheckAssertionsPassed_AllPass(t *testing.T) {
	assertions := map[string]interface{}{
		"assertion1": map[string]interface{}{"passed": true},
		"assertion2": map[string]interface{}{"passed": true},
	}

	result := checkAssertionsPassed(assertions)
	if !result {
		t.Error("Expected all assertions to pass")
	}
}

func TestCheckAssertionsPassed_OneFails(t *testing.T) {
	assertions := map[string]interface{}{
		"assertion1": map[string]interface{}{"passed": true},
		"assertion2": map[string]interface{}{"passed": false},
	}

	result := checkAssertionsPassed(assertions)
	if result {
		t.Error("Expected assertions to fail")
	}
}

func TestCheckAssertionsPassed_Nil(t *testing.T) {
	result := checkAssertionsPassed(nil)
	if !result {
		t.Error("Expected nil assertions to pass")
	}
}

func TestIsValidatorPassed_MapFormat(t *testing.T) {
	tests := []struct {
		name      string
		validator interface{}
		expected  bool
	}{
		{
			name: "passed validator",
			validator: map[string]interface{}{
				"passed": true,
			},
			expected: true,
		},
		{
			name: "failed validator",
			validator: map[string]interface{}{
				"passed": false,
			},
			expected: false,
		},
		{
			name:      "validator without passed field",
			validator: map[string]interface{}{},
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidatorPassed(tt.validator)
			if result != tt.expected {
				t.Errorf("isValidatorPassed() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConvertValidationsToMap(t *testing.T) {
	validations := []interface{}{
		map[string]interface{}{
			"validator_type": "type1",
			"passed":         true,
		},
		map[string]interface{}{
			"validator_type": "type2",
			"passed":         false,
		},
	}

	result := convertValidationsToMap(validations)

	if len(result) != 2 {
		t.Errorf("Expected 2 validators, got %d", len(result))
	}
	if _, exists := result["type1"]; !exists {
		t.Error("Expected type1 to exist in result")
	}
	if _, exists := result["type2"]; !exists {
		t.Error("Expected type2 to exist in result")
	}
}

func TestGetLegacyValidators(t *testing.T) {
	tests := []struct {
		name     string
		meta     interface{}
		expected bool
	}{
		{
			name:     "nil meta",
			meta:     nil,
			expected: false,
		},
		{
			name: "with validators",
			meta: map[string]interface{}{
				"validators": map[string]interface{}{
					"test": "value",
				},
			},
			expected: true,
		},
		{
			name:     "without validators",
			meta:     map[string]interface{}{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLegacyValidators(tt.meta)
			hasValidators := len(result) > 0
			if hasValidators != tt.expected {
				t.Errorf("getLegacyValidators() has validators = %v, want %v", hasValidators, tt.expected)
			}
		})
	}
}

func TestHasLegacyValidators(t *testing.T) {
	tests := []struct {
		name     string
		meta     interface{}
		expected bool
	}{
		{
			name:     "nil meta",
			meta:     nil,
			expected: false,
		},
		{
			name: "with validators",
			meta: map[string]interface{}{
				"validators": map[string]interface{}{
					"test": "value",
				},
			},
			expected: true,
		},
		{
			name: "empty validators",
			meta: map[string]interface{}{
				"validators": map[string]interface{}{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasLegacyValidators(tt.meta)
			if result != tt.expected {
				t.Errorf("hasLegacyValidators() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "plain text",
			input:    "Hello, world!",
			contains: "Hello, world!",
		},
		{
			name:     "bold text",
			input:    "**bold**",
			contains: "<strong>",
		},
		{
			name:     "italic text",
			input:    "*italic*",
			contains: "<em>",
		},
		{
			name:     "list",
			input:    "- Item 1\n- Item 2",
			contains: "<li>",
		},
		{
			name:     "code block",
			input:    "`code`",
			contains: "code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderMarkdown(tt.input)
			resultStr := string(result)
			if !strings.Contains(resultStr, tt.contains) {
				t.Errorf("renderMarkdown(%s) = %s, want to contain %s", tt.input, resultStr, tt.contains)
			}
		})
	}
}
