package results

import (
"testing"
"time"

"github.com/stretchr/testify/assert"
)

func TestResultMetadata_MatchesFilter(t *testing.T) {
	baseTime := time.Date(2025, 12, 1, 12, 0, 0, 0, time.UTC)
	
	tests := []struct {
		name     string
		metadata ResultMetadata
		filter   ResultFilter
		expected bool
	}{
		{
			name: "empty filter matches all",
			metadata: ResultMetadata{
				RunID:    "run-1",
				Scenario: "test-scenario",
				Provider: "openai",
				Status:   "success",
			},
			filter:   ResultFilter{},
			expected: true,
		},
		{
			name: "scenario filter matches",
			metadata: ResultMetadata{
				Scenario: "test-scenario",
			},
			filter: ResultFilter{
				Scenarios: []string{"test-scenario", "other-scenario"},
			},
			expected: true,
		},
		{
			name: "scenario filter does not match",
			metadata: ResultMetadata{
				Scenario: "test-scenario",
			},
			filter: ResultFilter{
				Scenarios: []string{"other-scenario"},
			},
			expected: false,
		},
		{
			name: "provider filter matches",
			metadata: ResultMetadata{
				Provider: "openai",
			},
			filter: ResultFilter{
				Providers: []string{"openai", "anthropic"},
			},
			expected: true,
		},
		{
			name: "provider filter does not match",
			metadata: ResultMetadata{
				Provider: "openai",
			},
			filter: ResultFilter{
				Providers: []string{"anthropic"},
			},
			expected: false,
		},
		{
			name: "region filter matches",
			metadata: ResultMetadata{
				Region: "us-east-1",
			},
			filter: ResultFilter{
				Regions: []string{"us-east-1", "us-west-2"},
			},
			expected: true,
		},
		{
			name: "status filter matches",
			metadata: ResultMetadata{
				Status: "success",
			},
			filter: ResultFilter{
				Status: []string{"success"},
			},
			expected: true,
		},
		{
			name: "status filter does not match",
			metadata: ResultMetadata{
				Status: "success",
			},
			filter: ResultFilter{
				Status: []string{"failed"},
			},
			expected: false,
		},
		{
			name: "start date filter matches",
			metadata: ResultMetadata{
				StartTime: baseTime,
			},
			filter: ResultFilter{
				StartDate: ptrTime(baseTime.Add(-1 * time.Hour)),
			},
			expected: true,
		},
		{
			name: "start date filter does not match",
			metadata: ResultMetadata{
				StartTime: baseTime,
			},
			filter: ResultFilter{
				StartDate: ptrTime(baseTime.Add(1 * time.Hour)),
			},
			expected: false,
		},
		{
			name: "end date filter matches",
			metadata: ResultMetadata{
				StartTime: baseTime,
			},
			filter: ResultFilter{
				EndDate: ptrTime(baseTime.Add(1 * time.Hour)),
			},
			expected: true,
		},
		{
			name: "end date filter does not match",
			metadata: ResultMetadata{
				StartTime: baseTime,
			},
			filter: ResultFilter{
				EndDate: ptrTime(baseTime.Add(-1 * time.Hour)),
			},
			expected: false,
		},
		{
			name: "multiple filters all match",
			metadata: ResultMetadata{
				Scenario:  "test-scenario",
				Provider:  "openai",
				Region:    "us-east-1",
				Status:    "success",
				StartTime: baseTime,
			},
			filter: ResultFilter{
				Scenarios: []string{"test-scenario"},
				Providers: []string{"openai"},
				Regions:   []string{"us-east-1"},
				Status:    []string{"success"},
				StartDate: ptrTime(baseTime.Add(-1 * time.Hour)),
				EndDate:   ptrTime(baseTime.Add(1 * time.Hour)),
			},
			expected: true,
		},
		{
			name: "multiple filters one does not match",
			metadata: ResultMetadata{
				Scenario:  "test-scenario",
				Provider:  "openai",
				Region:    "us-east-1",
				Status:    "success",
				StartTime: baseTime,
			},
			filter: ResultFilter{
				Scenarios: []string{"test-scenario"},
				Providers: []string{"anthropic"}, // Does not match
				Regions:   []string{"us-east-1"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
result := tt.metadata.MatchesFilter(&tt.filter)
assert.Equal(t, tt.expected, result)
})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "item exists",
			slice:    []string{"a", "b", "c"},
			item:     "b",
			expected: true,
		},
		{
			name:     "item does not exist",
			slice:    []string{"a", "b", "c"},
			item:     "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "a",
			expected: false,
		},
		{
			name:     "single item match",
			slice:    []string{"a"},
			item:     "a",
			expected: true,
		},
		{
			name:     "single item no match",
			slice:    []string{"a"},
			item:     "b",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
result := contains(tt.slice, tt.item)
assert.Equal(t, tt.expected, result)
})
	}
}

// ptrTime is a helper to create a pointer to a time.Time
func ptrTime(t time.Time) *time.Time {
	return &t
}
