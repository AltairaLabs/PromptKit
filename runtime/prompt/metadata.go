package prompt

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MetadataBuilder helps construct pack format metadata from prompt configs and test results
type MetadataBuilder struct {
	spec *PromptSpec
}

// NewMetadataBuilder creates a new metadata builder for a prompt spec
func NewMetadataBuilder(spec *PromptSpec) *MetadataBuilder {
	return &MetadataBuilder{spec: spec}
}

// BuildPromptMetadata generates PromptMetadata from test execution results
func (mb *MetadataBuilder) BuildPromptMetadata(domain, language string, tags []string, testResults []TestResultSummary) *PromptMetadata {
	metadata := &PromptMetadata{
		Domain:   domain,
		Language: language,
		Tags:     tags,
	}

	// Calculate cost estimate from test results
	if len(testResults) > 0 {
		var minCost, maxCost, totalCost float64
		minCost = testResults[0].Cost
		maxCost = testResults[0].Cost

		for _, result := range testResults {
			totalCost += result.Cost
			if result.Cost < minCost {
				minCost = result.Cost
			}
			if result.Cost > maxCost {
				maxCost = result.Cost
			}
		}

		metadata.CostEstimate = &CostEstimate{
			MinCostUSD: minCost,
			MaxCostUSD: maxCost,
			AvgCostUSD: totalCost / float64(len(testResults)),
		}

		// Calculate performance metrics
		var totalLatency int
		var totalTokens int
		var successCount int

		for _, result := range testResults {
			totalLatency += result.LatencyMs
			totalTokens += result.Tokens
			if result.Success {
				successCount++
			}
		}

		metadata.Performance = &PerformanceMetrics{
			AvgLatencyMs: totalLatency / len(testResults),
			P95LatencyMs: calculateP95Latency(testResults),
			AvgTokens:    totalTokens / len(testResults),
			SuccessRate:  float64(successCount) / float64(len(testResults)),
		}
	}

	return metadata
}

// BuildCompilationInfo generates compilation metadata
func (mb *MetadataBuilder) BuildCompilationInfo(compilerVersion string) *CompilationInfo {
	return &CompilationInfo{
		CompiledWith: compilerVersion,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Schema:       "v1",
	}
}

// GetDefaultPipelineConfig returns the default Arena pipeline configuration
// Returns as map to avoid import cycle with pipeline package
func GetDefaultPipelineConfig() map[string]interface{} {
	return map[string]interface{}{
		"stages": []string{"template", "provider", "validator"},
		"middleware": []map[string]interface{}{
			{
				"type": "template",
				"config": map[string]interface{}{
					"strict_mode":     false,
					"allow_undefined": true,
				},
			},
			{
				"type": "provider",
				"config": map[string]interface{}{
					"retry_policy": map[string]interface{}{
						"max_retries":      3,
						"backoff":          "exponential",
						"initial_delay_ms": 100,
					},
					"timeout_ms": 30000,
				},
			},
			{
				"type": "validator",
				"config": map[string]interface{}{
					"fail_fast":          false,
					"collect_all_errors": true,
				},
			},
		},
	}
}

// TestResultSummary contains summarized test execution data
type TestResultSummary struct {
	Success   bool
	Cost      float64
	LatencyMs int
	Tokens    int
}

// AggregateTestResults computes ModelTestResultRef from test execution summaries
func AggregateTestResults(results []TestResultSummary, provider, model string) *ModelTestResultRef {
	if len(results) == 0 {
		return nil
	}

	var totalTokens int
	var totalCost float64
	var totalLatency int
	var successCount int

	for _, result := range results {
		if result.Success {
			successCount++
		}
		totalTokens += result.Tokens
		totalCost += result.Cost
		totalLatency += result.LatencyMs
	}

	n := len(results)
	return &ModelTestResultRef{
		Provider:     provider,
		Model:        model,
		Date:         time.Now().Format("2006-01-02"),
		SuccessRate:  float64(successCount) / float64(n),
		AvgTokens:    totalTokens / n,
		AvgCost:      totalCost / float64(n),
		AvgLatencyMs: totalLatency / n,
	}
}

// ConvertFromEngineResults converts engine RunResults to TestResultSummary
// This is a helper to bridge between engine execution and metadata generation
func ConvertFromEngineResults(engineResults []interface{}) []TestResultSummary {
	// This will be implemented when we integrate with the engine
	// For now, return empty slice
	return []TestResultSummary{}
}

// calculateP95Latency computes the 95th percentile latency
func calculateP95Latency(results []TestResultSummary) int {
	if len(results) == 0 {
		return 0
	}

	// Extract latencies
	latencies := make([]int, len(results))
	for i, result := range results {
		latencies[i] = result.LatencyMs
	}

	// Sort latencies (simple bubble sort for small datasets)
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[i] > latencies[j] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	// Calculate P95 index
	idx := int(float64(len(latencies)) * 0.95)
	if idx >= len(latencies) {
		idx = len(latencies) - 1
	}

	return latencies[idx]
}

// AddChangelogEntry adds a new entry to the prompt's changelog
func (mb *MetadataBuilder) AddChangelogEntry(version, author, description string) {
	if mb.spec.Metadata == nil {
		mb.spec.Metadata = &PromptMetadata{}
	}

	entry := ChangelogEntry{
		Version:     version,
		Date:        time.Now().Format("2006-01-02"),
		Author:      author,
		Description: description,
	}

	mb.spec.Metadata.Changelog = append(mb.spec.Metadata.Changelog, entry)
}

// SetDomain sets the domain for the prompt metadata
func (mb *MetadataBuilder) SetDomain(domain string) {
	if mb.spec.Metadata == nil {
		mb.spec.Metadata = &PromptMetadata{}
	}
	mb.spec.Metadata.Domain = domain
}

// SetLanguage sets the language for the prompt metadata
func (mb *MetadataBuilder) SetLanguage(language string) {
	if mb.spec.Metadata == nil {
		mb.spec.Metadata = &PromptMetadata{}
	}
	mb.spec.Metadata.Language = language
}

// SetTags sets the tags for the prompt metadata
func (mb *MetadataBuilder) SetTags(tags []string) {
	if mb.spec.Metadata == nil {
		mb.spec.Metadata = &PromptMetadata{}
	}
	mb.spec.Metadata.Tags = tags
}

// ExtractVariablesFromTemplate analyzes a template string and extracts variable names
// This helps auto-generate variable metadata when not explicitly specified
func ExtractVariablesFromTemplate(template string) []string {
	vars := []string{}
	varMap := make(map[string]bool)

	i := 0
	for i < len(template)-1 {
		if isOpeningBrace(template, i) {
			varName, nextPos := extractVariable(template, i)
			if varName != "" && !varMap[varName] {
				varMap[varName] = true
				vars = append(vars, varName)
			}
			i = nextPos
		} else {
			i++
		}
	}

	return vars
}

func isOpeningBrace(template string, pos int) bool {
	return template[pos] == '{' && template[pos+1] == '{'
}

func extractVariable(template string, startPos int) (varName string, nextPos int) {
	closingPos := findClosingBrace(template, startPos+2)
	if closingPos == -1 {
		return "", startPos + 1
	}

	varName = template[startPos+2 : closingPos]
	return varName, closingPos + 1
}

func findClosingBrace(template string, startPos int) int {
	for j := startPos; j < len(template)-1; j++ {
		if template[j] == '}' && template[j+1] == '}' {
			return j
		}
	}
	return -1
}

// ValidateMetadata checks that metadata fields are properly populated
func (mb *MetadataBuilder) ValidateMetadata() []string {
	warnings := []string{}

	if mb.spec.Metadata == nil {
		warnings = append(warnings, "no metadata defined")
		return warnings
	}

	if mb.spec.Metadata.Domain == "" {
		warnings = append(warnings, "metadata.domain not set")
	}
	if mb.spec.Metadata.Language == "" {
		warnings = append(warnings, "metadata.language not set")
	}
	if len(mb.spec.Metadata.Tags) == 0 {
		warnings = append(warnings, "metadata.tags is empty")
	}

	return warnings
}

// UpdateFromCostInfo updates cost estimate from types.CostInfo
func (mb *MetadataBuilder) UpdateFromCostInfo(costs []types.CostInfo) {
	if mb.spec.Metadata == nil {
		mb.spec.Metadata = &PromptMetadata{}
	}

	if len(costs) == 0 {
		return
	}

	var minCost, maxCost, totalCost float64
	minCost = costs[0].InputCostUSD + costs[0].OutputCostUSD
	maxCost = minCost

	for _, cost := range costs {
		total := cost.InputCostUSD + cost.OutputCostUSD
		totalCost += total
		if total < minCost {
			minCost = total
		}
		if total > maxCost {
			maxCost = total
		}
	}

	mb.spec.Metadata.CostEstimate = &CostEstimate{
		MinCostUSD: minCost,
		MaxCostUSD: maxCost,
		AvgCostUSD: totalCost / float64(len(costs)),
	}
}
