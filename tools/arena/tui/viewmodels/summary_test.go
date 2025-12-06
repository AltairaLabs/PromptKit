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

func TestGetFormattedTotalRuns(t *testing.T) {
	data := SummaryData{TotalRuns: 42}
	vm := NewSummaryViewModel(&data)
	assert.Equal(t, "42", vm.GetFormattedTotalRuns())
}

func TestGetFormattedSuccessful(t *testing.T) {
	data := SummaryData{TotalRuns: 10, CompletedRuns: 8}
	vm := NewSummaryViewModel(&data)
	result := vm.GetFormattedSuccessful()
	assert.Contains(t, result, "8")
	assert.Contains(t, result, "80.0%")
}

func TestGetFormattedFailed(t *testing.T) {
	data := SummaryData{TotalRuns: 10, FailedRuns: 2}
	vm := NewSummaryViewModel(&data)
	result := vm.GetFormattedFailed()
	assert.Contains(t, result, "2")
	assert.Contains(t, result, "20.0%")
}

func TestGetFormattedAvgDurationWithSuffix(t *testing.T) {
	data := SummaryData{AvgDuration: 3 * time.Second}
	vm := NewSummaryViewModel(&data)
	result := vm.GetFormattedAvgDurationWithSuffix()
	assert.Contains(t, result, "per run")
	assert.Contains(t, result, "3s")
}

func TestHasAssertions(t *testing.T) {
	dataWithAssertions := SummaryData{AssertionTotal: 10}
	vmWith := NewSummaryViewModel(&dataWithAssertions)
	assert.True(t, vmWith.HasAssertions())

	dataWithoutAssertions := SummaryData{AssertionTotal: 0}
	vmWithout := NewSummaryViewModel(&dataWithoutAssertions)
	assert.False(t, vmWithout.HasAssertions())
}

func TestHasFailedAssertions(t *testing.T) {
	dataWithFailed := SummaryData{AssertionFailed: 5}
	vmWith := NewSummaryViewModel(&dataWithFailed)
	assert.True(t, vmWith.HasFailedAssertions())

	dataWithoutFailed := SummaryData{AssertionFailed: 0}
	vmWithout := NewSummaryViewModel(&dataWithoutFailed)
	assert.False(t, vmWithout.HasFailedAssertions())
}

func TestGetFormattedAssertionTotal(t *testing.T) {
	data := SummaryData{AssertionTotal: 25}
	vm := NewSummaryViewModel(&data)
	assert.Equal(t, "25 total", vm.GetFormattedAssertionTotal())
}

func TestGetFormattedAssertionFailed(t *testing.T) {
	data := SummaryData{AssertionFailed: 3}
	vm := NewSummaryViewModel(&data)
	assert.Equal(t, "3", vm.GetFormattedAssertionFailed())
}

func TestHasProviders(t *testing.T) {
	dataWith := SummaryData{
		ProviderStats: map[string]ProviderStat{"openai": {Runs: 5}},
	}
	vmWith := NewSummaryViewModel(&dataWith)
	assert.True(t, vmWith.HasProviders())

	dataWithout := SummaryData{ProviderStats: map[string]ProviderStat{}}
	vmWithout := NewSummaryViewModel(&dataWithout)
	assert.False(t, vmWithout.HasProviders())
}

func TestGetFormattedProviders(t *testing.T) {
	data := SummaryData{
		ProviderStats: map[string]ProviderStat{
			"openai": {Runs: 5, Tokens: 1000},
			"claude": {Runs: 3, Tokens: 500},
		},
	}
	vm := NewSummaryViewModel(&data)
	result := vm.GetFormattedProviders()
	assert.Contains(t, result, "openai")
	assert.Contains(t, result, "claude")
	assert.Contains(t, result, "(5)")
	assert.Contains(t, result, "(3)")
}

func TestGetFormattedScenarios(t *testing.T) {
	data := SummaryData{ScenarioCount: 7}
	vm := NewSummaryViewModel(&data)
	assert.Equal(t, "7 scenarios", vm.GetFormattedScenarios())
}

func TestHasRegions(t *testing.T) {
	dataWith := SummaryData{Regions: []string{"us-east-1", "eu-west-1"}}
	vmWith := NewSummaryViewModel(&dataWith)
	assert.True(t, vmWith.HasRegions())

	dataWithout := SummaryData{Regions: []string{}}
	vmWithout := NewSummaryViewModel(&dataWithout)
	assert.False(t, vmWithout.HasRegions())
}

func TestGetFormattedRegions(t *testing.T) {
	data := SummaryData{Regions: []string{"us-east-1", "eu-west-1", "ap-south-1"}}
	vm := NewSummaryViewModel(&data)
	result := vm.GetFormattedRegions()
	assert.Contains(t, result, "us-east-1")
	assert.Contains(t, result, "eu-west-1")
	assert.Contains(t, result, "ap-south-1")
}

func TestHasErrors(t *testing.T) {
	dataWith := SummaryData{
		Errors: []ErrorInfo{{RunID: "r1", Error: "timeout"}},
	}
	vmWith := NewSummaryViewModel(&dataWith)
	assert.True(t, vmWith.HasErrors())

	dataWithout := SummaryData{Errors: []ErrorInfo{}}
	vmWithout := NewSummaryViewModel(&dataWithout)
	assert.False(t, vmWithout.HasErrors())
}

func TestGetFormattedErrors(t *testing.T) {
	data := SummaryData{
		Errors: []ErrorInfo{
			{
				RunID:    "run1",
				Scenario: "test-scenario",
				Provider: "openai",
				Region:   "us-east-1",
				Error:    "Connection    timeout\nRetry failed",
			},
		},
	}
	vm := NewSummaryViewModel(&data)
	errors := vm.GetFormattedErrors()
	assert.Len(t, errors, 1)
	assert.Contains(t, errors[0], "test-scenario/openai/us-east-1")
	assert.Contains(t, errors[0], "Connection timeout Retry failed")
	assert.NotContains(t, errors[0], "\n")
}

func TestGetOutputDir(t *testing.T) {
	data := SummaryData{OutputDir: "/tmp/results"}
	vm := NewSummaryViewModel(&data)
	assert.Equal(t, "/tmp/results", vm.GetOutputDir())
}

func TestHasHTMLReport(t *testing.T) {
	dataWith := SummaryData{HTMLReport: "/tmp/report.html"}
	vmWith := NewSummaryViewModel(&dataWith)
	assert.True(t, vmWith.HasHTMLReport())

	dataWithout := SummaryData{HTMLReport: ""}
	vmWithout := NewSummaryViewModel(&dataWithout)
	assert.False(t, vmWithout.HasHTMLReport())
}

func TestGetHTMLReport(t *testing.T) {
	data := SummaryData{HTMLReport: "/tmp/report.html"}
	vm := NewSummaryViewModel(&data)
	assert.Equal(t, "/tmp/report.html", vm.GetHTMLReport())
}

func TestCompactString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "with newlines",
			input:    "Hello\nWorld\nTest",
			expected: "Hello World Test",
		},
		{
			name:     "with tabs",
			input:    "Hello\tWorld\tTest",
			expected: "Hello World Test",
		},
		{
			name:     "with multiple spaces",
			input:    "Hello    World     Test",
			expected: "Hello World Test",
		},
		{
			name:     "mixed whitespace",
			input:    "Hello\n\t  World   \n  Test  ",
			expected: "Hello World Test",
		},
		{
			name:     "already compact",
			input:    "Hello World Test",
			expected: "Hello World Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compactString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
