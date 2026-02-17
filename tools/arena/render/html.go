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
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
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

	// Pre-build index of results by scenario ID (O(N) instead of O(N×M))
	resultsByScenario := make(map[string][]engine.RunResult)
	providersByScenario := make(map[string]map[string]bool)
	regionsByScenario := make(map[string]map[string]bool)

	for i := range results {
		result := &results[i]
		scenarioID := result.ScenarioID

		resultsByScenario[scenarioID] = append(resultsByScenario[scenarioID], *result)

		if providersByScenario[scenarioID] == nil {
			providersByScenario[scenarioID] = make(map[string]bool)
		}
		providersByScenario[scenarioID][result.ProviderID] = true

		if regionsByScenario[scenarioID] == nil {
			regionsByScenario[scenarioID] = make(map[string]bool)
		}
		regionsByScenario[scenarioID][result.Region] = true
	}

	for _, scenario := range scenarios {
		scenarioResults := resultsByScenario[scenario]
		providersMap := providersByScenario[scenario]
		regionsMap := regionsByScenario[scenario]

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
		"formatCost":                   formatCost,
		"formatDuration":               formatDuration,
		"formatPercent":                formatPercent,
		"statusClass":                  getStatusClass,
		"prettyJSON":                   prettyJSON,
		"renderMarkdown":               renderMarkdown,
		"renderMessageContent":         renderMessageContent,
		"formatBytes":                  formatBytesHTML,
		"json":                         convertToJS,
		"add":                          func(a, b int) int { return a + b },
		"getAssertions":                getAssertions,
		"getValidators":                getValidatorsFromMessage,
		"assertionsPassed":             checkAssertionsPassed,
		"validatorsPassed":             checkValidatorsPassed,
		"hasAssertions":                hasAssertions,
		"hasValidators":                hasValidatorsInMessage,
		"getOK":                        getOKFromResult,
		"getDetails":                   getDetailsFromResult,
		"getMessage":                   getMessage,
		"hasMediaOutputs":              hasMediaOutputs,
		"renderMediaOutputs":           renderMediaOutputs,
		"hasAssertionResults":          hasAssertionResults,
		"getAssertionResults":          getAssertionResults,
		"getAssertionType":             getAssertionType,
		"hasConversationAssertions":    hasConversationAssertions,
		"conversationAssertionsPassed": conversationAssertionsPassed,
		"renderConversationAssertions": renderConversationAssertions,
		"getConversationAssertionResults": func(r engine.RunResult) []assertions.ConversationValidationResult {
			return r.ConversationAssertions.Results
		},
		"isAgentTool":         isAgentTool,
		"hasA2AAgents":        hasA2AAgents,
		"renderA2AAgentCards": renderA2AAgentCards,
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
	if jsonBytes, _ := json.Marshal(msgOrMeta); jsonBytes != nil {
		var msgMap map[string]interface{}
		if json.Unmarshal(jsonBytes, &msgMap) == nil {
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
	if jsonBytes, _ := json.Marshal(msgOrMeta); jsonBytes != nil {
		var msgMap map[string]interface{}
		if json.Unmarshal(jsonBytes, &msgMap) == nil {
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

	// Check top-level "passed" field (new format)
	if passed, ok := assertions["passed"].(bool); ok {
		return passed
	}

	// Check "results" array (new format)
	if results, ok := assertions["results"].([]interface{}); ok {
		return checkAssertionResultsArray(results)
	}

	// Legacy format: iterate over assertions map
	return checkLegacyAssertions(assertions)
}

// checkAssertionResultsArray checks if all assertions in the results array passed
func checkAssertionResultsArray(results []interface{}) bool {
	for _, v := range results {
		if assertion, ok := v.(map[string]interface{}); ok {
			if passed, exists := assertion["passed"].(bool); exists && !passed {
				return false
			}
		}
	}
	return true
}

// checkLegacyAssertions checks legacy assertion format
func checkLegacyAssertions(assertions map[string]interface{}) bool {
	for _, v := range assertions {
		if !checkSingleAssertion(v) {
			return false
		}
	}
	return true
}

// checkSingleAssertion checks if a single assertion passed
func checkSingleAssertion(assertion interface{}) bool {
	// Try to access as map first
	if assertionMap, ok := assertion.(map[string]interface{}); ok {
		if passed, exists := assertionMap["passed"].(bool); exists {
			return passed
		}
	}
	// Try to access Passed field directly (for struct types)
	if assertionStruct, ok := assertion.(struct {
		Passed  bool
		Details interface{}
	}); ok {
		return assertionStruct.Passed
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
		if json.Unmarshal([]byte(val), &jsonObj) == nil {
			// It's valid JSON, format it nicely
			if pretty, _ := json.MarshalIndent(jsonObj, "", "  "); pretty != nil {
				return string(pretty)
			}
		}
		// Not valid JSON, return as-is
		return val
	default:
		// For other types (like the Args object), marshal to pretty JSON
		if pretty, _ := json.MarshalIndent(val, "", "  "); pretty != nil {
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
	if jsonBytes, _ := json.Marshal(v); jsonBytes != nil {
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
	if !ok {
		return false
	}

	// Check for new format: assertions.results array
	if results, hasResults := assertions["results"].([]interface{}); hasResults {
		return len(results) > 0
	}

	// Legacy format: check map
	return len(assertions) > 0
}

// hasAssertionResults checks if assertions has a results array (new format)
func hasAssertionResults(assertions map[string]interface{}) bool {
	if assertions == nil {
		return false
	}
	results, ok := assertions["results"].([]interface{})
	return ok && len(results) > 0
}

// getAssertionResults extracts the results array from assertions
func getAssertionResults(assertions map[string]interface{}) []interface{} {
	if assertions == nil {
		return nil
	}
	if results, ok := assertions["results"].([]interface{}); ok {
		return results
	}
	return nil
}

// getAssertionType extracts the type field from an assertion result
func getAssertionType(assertion interface{}) string {
	if m, ok := assertion.(map[string]interface{}); ok {
		if t, exists := m["type"]; exists {
			if str, ok := t.(string); ok {
				return str
			}
		}
	}
	return ""
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

// hasMediaOutputs checks if a result has any media outputs.
func hasMediaOutputs(result engine.RunResult) bool {
	return len(result.MediaOutputs) > 0
}

// renderMediaOutputs renders the MediaOutputs section for a result as HTML.
func renderMediaOutputs(outputs []engine.MediaOutput) template.HTML {
	if len(outputs) == 0 {
		return ""
	}

	var html strings.Builder
	html.WriteString(`<div class="media-outputs-section">`)
	html.WriteString(`<div class="media-outputs-header">📸 Media Outputs (`)
	html.WriteString(fmt.Sprintf("%d", len(outputs)))
	html.WriteString(`)</div>`)
	html.WriteString(`<div class="media-outputs-grid">`)

	for _, output := range outputs {
		html.WriteString(`<div class="media-output-card">`)

		// Media type icon and header
		icon := getMediaTypeIcon(output.Type)
		html.WriteString(`<div class="media-output-header">`)
		html.WriteString(fmt.Sprintf(`<span class="media-icon">%s</span>`, icon))
		html.WriteString(fmt.Sprintf(`<span class="media-type">%s</span>`, output.Type))
		html.WriteString(`</div>`)

		// Thumbnail for images (if available and small enough)
		if output.Type == "image" && output.Thumbnail != "" {
			html.WriteString(fmt.Sprintf(`<div class="media-thumbnail"><img src="data:%s;base64,%s" alt="Image thumbnail" /></div>`, output.MIMEType, output.Thumbnail))
		}

		// Media details
		html.WriteString(`<div class="media-details">`)

		// MIME type
		html.WriteString(`<div class="media-detail-item">`)
		html.WriteString(fmt.Sprintf(`<span class="media-detail-label">Type:</span> <span class="media-detail-value">%s</span>`, output.MIMEType))
		html.WriteString(`</div>`)

		// Size
		if output.SizeBytes > 0 {
			html.WriteString(`<div class="media-detail-item">`)
			html.WriteString(fmt.Sprintf(`<span class="media-detail-label">Size:</span> <span class="media-detail-value">%s</span>`, formatBytes(int(output.SizeBytes))))
			html.WriteString(`</div>`)
		}

		// Duration for audio/video
		if output.Duration != nil && *output.Duration > 0 {
			html.WriteString(`<div class="media-detail-item">`)
			html.WriteString(fmt.Sprintf(`<span class="media-detail-label">Duration:</span> <span class="media-detail-value">%ds</span>`, *output.Duration))
			html.WriteString(`</div>`)
		}

		// Dimensions for images/video
		if output.Width != nil && output.Height != nil && *output.Width > 0 && *output.Height > 0 {
			html.WriteString(`<div class="media-detail-item">`)
			html.WriteString(fmt.Sprintf(`<span class="media-detail-label">Dimensions:</span> <span class="media-detail-value">%d×%d</span>`, *output.Width, *output.Height))
			html.WriteString(`</div>`)
		}

		// File path
		if output.FilePath != "" {
			html.WriteString(`<div class="media-detail-item">`)
			html.WriteString(fmt.Sprintf(`<span class="media-detail-label">Source:</span> <span class="media-detail-value media-filepath" title="%s">%s</span>`, output.FilePath, truncateSource(output.FilePath, 30)))
			html.WriteString(`</div>`)
		}

		// Message reference
		html.WriteString(`<div class="media-detail-item">`)
		html.WriteString(fmt.Sprintf(`<span class="media-detail-label">Message:</span> <span class="media-detail-value">#%d</span>`, output.MessageIdx+1))
		html.WriteString(`</div>`)

		html.WriteString(`</div>`) // media-details
		html.WriteString(`</div>`) // media-output-card
	}

	html.WriteString(`</div>`) // media-outputs-grid
	html.WriteString(`</div>`) // media-outputs-section

	return template.HTML(html.String())
}

// hasConversationAssertions checks if a result has conversation-level assertions.
//
//nolint:gocritic // hugeParam: template functions can't use pointers
func hasConversationAssertions(result engine.RunResult) bool {
	return result.ConversationAssertions.Total > 0
}

// conversationAssertionsPassed checks if all conversation assertions passed.
func conversationAssertionsPassed(results []assertions.ConversationValidationResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

// renderConversationAssertions renders conversation-level assertions as an HTML table.
func renderConversationAssertions(results []assertions.ConversationValidationResult) template.HTML {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(`<div class="conversation-assertions-section">`)
	b.WriteString(renderConversationHeader(results))
	b.WriteString(`<table class="conversation-assertions-table">`)
	b.WriteString(`<thead><tr><th>#</th><th>Type</th><th>Status</th><th>Message</th><th>Details</th></tr></thead>`)
	b.WriteString(`<tbody>`)
	for i, r := range results {
		b.WriteString(renderConversationRow(i, r))
	}
	b.WriteString(`</tbody></table></div>`)
	//nolint:gosec // G203: HTML generation is intentional for template rendering
	return template.HTML(b.String())
}

// renderConversationHeader builds the header (badge, title, counts).
func renderConversationHeader(results []assertions.ConversationValidationResult) string {
	passed, failed := 0, 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	statusClass := "passed"
	statusIcon := "✓"
	if failed > 0 {
		statusClass = "failed"
		statusIcon = "✗"
	}
	var b strings.Builder
	b.WriteString(`<div class="conversation-assertions-header">`)
	b.WriteString(fmt.Sprintf(`<span class="conversation-assertions-badge %s">%s</span>`, statusClass, statusIcon))
	b.WriteString(`<span class="conversation-assertions-title">Conversation Assertions</span>`)
	b.WriteString(fmt.Sprintf(`<span class="conversation-assertions-count">%d passed, %d failed</span>`, passed, failed))
	b.WriteString(`</div>`)
	return b.String()
}

// renderConversationRow builds a single table row for a validation result.
func renderConversationRow(index int, result assertions.ConversationValidationResult) string {
	rowClass := "passed"
	statusText := "Passed"
	statusIcon := "✓"
	if !result.Passed {
		rowClass = "failed"
		statusText = "Failed"
		statusIcon = "✗"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<tr class=%q>`, rowClass))
	b.WriteString(fmt.Sprintf(`<td class="assertion-index">%d</td>`, index+1))
	// Type column
	atype := result.Type
	if atype == "" {
		atype = "—"
	}
	b.WriteString(fmt.Sprintf(`<td class="assertion-type">%s</td>`, template.HTMLEscapeString(atype)))
	b.WriteString(`<td class="assertion-status">`)
	b.WriteString(fmt.Sprintf(`<span class="status-icon %s">%s</span> `, rowClass, statusIcon))
	b.WriteString(statusText)
	b.WriteString(`</td>`)
	msg := result.Message
	if msg == "" {
		msg = "—"
	}
	b.WriteString(fmt.Sprintf(`<td class="assertion-message">%s</td>`, template.HTMLEscapeString(msg)))
	b.WriteString(`<td class="assertion-details">`)
	b.WriteString(renderConversationDetails(result))
	b.WriteString(`</td></tr>`)
	return b.String()
}

// renderConversationDetails renders violations and details JSON, or em dash.
func renderConversationDetails(result assertions.ConversationValidationResult) string {
	var b strings.Builder
	// Wrap details in an expandable section
	b.WriteString(`<details class="conversation-details"><summary>Show details</summary>`)
	// Violations list
	if len(result.Violations) > 0 {
		b.WriteString(fmt.Sprintf(`<div class="violation-summary">%d violation(s)</div>`, len(result.Violations)))
		b.WriteString(`<ul class="violations-list">`)
		for _, v := range result.Violations {
			b.WriteString(`<li>`)
			b.WriteString(fmt.Sprintf(`<span class="violation-turn">Turn %d:</span> `, v.TurnIndex+1))
			b.WriteString(template.HTMLEscapeString(v.Description))
			if len(v.Evidence) > 0 {
				evJSON, _ := json.MarshalIndent(v.Evidence, "", "  ")
				b.WriteString(fmt.Sprintf(`<pre class="violation-evidence">%s</pre>`, template.HTMLEscapeString(string(evJSON))))
			}
			b.WriteString(`</li>`)
		}
		b.WriteString(`</ul>`)
	}
	// Details JSON
	if len(result.Details) > 0 {
		detailsJSON, _ := json.MarshalIndent(result.Details, "", "  ")
		b.WriteString(`<pre class="assertion-details-json">`)
		b.WriteString(template.HTMLEscapeString(string(detailsJSON)))
		b.WriteString(`</pre>`)
	}
	if len(result.Violations) == 0 && len(result.Details) == 0 {
		b.WriteString(`—`)
	}
	b.WriteString(`</details>`)
	return b.String()
}

// isAgentTool returns true if the tool name is an A2A agent tool.
func isAgentTool(name string) bool {
	return strings.HasPrefix(name, "a2a__")
}

// hasA2AAgents checks if a result has A2A agent metadata.
//
//nolint:gocritic // hugeParam: template functions can't use pointers
func hasA2AAgents(result engine.RunResult) bool {
	return len(result.A2AAgents) > 0
}

// renderA2AAgentCards renders the A2A agent cards section as HTML.
func renderA2AAgentCards(agents []engine.A2AAgentInfo) template.HTML {
	if len(agents) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(`<div class="a2a-agents-section">`)
	b.WriteString(`<div class="a2a-agents-header">`)
	b.WriteString(fmt.Sprintf(
		`<span class="agent-icon">🤖</span> A2A Agents (%d)`, len(agents)))
	b.WriteString(`</div>`)
	b.WriteString(`<div class="a2a-agents-grid">`)

	for _, agent := range agents {
		renderA2AAgentCard(&b, agent)
	}

	b.WriteString(`</div>`) // a2a-agents-grid
	b.WriteString(`</div>`) // a2a-agents-section

	//nolint:gosec // G203: HTML generation is intentional for template rendering
	return template.HTML(b.String())
}

// renderA2AAgentCard renders a single agent card.
func renderA2AAgentCard(b *strings.Builder, agent engine.A2AAgentInfo) {
	b.WriteString(`<div class="a2a-agent-card">`)
	fmt.Fprintf(b, `<div class="a2a-agent-name">%s</div>`,
		template.HTMLEscapeString(agent.Name))
	if agent.Description != "" {
		fmt.Fprintf(b, `<div class="a2a-agent-description">%s</div>`,
			template.HTMLEscapeString(agent.Description))
	}
	if len(agent.Skills) > 0 {
		b.WriteString(`<div class="a2a-skills-list">`)
		for _, skill := range agent.Skills {
			renderA2ASkillItem(b, skill)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
}

// renderA2ASkillItem renders a single skill within an agent card.
func renderA2ASkillItem(b *strings.Builder, skill engine.A2ASkillInfo) {
	b.WriteString(`<div class="a2a-skill-item">`)
	fmt.Fprintf(b, `<span class="a2a-skill-name">%s</span>`,
		template.HTMLEscapeString(skill.Name))
	if skill.Description != "" {
		fmt.Fprintf(b, ` <span class="a2a-skill-desc">— %s</span>`,
			template.HTMLEscapeString(skill.Description))
	}
	for _, tag := range skill.Tags {
		fmt.Fprintf(b, ` <span class="a2a-skill-tag">%s</span>`,
			template.HTMLEscapeString(tag))
	}
	b.WriteString(`</div>`)
}
