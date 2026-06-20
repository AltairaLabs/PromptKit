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
	_ "embed" // for go:embed directives on the HTML template files below
	"encoding/json"
	"fmt"
	"html/template"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// markdownSanitizer is a package-level bluemonday UGC policy used
// to sanitize markdown-rendered HTML and prevent XSS.
var markdownSanitizer = bluemonday.UGCPolicy()

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
	// ArtifactsByRun maps a run's RunID to artifacts a hook declared for it via a
	// manifest at <outDir>/artifacts/<RunID>.json, rendered as per-run links. The
	// report knows nothing about what hooks produce — it only surfaces what they
	// declare. Empty when no manifest exists.
	ArtifactsByRun map[string][]Artifact `json:"artifacts_by_run,omitempty"`
}

// Artifact is a hook-declared output surfaced in the report's per-run Artifacts
// section. Hooks may record any number (a session hook a captured workspace, a
// turn hook a per-turn dump, etc.). Path is relative to the report's output
// directory so the link resolves when the report is opened from disk.
type Artifact struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
}

// ScenarioGroup represents a scenario with its provider×region matrix
type ScenarioGroup struct {
	ScenarioID string             `json:"scenario_id"`
	Providers  []string           `json:"providers"`
	Regions    []string           `json:"regions"`
	Matrix     [][]MatrixCell     `json:"matrix"`
	Results    []engine.RunResult `json:"results"`
	// Labels surfaces stratification labels copied from the scenario manifest's
	// metadata.labels. Picked from the first labeled result in the group; all
	// repetitions of a scenario carry identical labels.
	Labels map[string]string `json:"labels,omitempty"`
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

// loadRunArtifacts reads per-run artifact manifests and returns display
// artifacts keyed by RunID. Arena owns the base path
// (<outDir>/artifacts/<RunID>/, created by the engine and handed to hooks via
// SessionEvent metadata); a hook writes its files there and records only
// {name, description, filename} in <base>/manifest.json. This joins the base and
// the hook's filename into the report link. Missing/malformed manifests are
// skipped — artifacts are purely additive and must never break report generation.
func loadRunArtifacts(outDir string, results []engine.RunResult) map[string][]Artifact {
	byRun := make(map[string][]Artifact)
	for i := range results {
		runID := results[i].RunID
		if runID == "" {
			continue
		}
		manifestPath := filepath.Join(outDir, "artifacts", runID, "manifest.json")
		raw, err := os.ReadFile(manifestPath) //nolint:gosec // outDir is the report output dir
		if err != nil {
			continue
		}
		var manifest struct {
			Artifacts []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Filename    string `json:"filename"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			continue
		}
		var arts []Artifact
		for _, a := range manifest.Artifacts {
			if a.Filename == "" {
				continue
			}
			arts = append(arts, Artifact{
				Name:        a.Name,
				Description: a.Description,
				// Base is Arena's (relative to the report dir); filename is the hook's.
				Path: path.Join("artifacts", runID, a.Filename),
			})
		}
		if len(arts) > 0 {
			byRun[runID] = arts
		}
	}
	return byRun
}

// GenerateHTMLReport creates an HTML report from run results
func GenerateHTMLReport(results []engine.RunResult, outputPath string) error {
	// Prepare data
	data := prepareReportData(results)
	// Surface any hook-declared per-run artifacts (e.g. a captured workspace) as
	// links. Manifests live next to the report under artifacts/<RunID>.json.
	data.ArtifactsByRun = loadRunArtifacts(filepath.Dir(outputPath), results)

	// Generate HTML
	html, err := generateHTML(&data)
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

	// Convert maps to sorted slices for deterministic output
	providerList := make([]string, 0, len(providers))
	for p := range providers {
		providerList = append(providerList, p)
	}
	sort.Strings(providerList)

	regionList := make([]string, 0, len(regions))
	for r := range regions {
		regionList = append(regionList, r)
	}
	sort.Strings(regionList)

	scenarioList := make([]string, 0, len(scenarios))
	for s := range scenarios {
		scenarioList = append(scenarioList, s)
	}
	sort.Strings(scenarioList)

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

// firstNonEmptyLabels returns a copy of the first non-empty Labels map across
// the given results. All repetitions of the same scenario share labels, so the
// first labeled result is authoritative.
func firstNonEmptyLabels(results []engine.RunResult) map[string]string {
	for i := range results {
		if len(results[i].Labels) == 0 {
			continue
		}
		return maps.Clone(results[i].Labels)
	}
	return nil
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

		// Convert maps to sorted slices for deterministic output
		providers := make([]string, 0, len(providersMap))
		for p := range providersMap {
			providers = append(providers, p)
		}
		sort.Strings(providers)

		regions := make([]string, 0, len(regionsMap))
		for r := range regionsMap {
			regions = append(regions, r)
		}
		sort.Strings(regions)

		// Generate matrix for this scenario
		matrix := generateMatrix(scenarioResults, providers, regions)

		groups = append(groups, ScenarioGroup{
			ScenarioID: scenario,
			Providers:  providers,
			Regions:    regions,
			Matrix:     matrix,
			Results:    scenarioResults,
			Labels:     firstNonEmptyLabels(scenarioResults),
		})
	}

	return groups
}

var (
	reportTmplOnce sync.Once
	reportTmpl     *template.Template
)

func getReportTemplate() *template.Template {
	reportTmplOnce.Do(func() {
		reportTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
			"formatCost": formatCost,
			"isoTime": func(t time.Time) string {
				if t.IsZero() {
					return ""
				}
				return t.Format(time.RFC3339Nano)
			},
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
			"hasEvalResults":    hasEvalResults,
			"renderEvalResults": renderEvalResults,
			"getEvalResults": func(r engine.RunResult) []evals.EvalResult {
				return r.EvalResults
			},
			"renderConversationAssertionsSummary": renderConversationAssertionsSummary,
			"renderEvalResultsSummary":            renderEvalResultsSummary,
			"renderToolResultMediaBadges":         renderToolResultMediaBadges,
			"isAgentTool":                         isAgentTool,
			"isWorkflowTool":                      isWorkflowTool,
			"isMemoryTool":                        isMemoryTool,
			"hasA2AAgents":                        hasA2AAgents,
			"renderA2AAgentCards":                 renderA2AAgentCards,
			"consentStatus":                       consentStatus,
			"selfPlayPrompt":                      selfPlayPrompt,
			"selfPlayPersona":                     selfPlayPersona,
			"selfPlayDetails":                     selfPlayDetails,
			"hasSelfPlayDetails":                  hasSelfPlayDetails,
		}).Parse(reportTemplate))
	})
	return reportTmpl
}

func generateHTML(data *HTMLReportData) (string, error) {
	var buf strings.Builder
	if err := getReportTemplate().Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// getValidatorsFromMessage extracts validators from a message's validations array
func getValidatorsFromMessage(msgOrMeta interface{}) map[string]interface{} {
	// Try direct struct field access via reflection first (avoids JSON round-trip)
	if validations := extractValidationsViaReflection(msgOrMeta); validations != nil {
		return validations
	}

	// Try map access (handles map[string]interface{} representations)
	if msgMap, ok := msgOrMeta.(map[string]interface{}); ok {
		if validationsRaw, exists := msgMap["validations"]; exists {
			if validations, ok := validationsRaw.([]interface{}); ok && len(validations) > 0 {
				return convertValidationsToMap(validations)
			}
		}
	}

	// Fallback: try meta.validators (legacy)
	return getLegacyValidators(msgOrMeta)
}

// extractValidationsViaReflection uses reflection to access the Validations field directly.
func extractValidationsViaReflection(msgOrMeta interface{}) map[string]interface{} {
	rv, ok := derefToStruct(msgOrMeta)
	if !ok {
		return nil
	}
	field := rv.FieldByName("Validations")
	if !field.IsValid() || field.Kind() != reflect.Slice || field.Len() == 0 {
		return nil
	}
	result := make(map[string]interface{})
	for i := 0; i < field.Len(); i++ {
		elem := field.Index(i)
		if elem.Kind() == reflect.Struct {
			vtField := elem.FieldByName("ValidatorType")
			if vtField.IsValid() && vtField.Kind() == reflect.String {
				result[vtField.String()] = elem.Interface()
			}
		}
	}
	if len(result) > 0 {
		return result
	}
	return nil
}

// selfPlayPrompt extracts the self-play system prompt from a message's metadata.
// Looks at meta.system_prompt (duplex executor) and falls back to
// meta.raw_response.system_prompt (legacy text-mode selfplay). Returns empty
// string if not a self-play message.
func selfPlayPrompt(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	if sp, ok := meta["system_prompt"].(string); ok && sp != "" {
		return sp
	}
	rawResp := selfPlayRawResponse(meta)
	if rawResp == nil {
		return ""
	}
	sp, _ := rawResp["system_prompt"].(string)
	return sp
}

// selfPlayPersona extracts the persona ID from a message's metadata.
// Looks at meta.persona (duplex executor) and falls back to
// meta.raw_response.persona (legacy text-mode selfplay). Returns empty string
// if not a self-play message.
func selfPlayPersona(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	if p, ok := meta["persona"].(string); ok && p != "" {
		return p
	}
	rawResp := selfPlayRawResponse(meta)
	if rawResp == nil {
		return ""
	}
	p, _ := rawResp["persona"].(string)
	return p
}

// hasSelfPlayDetails reports whether the message metadata carries any
// selfplay information worth surfacing in the devtools panel.
func hasSelfPlayDetails(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	if v, ok := meta["self_play"].(bool); ok && v {
		return true
	}
	if p, ok := meta["persona"].(string); ok && p != "" {
		return true
	}
	if selfPlayPrompt(meta) != "" {
		return true
	}
	return false
}

// selfPlayDetails returns the structured selfplay metadata for rendering in
// the Self-Play devtools tab. Fields are nil-safe; missing values are simply
// omitted from the returned map.
func selfPlayDetails(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}
	out := map[string]interface{}{}
	if p, ok := meta["persona"].(string); ok && p != "" {
		out["persona"] = p
	}
	if p, ok := meta["self_play_provider"].(string); ok && p != "" {
		out["provider"] = p
	}
	if idx, ok := meta["selfplay_turn_index"]; ok {
		out["turn_index"] = idx
	}
	if w, ok := meta["validation_warning"].(string); ok && w != "" {
		out["validation_warning"] = w
	}
	if cost, ok := meta["self_play_cost"].(map[string]interface{}); ok {
		out["cost"] = cost
	}
	if prompt := selfPlayPrompt(meta); prompt != "" {
		out["prompt"] = prompt
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// selfPlayRawResponse extracts the raw_response map from message metadata.
func selfPlayRawResponse(meta map[string]interface{}) map[string]interface{} {
	if meta == nil {
		return nil
	}
	rawResp, ok := meta["raw_response"].(map[string]interface{})
	if !ok {
		return nil
	}
	return rawResp
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
	// Try direct struct field access via reflection first (avoids JSON round-trip)
	if hasValidationsViaReflection(msgOrMeta) {
		return true
	}

	// Try map access
	if msgMap, ok := msgOrMeta.(map[string]interface{}); ok {
		if validationsRaw, exists := msgMap["validations"]; exists {
			if validations, ok := validationsRaw.([]interface{}); ok && len(validations) > 0 {
				return true
			}
		}
	}

	// Fallback: try meta.validators (legacy)
	return hasLegacyValidators(msgOrMeta)
}

// hasValidationsViaReflection checks for non-empty Validations field via reflection.
func hasValidationsViaReflection(msgOrMeta interface{}) bool {
	rv, ok := derefToStruct(msgOrMeta)
	if !ok {
		return false
	}
	field := rv.FieldByName("Validations")
	return field.IsValid() && field.Kind() == reflect.Slice && field.Len() > 0
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
		return true
	}
	// Try struct field via reflection
	return extractBoolFieldViaReflection(assertion, "Passed")
}

// isValidatorPassed checks if a single validator passed
func isValidatorPassed(validator interface{}) bool {
	// Try to access as map first (handles both legacy and new format)
	if validatorMap, ok := validator.(map[string]interface{}); ok {
		if passed, exists := validatorMap["passed"].(bool); exists && !passed {
			return false
		}
		return true
	}
	// Try struct field via reflection (handles types.ValidationResult and similar structs)
	rv, ok := derefToStruct(validator)
	if ok {
		field := rv.FieldByName("Passed")
		if field.IsValid() && field.Kind() == reflect.Bool {
			return field.Bool()
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
	// Try struct field via reflection
	return extractBoolFieldViaReflection(result, "Passed")
}

// derefToStruct dereferences a pointer (if needed) and returns the underlying reflect.Value
// and true if it is a struct, or the zero Value and false otherwise.
func derefToStruct(v interface{}) (reflect.Value, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return rv, true
}

// extractBoolFieldViaReflection extracts a bool field from a struct using reflection.
func extractBoolFieldViaReflection(v interface{}, fieldName string) bool {
	rv, ok := derefToStruct(v)
	if !ok {
		return false
	}
	field := rv.FieldByName(fieldName)
	if field.IsValid() && field.Kind() == reflect.Bool {
		return field.Bool()
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
	// Try struct field via reflection
	return extractFieldViaReflection(result, "Details")
}

// extractFieldViaReflection extracts a field value from a struct using reflection.
func extractFieldViaReflection(v interface{}, fieldName string) interface{} {
	rv, ok := derefToStruct(v)
	if !ok {
		return nil
	}
	field := rv.FieldByName(fieldName)
	if field.IsValid() {
		return field.Interface()
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

// renderMarkdown converts markdown to sanitized HTML.
// The output is sanitized via bluemonday's UGC policy to prevent XSS.
func renderMarkdown(content string) template.HTML {
	// Convert markdown to HTML with extensions for better list handling
	extensions := blackfriday.CommonExtensions |
		blackfriday.HardLineBreak |
		blackfriday.NoEmptyLineBeforeBlock
	raw := blackfriday.Run([]byte(content), blackfriday.WithExtensions(extensions))
	sanitized := markdownSanitizer.SanitizeBytes(raw)
	return template.HTML(sanitized) //nolint:gosec // Output is sanitized by bluemonday UGC policy
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

	// No JSON round-trip fallback needed — struct and map access above cover all cases
	return ""
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
	rv, ok := derefToStruct(result)
	if !ok {
		return ""
	}
	messageField := rv.FieldByName("Message")
	if messageField.IsValid() && messageField.Kind() == reflect.String {
		return messageField.String()
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
	// Assertions are pass/fail. Numeric "scores" only made sense when
	// arena was repurposing the eval pipeline as a gating wrapper —
	// the public contract is binary, so the column is gone.
	b.WriteString(`<thead><tr><th>#</th><th>Type</th><th>Status</th>`)
	b.WriteString(`<th>Message</th><th>Details</th></tr></thead>`)
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
	passed, failed, skipped := 0, 0, 0
	hasPackEval := false
	for _, r := range results {
		if _, sk := assertions.IsSkipped(r.Details); sk {
			skipped++
		} else if r.Passed {
			passed++
		} else {
			failed++
		}
		if assertions.IsPackEval(r) {
			hasPackEval = true
		}
	}
	statusClass := "passed"
	statusIcon := "✓"
	if failed > 0 {
		statusClass = "failed"
		statusIcon = "✗"
	}
	title := "Conversation Assertions"
	if hasPackEval {
		title = "Evaluations"
	}
	var b strings.Builder
	b.WriteString(`<div class="conversation-assertions-header">`)
	b.WriteString(fmt.Sprintf(`<span class="conversation-assertions-badge %s">%s</span>`, statusClass, statusIcon))
	b.WriteString(fmt.Sprintf(`<span class="conversation-assertions-title">%s</span>`, title))
	countText := fmt.Sprintf("%d passed, %d failed", passed, failed)
	if skipped > 0 {
		countText += fmt.Sprintf(", %d skipped", skipped)
	}
	b.WriteString(fmt.Sprintf(`<span class="conversation-assertions-count">%s</span>`, countText))
	b.WriteString(`</div>`)
	return b.String()
}

// renderConversationRow builds a single table row for a validation result.
func renderConversationRow(index int, result assertions.ConversationValidationResult) string {
	rowClass := "passed"
	statusText := "Passed"
	statusIcon := "✓"
	if reason, skipped := assertions.IsSkipped(result.Details); skipped {
		rowClass = "skipped"
		statusText = "Skipped"
		statusIcon = "⊘"
		if reason != "" {
			statusText = fmt.Sprintf("Skipped: %s", reason)
		}
	} else if !result.Passed {
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

// hasEvalResults checks if a result has pack-level eval observations.
//
//nolint:gocritic // hugeParam: template functions can't use pointers
func hasEvalResults(result engine.RunResult) bool {
	return len(result.EvalResults) > 0
}

// renderEvalResults renders pack-level eval observations as a table.
// Distinct from renderConversationAssertions: evals are non-gating
// measurements, so the table foregrounds the captured metrics rather
// than a single pass/fail score. Each row shows the eval id, type,
// and a "Metrics" column listing the structured key/value payload
// (the eval handler's Details, with any nested JSON-string `result`
// expanded so its keys surface as named metrics).
func renderEvalResults(results []evals.EvalResult) template.HTML {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<div class="eval-results-section">`)
	b.WriteString(renderEvalResultsHeader(results))
	b.WriteString(`<table class="eval-results-table">`)
	b.WriteString(`<thead><tr><th>#</th><th>Eval ID</th><th>Type</th><th>Metrics</th></tr></thead>`)
	b.WriteString(`<tbody>`)
	for i := range results {
		b.WriteString(renderEvalResultRow(i, &results[i]))
	}
	b.WriteString(`</tbody></table></div>`)
	//nolint:gosec // G203: HTML generation is intentional for template rendering
	return template.HTML(b.String())
}

// renderEvalResultsHeader builds the header row (count, title).
func renderEvalResultsHeader(results []evals.EvalResult) string {
	var b strings.Builder
	b.WriteString(`<div class="eval-results-header">`)
	b.WriteString(`<span class="eval-results-badge">📊</span>`)
	b.WriteString(`<span class="eval-results-title">Eval Observations</span>`)
	b.WriteString(fmt.Sprintf(`<span class="eval-results-count">%d eval(s)</span>`, len(results)))
	b.WriteString(`</div>`)
	return b.String()
}

// renderEvalResultRow renders a single eval result row.
func renderEvalResultRow(index int, r *evals.EvalResult) string {
	var b strings.Builder
	b.WriteString(`<tr class="eval-row">`)
	b.WriteString(fmt.Sprintf(`<td class="eval-index">%d</td>`, index+1))
	id := r.EvalID
	if id == "" {
		id = "—"
	}
	b.WriteString(fmt.Sprintf(`<td class="eval-id">%s</td>`, template.HTMLEscapeString(id)))
	etype := r.Type
	if etype == "" {
		etype = "—"
	}
	b.WriteString(fmt.Sprintf(`<td class="eval-type">%s</td>`, template.HTMLEscapeString(etype)))
	b.WriteString(`<td class="eval-metrics">`)
	b.WriteString(renderEvalMetrics(r))
	b.WriteString(`</td></tr>`)
	return b.String()
}

// renderEvalMetrics renders the eval's structured metrics as a
// key=value list, preferring the handler's Details map. tool_exec's
// `result` field carries the tool's parsed response — when it's a map
// of metrics, those keys surface as top-level metrics; when it's a
// scalar (string blob, number), it shows under the `result` label.
// Falls back to a JSON-formatted Value blob, then em dash.
func renderEvalMetrics(r *evals.EvalResult) string {
	metrics := evalMetricsMap(r)
	if len(metrics) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(metrics))
	for k := range metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(`<dl class="eval-metric-list">`)
	for _, k := range keys {
		b.WriteString(`<div class="eval-metric"><dt>`)
		b.WriteString(template.HTMLEscapeString(k))
		b.WriteString(`</dt><dd>`)
		b.WriteString(template.HTMLEscapeString(formatMetricValue(metrics[k])))
		b.WriteString(`</dd></div>`)
	}
	b.WriteString(`</dl>`)
	return b.String()
}

// evalMetricsMap extracts the metric set from an EvalResult. Skips
// transport-level fields (latency_ms, tool, eval_id, etc.) — these
// describe the call, not the measurement. When `result` is a map,
// its keys are promoted to top-level metric names.
func evalMetricsMap(r *evals.EvalResult) map[string]any {
	out := map[string]any{}
	src := r.Details
	if len(src) == 0 {
		// Fall back to Value when no Details. Only use it when Value
		// is itself a map (assertion wrappers replace Value with a
		// boolean — that's not a metric).
		if m, ok := r.Value.(map[string]any); ok {
			src = m
		}
	}
	skip := map[string]bool{"latency_ms": true, "tool": true, "eval_id": true, "duration_ms": true}
	for k, v := range src {
		if skip[k] {
			continue
		}
		if k == "result" {
			if inner, ok := v.(map[string]any); ok {
				for ik, iv := range inner {
					out[ik] = iv
				}
				continue
			}
		}
		out[k] = v
	}
	return out
}

// formatMetricValue renders a metric value into a compact display
// string. Numbers and booleans inline; strings as-is; everything else
// JSON-marshaled.
func formatMetricValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "—"
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// JSON decode produces float64 for all numeric kinds. Render
		// integers without a trailing decimal.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	}
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(body)
}

// renderConversationAssertionsSummary returns a compact pass/fail
// chip set suitable for inclusion in the result-card header. One
// chip per assertion, color-coded; truncated to a small budget when
// many assertions are present.
func renderConversationAssertionsSummary(results []assertions.ConversationValidationResult) template.HTML {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<span class="result-summary-group" title="Conversation assertions">`)
	for _, r := range results {
		cls := "passed"
		icon := "✓"
		if _, sk := assertions.IsSkipped(r.Details); sk {
			cls = "skipped"
			icon = "⊘"
		} else if !r.Passed {
			cls = "failed"
			icon = "✗"
		}
		label := r.Type
		if label == "" {
			label = "assertion"
		}
		b.WriteString(fmt.Sprintf(
			`<span class="summary-chip assertion-chip %s" title=%q>%s %s</span>`,
			cls, template.HTMLEscapeString(r.Message), icon, template.HTMLEscapeString(label),
		))
	}
	b.WriteString(`</span>`)
	//nolint:gosec // G203: HTML generation is intentional for template rendering
	return template.HTML(b.String())
}

// renderEvalResultsSummary returns a compact metrics chip strip for
// the result-card header. Each eval contributes its key=value metrics
// inline so the high-signal numbers are visible without expanding.
func renderEvalResultsSummary(results []evals.EvalResult) template.HTML {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<span class="result-summary-group" title="Eval observations">`)
	chipTpl := `<span class="summary-chip metric-chip" title=%q>` +
		`<span class="chip-key">%s</span>=<span class="chip-val">%s</span></span>`
	for i := range results {
		r := &results[i]
		metrics := evalMetricsMap(r)
		if len(metrics) == 0 {
			continue
		}
		keys := make([]string, 0, len(metrics))
		for k := range metrics {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		title := r.EvalID
		if title == "" {
			title = r.Type
		}
		for _, k := range keys {
			b.WriteString(fmt.Sprintf(
				chipTpl,
				template.HTMLEscapeString(title),
				template.HTMLEscapeString(k),
				template.HTMLEscapeString(formatMetricValue(metrics[k])),
			))
		}
	}
	b.WriteString(`</span>`)
	//nolint:gosec // G203: HTML generation is intentional for template rendering
	return template.HTML(b.String())
}

// renderConversationDetails renders violations and details JSON, or em dash.
func renderConversationDetails(result assertions.ConversationValidationResult) string {
	var b strings.Builder
	// Wrap details in an expandable section
	b.WriteString(`<details class="conversation-details"><summary>Show details</summary>`)
	// Duration inline
	if durationMs, ok := assertions.ExtractDurationMs(result.Details); ok {
		b.WriteString(fmt.Sprintf(`<div class="assertion-duration">Duration: %dms</div>`, durationMs))
	}
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

// consentStatus extracts consent status from a message's Meta field.
// Returns "granted", "denied", "timeout", or "" if no consent metadata.
//
//nolint:gocritic // hugeParam: template functions can't use pointers
func consentStatus(msg types.Message) string {
	if msg.Meta == nil {
		return ""
	}
	if status, ok := msg.Meta["consent_status"].(string); ok {
		return status
	}
	return ""
}

// isAgentTool returns true if the tool name is an A2A agent tool.
func isAgentTool(name string) bool {
	return strings.HasPrefix(name, "a2a__")
}

// isWorkflowTool returns true if the tool name is a workflow transition tool.
func isWorkflowTool(name string) bool {
	return strings.HasPrefix(name, "workflow__")
}

// isMemoryTool returns true if the tool name is a memory tool.
func isMemoryTool(name string) bool {
	return strings.HasPrefix(name, "memory__")
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
