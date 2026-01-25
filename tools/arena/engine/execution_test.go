package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
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

func TestEngine_ApplyProviderFilter(t *testing.T) {
	e := &Engine{}

	t.Run("empty filter returns all", func(t *testing.T) {
		providers := []string{"p1", "p2", "p3"}
		result := e.applyProviderFilter(providers, []string{})
		assert.Equal(t, providers, result)
	})

	t.Run("filter matches subset", func(t *testing.T) {
		providers := []string{"p1", "p2", "p3"}
		filter := []string{"p1", "p3"}
		result := e.applyProviderFilter(providers, filter)
		assert.Equal(t, []string{"p1", "p3"}, result)
	})

	t.Run("filter with no matches", func(t *testing.T) {
		providers := []string{"p1", "p2"}
		filter := []string{"p3", "p4"}
		result := e.applyProviderFilter(providers, filter)
		assert.Empty(t, result)
	})
}

func TestEngine_GetInitialProviders(t *testing.T) {
	p1 := &config.Provider{ID: "p1"}
	p2 := &config.Provider{ID: "p2"}
	p3 := &config.Provider{ID: "p3"}

	e := &Engine{
		providers: map[string]*config.Provider{
			"p1": p1,
			"p2": p2,
			"p3": p3,
		},
		config: &config.Config{
			ProviderGroups: map[string]string{
				"p1": "group-a",
				"p2": "group-a",
				"p3": "group-b",
			},
		},
		providerRegistry: providers.NewRegistry(),
	}

	// Add providers to registry
	mockProv1 := mock.NewProvider("p1", "model", false)
	mockProv2 := mock.NewProvider("p2", "model", false)
	mockProv3 := mock.NewProvider("p3", "model", false)
	e.providerRegistry.Register(mockProv1)
	e.providerRegistry.Register(mockProv2)
	e.providerRegistry.Register(mockProv3)

	t.Run("scenario with explicit providers", func(t *testing.T) {
		scenario := &config.Scenario{
			Providers: []string{"p1", "p2"},
		}
		result := e.getInitialProviders(scenario, nil)
		assert.Equal(t, []string{"p1", "p2"}, result)
	})

	t.Run("scenario with provider group", func(t *testing.T) {
		scenario := &config.Scenario{
			ProviderGroup: "group-a",
		}
		result := e.getInitialProviders(scenario, nil)
		assert.Contains(t, result, "p1")
		assert.Contains(t, result, "p2")
		assert.NotContains(t, result, "p3")
	})

	t.Run("scenario with filter", func(t *testing.T) {
		scenario := &config.Scenario{}
		filter := []string{"p1"}
		result := e.getInitialProviders(scenario, filter)
		assert.Equal(t, []string{"p1"}, result)
	})
}

func TestEngine_ResolveProvidersForScenario(t *testing.T) {
	e := &Engine{
		providerRegistry: providers.NewRegistry(),
	}

	mockProv1 := mock.NewProvider("p1", "model", false)
	mockProv2 := mock.NewProvider("p2", "model", false)
	mockProv3 := mock.NewProvider("p3", "model", false)
	e.providerRegistry.Register(mockProv1)
	e.providerRegistry.Register(mockProv2)
	e.providerRegistry.Register(mockProv3)

	t.Run("scenario providers with filter intersection", func(t *testing.T) {
		scenario := &config.Scenario{
			Providers: []string{"p1", "p2", "p3"},
		}
		filter := []string{"p2", "p3"}
		result := e.resolveProvidersForScenario(scenario, filter)
		assert.Equal(t, []string{"p2", "p3"}, result)
	})

	t.Run("scenario providers without filter", func(t *testing.T) {
		scenario := &config.Scenario{
			Providers: []string{"p1", "p2"},
		}
		result := e.resolveProvidersForScenario(scenario, nil)
		assert.Equal(t, []string{"p1", "p2"}, result)
	})
}

// mockProviderRegistry for testing
type mockProviderRegistry struct {
	providers []string
}

func (m *mockProviderRegistry) List() []string {
	return m.providers
}

func (m *mockProviderRegistry) Get(id string) (interface{}, bool) {
	for _, p := range m.providers {
		if p == id {
			return nil, true
		}
	}
	return nil, false
}

func TestHasAllCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		have     []string
		need     []string
		expected bool
	}{
		{
			name:     "has all capabilities",
			have:     []string{"text", "streaming", "vision", "tools"},
			need:     []string{"text", "streaming"},
			expected: true,
		},
		{
			name:     "missing one capability",
			have:     []string{"text", "streaming"},
			need:     []string{"text", "streaming", "vision"},
			expected: false,
		},
		{
			name:     "empty need",
			have:     []string{"text", "streaming"},
			need:     []string{},
			expected: true,
		},
		{
			name:     "empty have",
			have:     []string{},
			need:     []string{"text"},
			expected: false,
		},
		{
			name:     "both empty",
			have:     []string{},
			need:     []string{},
			expected: true,
		},
		{
			name:     "exact match",
			have:     []string{"text", "streaming"},
			need:     []string{"text", "streaming"},
			expected: true,
		},
		{
			name:     "nil need",
			have:     []string{"text"},
			need:     nil,
			expected: true,
		},
		{
			name:     "nil have",
			have:     nil,
			need:     []string{"text"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAllCapabilities(tt.have, tt.need)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEngine_FilterByCapabilities(t *testing.T) {
	e := &Engine{
		config: &config.Config{
			ProviderCapabilities: map[string][]string{
				"openai-gpt4o":       {"text", "streaming", "vision", "tools", "json"},
				"openai-o1":          {"text", "streaming", "tools", "json"},
				"gemini-25-pro":      {"text", "streaming", "vision", "tools", "json", "audio", "video"},
				"anthropic-opus45":   {"text", "streaming", "vision", "tools", "json", "documents"},
				"mock":               {"text", "streaming", "tools", "json"},
			},
		},
	}

	tests := []struct {
		name      string
		providers []string
		required  []string
		expected  []string
	}{
		{
			name:      "filter by vision",
			providers: []string{"openai-gpt4o", "openai-o1", "gemini-25-pro", "mock"},
			required:  []string{"vision"},
			expected:  []string{"openai-gpt4o", "gemini-25-pro"},
		},
		{
			name:      "filter by streaming + tools",
			providers: []string{"openai-gpt4o", "openai-o1", "mock"},
			required:  []string{"streaming", "tools"},
			expected:  []string{"openai-gpt4o", "openai-o1", "mock"},
		},
		{
			name:      "filter by video",
			providers: []string{"openai-gpt4o", "openai-o1", "gemini-25-pro"},
			required:  []string{"video"},
			expected:  []string{"gemini-25-pro"},
		},
		{
			name:      "filter by documents",
			providers: []string{"openai-gpt4o", "anthropic-opus45", "mock"},
			required:  []string{"documents"},
			expected:  []string{"anthropic-opus45"},
		},
		{
			name:      "filter by vision + tools",
			providers: []string{"openai-gpt4o", "openai-o1", "gemini-25-pro", "mock"},
			required:  []string{"vision", "tools"},
			expected:  []string{"openai-gpt4o", "gemini-25-pro"},
		},
		{
			name:      "empty required returns all",
			providers: []string{"openai-gpt4o", "openai-o1"},
			required:  []string{},
			expected:  []string{"openai-gpt4o", "openai-o1"},
		},
		{
			name:      "no matches returns empty",
			providers: []string{"openai-gpt4o", "mock"},
			required:  []string{"video"},
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.filterByCapabilities(tt.providers, tt.required)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEngine_GetInitialProvidersWithCapabilities(t *testing.T) {
	p1 := &config.Provider{ID: "openai-gpt4o", Capabilities: []string{"text", "streaming", "vision", "tools"}}
	p2 := &config.Provider{ID: "openai-o1", Capabilities: []string{"text", "streaming", "tools"}}
	p3 := &config.Provider{ID: "gemini-25-pro", Capabilities: []string{"text", "streaming", "vision", "tools", "video"}}

	e := &Engine{
		providers: map[string]*config.Provider{
			"openai-gpt4o":  p1,
			"openai-o1":     p2,
			"gemini-25-pro": p3,
		},
		config: &config.Config{
			ProviderGroups: map[string]string{
				"openai-gpt4o":  "default",
				"openai-o1":     "default",
				"gemini-25-pro": "default",
			},
			ProviderCapabilities: map[string][]string{
				"openai-gpt4o":  {"text", "streaming", "vision", "tools"},
				"openai-o1":     {"text", "streaming", "tools"},
				"gemini-25-pro": {"text", "streaming", "vision", "tools", "video"},
			},
		},
		providerRegistry: providers.NewRegistry(),
	}

	// Add providers to registry
	mockProv1 := mock.NewProvider("openai-gpt4o", "gpt-4o", false)
	mockProv2 := mock.NewProvider("openai-o1", "o1", false)
	mockProv3 := mock.NewProvider("gemini-25-pro", "gemini-2.5-pro", false)
	e.providerRegistry.Register(mockProv1)
	e.providerRegistry.Register(mockProv2)
	e.providerRegistry.Register(mockProv3)

	t.Run("scenario with required capabilities filters providers", func(t *testing.T) {
		scenario := &config.Scenario{
			RequiredCapabilities: []string{"vision"},
		}
		result := e.getInitialProviders(scenario, nil)
		assert.Contains(t, result, "openai-gpt4o")
		assert.Contains(t, result, "gemini-25-pro")
		assert.NotContains(t, result, "openai-o1")
	})

	t.Run("scenario with multiple required capabilities", func(t *testing.T) {
		scenario := &config.Scenario{
			RequiredCapabilities: []string{"vision", "video"},
		}
		result := e.getInitialProviders(scenario, nil)
		assert.Equal(t, []string{"gemini-25-pro"}, result)
	})

	t.Run("scenario without required capabilities returns all in group", func(t *testing.T) {
		scenario := &config.Scenario{}
		result := e.getInitialProviders(scenario, nil)
		assert.Contains(t, result, "openai-gpt4o")
		assert.Contains(t, result, "openai-o1")
		assert.Contains(t, result, "gemini-25-pro")
	})
}
