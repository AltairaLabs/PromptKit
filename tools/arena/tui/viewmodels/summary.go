package viewmodels

import (
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/tools/arena/tui/theme"
)

// SummaryData contains the raw summary statistics
type SummaryData struct {
	TotalRuns       int
	CompletedRuns   int
	FailedRuns      int
	TotalTokens     int64
	TotalCost       float64
	TotalDuration   time.Duration
	AvgDuration     time.Duration
	ProviderStats   map[string]ProviderStat
	ProviderCosts   map[string]float64
	FailuresByError map[string]int
	ScenarioCount   int
	Regions         []string
	Errors          []ErrorInfo
	OutputDir       string
	HTMLReport      string
	AssertionTotal  int
	AssertionFailed int
}

// ErrorInfo represents a failed run with details
type ErrorInfo struct {
	RunID    string
	Scenario string
	Provider string
	Region   string
	Error    string
}

// ProviderStat contains statistics for a single provider
type ProviderStat struct {
	Runs   int
	Tokens int64
}

// SummaryViewModel transforms summary data for display
type SummaryViewModel struct {
	data SummaryData
}

// NewSummaryViewModel creates a new SummaryViewModel
func NewSummaryViewModel(data *SummaryData) *SummaryViewModel {
	return &SummaryViewModel{data: *data}
}

// GetFormattedTotalTokens returns formatted total tokens
func (vm *SummaryViewModel) GetFormattedTotalTokens() string {
	return theme.FormatNumber(vm.data.TotalTokens)
}

// GetFormattedTotalDuration returns formatted total duration
func (vm *SummaryViewModel) GetFormattedTotalDuration() string {
	return theme.FormatDuration(vm.data.TotalDuration)
}

// GetFormattedAvgDuration returns formatted average duration
func (vm *SummaryViewModel) GetFormattedAvgDuration() string {
	return theme.FormatDuration(vm.data.AvgDuration)
}

// GetFormattedTotalCost returns formatted total cost
func (vm *SummaryViewModel) GetFormattedTotalCost() string {
	return theme.FormatCost(vm.data.TotalCost)
}

// GetSuccessRate returns formatted success rate percentage
func (vm *SummaryViewModel) GetSuccessRate() string {
	if vm.data.TotalRuns == 0 {
		return "0%"
	}
	return theme.FormatPercentage(vm.data.CompletedRuns, vm.data.TotalRuns)
}

// GetFailureRate returns formatted failure rate percentage
func (vm *SummaryViewModel) GetFailureRate() string {
	if vm.data.TotalRuns == 0 {
		return "0%"
	}
	return theme.FormatPercentage(vm.data.FailedRuns, vm.data.TotalRuns)
}

// GetTotalRuns returns the total number of runs
func (vm *SummaryViewModel) GetTotalRuns() int {
	return vm.data.TotalRuns
}

// GetCompletedRuns returns the number of completed runs
func (vm *SummaryViewModel) GetCompletedRuns() int {
	return vm.data.CompletedRuns
}

// GetFailedRuns returns the number of failed runs
func (vm *SummaryViewModel) GetFailedRuns() int {
	return vm.data.FailedRuns
}

// GetProviderStats returns provider statistics
func (vm *SummaryViewModel) GetProviderStats() map[string]ProviderStat {
	return vm.data.ProviderStats
}

// GetProviderCosts returns provider costs
func (vm *SummaryViewModel) GetProviderCosts() map[string]float64 {
	return vm.data.ProviderCosts
}

// GetFailuresByError returns failure counts by error message
func (vm *SummaryViewModel) GetFailuresByError() map[string]int {
	return vm.data.FailuresByError
}

// GetFormattedProviderTokens returns formatted token count for a provider
func (vm *SummaryViewModel) GetFormattedProviderTokens(provider string) string {
	if stat, ok := vm.data.ProviderStats[provider]; ok {
		return theme.FormatNumber(stat.Tokens)
	}
	return "0"
}

// GetFormattedProviderCost returns formatted cost for a provider
func (vm *SummaryViewModel) GetFormattedProviderCost(provider string) string {
	if cost, ok := vm.data.ProviderCosts[provider]; ok {
		return theme.FormatCost(cost)
	}
	return theme.FormatCost(0)
}

// GetFormattedTotalRuns returns formatted total runs
func (vm *SummaryViewModel) GetFormattedTotalRuns() string {
	return fmt.Sprintf("%d", vm.data.TotalRuns)
}

// GetFormattedSuccessful returns formatted successful runs with percentage
func (vm *SummaryViewModel) GetFormattedSuccessful() string {
	return fmt.Sprintf("%d (%s)", vm.data.CompletedRuns, vm.GetSuccessRate())
}

// GetFormattedFailed returns formatted failed runs with percentage
func (vm *SummaryViewModel) GetFormattedFailed() string {
	return fmt.Sprintf("%d (%s)", vm.data.FailedRuns, vm.GetFailureRate())
}

// GetFormattedAvgDurationWithSuffix returns formatted average duration with suffix
func (vm *SummaryViewModel) GetFormattedAvgDurationWithSuffix() string {
	return fmt.Sprintf("%s per run", theme.FormatDuration(vm.data.AvgDuration))
}

// HasAssertions returns true if there are assertions
func (vm *SummaryViewModel) HasAssertions() bool {
	return vm.data.AssertionTotal > 0
}

// HasFailedAssertions returns true if there are failed assertions
func (vm *SummaryViewModel) HasFailedAssertions() bool {
	return vm.data.AssertionFailed > 0
}

// GetFormattedAssertionTotal returns formatted assertion total
func (vm *SummaryViewModel) GetFormattedAssertionTotal() string {
	return fmt.Sprintf("%d total", vm.data.AssertionTotal)
}

// GetFormattedAssertionFailed returns formatted failed assertions
func (vm *SummaryViewModel) GetFormattedAssertionFailed() string {
	return fmt.Sprintf("%d", vm.data.AssertionFailed)
}

// HasProviders returns true if there are provider stats
func (vm *SummaryViewModel) HasProviders() bool {
	return len(vm.data.ProviderStats) > 0
}

// GetFormattedProviders returns formatted provider list with counts
func (vm *SummaryViewModel) GetFormattedProviders() string {
	if len(vm.data.ProviderStats) == 0 {
		return ""
	}
	providerList := make([]string, 0, len(vm.data.ProviderStats))
	for provider, stat := range vm.data.ProviderStats {
		providerList = append(providerList, fmt.Sprintf("%s (%d)", provider, stat.Runs))
	}
	return strings.Join(providerList, ", ")
}

// GetFormattedScenarios returns formatted scenario count
func (vm *SummaryViewModel) GetFormattedScenarios() string {
	return fmt.Sprintf("%d scenarios", vm.data.ScenarioCount)
}

// HasRegions returns true if there are regions
func (vm *SummaryViewModel) HasRegions() bool {
	return len(vm.data.Regions) > 0
}

// GetFormattedRegions returns formatted region list
func (vm *SummaryViewModel) GetFormattedRegions() string {
	return strings.Join(vm.data.Regions, ", ")
}

// HasErrors returns true if there are errors
func (vm *SummaryViewModel) HasErrors() bool {
	return len(vm.data.Errors) > 0
}

// GetFormattedErrors returns formatted error list
func (vm *SummaryViewModel) GetFormattedErrors() []string {
	result := make([]string, 0, len(vm.data.Errors))
	for _, errInfo := range vm.data.Errors {
		runDesc := fmt.Sprintf("%s/%s/%s", errInfo.Scenario, errInfo.Provider, errInfo.Region)
		compactError := compactString(errInfo.Error)
		result = append(result, fmt.Sprintf("%s: %s", runDesc, compactError))
	}
	return result
}

// GetOutputDir returns the output directory
func (vm *SummaryViewModel) GetOutputDir() string {
	return vm.data.OutputDir
}

// HasHTMLReport returns true if there's an HTML report
func (vm *SummaryViewModel) HasHTMLReport() bool {
	return vm.data.HTMLReport != ""
}

// GetHTMLReport returns the HTML report path
func (vm *SummaryViewModel) GetHTMLReport() string {
	return vm.data.HTMLReport
}

// compactString removes excess whitespace and newlines from a string
func compactString(s string) string {
	// Replace newlines and multiple spaces with single space
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse multiple spaces into one
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
