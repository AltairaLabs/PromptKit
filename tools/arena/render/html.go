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
	"strings"
	"time"

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
			TotalRuns:      len(results),
			SuccessfulRuns: successfulRuns,
			ErrorRuns:      len(results) - successfulRuns,
			TotalCost:      totalCost,
			TotalTokens:    totalTokens,
			AvgLatency:     avgLatency,
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
	matrix := make([][]MatrixCell, len(providers))
	for i := range matrix {
		matrix[i] = make([]MatrixCell, len(regions))
	}

	// Initialize matrix cells
	for i, provider := range providers {
		for j, region := range regions {
			matrix[i][j] = MatrixCell{
				Provider: provider,
				Region:   region,
			}
		}
	}

	// Populate matrix with data
	for _, result := range results {
		providerIdx := -1
		regionIdx := -1

		for i, p := range providers {
			if p == result.ProviderID {
				providerIdx = i
				break
			}
		}

		for i, r := range regions {
			if r == result.Region {
				regionIdx = i
				break
			}
		}

		if providerIdx >= 0 && regionIdx >= 0 {
			cell := &matrix[providerIdx][regionIdx]
			cell.Scenarios++
			cell.Cost += result.Cost.TotalCost

			if result.Error == "" {
				cell.Successful++
			} else {
				cell.Errors++
			}
		}
	}

	return matrix
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
		"formatCost": func(cost float64) string {
			return fmt.Sprintf("$%.4f", cost)
		},
		"formatDuration": func(d time.Duration) string {
			if d < time.Millisecond {
				return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
			}
			if d < time.Second {
				return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1000000)
			}
			return fmt.Sprintf("%.2fs", d.Seconds())
		},
		"formatPercent": func(part, total int) string {
			if total == 0 {
				return "0%"
			}
			return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
		},
		"statusClass": func(successful, errors int) string {
			if errors > 0 {
				return "error"
			}
			if successful > 0 {
				return "success"
			}
			return "empty"
		},
		"prettyJSON": func(v interface{}) string {
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
		},
		"renderMarkdown": func(content string) template.HTML {
			// Convert markdown to HTML with extensions for better list handling
			extensions := blackfriday.CommonExtensions |
				blackfriday.HardLineBreak |
				blackfriday.NoEmptyLineBeforeBlock
			html := blackfriday.Run([]byte(content), blackfriday.WithExtensions(extensions))
			return template.HTML(html)
		},
		"json": func(v interface{}) template.JS {
			// Convert Go value to JSON for use in JavaScript
			if jsonBytes, err := json.Marshal(v); err == nil {
				return template.JS(jsonBytes)
			}
			return template.JS("[]")
		},
		"add": func(a, b int) int {
			return a + b
		},
		"getAssertions": func(meta interface{}) map[string]interface{} {
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
		},
		"getValidators": func(meta interface{}) map[string]interface{} {
			if meta == nil {
				return nil
			}
			metaMap, ok := meta.(map[string]interface{})
			if !ok {
				return nil
			}
			validators, ok := metaMap["validators"].(map[string]interface{})
			if !ok {
				return nil
			}
			return validators
		},
		"assertionsPassed": func(assertions map[string]interface{}) bool {
			if assertions == nil {
				return true
			}
			for _, v := range assertions {
				// Try to access as map first
				if assertion, ok := v.(map[string]interface{}); ok {
					if ok, exists := assertion["ok"].(bool); exists && !ok {
						return false
					}
				}
				// Try to access OK field directly (for struct types)
				if assertion, ok := v.(struct {
					OK      bool
					Details interface{}
				}); ok {
					if !assertion.OK {
						return false
					}
				}
			}
			return true
		},
		"validatorsPassed": func(validators map[string]interface{}) bool {
			if validators == nil {
				return true
			}
			for _, v := range validators {
				// Try to access as map first
				if validator, ok := v.(map[string]interface{}); ok {
					if ok, exists := validator["ok"].(bool); exists && !ok {
						return false
					}
				}
				// Try to access OK field directly (for struct types)
				if validator, ok := v.(struct {
					OK      bool
					Details interface{}
				}); ok {
					if !validator.OK {
						return false
					}
				}
			}
			return true
		},
		"hasAssertions": func(meta interface{}) bool {
			if meta == nil {
				return false
			}
			metaMap, ok := meta.(map[string]interface{})
			if !ok {
				return false
			}
			assertions, ok := metaMap["assertions"].(map[string]interface{})
			return ok && len(assertions) > 0
		},
		"hasValidators": func(meta interface{}) bool {
			if meta == nil {
				return false
			}
			metaMap, ok := meta.(map[string]interface{})
			if !ok {
				return false
			}
			validators, ok := metaMap["validators"].(map[string]interface{})
			return ok && len(validators) > 0
		},
		"getOK": func(result interface{}) bool {
			// Try map access first
			if m, ok := result.(map[string]interface{}); ok {
				if okVal, exists := m["ok"]; exists {
					if b, isBool := okVal.(bool); isBool {
						return b
					}
				}
				if okVal, exists := m["OK"]; exists {
					if b, isBool := okVal.(bool); isBool {
						return b
					}
				}
			}
			// Try struct field via reflection through JSON round-trip
			jsonBytes, err := json.Marshal(result)
			if err == nil {
				var m map[string]interface{}
				if err := json.Unmarshal(jsonBytes, &m); err == nil {
					if okVal, exists := m["ok"]; exists {
						if b, isBool := okVal.(bool); isBool {
							return b
						}
					}
				}
			}
			return false
		},
		"getDetails": func(result interface{}) interface{} {
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
		},
	}).Parse(reportTemplate))

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
