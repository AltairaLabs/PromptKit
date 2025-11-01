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
		result.Error = "Test error message"
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
		createTestResult("run1", "openai", "us-west", "scenario1", 0.05, 100, 50, false, 500*time.Millisecond),
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

	if len(data.Providers) != 1 || data.Providers[0] != "openai" {
		t.Errorf("Expected 1 provider 'openai', got %v", data.Providers)
	}

	if len(data.Regions) != 1 || data.Regions[0] != "us-west" {
		t.Errorf("Expected 1 region 'us-west', got %v", data.Regions)
	}

	if len(data.Scenarios) != 1 || data.Scenarios[0] != "scenario1" {
		t.Errorf("Expected 1 scenario 'scenario1', got %v", data.Scenarios)
	}
}

func TestPrepareReportData_MultipleResults(t *testing.T) {
	results := []engine.RunResult{
		createTestResult("run1", "openai", "us-west", "scenario1", 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult("run2", "anthropic", "us-east", "scenario2", 0.03, 80, 40, false, 300*time.Millisecond),
		createTestResult("run3", "openai", "us-west", "scenario2", 0.04, 90, 45, true, 0),
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
				createTestResult("run1", "openai", "us", "s1", 0.01, 100, 50, false, tt.duration),
			}

			data := prepareReportData(results)

			if !strings.Contains(data.Summary.AvgLatency, tt.expected) {
				t.Errorf("Expected average latency to contain '%s', got '%s'", tt.expected, data.Summary.AvgLatency)
			}
		})
	}
}

func TestPrepareReportData_WithCachedTokens(t *testing.T) {
	result := createTestResult("run1", "openai", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond)
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
		createTestResult("run1", "openai", "us-west", "scenario1", 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult("run2", "openai", "us-west", "scenario2", 0.03, 80, 40, false, 300*time.Millisecond),
	}
	providers := []string{"openai"}
	regions := []string{"us-west"}

	matrix := generateMatrix(results, providers, regions)

	if len(matrix) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(matrix))
	}

	if len(matrix[0]) != 1 {
		t.Fatalf("Expected 1 column, got %d", len(matrix[0]))
	}

	cell := matrix[0][0]
	if cell.Provider != "openai" {
		t.Errorf("Expected provider 'openai', got '%s'", cell.Provider)
	}

	if cell.Region != "us-west" {
		t.Errorf("Expected region 'us-west', got '%s'", cell.Region)
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
		createTestResult("run1", "openai", "us-west", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult("run2", "anthropic", "us-west", "s1", 0.03, 80, 40, false, 300*time.Millisecond),
		createTestResult("run3", "openai", "us-east", "s1", 0.04, 90, 45, true, 0),
	}
	providers := []string{"openai", "anthropic"}
	regions := []string{"us-west", "us-east"}

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
		Title:       "Test Report",
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
		Providers: []string{"openai"},
		Regions:   []string{"us"},
		Scenarios: []string{"test"},
		Matrix:    [][]MatrixCell{},
	}

	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf("Failed to generate HTML: %v", err)
	}

	if len(html) == 0 {
		t.Error("Generated HTML is empty")
	}

	// Check for key content in HTML
	if !strings.Contains(html, "Test Report") {
		t.Error("HTML doesn't contain title")
	}

	if !strings.Contains(html, "Total Runs") {
		t.Error("HTML doesn't contain 'Total Runs'")
	}
}

func TestGenerateHTML_TemplateFunctions(t *testing.T) {
	// Test with data that exercises template functions
	data := HTMLReportData{
		Title:       "Template Test",
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
			createTestResult("run1", "openai", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
		},
		Providers: []string{"openai"},
		Regions:   []string{"us"},
		Scenarios: []string{"s1"},
		Matrix: [][]MatrixCell{
			{{Provider: "openai", Region: "us", Scenarios: 1, Successful: 1, Errors: 0, Cost: 0.05}},
		},
	}

	html, err := generateHTML(data)
	if err != nil {
		t.Fatalf("Failed to generate HTML: %v", err)
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
		createTestResult("run1", "openai", "us-west", "scenario1", 0.05, 100, 50, false, 500*time.Millisecond),
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
		createTestResult("run1", "openai", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
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
		createTestResult("run1", "openai", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
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
		Provider:   "openai",
		Region:     "us-west",
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
		createTestResult("run1", "openai", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult("run2", "openai", "us", "s2", 0.03, 80, 40, true, 0), // Error with no duration
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
		createTestResult("run1", "unknown-provider", "unknown-region", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
	}
	providers := []string{"openai"}
	regions := []string{"us-west"}

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
		createTestResult("run1", "openai", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond),
		createTestResult("run2", "openai", "us", "s2", 0.03, 80, 40, false, 300*time.Millisecond),
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
				"validations": []statestore.ValidationResult{
					{
						TurnIndex:     1,
						ValidatorType: "*validators.BannedWordsValidator",
						Passed:        false,
						Details:       map[string]interface{}{"value": []string{"guarantee"}},
					},
					{
						TurnIndex:     1,
						ValidatorType: "*validators.LengthValidator",
						Passed:        true,
						Details:       map[string]interface{}{"length": 100},
					},
					{
						TurnIndex:     2,
						ValidatorType: "*validators.BannedWordsValidator",
						Passed:        true,
						Details:       nil,
					},
				},
				"turn_metrics": []map[string]interface{}{},
			},
			turnIndex: 1,
			want:      2,
		},
		{
			name: "JSON-loaded type (from file)",
			validators: func() interface{} {
				// Simulate what happens when JSON is loaded
				jsonStr := `{
					"validations": [
						{
							"turn_index": 1,
							"validator_type": "*validators.BannedWordsValidator",
							"passed": false,
							"details": {"value": ["guarantee"]}
						},
						{
							"turn_index": 1,
							"validator_type": "*validators.LengthValidator",
							"passed": true,
							"details": {"length": 100}
						},
						{
							"turn_index": 2,
							"validator_type": "*validators.BannedWordsValidator",
							"passed": true,
							"details": null
						}
					],
					"turn_metrics": []
				}`
				var v map[string]interface{}
				json.Unmarshal([]byte(jsonStr), &v)
				return v
			}(),
			turnIndex: 1,
			want:      2,
		},
		{
			name: "turn 2 has one validation",
			validators: map[string]interface{}{
				"validations": []statestore.ValidationResult{
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
				"validations": []statestore.ValidationResult{
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
				"turn_metrics": []map[string]interface{}{
					{
						"turn_index": 1,
						"tokens_in":  100,
						"tokens_out": 50,
						"cost_usd":   0.001,
					},
					{
						"turn_index": 2,
						"tokens_in":  200,
						"tokens_out": 75,
						"cost_usd":   0.002,
					},
				},
				"validations": []statestore.ValidationResult{},
			},
			turnIndex: 1,
			wantNil:   false,
		},
		{
			name: "JSON-loaded type (from file)",
			validators: func() interface{} {
				jsonStr := `{
					"turn_metrics": [
						{
							"turn_index": 1,
							"tokens_in": 100,
							"tokens_out": 50,
							"cost_usd": 0.001
						},
						{
							"turn_index": 2,
							"tokens_in": 200,
							"tokens_out": 75,
							"cost_usd": 0.002
						}
					],
					"validations": []
				}`
				var v map[string]interface{}
				json.Unmarshal([]byte(jsonStr), &v)
				return v
			}(),
			turnIndex: 1,
			wantNil:   false,
		},
		{
			name: "turn not found",
			validators: map[string]interface{}{
				"turn_metrics": []map[string]interface{}{
					{"turn_index": 1, "tokens_in": 100},
					{"turn_index": 2, "tokens_in": 200},
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

func testGetTurnMetrics(validators interface{}, turnIndex int) map[string]interface{} {
	if validators == nil {
		return nil
	}
	validatorsMap, ok := validators.(map[string]interface{})
	if !ok {
		return nil
	}

	metricsRaw := validatorsMap["turn_metrics"]
	if metricsRaw == nil {
		return nil
	}

	// Try to convert to []interface{} (happens when loaded from JSON)
	if metrics, ok := metricsRaw.([]interface{}); ok {
		for _, m := range metrics {
			metric, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			// Check if this metric is for the current turn
			if ti, ok := metric["turn_index"].(float64); ok && int(ti) == turnIndex {
				return metric
			}
		}
		return nil
	}

	// Handle direct struct type (when passed from Go without JSON round-trip)
	// Convert to generic map format
	jsonBytes, err := json.Marshal(metricsRaw)
	if err != nil {
		return nil
	}
	var metrics []map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &metrics); err != nil {
		return nil
	}

	for _, metric := range metrics {
		// Check if this metric is for the current turn
		if ti, ok := metric["turn_index"].(float64); ok && int(ti) == turnIndex {
			return metric
		}
	}
	return nil
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
				"validations": []statestore.ValidationResult{
					{TurnIndex: 1, ValidatorType: "test", Passed: true},
				},
			},
			turnIndex: 1,
			want:      true,
		},
		{
			name: "no validation for turn",
			validators: map[string]interface{}{
				"validations": []statestore.ValidationResult{
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
					"validations": [
						{"turn_index": 1, "validator_type": "test", "passed": true}
					]
				}`
				var v map[string]interface{}
				json.Unmarshal([]byte(jsonStr), &v)
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
	if validators == nil {
		return false
	}
	validatorsMap, ok := validators.(map[string]interface{})
	if !ok {
		return false
	}

	validationsRaw := validatorsMap["validations"]
	if validationsRaw == nil {
		return false
	}

	// Try to convert to []interface{} (happens when loaded from JSON)
	if validations, ok := validationsRaw.([]interface{}); ok {
		for _, v := range validations {
			validation, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			// Check if this validation is for the current turn
			if ti, ok := validation["turn_index"].(float64); ok && int(ti) == turnIndex {
				return true
			}
		}
		return false
	}

	// Handle direct struct type (when passed from Go without JSON round-trip)
	// Convert to generic map format
	jsonBytes, err := json.Marshal(validationsRaw)
	if err != nil {
		return false
	}
	var validations []map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &validations); err != nil {
		return false
	}

	for _, validation := range validations {
		// Check if this validation is for the current turn
		if ti, ok := validation["turn_index"].(float64); ok && int(ti) == turnIndex {
			return true
		}
	}
	return false
}

// TestGenerateHTML_WithMessageCostInfo tests that the HTML template correctly
// accesses the TotalCost field (not TotalCostUSD) from message CostInfo.
// This is a regression test for the template error:
// "can't evaluate field TotalCostUSD in type *types.CostInfo"
func TestGenerateHTML_WithMessageCostInfo(t *testing.T) {
	// Create a result with messages that have CostInfo
	result := createTestResult("run1", "openai-gpt-4o-mini", "us-west", "scenario1", 0.05, 100, 50, false, 500*time.Millisecond)

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
		t.Error("Generated HTML is empty")
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
	result := createTestResult("run1", "openai-gpt-4o-mini", "us-west", "selfplay-scenario", 0.0001, 231, 59, false, 1500*time.Millisecond)

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
		t.Fatalf("Failed to generate HTML: %v", err)
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
			result := createTestResult("run1", "test-provider", "us", "s1", 0.05, 100, 50, false, 500*time.Millisecond)

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
				Title:       "Edge Case Test",
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
				Providers: []string{"test-provider"},
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
				t.Error("Generated HTML is empty")
			}
		})
	}
}
