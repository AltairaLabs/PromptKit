package viewmodels

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSummaryViewModel(t *testing.T) {
	data := SummaryData{
		TotalRuns:     10,
		CompletedRuns: 8,
		FailedRuns:    2,
	}

	vm := NewSummaryViewModel(&data)

	assert.NotNil(t, vm)
	assert.Equal(t, 10, vm.GetTotalRuns())
	assert.Equal(t, 8, vm.GetCompletedRuns())
	assert.Equal(t, 2, vm.GetFailedRuns())
}

func TestGetFormattedTotalTokens(t *testing.T) {
	data := SummaryData{TotalTokens: 1234567}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFormattedTotalTokens()

	assert.Equal(t, "1,234,567", result)
}

func TestGetFormattedTotalDuration(t *testing.T) {
	data := SummaryData{TotalDuration: 2*time.Minute + 30*time.Second}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFormattedTotalDuration()

	assert.Contains(t, result, "2m")
}

func TestGetFormattedAvgDuration(t *testing.T) {
	data := SummaryData{AvgDuration: 5 * time.Second}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFormattedAvgDuration()

	assert.Contains(t, result, "5s")
}

func TestGetFormattedTotalCost(t *testing.T) {
	data := SummaryData{TotalCost: 1.2345}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFormattedTotalCost()

	assert.Equal(t, "$1.2345", result)
}

func TestGetSuccessRate(t *testing.T) {
	tests := []struct {
		name          string
		totalRuns     int
		completedRuns int
		expected      string
	}{
		{"perfect success", 10, 10, "100.0%"},
		{"partial success", 10, 8, "80.0%"},
		{"no success", 10, 0, "0.0%"},
		{"zero runs", 0, 0, "0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := SummaryData{
				TotalRuns:     tt.totalRuns,
				CompletedRuns: tt.completedRuns,
			}
			vm := NewSummaryViewModel(&data)

			result := vm.GetSuccessRate()

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetFailureRate(t *testing.T) {
	tests := []struct {
		name       string
		totalRuns  int
		failedRuns int
		expected   string
	}{
		{"no failures", 10, 0, "0.0%"},
		{"some failures", 10, 2, "20.0%"},
		{"all failures", 10, 10, "100.0%"},
		{"zero runs", 0, 0, "0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := SummaryData{
				TotalRuns:  tt.totalRuns,
				FailedRuns: tt.failedRuns,
			}
			vm := NewSummaryViewModel(&data)

			result := vm.GetFailureRate()

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetProviderStats(t *testing.T) {
	providerStats := map[string]ProviderStat{
		"openai":    {Runs: 5, Tokens: 1000},
		"anthropic": {Runs: 3, Tokens: 800},
	}
	data := SummaryData{ProviderStats: providerStats}
	vm := NewSummaryViewModel(&data)

	result := vm.GetProviderStats()

	assert.Len(t, result, 2)
	assert.Equal(t, 5, result["openai"].Runs)
	assert.Equal(t, int64(1000), result["openai"].Tokens)
}

func TestGetProviderCosts(t *testing.T) {
	providerCosts := map[string]float64{
		"openai":    0.50,
		"anthropic": 0.30,
	}
	data := SummaryData{ProviderCosts: providerCosts}
	vm := NewSummaryViewModel(&data)

	result := vm.GetProviderCosts()

	assert.Len(t, result, 2)
	assert.Equal(t, 0.50, result["openai"])
	assert.Equal(t, 0.30, result["anthropic"])
}

func TestGetFailuresByError(t *testing.T) {
	failures := map[string]int{
		"timeout":    3,
		"rate_limit": 2,
	}
	data := SummaryData{FailuresByError: failures}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFailuresByError()

	assert.Len(t, result, 2)
	assert.Equal(t, 3, result["timeout"])
	assert.Equal(t, 2, result["rate_limit"])
}

func TestGetFormattedProviderTokens(t *testing.T) {
	providerStats := map[string]ProviderStat{
		"openai": {Tokens: 123456},
	}
	data := SummaryData{ProviderStats: providerStats}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFormattedProviderTokens("openai")
	assert.Equal(t, "123,456", result)

	result = vm.GetFormattedProviderTokens("nonexistent")
	assert.Equal(t, "0", result)
}

func TestGetFormattedProviderCost(t *testing.T) {
	providerCosts := map[string]float64{
		"openai": 1.5678,
	}
	data := SummaryData{ProviderCosts: providerCosts}
	vm := NewSummaryViewModel(&data)

	result := vm.GetFormattedProviderCost("openai")
	assert.Equal(t, "$1.5678", result)

	result = vm.GetFormattedProviderCost("nonexistent")
	assert.Equal(t, "$0.0000", result)
}

func TestSummaryViewModel_CompleteScenario(t *testing.T) {
	data := SummaryData{
		TotalRuns:     20,
		CompletedRuns: 18,
		FailedRuns:    2,
		TotalTokens:   5000000,
		TotalCost:     25.50,
		TotalDuration: 10 * time.Minute,
		AvgDuration:   30 * time.Second,
		ProviderStats: map[string]ProviderStat{
			"openai":    {Runs: 10, Tokens: 3000000},
			"anthropic": {Runs: 10, Tokens: 2000000},
		},
		ProviderCosts: map[string]float64{
			"openai":    15.00,
			"anthropic": 10.50,
		},
		FailuresByError: map[string]int{
			"timeout":    1,
			"rate_limit": 1,
		},
	}

	vm := NewSummaryViewModel(&data)

	// Test all formatted outputs
	assert.Equal(t, "5,000,000", vm.GetFormattedTotalTokens())
	assert.Contains(t, vm.GetFormattedTotalDuration(), "10m")
	assert.Contains(t, vm.GetFormattedAvgDuration(), "30s")
	assert.Equal(t, "$25.5000", vm.GetFormattedTotalCost())
	assert.Equal(t, "90.0%", vm.GetSuccessRate())
	assert.Equal(t, "10.0%", vm.GetFailureRate())

	// Test provider-specific data
	assert.Equal(t, "3,000,000", vm.GetFormattedProviderTokens("openai"))
	assert.Equal(t, "$15.0000", vm.GetFormattedProviderCost("openai"))

	// Test raw data accessors
	assert.Equal(t, 20, vm.GetTotalRuns())
	assert.Equal(t, 18, vm.GetCompletedRuns())
	assert.Equal(t, 2, vm.GetFailedRuns())
	assert.Len(t, vm.GetProviderStats(), 2)
	assert.Len(t, vm.GetProviderCosts(), 2)
	assert.Len(t, vm.GetFailuresByError(), 2)
}
