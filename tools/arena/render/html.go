// Package render generates HTML and JSON reports from test execution results.
//
// This package transforms RunResult data into formatted reports with:
//   - Provider × Region performance matrices
//   - Token usage and cost breakdowns
//   - Average latency calculations
//   - Success/error rate summaries
//   - Markdown-formatted conversation logs
//
// Reports can be generated as standalone HTML files or JSON data exports.
package render

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/russross/blackfriday/v2"
)

// HTML template is embedded from external file using go:embed directive.
// This allows for easier editing and maintenance of the template while
// still embedding it in the binary for deployment.
//
//go:embed templates/report.html.tmpl
var reportTemplate string

// HTMLReportData contains all data needed for the HTML report
type HTMLReportData struct {
	Title          string             `json:"title"`
	GeneratedAt    time.Time          `json:"generated_at"`
	Summary        ReportSummary      `json:"summary"`
	Results        []engine.RunResult `json:"results"`
	Providers      []string           `json:"providers"`
	Regions        []string           `json:"regions"`
	Scenarios      []string           `json:"scenarios"`
	Matrix         [][]MatrixCell     `json:"matrix"` // Deprecated: kept for backwards compatibility
	ScenarioGroups []ScenarioGroup    `json:"scenario_groups"`
}

// ScenarioGroup represents a scenario with its provider×region matrix
type ScenarioGroup struct {
	ScenarioID string             `json:"scenario_id"`
	Providers  []string           `json:"providers"`
	Regions    []string           `json:"regions"`
	Matrix     [][]MatrixCell     `json:"matrix"`
	Results    []engine.RunResult `json:"results"`
}

// ReportSummary contains aggregate statistics
type ReportSummary struct {
	TotalRuns      int     `json:"total_runs"`
	SuccessfulRuns int     `json:"successful_runs"`
	ErrorRuns      int     `json:"error_runs"`
	TotalCost      float64 `json:"total_cost"`
	TotalTokens    int     `json:"total_tokens"`
	AvgLatency     string  `json:"avg_latency"`
	// Media statistics
	TotalImages      int   `json:"total_images"`
	TotalAudio       int   `json:"total_audio"`
	TotalVideo       int   `json:"total_video"`
	MediaLoadSuccess int   `json:"media_load_success"`
	MediaLoadErrors  int   `json:"media_load_errors"`
	TotalMediaSize   int64 `json:"total_media_size_bytes"`
}

// MatrixCell represents a cell in the provider x region matrix
type MatrixCell struct {
	Provider   string  `json:"provider"`
	Region     string  `json:"region"`
	Scenarios  int     `json:"scenarios"`
	Successful int     `json:"successful"`
	Errors     int     `json:"errors"`
	Cost       float64 `json:"cost"`
}

// GenerateHTMLReport creates an HTML report from run results
func GenerateHTMLReport(results []engine.RunResult, outputPath string) error {
	// Prepare data
	data := prepareReportData(results)

	// Generate HTML
	html, err := generateHTML(data)
	if err != nil {
		return fmt.Errorf("failed to generate HTML: %w", err)
	}

	// Create output directory if needed
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write HTML file
	if err := os.WriteFile(outputPath, []byte(html), 0644); err != nil {
		return fmt.Errorf("failed to write HTML file: %w", err)
	}

	// Generate companion JSON data file
	jsonPath := strings.TrimSuffix(outputPath, ".html") + "-data.json"
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON data: %w", err)
	}

	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write JSON data file: %w", err)
	}

	return nil
}

func prepareReportData(results []engine.RunResult) HTMLReportData {
	// Extract unique values
	providers := make(map[string]bool)
	regions := make(map[string]bool)
	scenarios := make(map[string]bool)

	var totalCost float64
	var totalTokens int
	var totalLatency time.Duration
	var successfulRuns int
	var validLatencyCount int

	for _, result := range results {
		providers[result.ProviderID] = true
		regions[result.Region] = true
		scenarios[result.ScenarioID] = true

		totalCost += result.Cost.TotalCost
		totalTokens += result.Cost.InputTokens + result.Cost.OutputTokens + result.Cost.CachedTokens

		if result.Error == "" {
			successfulRuns++
		}

		if result.Duration > 0 {
			totalLatency += result.Duration
			validLatencyCount++
		}
	}

	// Calculate media statistics
	mediaStats := calculateMediaStats(results)

	// Convert maps to slices
	providerList := make([]string, 0, len(providers))
	for p := range providers {
		providerList = append(providerList, p)
	}

	regionList := make([]string, 0, len(regions))
	for r := range regions {
		regionList = append(regionList, r)
	}

	scenarioList := make([]string, 0, len(scenarios))
	for s := range scenarios {
		scenarioList = append(scenarioList, s)
	}

	// Calculate average latency
	avgLatency := "N/A"
	if validLatencyCount > 0 {
		avg := totalLatency / time.Duration(validLatencyCount)
		if avg < time.Millisecond {
			avgLatency = fmt.Sprintf("%.2fµs", float64(avg.Nanoseconds())/1000)
		} else if avg < time.Second {
			avgLatency = fmt.Sprintf("%.2fms", float64(avg.Nanoseconds())/1000000)
		} else {
			avgLatency = fmt.Sprintf("%.2fs", avg.Seconds())
		}
	}

	// Generate legacy matrix (for backwards compatibility)
	matrix := generateMatrix(results, providerList, regionList)

	// Generate scenario groups with their own matrices
	scenarioGroups := generateScenarioGroups(results, scenarioList)

	return HTMLReportData{
		Title:       "Altaira Prompt Arena Report",
		GeneratedAt: time.Now(),
		Summary: ReportSummary{
			TotalRuns:        len(results),
			SuccessfulRuns:   successfulRuns,
			ErrorRuns:        len(results) - successfulRuns,
			TotalCost:        totalCost,
			TotalTokens:      totalTokens,
			AvgLatency:       avgLatency,
			TotalImages:      mediaStats.TotalImages,
			TotalAudio:       mediaStats.TotalAudio,
			TotalVideo:       mediaStats.TotalVideo,
			MediaLoadSuccess: mediaStats.MediaLoadSuccess,
			MediaLoadErrors:  mediaStats.MediaLoadErrors,
			TotalMediaSize:   mediaStats.TotalMediaSize,
		},
		Results:        results,
		Providers:      providerList,
		Regions:        regionList,
		Scenarios:      scenarioList,
		Matrix:         matrix,
		ScenarioGroups: scenarioGroups,
	}
}

func generateMatrix(results []engine.RunResult, providers, regions []string) [][]MatrixCell {
	matrix := initializeMatrix(providers, regions)
	populateMatrixWithResults(matrix, results, providers, regions)
	return matrix
}

// initializeMatrix creates and initializes a provider x region matrix
func initializeMatrix(providers, regions []string) [][]MatrixCell {
	matrix := make([][]MatrixCell, len(providers))
	for i := range matrix {
		matrix[i] = make([]MatrixCell, len(regions))
	}

	// Initialize matrix cells with provider/region info
	for i, provider := range providers {
		for j, region := range regions {
			matrix[i][j] = MatrixCell{
				Provider: provider,
				Region:   region,
			}
		}
	}
	return matrix
}

// populateMatrixWithResults fills the matrix with result data
func populateMatrixWithResults(matrix [][]MatrixCell, results []engine.RunResult, providers, regions []string) {
	for _, result := range results {
		providerIdx := findProviderIndex(result.ProviderID, providers)
		regionIdx := findRegionIndex(result.Region, regions)

		if providerIdx >= 0 && regionIdx >= 0 {
			updateMatrixCell(&matrix[providerIdx][regionIdx], result)
		}
	}
}

// findProviderIndex finds the index of a provider in the providers list
func findProviderIndex(providerID string, providers []string) int {
	for i, p := range providers {
		if p == providerID {
			return i
		}
	}
	return -1
}

// findRegionIndex finds the index of a region in the regions list
func findRegionIndex(region string, regions []string) int {
	for i, r := range regions {
		if r == region {
			return i
		}
	}
	return -1
}

// updateMatrixCell updates a matrix cell with result data
func updateMatrixCell(cell *MatrixCell, result engine.RunResult) {
	cell.Scenarios++
	cell.Cost += result.Cost.TotalCost

	if result.Error == "" {
		cell.Successful++
	} else {
		cell.Errors++
	}
}

func generateScenarioGroups(results []engine.RunResult, scenarios []string) []ScenarioGroup {
	groups := make([]ScenarioGroup, 0, len(scenarios))

	for _, scenario := range scenarios {
		// Filter results for this scenario
		var scenarioResults []engine.RunResult
		providersMap := make(map[string]bool)
		regionsMap := make(map[string]bool)

		for _, result := range results {
			if result.ScenarioID == scenario {
				scenarioResults = append(scenarioResults, result)
				providersMap[result.ProviderID] = true
				regionsMap[result.Region] = true
			}
		}

		// Convert maps to slices
		providers := make([]string, 0, len(providersMap))
		for p := range providersMap {
			providers = append(providers, p)
		}

		regions := make([]string, 0, len(regionsMap))
		for r := range regionsMap {
			regions = append(regions, r)
		}

		// Generate matrix for this scenario
		matrix := generateMatrix(scenarioResults, providers, regions)

		groups = append(groups, ScenarioGroup{
			ScenarioID: scenario,
			Providers:  providers,
			Regions:    regions,
			Matrix:     matrix,
			Results:    scenarioResults,
		})
	}

	return groups
}

func generateHTML(data HTMLReportData) (string, error) {
	tmpl := template.Must(template.New("report").Funcs(template.FuncMap{
		"formatCost":           formatCost,
		"formatDuration":       formatDuration,
		"formatPercent":        formatPercent,
		"statusClass":          getStatusClass,
		"prettyJSON":           prettyJSON,
		"renderMarkdown":       renderMarkdown,
		"renderMessageContent": renderMessageContent,
		"formatBytes":          formatBytesHTML,
		"json":                 convertToJS,
		"add":                  func(a, b int) int { return a + b },
		"getAssertions":        getAssertions,
		"getValidators":        getValidatorsFromMessage,
		"assertionsPassed":     checkAssertionsPassed,
		"validatorsPassed":     checkValidatorsPassed,
		"hasAssertions":        hasAssertions,
		"hasValidators":        hasValidatorsInMessage,
		"getOK":                getOKFromResult,
		"getDetails":           getDetailsFromResult,
		"getMessage":           getMessage,
	}).Parse(reportTemplate))

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// getValidatorsFromMessage extracts validators from a message's validations array
func getValidatorsFromMessage(msgOrMeta interface{}) map[string]interface{} {
	// First try to extract from message.Validations
	if jsonBytes, err := json.Marshal(msgOrMeta); err == nil {
		var msgMap map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &msgMap); err == nil {
			// Check for validations array in the message
			if validationsRaw, exists := msgMap["validations"]; exists {
				if validations, ok := validationsRaw.([]interface{}); ok && len(validations) > 0 {
					return convertValidationsToMap(validations)
				}
			}
		}
	}

	// Fallback: try meta.validators (legacy)
	return getLegacyValidators(msgOrMeta)
}

// convertValidationsToMap converts validations array to map by validator type
func convertValidationsToMap(validations []interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for _, v := range validations {
		if validation, ok := v.(map[string]interface{}); ok {
			if validatorType, ok := validation["validator_type"].(string); ok {
				result[validatorType] = validation
			}
		}
	}
	return result
}

// getLegacyValidators extracts validators from meta.validators (legacy format)
func getLegacyValidators(msgOrMeta interface{}) map[string]interface{} {
	if msgOrMeta == nil {
		return nil
	}
	metaMap, ok := msgOrMeta.(map[string]interface{})
	if !ok {
		return nil
	}
	validators, ok := metaMap["validators"].(map[string]interface{})
	if !ok {
		return nil
	}
	return validators
}

// hasValidatorsInMessage checks if a message has validations
func hasValidatorsInMessage(msgOrMeta interface{}) bool {
	// First try to check message.Validations
	if jsonBytes, err := json.Marshal(msgOrMeta); err == nil {
		var msgMap map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &msgMap); err == nil {
			if validationsRaw, exists := msgMap["validations"]; exists {
				if validations, ok := validationsRaw.([]interface{}); ok && len(validations) > 0 {
					return true
				}
			}
		}
	}

	// Fallback: try meta.validators (legacy)
	return hasLegacyValidators(msgOrMeta)
}

// hasLegacyValidators checks for legacy meta.validators format
func hasLegacyValidators(msgOrMeta interface{}) bool {
	if msgOrMeta == nil {
		return false
	}
	metaMap, ok := msgOrMeta.(map[string]interface{})
	if !ok {
		return false
	}
	validators, ok := metaMap["validators"].(map[string]interface{})
	return ok && len(validators) > 0
}

// checkValidatorsPassed checks if all validators passed
func checkValidatorsPassed(validators map[string]interface{}) bool {
	if validators == nil {
		return true
	}
	for _, v := range validators {
		if !isValidatorPassed(v) {
			return false
		}
	}
	return true
}

// checkAssertionsPassed checks if all assertions passed
func checkAssertionsPassed(assertions map[string]interface{}) bool {
	if assertions == nil {
		return true
	}
	for _, v := range assertions {
		// Try to access as map first
		if assertion, ok := v.(map[string]interface{}); ok {
			if passed, exists := assertion["passed"].(bool); exists && !passed {
				return false
			}
		}
		// Try to access Passed field directly (for struct types)
		if assertion, ok := v.(struct {
			Passed  bool
			Details interface{}
		}); ok {
			if !assertion.Passed {
				return false
			}
		}
	}
	return true
}

// isValidatorPassed checks if a single validator passed
func isValidatorPassed(validator interface{}) bool {
	// Try to access as map first (handles both legacy and new format)
	if validatorMap, ok := validator.(map[string]interface{}); ok {
		// Check "passed" field (unified format)
		if passed, exists := validatorMap["passed"].(bool); exists && !passed {
			return false
		}
	}
	// Try to access Passed field directly (for struct types)
	if validatorStruct, ok := validator.(struct {
		Passed  bool
		Details interface{}
	}); ok {
		if !validatorStruct.Passed {
			return false
		}
	}
	return true
}

// getOKFromResult extracts the passed boolean from a result
func getOKFromResult(result interface{}) bool {
	// Try map access first
	if m, ok := result.(map[string]interface{}); ok {
		if passedVal, exists := m["passed"]; exists {
			if b, isBool := passedVal.(bool); isBool {
				return b
			}
		}
		if passedVal, exists := m["Passed"]; exists {
			if b, isBool := passedVal.(bool); isBool {
				return b
			}
		}
	}
	// Try struct field via reflection through JSON round-trip
	return tryJSONRoundtripForOK(result)
}

// tryJSONRoundtripForOK tries to extract passed via JSON marshal/unmarshal
func tryJSONRoundtripForOK(result interface{}) bool {
	jsonBytes, err := json.Marshal(result)
	if err == nil {
		var m map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &m); err == nil {
			if passedVal, exists := m["passed"]; exists {
				if b, isBool := passedVal.(bool); isBool {
					return b
				}
			}
		}
	}
	return false
}

// getDetailsFromResult extracts details from a result
func getDetailsFromResult(result interface{}) interface{} {
	// Try map access first
	if m, ok := result.(map[string]interface{}); ok {
		if details, exists := m["details"]; exists {
			return details
		}
		if details, exists := m["Details"]; exists {
			return details
		}
	}
	// Try struct field via reflection through JSON round-trip
	return tryJSONRoundtripForDetails(result)
}

// tryJSONRoundtripForDetails tries to extract details via JSON marshal/unmarshal
func tryJSONRoundtripForDetails(result interface{}) interface{} {
	jsonBytes, err := json.Marshal(result)
	if err == nil {
		var m map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &m); err == nil {
			if details, exists := m["details"]; exists {
				return details
			}
		}
	}
	return nil
}

// formatCost formats a cost value as a currency string
func formatCost(cost float64) string {
	return fmt.Sprintf("$%.4f", cost)
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1000000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// formatPercent calculates and formats a percentage
func formatPercent(part, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
}

// getStatusClass returns CSS class based on success/error counts
func getStatusClass(successful, errors int) string {
	if errors > 0 {
		return "error"
	}
	if successful > 0 {
		return "success"
	}
	return "empty"
}

// prettyJSON formats a value as pretty-printed JSON
func prettyJSON(v interface{}) string {
	switch val := v.(type) {
	case string:
		// Try to parse the string as JSON first
		var jsonObj interface{}
		if err := json.Unmarshal([]byte(val), &jsonObj); err == nil {
			// It's valid JSON, format it nicely
			if pretty, err := json.MarshalIndent(jsonObj, "", "  "); err == nil {
				return string(pretty)
			}
		}
		// Not valid JSON, return as-is
		return val
	default:
		// For other types (like the Args object), marshal to pretty JSON
		if pretty, err := json.MarshalIndent(val, "", "  "); err == nil {
			return string(pretty)
		}
		return fmt.Sprintf("%v", val)
	}
}

// renderMarkdown converts markdown to HTML
func renderMarkdown(content string) template.HTML {
	// Convert markdown to HTML with extensions for better list handling
	extensions := blackfriday.CommonExtensions |
		blackfriday.HardLineBreak |
		blackfriday.NoEmptyLineBeforeBlock
	html := blackfriday.Run([]byte(content), blackfriday.WithExtensions(extensions))
	return template.HTML(html)
}

// convertToJS converts a Go value to JSON for use in JavaScript
func convertToJS(v interface{}) template.JS {
	// Convert Go value to JSON for use in JavaScript
	if jsonBytes, err := json.Marshal(v); err == nil {
		return template.JS(jsonBytes)
	}
	return template.JS("[]")
}

// getAssertions extracts assertions from meta
func getAssertions(meta interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}
	metaMap, ok := meta.(map[string]interface{})
	if !ok {
		return nil
	}
	assertions, ok := metaMap["assertions"].(map[string]interface{})
	if !ok {
		return nil
	}
	return assertions
}

// hasAssertions checks if meta contains assertions
func hasAssertions(meta interface{}) bool {
	if meta == nil {
		return false
	}
	metaMap, ok := meta.(map[string]interface{})
	if !ok {
		return false
	}
	assertions, ok := metaMap["assertions"].(map[string]interface{})
	return ok && len(assertions) > 0
}

// getMessage extracts the message field from an assertion or validation result
func getMessage(result interface{}) string {
	// Try map access first
	if msg := getMessageFromMap(result); msg != "" {
		return msg
	}

	// Try struct access via reflection
	if msg := getMessageFromStruct(result); msg != "" {
		return msg
	}

	// Try via JSON round-trip as last resort
	return getMessageViaJSON(result)
}

// getMessageFromMap extracts message from a map
func getMessageFromMap(result interface{}) string {
	if m, ok := result.(map[string]interface{}); ok {
		if msg, exists := m["message"]; exists {
			if str, ok := msg.(string); ok {
				return str
			}
		}
	}
	return ""
}

// getMessageFromStruct extracts message from a struct via reflection
func getMessageFromStruct(result interface{}) string {
	v := reflect.ValueOf(result)
	if v.Kind() == reflect.Struct {
		messageField := v.FieldByName("Message")
		if messageField.IsValid() && messageField.Kind() == reflect.String {
			return messageField.String()
		}
	}
	return ""
}

// getMessageViaJSON extracts message via JSON marshaling round-trip
func getMessageViaJSON(result interface{}) string {
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return ""
	}

	var m map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return ""
	}

	if msg, exists := m["message"]; exists {
		if str, ok := msg.(string); ok {
			return str
		}
	}
	return ""
}

// renderMessageContent renders a message with multimodal media support.
// This is a bridge function for template use.
func renderMessageContent(msg types.Message) template.HTML {
	return template.HTML(renderMessageWithMedia(msg))
}

// formatBytesHTML formats a byte count as a human-readable string.
// This is a bridge function for template use.
func formatBytesHTML(bytes int64) string {
	return formatBytes(int(bytes))
}
