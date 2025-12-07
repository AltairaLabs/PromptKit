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

	"github.com/AltairaLabs/PromptKit/runtime/types"
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

	// Calculate media stats for all results
	mediaStats := calculateMediaStats(runResults)
	mediaProperties := renderMediaProperties(mediaStats)

	for _, suite := range suiteMap {
		// Add media properties to each suite
		if len(mediaProperties) > 0 {
			suite.Properties = mediaProperties
		}

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

	// Add MediaOutputs as properties if present
	if len(result.MediaOutputs) > 0 {
		testCase.Properties = r.buildMediaOutputProperties(result.MediaOutputs)
	}

	// Add metadata as system-out
	r.addSystemOut(&testCase, result)

	// Determine failure or error status
	r.addErrorOrFailure(&testCase, result)

	// Add conversation-level assertions as properties and potential failure
	r.addConversationAssertions(&testCase, result)

	return testCase
}

// addSystemOut adds system-out metadata to the test case if configured
func (r *JUnitResultRepository) addSystemOut(testCase *JUnitTestCase, result *engine.RunResult) {
	if !r.options.IncludeSystemOut {
		return
	}
	metadata := r.buildMetadata(result)
	if metadata != "" {
		testCase.SystemOut = &JUnitOutput{Content: metadata}
	}
}

// addErrorOrFailure determines and adds error or failure status to the test case
func (r *JUnitResultRepository) addErrorOrFailure(testCase *JUnitTestCase, result *engine.RunResult) {
	if result.Error != "" {
		// Execution error
		testCase.Error = &JUnitError{
			Message: result.Error,
			Type:    "ExecutionError",
			Content: r.buildErrorDetails(result),
		}
		return
	}

	if len(result.Violations) > 0 {
		// Only report violations as failures if assertions didn't expect them
		// - No assertions = violations are unexpected = failure
		// - Some assertions fail = failure
		// - All assertions pass = violations were expected = not a failure
		if !results.HasAssertions(result) || !results.AllAssertionsPassed(result) {
			testCase.Failure = &JUnitFailure{
				Message: fmt.Sprintf("Validation failed: %d violation(s)", len(result.Violations)),
				Type:    "ValidationError",
				Content: r.buildValidationDetails(result.Violations),
			}
		}
	}
}

// addConversationAssertions adds conversation-level assertions as properties and potential failure
func (r *JUnitResultRepository) addConversationAssertions(testCase *JUnitTestCase, result *engine.RunResult) {
	if result.ConversationAssertions.Total == 0 {
		return
	}

	// Summary properties
	passVal := "true"
	if !result.ConversationAssertions.Passed {
		passVal = "false"
	}
	props := []JUnitProperty{
		{Name: "conversation_assertions.total", Value: fmt.Sprintf("%d", result.ConversationAssertions.Total)},
		{Name: "conversation_assertions.failed", Value: fmt.Sprintf("%d", result.ConversationAssertions.Failed)},
		{Name: "conversation_assertions.passed", Value: passVal},
	}
	testCase.Properties = append(testCase.Properties, props...)

	// If any failed, attach a failure with messages (only if no other failure/error exists)
	if !result.ConversationAssertions.Passed && testCase.Failure == nil && testCase.Error == nil {
		testCase.Failure = r.buildConversationAssertionFailure(result)
	}
}

// buildConversationAssertionFailure creates a JUnit failure for failed conversation assertions
func (r *JUnitResultRepository) buildConversationAssertionFailure(result *engine.RunResult) *JUnitFailure {
	var details strings.Builder
	details.WriteString("Conversation assertions failed:\n")
	for i := range result.ConversationAssertions.Results {
		res := result.ConversationAssertions.Results[i]
		if !res.Passed {
			details.WriteString(fmt.Sprintf("  - %s", res.Message))
			if len(res.Details) > 0 {
				if msg := formatConversationAssertionDetails(res.Details); msg != "" {
					details.WriteString(fmt.Sprintf(" (%s)", msg))
				}
			}
			details.WriteString("\n")
		}
	}
	return &JUnitFailure{
		Message: "Conversation assertions failed",
		Type:    "ConversationAssertionFailure",
		Content: details.String(),
	}
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

const (
	truncateReasonLimit = 120
	truncateMinChars    = 3
)

func formatConversationAssertionDetails(details map[string]interface{}) string {
	if len(details) == 0 {
		return ""
	}
	var parts []string
	if score, ok := details["score"]; ok {
		parts = append(parts, fmt.Sprintf("score=%v", score))
	}
	if reasoning, ok := details["reasoning"].(string); ok && reasoning != "" {
		parts = append(parts, truncateReasoning(reasoning, truncateReasonLimit))
	}
	return strings.Join(parts, " · ")
}

func truncateReasoning(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if limit <= truncateMinChars {
		return s[:limit]
	}
	return s[:limit-truncateMinChars] + "..."
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

// buildMediaOutputProperties creates JUnit properties for MediaOutputs
func (r *JUnitResultRepository) buildMediaOutputProperties(mediaOutputs []engine.MediaOutput) []JUnitProperty {
	var properties []JUnitProperty

	// Add aggregate statistics
	properties = append(properties, r.buildMediaOutputSummaryProperties(mediaOutputs)...)

	// Add individual media output details
	properties = append(properties, r.buildIndividualMediaOutputProperties(mediaOutputs)...)

	return properties
}

// buildMediaOutputSummaryProperties creates summary properties for all media outputs
func (r *JUnitResultRepository) buildMediaOutputSummaryProperties(mediaOutputs []engine.MediaOutput) []JUnitProperty {
	var properties []JUnitProperty

	// Count media outputs by type
	imageCount := 0
	audioCount := 0
	videoCount := 0
	var totalSize int64

	for i := range mediaOutputs {
		output := &mediaOutputs[i]
		switch output.Type {
		case "image":
			imageCount++
		case "audio":
			audioCount++
		case "video":
			videoCount++
		}
		totalSize += output.SizeBytes
	}

	// Add properties for media output counts
	properties = append(properties, JUnitProperty{
		Name:  "media_outputs.total",
		Value: fmt.Sprintf("%d", len(mediaOutputs)),
	})

	if imageCount > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media_outputs.images",
			Value: fmt.Sprintf("%d", imageCount),
		})
	}

	if audioCount > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media_outputs.audio",
			Value: fmt.Sprintf("%d", audioCount),
		})
	}

	if videoCount > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media_outputs.video",
			Value: fmt.Sprintf("%d", videoCount),
		})
	}

	if totalSize > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media_outputs.total_size_bytes",
			Value: fmt.Sprintf("%d", totalSize),
		})
	}

	return properties
}

// buildIndividualMediaOutputProperties creates properties for each media output
func (r *JUnitResultRepository) buildIndividualMediaOutputProperties(mediaOutputs []engine.MediaOutput) []JUnitProperty {
	var properties []JUnitProperty

	for i := range mediaOutputs {
		output := &mediaOutputs[i]
		prefix := fmt.Sprintf("media_outputs.%d", i)

		properties = append(properties, JUnitProperty{
			Name:  fmt.Sprintf("%s.type", prefix),
			Value: output.Type,
		})

		properties = append(properties, JUnitProperty{
			Name:  fmt.Sprintf("%s.mime_type", prefix),
			Value: output.MIMEType,
		})

		if output.SizeBytes > 0 {
			properties = append(properties, JUnitProperty{
				Name:  fmt.Sprintf("%s.size_bytes", prefix),
				Value: fmt.Sprintf("%d", output.SizeBytes),
			})
		}

		if output.Duration != nil && *output.Duration > 0 {
			properties = append(properties, JUnitProperty{
				Name:  fmt.Sprintf("%s.duration_seconds", prefix),
				Value: fmt.Sprintf("%d", *output.Duration),
			})
		}

		if output.Width != nil && output.Height != nil && *output.Width > 0 && *output.Height > 0 {
			properties = append(properties, JUnitProperty{
				Name:  fmt.Sprintf("%s.dimensions", prefix),
				Value: fmt.Sprintf("%dx%d", *output.Width, *output.Height),
			})
		}

		if output.FilePath != "" {
			properties = append(properties, JUnitProperty{
				Name:  fmt.Sprintf("%s.file_path", prefix),
				Value: output.FilePath,
			})
		}

		properties = append(properties, JUnitProperty{
			Name:  fmt.Sprintf("%s.message_index", prefix),
			Value: fmt.Sprintf("%d", output.MessageIdx),
		})
	}

	return properties
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

// MediaStats holds aggregated media statistics for a test suite
type MediaStats struct {
	TotalImages      int
	TotalAudio       int
	TotalVideo       int
	MediaLoadSuccess int
	MediaLoadErrors  int
	TotalMediaSize   int64
}

// calculateMediaStats aggregates media statistics from all messages in results
func calculateMediaStats(runResults []engine.RunResult) MediaStats {
	stats := MediaStats{}

	for i := range runResults {
		result := &runResults[i]
		for j := range result.Messages {
			msg := &result.Messages[j]
			if len(msg.Parts) == 0 {
				continue
			}

			for k := range msg.Parts {
				part := &msg.Parts[k]
				if part.Media == nil {
					continue
				}
				processMediaPartStats(&stats, part)
			}
		}
	}

	return stats
}

// processMediaPartStats processes a single media part and updates statistics
func processMediaPartStats(stats *MediaStats, part *types.ContentPart) {
	// Count by type
	countMediaByType(stats, part.Type)

	// Count load status
	if mediaHasData(part.Media) {
		stats.MediaLoadSuccess++
		stats.TotalMediaSize += calculateMediaSize(part.Media)
	} else {
		stats.MediaLoadErrors++
	}
}

// countMediaByType increments the appropriate media type counter
func countMediaByType(stats *MediaStats, contentType string) {
	switch contentType {
	case types.ContentTypeImage:
		stats.TotalImages++
	case types.ContentTypeAudio:
		stats.TotalAudio++
	case types.ContentTypeVideo:
		stats.TotalVideo++
	}
}

// mediaHasData checks if media has any data source
func mediaHasData(media *types.MediaContent) bool {
	return (media.Data != nil && *media.Data != "") ||
		(media.FilePath != nil && *media.FilePath != "") ||
		(media.URL != nil && *media.URL != "")
}

// calculateMediaSize calculates the size of inline media data
func calculateMediaSize(media *types.MediaContent) int64 {
	if media.Data != nil && *media.Data != "" {
		return int64(len(*media.Data))
	}
	return 0
}

// renderMediaProperties creates JUnit properties for media statistics
func renderMediaProperties(stats MediaStats) []JUnitProperty {
	var properties []JUnitProperty

	if stats.TotalImages > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media.images.total",
			Value: fmt.Sprintf("%d", stats.TotalImages),
		})
	}

	if stats.TotalAudio > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media.audio.total",
			Value: fmt.Sprintf("%d", stats.TotalAudio),
		})
	}

	if stats.TotalVideo > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media.video.total",
			Value: fmt.Sprintf("%d", stats.TotalVideo),
		})
	}

	if stats.MediaLoadSuccess > 0 || stats.MediaLoadErrors > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media.loaded.success",
			Value: fmt.Sprintf("%d", stats.MediaLoadSuccess),
		})
	}

	if stats.MediaLoadErrors > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media.loaded.errors",
			Value: fmt.Sprintf("%d", stats.MediaLoadErrors),
		})
	}

	if stats.TotalMediaSize > 0 {
		properties = append(properties, JUnitProperty{
			Name:  "media.size.total_bytes",
			Value: fmt.Sprintf("%d", stats.TotalMediaSize),
		})
	}

	return properties
}
