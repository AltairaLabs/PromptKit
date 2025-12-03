package viewmodels

import (
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
