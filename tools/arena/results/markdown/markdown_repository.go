// Package markdown provides Markdown file-based result storage for Arena.
// This package implements the ResultRepository interface to save Arena
// test results as Markdown formatted files, enabling seamless integration
// with CI/CD pipelines and GitHub workflows.
package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results"
)

const (
	// File names and error messages
	markdownFileName           = "results.md"
	errFailedToCreateOutputDir = "failed to create output directory: %w"
)

// MarkdownConfig holds configuration options for markdown output formatting
type MarkdownConfig struct {
	IncludeDetails    bool // Include detailed test information
	ShowOverview      bool // Show executive overview section
	ShowResultsMatrix bool // Show results matrix table
	ShowFailedTests   bool // Show failed tests section
	ShowCostSummary   bool // Show cost analysis section
}

// MarkdownResultRepository stores results as a Markdown file.
// This provides human-readable output suitable for CI/CD integration,
// GitHub PR comments, and documentation generation.
type MarkdownResultRepository struct {
	outputDir  string
	outputFile string
	config     *MarkdownConfig
}

// NewMarkdownResultRepository creates a new Markdown result repository that writes
// to the specified output directory with default configuration.
func NewMarkdownResultRepository(outputDir string) *MarkdownResultRepository {
	return NewMarkdownResultRepositoryWithConfig(outputDir, nil)
}

// NewMarkdownResultRepositoryWithFile creates a new Markdown result repository
// with a custom output file path and default configuration.
func NewMarkdownResultRepositoryWithFile(outputFile string) *MarkdownResultRepository {
	config := &MarkdownConfig{
		IncludeDetails:    true,
		ShowOverview:      true,
		ShowResultsMatrix: true,
		ShowFailedTests:   true,
		ShowCostSummary:   true,
	}

	return &MarkdownResultRepository{
		outputDir:  filepath.Dir(outputFile),
		outputFile: outputFile,
		config:     config,
	}
}

// NewMarkdownResultRepositoryWithConfig creates a new Markdown result repository
// with the specified output directory and configuration.
func NewMarkdownResultRepositoryWithConfig(outputDir string, config *MarkdownConfig) *MarkdownResultRepository {
	if config == nil {
		config = &MarkdownConfig{
			IncludeDetails:    true,
			ShowOverview:      true,
			ShowResultsMatrix: true,
			ShowFailedTests:   true,
			ShowCostSummary:   true,
		}
	}

	return &MarkdownResultRepository{
		outputDir:  outputDir,
		outputFile: filepath.Join(outputDir, markdownFileName),
		config:     config,
	}
}

// CreateMarkdownConfigFromDefaults creates a MarkdownConfig from arena defaults.
func CreateMarkdownConfigFromDefaults(defaults *config.Defaults) *MarkdownConfig {
	if defaults == nil {
		return createDefaultMarkdownConfig()
	}

	// Get the effective output configuration
	outputConfig := defaults.GetOutputConfig()
	markdownOutputConfig := outputConfig.GetMarkdownOutputConfig()

	return &MarkdownConfig{
		IncludeDetails:    markdownOutputConfig.IncludeDetails,
		ShowOverview:      markdownOutputConfig.ShowOverview,
		ShowResultsMatrix: markdownOutputConfig.ShowResultsMatrix,
		ShowFailedTests:   markdownOutputConfig.ShowFailedTests,
		ShowCostSummary:   markdownOutputConfig.ShowCostSummary,
	}
}

// createDefaultMarkdownConfig returns the default markdown configuration
func createDefaultMarkdownConfig() *MarkdownConfig {
	return &MarkdownConfig{
		IncludeDetails:    true,
		ShowOverview:      true,
		ShowResultsMatrix: true,
		ShowFailedTests:   true,
		ShowCostSummary:   true,
	}
} // SetIncludeDetails configures whether to include detailed test information.
// Deprecated: Use NewMarkdownResultRepositoryWithConfig instead.
func (r *MarkdownResultRepository) SetIncludeDetails(include bool) {
	r.config.IncludeDetails = include
}

// SetOutputFile sets a custom output file path for the markdown repository.
func (r *MarkdownResultRepository) SetOutputFile(outputFile string) {
	r.outputFile = outputFile
	r.outputDir = filepath.Dir(outputFile)
}

// GetOutputFile returns the output file path for this repository
func (r *MarkdownResultRepository) GetOutputFile() string {
	return r.outputFile
}

// SaveResults saves all results as a Markdown formatted file.
func (r *MarkdownResultRepository) SaveResults(runResults []engine.RunResult) error {
	// Validate inputs
	if err := results.ValidateResults(runResults); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Create output directory
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf(errFailedToCreateOutputDir, err)
	}

	// Generate markdown content
	markdownContent := r.generateMarkdownReport(runResults)

	// Write to file
	if err := os.WriteFile(r.outputFile, []byte(markdownContent), 0600); err != nil {
		return fmt.Errorf("failed to write markdown file %s: %w", r.outputFile, err)
	}

	return nil
}

// SaveSummary saves a summary of all test results
func (r *MarkdownResultRepository) SaveSummary(summary *results.ResultSummary) error {
	// For markdown, we integrate summary into the main report
	// This method can be used to update an existing report with summary info
	return nil
}

// generateMarkdownReport creates the complete markdown report
func (r *MarkdownResultRepository) generateMarkdownReport(runResults []engine.RunResult) string {
	if len(runResults) == 0 {
		return "# PromptArena Evaluation Results\n\n*No results to display*\n"
	}

	summary := r.calculateSummary(runResults)

	var content strings.Builder

	// Header with summary
	content.WriteString("# 🧪 PromptArena Test Results\n\n")

	// Overview section (configurable)
	if r.config.ShowOverview {
		r.writeOverviewSection(&content, summary)
	}

	// Results matrix (configurable)
	if r.config.ShowResultsMatrix {
		r.writeResultsMatrix(&content, runResults)
	}

	// Failed tests details (if configured and have failures)
	if r.config.ShowFailedTests && summary.Failed > 0 {
		r.writeFailedTestsSection(&content, runResults)
	}

	// Cost breakdown (if configured and have costs)
	if r.config.ShowCostSummary && summary.TotalCost > 0 {
		r.writeCostSection(&content, runResults, summary)
	}

	return content.String()
}

// LoadResults returns an error as markdown format doesn't support loading
func (r *MarkdownResultRepository) LoadResults() ([]engine.RunResult, error) {
	return nil, fmt.Errorf("markdown repository does not support loading results")
}

// SupportsStreaming returns false as markdown generates a complete report
func (r *MarkdownResultRepository) SupportsStreaming() bool {
	return false
}

// SaveResult returns an error as markdown doesn't support streaming
func (r *MarkdownResultRepository) SaveResult(result *engine.RunResult) error {
	return fmt.Errorf("markdown repository does not support streaming individual results")
}

// testSummary holds summary statistics for the markdown report
type testSummary struct {
	Total       int
	Passed      int
	Failed      int
	TotalCost   float64
	TotalTokens int
	Duration    time.Duration
	// Media statistics
	TotalImages      int
	TotalAudio       int
	TotalVideo       int
	MediaLoadSuccess int
	MediaLoadErrors  int
	TotalMediaSize   int64
}

// calculateSummary computes summary statistics from run results
func (r *MarkdownResultRepository) calculateSummary(runResults []engine.RunResult) testSummary {
	summary := testSummary{
		Total: len(runResults),
	}

	for _, result := range runResults {
		// Count passed/failed based on errors and assertion failures
		if result.Error != "" || len(result.Violations) > 0 || r.hasFailedAssertions(&result) {
			summary.Failed++
		} else {
			summary.Passed++
		}

		// Accumulate costs and tokens
		summary.TotalCost += result.Cost.TotalCost
		summary.TotalTokens += result.Cost.InputTokens + result.Cost.OutputTokens
		summary.Duration += result.Duration

		// Calculate media statistics
		r.addMediaStats(&summary, &result)
	}

	return summary
}

// addMediaStats adds media statistics from a result to the summary
func (r *MarkdownResultRepository) addMediaStats(summary *testSummary, result *engine.RunResult) {
	for i := range result.Messages {
		msg := &result.Messages[i]
		if len(msg.Parts) == 0 {
			continue
		}

		for j := range msg.Parts {
			part := &msg.Parts[j]
			if part.Media == nil {
				continue
			}
			r.processMediaPart(summary, part)
		}
	}
}

// processMediaPart processes a single media part and updates statistics
func (r *MarkdownResultRepository) processMediaPart(summary *testSummary, part *types.ContentPart) {
	// Count by type
	r.countMediaByType(summary, part.Type)

	// Count load status
	if r.mediaHasData(part.Media) {
		summary.MediaLoadSuccess++
		summary.TotalMediaSize += r.calculateMediaSize(part.Media)
	} else {
		summary.MediaLoadErrors++
	}
}

// countMediaByType increments the appropriate media type counter
func (r *MarkdownResultRepository) countMediaByType(summary *testSummary, contentType string) {
	switch contentType {
	case types.ContentTypeImage:
		summary.TotalImages++
	case types.ContentTypeAudio:
		summary.TotalAudio++
	case types.ContentTypeVideo:
		summary.TotalVideo++
	}
}

// mediaHasData checks if media has any data source
func (r *MarkdownResultRepository) mediaHasData(media *types.MediaContent) bool {
	return (media.Data != nil && *media.Data != "") ||
		(media.FilePath != nil && *media.FilePath != "") ||
		(media.URL != nil && *media.URL != "")
}

// calculateMediaSize calculates the size of inline media data
func (r *MarkdownResultRepository) calculateMediaSize(media *types.MediaContent) int64 {
	if media.Data != nil && *media.Data != "" {
		return int64(len(*media.Data))
	}
	return 0
}

// hasFailedAssertions checks if a result has any failed assertions
func (r *MarkdownResultRepository) hasFailedAssertions(result *engine.RunResult) bool {
	for _, msg := range result.Messages {
		if r.messageHasFailedAssertions(&msg) {
			return true
		}
	}
	return false
}

// messageHasFailedAssertions checks if a single message has failed assertions
func (r *MarkdownResultRepository) messageHasFailedAssertions(msg *types.Message) bool {
	if msg.Meta == nil {
		return false
	}

	assertions, ok := msg.Meta["assertions"]
	if !ok {
		return false
	}

	assertionMap, ok := assertions.(map[string]interface{})
	if !ok {
		return false
	}

	return r.hasFailedAssertionInMap(assertionMap)
}

// hasFailedAssertionInMap checks if any assertion in the map failed
func (r *MarkdownResultRepository) hasFailedAssertionInMap(assertionMap map[string]interface{}) bool {
	for _, assertion := range assertionMap {
		if r.isFailedAssertion(assertion) {
			return true
		}
	}
	return false
}

// isFailedAssertion checks if a single assertion failed
func (r *MarkdownResultRepository) isFailedAssertion(assertion interface{}) bool {
	assertionResult, ok := assertion.(map[string]interface{})
	if !ok {
		return false
	}

	passed, exists := assertionResult["passed"]
	if !exists {
		return false
	}

	passedBool, ok := passed.(bool)
	return ok && !passedBool
}

// writeOverviewSection writes the summary overview section
func (r *MarkdownResultRepository) writeOverviewSection(content *strings.Builder, summary testSummary) {
	content.WriteString("## 📊 Overview\n\n")
	content.WriteString("| Metric | Value |\n")
	content.WriteString("|--------|-------|\n")
	content.WriteString(fmt.Sprintf("| Tests Run | %d |\n", summary.Total))
	content.WriteString(fmt.Sprintf("| Passed | %d ✅ |\n", summary.Passed))
	content.WriteString(fmt.Sprintf("| Failed | %d ❌ |\n", summary.Failed))

	if summary.Total > 0 {
		successRate := float64(summary.Passed) / float64(summary.Total) * 100
		content.WriteString(fmt.Sprintf("| Success Rate | %.1f%% |\n", successRate))
	}

	if summary.TotalCost > 0 {
		content.WriteString(fmt.Sprintf("| Total Cost | $%.4f |\n", summary.TotalCost))
	}

	if summary.Duration > 0 {
		content.WriteString(fmt.Sprintf("| Total Duration | %s |\n", summary.Duration.String()))
	}

	// Add media statistics if any media content exists
	if summary.TotalImages > 0 || summary.TotalAudio > 0 || summary.TotalVideo > 0 {
		content.WriteString("\n### 🎨 Media Content\n\n")
		content.WriteString("| Type | Count |\n")
		content.WriteString("|------|-------|\n")

		if summary.TotalImages > 0 {
			content.WriteString(fmt.Sprintf("| 🖼️  Images | %d |\n", summary.TotalImages))
		}
		if summary.TotalAudio > 0 {
			content.WriteString(fmt.Sprintf("| 🎵 Audio Files | %d |\n", summary.TotalAudio))
		}
		if summary.TotalVideo > 0 {
			content.WriteString(fmt.Sprintf("| 🎬 Videos | %d |\n", summary.TotalVideo))
		}

		content.WriteString(fmt.Sprintf("| ✅ Loaded | %d |\n", summary.MediaLoadSuccess))
		if summary.MediaLoadErrors > 0 {
			content.WriteString(fmt.Sprintf("| ❌ Errors | %d |\n", summary.MediaLoadErrors))
		}
		if summary.TotalMediaSize > 0 {
			content.WriteString(fmt.Sprintf("| 💾 Total Size | %s |\n", r.formatBytes(summary.TotalMediaSize)))
		}
	}

	content.WriteString("\n")
}

// writeResultsMatrix writes the detailed results matrix
func (r *MarkdownResultRepository) writeResultsMatrix(content *strings.Builder, runResults []engine.RunResult) {
	content.WriteString("## 🔍 Test Results\n\n")
	content.WriteString("| Provider | Scenario | Region | Status | Duration | Guardrails | Assertions | Tools | Cost |\n")
	content.WriteString("|----------|----------|--------|---------|-----------|------------|------------|-------|------|\n")

	for _, result := range runResults {
		r.writeResultRow(content, &result)
	}

	content.WriteString("\n")
}

// writeResultRow writes a single result row in the matrix
func (r *MarkdownResultRepository) writeResultRow(content *strings.Builder, result *engine.RunResult) {
	// Determine status
	status := "✅ Pass"
	if result.Error != "" || len(result.Violations) > 0 || r.hasFailedAssertions(result) {
		status = "❌ Fail"
	}

	// Check for guardrails (violations indicate guardrails were present)
	hasGuardrails := "❌"
	if len(result.Violations) > 0 {
		hasGuardrails = "✅"
	} else {
		// Check if any prompt config had validators - we can infer from scenario/provider setup
		// For now, mark as unknown
		hasGuardrails = "-"
	}

	// Check for assertions
	hasAssertions := "-"
	assertionCount := r.countAssertions(result)
	if assertionCount > 0 {
		hasAssertions = fmt.Sprintf("✅ (%d)", assertionCount)
	}

	// Check for tool usage
	toolsUsed := "-"
	if result.ToolStats != nil && result.ToolStats.TotalCalls > 0 {
		toolsUsed = fmt.Sprintf("✅ (%d calls)", result.ToolStats.TotalCalls)
	}

	// Format cost
	cost := "-"
	if result.Cost.TotalCost > 0 {
		cost = fmt.Sprintf("$%.4f", result.Cost.TotalCost)
	}

	// Format duration
	duration := result.Duration.Truncate(time.Millisecond).String()

	content.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
		result.ProviderID,
		result.ScenarioID,
		result.Region,
		status,
		duration,
		hasGuardrails,
		hasAssertions,
		toolsUsed,
		cost,
	))
}

// countAssertions counts the number of assertions configured for a result
func (r *MarkdownResultRepository) countAssertions(result *engine.RunResult) int {
	count := 0
	for _, msg := range result.Messages {
		if msg.Meta != nil {
			if assertions, ok := msg.Meta["assertions"]; ok {
				if assertionMap, ok := assertions.(map[string]interface{}); ok {
					count += len(assertionMap)
				}
			}
		}
	}
	return count
}

// writeFailedTestsSection writes detailed information about failed tests
func (r *MarkdownResultRepository) writeFailedTestsSection(content *strings.Builder, runResults []engine.RunResult) {
	content.WriteString("## 🔍 Failed Tests\n\n")

	for _, result := range runResults {
		if result.Error != "" || len(result.Violations) > 0 || r.hasFailedAssertions(&result) {
			r.writeFailedTestDetail(content, &result)
		}
	}
}

// writeFailedTestDetail writes details for a single failed test
func (r *MarkdownResultRepository) writeFailedTestDetail(content *strings.Builder, result *engine.RunResult) {
	content.WriteString(fmt.Sprintf("### ❌ %s → %s (%s)\n\n", result.ScenarioID, result.ProviderID, result.Region))

	// Basic info
	content.WriteString(fmt.Sprintf("- **Duration**: %s\n", result.Duration.String()))
	if result.Cost.TotalCost > 0 {
		content.WriteString(fmt.Sprintf("- **Cost**: $%.4f\n", result.Cost.TotalCost))
	}

	// Error details
	if result.Error != "" {
		content.WriteString(fmt.Sprintf("- **Error**: %s\n", result.Error))
	}

	// Guardrail violations
	if len(result.Violations) > 0 {
		content.WriteString(fmt.Sprintf("- **Guardrail Violations**: %d\n", len(result.Violations)))
		for _, violation := range result.Violations {
			content.WriteString(fmt.Sprintf("  - `%s`: %s\n", violation.Type, violation.Detail))
		}
	}

	// Assertion failures
	r.writeAssertionFailures(content, result)

	// Tool usage
	if result.ToolStats != nil && result.ToolStats.TotalCalls > 0 {
		content.WriteString(fmt.Sprintf("- **Tools Used**: %d total calls\n", result.ToolStats.TotalCalls))
		for toolName, count := range result.ToolStats.ByTool {
			content.WriteString(fmt.Sprintf("  - `%s`: %d calls\n", toolName, count))
		}
	}

	content.WriteString("\n")
}

// writeAssertionFailures writes assertion failure details
func (r *MarkdownResultRepository) writeAssertionFailures(content *strings.Builder, result *engine.RunResult) {
	failures := r.collectAssertionFailures(result)

	if len(failures) == 0 {
		return
	}

	content.WriteString("- **Assertion Failures**:\n")
	for _, failure := range failures {
		r.writeAssertionFailure(content, failure)
	}
}

// assertionFailure represents a single assertion failure
type assertionFailure struct {
	Type    string
	Message string
	Details string
}

// collectAssertionFailures gathers all assertion failures from a result
func (r *MarkdownResultRepository) collectAssertionFailures(result *engine.RunResult) []assertionFailure {
	var failures []assertionFailure

	for _, msg := range result.Messages {
		msgFailures := r.extractMessageAssertionFailures(&msg)
		failures = append(failures, msgFailures...)
	}

	return failures
}

// extractMessageAssertionFailures extracts failures from a single message
func (r *MarkdownResultRepository) extractMessageAssertionFailures(msg *types.Message) []assertionFailure {
	var failures []assertionFailure

	if msg.Meta == nil {
		return failures
	}

	assertions, ok := msg.Meta["assertions"]
	if !ok {
		return failures
	}

	assertionMap, ok := assertions.(map[string]interface{})
	if !ok {
		return failures
	}

	for assertionType, assertion := range assertionMap {
		if failure := r.extractSingleAssertionFailure(assertionType, assertion); failure != nil {
			failures = append(failures, *failure)
		}
	}

	return failures
}

// extractSingleAssertionFailure extracts a failure from a single assertion
func (r *MarkdownResultRepository) extractSingleAssertionFailure(assertionType string, assertion interface{}) *assertionFailure {
	assertionResult, ok := assertion.(map[string]interface{})
	if !ok {
		return nil
	}

	passed, exists := assertionResult["passed"]
	if !exists {
		return nil
	}

	passedBool, ok := passed.(bool)
	if !ok || passedBool {
		return nil
	}

	// Extract message and details
	message := r.extractStringFromMap(assertionResult, "message")
	details := r.extractDetailsFromMap(assertionResult, "details")

	return &assertionFailure{
		Type:    assertionType,
		Message: message,
		Details: details,
	}
}

// writeAssertionFailure writes a single assertion failure
func (r *MarkdownResultRepository) writeAssertionFailure(content *strings.Builder, failure assertionFailure) {
	content.WriteString(fmt.Sprintf("  - `%s`: %s", failure.Type, failure.Message))
	if failure.Details != "" && failure.Details != "<nil>" {
		content.WriteString(fmt.Sprintf(" (%s)", failure.Details))
	}
	content.WriteString("\n")
}

// extractStringFromMap safely extracts a string value from a map
func (r *MarkdownResultRepository) extractStringFromMap(m map[string]interface{}, key string) string {
	if value, exists := m[key]; exists {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

// extractDetailsFromMap safely extracts details from a map and formats them
func (r *MarkdownResultRepository) extractDetailsFromMap(m map[string]interface{}, key string) string {
	if value, exists := m[key]; exists {
		return fmt.Sprintf("%v", value)
	}
	return ""
}

// writeCostSection writes the cost breakdown section
func (r *MarkdownResultRepository) writeCostSection(content *strings.Builder, runResults []engine.RunResult, summary testSummary) {
	content.WriteString("## 💰 Cost Breakdown\n\n")

	// Group by provider
	providerCosts := make(map[string]float64)
	providerCounts := make(map[string]int)

	for _, result := range runResults {
		providerCosts[result.ProviderID] += result.Cost.TotalCost
		providerCounts[result.ProviderID]++
	}

	content.WriteString("| Provider | Total Cost | Runs | Avg Cost |\n")
	content.WriteString("|----------|------------|------|----------|\n")

	for provider, totalCost := range providerCosts {
		avgCost := totalCost / float64(providerCounts[provider])
		content.WriteString(fmt.Sprintf("| %s | $%.4f | %d | $%.4f |\n",
			provider, totalCost, providerCounts[provider], avgCost))
	}

	content.WriteString("\n")
}

// formatBytes formats a byte count as a human-readable string
func (r *MarkdownResultRepository) formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
