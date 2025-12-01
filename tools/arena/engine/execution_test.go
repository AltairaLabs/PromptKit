package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestEngine_ResolveRegions(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name     string
		filter   []string
		expected []string
	}{
		{
			name:     "empty filter returns default",
			filter:   []string{},
			expected: []string{"default"},
		},
		{
			name:     "nil filter returns default",
			filter:   nil,
			expected: []string{"default"},
		},
		{
			name:     "single region",
			filter:   []string{"us-west-1"},
			expected: []string{"us-west-1"},
		},
		{
			name:     "multiple regions",
			filter:   []string{"us-west-1", "eu-west-1", "ap-south-1"},
			expected: []string{"us-west-1", "eu-west-1", "ap-south-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.resolveRegions(tt.filter)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEngine_ResolveScenarios(t *testing.T) {
	tests := []struct {
		name          string
		scenarios     map[string]*config.Scenario
		filter        []string
		expectedIDs   []string
		expectError   bool
		checkContains bool // For unordered results from map iteration
	}{
		{
			name: "empty filter returns all scenarios",
			scenarios: map[string]*config.Scenario{
				"scenario-1": {},
				"scenario-2": {},
				"scenario-3": {},
			},
			filter:        []string{},
			expectedIDs:   []string{"scenario-1", "scenario-2", "scenario-3"},
			checkContains: true,
		},
		{
			name: "nil filter returns all scenarios",
			scenarios: map[string]*config.Scenario{
				"test-1": {},
				"test-2": {},
			},
			filter:        nil,
			expectedIDs:   []string{"test-1", "test-2"},
			checkContains: true,
		},
		{
			name: "single scenario filter",
			scenarios: map[string]*config.Scenario{
				"scenario-1": {},
				"scenario-2": {},
			},
			filter:      []string{"scenario-1"},
			expectedIDs: []string{"scenario-1"},
		},
		{
			name: "multiple scenario filter",
			scenarios: map[string]*config.Scenario{
				"scenario-1": {},
				"scenario-2": {},
				"scenario-3": {},
			},
			filter:      []string{"scenario-1", "scenario-3"},
			expectedIDs: []string{"scenario-1", "scenario-3"},
		},
		{
			name:        "empty scenarios with empty filter",
			scenarios:   map[string]*config.Scenario{},
			filter:      []string{},
			expectedIDs: nil, // Empty map iteration returns nil slice
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{
				scenarios: tt.scenarios,
			}

			result, err := e.resolveScenarios(tt.filter)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.checkContains {
				// For map iteration, check length and contains
				assert.Equal(t, len(tt.expectedIDs), len(result))
				for _, expected := range tt.expectedIDs {
					assert.Contains(t, result, expected)
				}
			} else {
				assert.Equal(t, tt.expectedIDs, result)
			}
		})
	}
}

func TestEngine_GenerateCombinations_EmptyInputs(t *testing.T) {
	tests := []struct {
		name           string
		regions        []string
		scenarioIDs    []string
		providerFilter []string
		scenarios      map[string]*config.Scenario
		expectedCount  int
	}{
		{
			name:           "empty regions produces no combinations",
			regions:        []string{},
			scenarioIDs:    []string{"scenario-1"},
			providerFilter: []string{"openai"},
			scenarios: map[string]*config.Scenario{
				"scenario-1": {},
			},
			expectedCount: 0,
		},
		{
			name:           "empty scenarios produces no combinations",
			regions:        []string{"us-west-1"},
			scenarioIDs:    []string{},
			providerFilter: []string{"openai"},
			scenarios:      map[string]*config.Scenario{},
			expectedCount:  0,
		},
		{
			name:           "both empty produces no combinations",
			regions:        []string{},
			scenarioIDs:    []string{},
			providerFilter: []string{"openai"},
			scenarios:      map[string]*config.Scenario{},
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{
				scenarios: tt.scenarios,
			}

			result, err := e.generateCombinations(tt.regions, tt.scenarioIDs, tt.providerFilter)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, len(result))
		})
	}
}

func TestEngine_IntersectProviders(t *testing.T) {
	e := &Engine{}
	scenarioProviders := []string{"p1", "p2", "p3"}
	filter := []string{"p2", "p3", "p4"}

	got := e.intersectProviders(scenarioProviders, filter)
	require.Equal(t, []string{"p2", "p3"}, got)
}
