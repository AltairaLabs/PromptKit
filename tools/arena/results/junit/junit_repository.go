// Package junit provides JUnit XML result output for Arena.
// This package implements the ResultRepository interface to generate
// JUnit XML format files that are natively supported by CI/CD systems
// like GitHub Actions, GitLab CI, Jenkins, and Azure DevOps.
package junit

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
)

// JUnitResultRepository writes results in JUnit XML format for CI integration
type JUnitResultRepository struct {
	outputPath string
	options    *JUnitOptions
}

// JUnitOptions allows customization of JUnit XML output
type JUnitOptions struct {
	// Include additional metadata in system-out
	IncludeSystemOut bool

	// Include error details in system-err
	IncludeSystemErr bool

	// Template for suite names (default: scenario ID)
	SuiteNameTemplate string

	// Template for test names (default: provider.region.runID)
	TestNameTemplate string

	// Include cost and token information
	IncludeMetrics bool
}

// DefaultJUnitOptions provides sensible defaults for JUnit output
func DefaultJUnitOptions() *JUnitOptions {
	return &JUnitOptions{
		IncludeSystemOut:  true,
		IncludeSystemErr:  false,
		SuiteNameTemplate: "{{.ScenarioID}}",
		TestNameTemplate:  "{{.ProviderID}}.{{.Region}}.{{.RunID}}",
		IncludeMetrics:    true,
	}
}

// NewJUnitResultRepository creates a new JUnit XML result repository
func NewJUnitResultRepository(outputPath string) *JUnitResultRepository {
	return NewJUnitResultRepositoryWithOptions(outputPath, DefaultJUnitOptions())
}

// NewJUnitResultRepositoryWithOptions creates a new JUnit repository with custom options
func NewJUnitResultRepositoryWithOptions(outputPath string, options *JUnitOptions) *JUnitResultRepository {
	return &JUnitResultRepository{
		outputPath: outputPath,
		options:    options,
	}
}

// GetOutputPath returns the output path for this repository
func (r *JUnitResultRepository) GetOutputPath() string {
	return r.outputPath
}

// SaveResults saves all results in JUnit XML format
func (r *JUnitResultRepository) SaveResults(runResults []engine.RunResult) error {
	// Validate inputs
	if err := results.ValidateResults(runResults); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Convert to JUnit format
	testSuites := r.convertToJUnit(runResults)

	// Create output directory
	if err := os.MkdirAll(filepath.Dir(r.outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write XML file
	return r.writeJUnitXML(testSuites)
}

// convertToJUnit converts Arena results to JUnit XML structure
func (r *JUnitResultRepository) convertToJUnit(runResults []engine.RunResult) *JUnitTestSuites {
	// Group results by scenario (test suite)
	suiteMap := make(map[string]*JUnitTestSuite)

	for i := range runResults {
		result := &runResults[i]
		suiteName := r.generateSuiteName(result)

		// Get or create suite
		suite, exists := suiteMap[suiteName]
		if !exists {
			suite = &JUnitTestSuite{
				Name:      suiteName,
				Tests:     0,
				Failures:  0,
				Errors:    0,
				Timestamp: result.StartTime.Format(time.RFC3339),
			}
			suiteMap[suiteName] = suite
		}

		// Create test case
		testCase := r.convertTestCase(result)
		suite.TestCases = append(suite.TestCases, testCase)
		suite.Tests++
		suite.Time += result.Duration.Seconds()

		// Count failures and errors
		if testCase.Failure != nil {
			suite.Failures++
		}
		if testCase.Error != nil {
			suite.Errors++
		}
	}

	// Convert map to slice and calculate totals
	var suites []*JUnitTestSuite
	totalTests := 0
	totalFailures := 0
	totalErrors := 0
	totalTime := 0.0

	for _, suite := range suiteMap {
		suites = append(suites, suite)
		totalTests += suite.Tests
		totalFailures += suite.Failures
		totalErrors += suite.Errors
		totalTime += suite.Time
	}

	return &JUnitTestSuites{
		Name:       "Arena Test Results",
		Tests:      totalTests,
		Failures:   totalFailures,
		Errors:     totalErrors,
		Time:       totalTime,
		TestSuites: suites,
	}
}

// generateSuiteName creates a suite name using the configured template
func (r *JUnitResultRepository) generateSuiteName(result *engine.RunResult) string {
	// For now, use ScenarioID as suite name
	// Future: implement template processing
	return result.ScenarioID
}

// generateTestName creates a test name using the configured template
func (r *JUnitResultRepository) generateTestName(result *engine.RunResult) string {
	// For now, use provider.region.runID format
	// Future: implement template processing
	return fmt.Sprintf("%s.%s.%s", result.ProviderID, result.Region, result.RunID)
}

// convertTestCase converts a single Arena result to a JUnit test case
func (r *JUnitResultRepository) convertTestCase(result *engine.RunResult) JUnitTestCase {
	testName := r.generateTestName(result)

	testCase := JUnitTestCase{
		Name:      testName,
		Classname: result.ScenarioID,
		Time:      result.Duration.Seconds(),
	}

	// Add metadata as system-out
	if r.options.IncludeSystemOut {
		metadata := r.buildMetadata(result)
		if metadata != "" {
			testCase.SystemOut = &JUnitOutput{Content: metadata}
		}
	}

	// Determine failure or error status
	if result.Error != "" {
		// Execution error
		testCase.Error = &JUnitError{
			Message: result.Error,
			Type:    "ExecutionError",
			Content: r.buildErrorDetails(result),
		}
	} else if len(result.Violations) > 0 {
		// Validation failures
		testCase.Failure = &JUnitFailure{
			Message: fmt.Sprintf("Validation failed: %d violation(s)", len(result.Violations)),
			Type:    "ValidationError",
			Content: r.buildValidationDetails(result.Violations),
		}
	}

	return testCase
}

// buildMetadata creates system-out content with Arena-specific metadata
func (r *JUnitResultRepository) buildMetadata(result *engine.RunResult) string {
	var metadata strings.Builder

	metadata.WriteString(fmt.Sprintf("Run ID: %s\n", result.RunID))
	metadata.WriteString(fmt.Sprintf("Provider: %s\n", result.ProviderID))
	metadata.WriteString(fmt.Sprintf("Region: %s\n", result.Region))
	metadata.WriteString(fmt.Sprintf("Duration: %s\n", result.Duration))

	// Add cost and token information if available and enabled
	if r.options.IncludeMetrics && result.Cost.TotalCost > 0 {
		metadata.WriteString(fmt.Sprintf("Cost: $%.6f\n", result.Cost.TotalCost))
		metadata.WriteString(fmt.Sprintf("Input Tokens: %d\n", result.Cost.InputTokens))
		metadata.WriteString(fmt.Sprintf("Output Tokens: %d\n", result.Cost.OutputTokens))
	}

	// Add tool statistics if available
	if r.options.IncludeMetrics && result.ToolStats != nil {
		metadata.WriteString(fmt.Sprintf("Tool Calls: %d\n", result.ToolStats.TotalCalls))
	}

	// Add self-play information if applicable
	if result.SelfPlay {
		metadata.WriteString("Self-Play: true\n")
		if result.PersonaID != "" {
			metadata.WriteString(fmt.Sprintf("Persona: %s\n", result.PersonaID))
		}
	}

	return metadata.String()
}

// buildErrorDetails creates detailed error information for system-err
func (r *JUnitResultRepository) buildErrorDetails(result *engine.RunResult) string {
	var details strings.Builder

	details.WriteString(fmt.Sprintf("Error: %s\n\n", result.Error))

	// Include last few messages for context
	if len(result.Messages) > 0 {
		details.WriteString("Recent conversation:\n")
		start := len(result.Messages) - 3
		if start < 0 {
			start = 0
		}
		for _, msg := range result.Messages[start:] {
			content := msg.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			details.WriteString(fmt.Sprintf("  [%s]: %s\n", msg.Role, content))
		}
	}

	return details.String()
}

// buildValidationDetails creates detailed validation failure information
func (r *JUnitResultRepository) buildValidationDetails(violations []ValidationError) string {
	var details strings.Builder

	for i, violation := range violations {
		details.WriteString(fmt.Sprintf("Violation %d:\n", i+1))
		details.WriteString(fmt.Sprintf("  Type: %s\n", violation.Type))
		if violation.Tool != "" {
			details.WriteString(fmt.Sprintf("  Tool: %s\n", violation.Tool))
		}
		details.WriteString(fmt.Sprintf("  Details: %s\n", violation.Detail))
		details.WriteString("\n")
	}

	return details.String()
}

// writeJUnitXML writes the JUnit XML to file
func (r *JUnitResultRepository) writeJUnitXML(testSuites *JUnitTestSuites) error {
	// Marshal to XML
	output, err := xml.MarshalIndent(testSuites, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JUnit XML: %w", err)
	}

	// Add XML header
	xmlContent := []byte(xml.Header + string(output))

	// Write file
	if err := os.WriteFile(r.outputPath, xmlContent, 0644); err != nil {
		return fmt.Errorf("failed to write JUnit file: %w", err)
	}

	return nil
}

// SaveSummary saves summary information (JUnit XML includes this in structure)
func (r *JUnitResultRepository) SaveSummary(summary *results.ResultSummary) error {
	// JUnit format includes summary in the XML structure itself
	// This is a no-op since SaveResults() handles the complete output
	return nil
}

// LoadResults is not supported by JUnit format
func (r *JUnitResultRepository) LoadResults() ([]engine.RunResult, error) {
	return nil, results.NewUnsupportedOperationError("LoadResults", "JUnit repository does not support loading results")
}

// SupportsStreaming returns false as JUnit XML requires all results before writing
func (r *JUnitResultRepository) SupportsStreaming() bool {
	return false
}

// SaveResult is not supported for JUnit (needs all results for proper XML structure)
func (r *JUnitResultRepository) SaveResult(result *engine.RunResult) error {
	return results.NewUnsupportedOperationError("SaveResult", "JUnit repository does not support streaming writes")
}
