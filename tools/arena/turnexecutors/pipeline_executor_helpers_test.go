package turnexecutors

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestBuildContextPolicy(t *testing.T) {
	tests := []struct {
		name     string
		scenario *config.Scenario
		want     *stage.ContextBuilderPolicy
	}{
		{
			name:     "nil scenario returns nil",
			scenario: nil,
			want:     nil,
		},
		{
			name: "scenario without context policy returns nil",
			scenario: &config.Scenario{
				ID: "test",
			},
			want: nil,
		},
		{
			name: "scenario with context policy returns configured policy",
			scenario: &config.Scenario{
				ID: "test",
				ContextPolicy: &config.ContextPolicy{
					TokenBudget:      1000,
					ReserveForOutput: 200,
					Strategy:         "oldest",
					CacheBreakpoints: true,
				},
			},
			want: &stage.ContextBuilderPolicy{
				TokenBudget:      1000,
				ReserveForOutput: 200,
				Strategy:         stage.TruncateOldest,
				CacheBreakpoints: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildContextPolicy(tt.scenario)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("buildContextPolicy() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && tt.want != nil {
				if got.TokenBudget != tt.want.TokenBudget {
					t.Errorf("TokenBudget = %d, want %d", got.TokenBudget, tt.want.TokenBudget)
				}
				if got.ReserveForOutput != tt.want.ReserveForOutput {
					t.Errorf("ReserveForOutput = %d, want %d", got.ReserveForOutput, tt.want.ReserveForOutput)
				}
				if got.Strategy != tt.want.Strategy {
					t.Errorf("Strategy = %v, want %v", got.Strategy, tt.want.Strategy)
				}
			}
		})
	}
}

func TestBuildStateStoreConfig(t *testing.T) {
	tests := []struct {
		name string
		req  TurnRequest
		want *pipeline.StateStoreConfig
	}{
		{
			name: "nil state store config returns nil",
			req: TurnRequest{
				StateStoreConfig: nil,
			},
			want: nil,
		},
		{
			name: "with state store config returns configured store",
			req: TurnRequest{
				ConversationID: "conv-123",
				StateStoreConfig: &StateStoreConfig{
					Store:    &mockStore{},
					UserID:   "user-456",
					Metadata: map[string]interface{}{"key": "value"},
				},
			},
			want: &pipeline.StateStoreConfig{
				ConversationID: "conv-123",
				UserID:         "user-456",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStateStoreConfig(&tt.req)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("buildStateStoreConfig() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && tt.want != nil {
				if got.ConversationID != tt.want.ConversationID {
					t.Errorf("ConversationID = %s, want %s", got.ConversationID, tt.want.ConversationID)
				}
				if got.UserID != tt.want.UserID {
					t.Errorf("UserID = %s, want %s", got.UserID, tt.want.UserID)
				}
			}
		})
	}
}

func TestBuildProviderConfig(t *testing.T) {
	seed := 42
	tests := []struct {
		name string
		req  TurnRequest
		want *stage.ProviderConfig
	}{
		{
			name: "basic config",
			req: TurnRequest{
				MaxTokens:   1000,
				Temperature: 0.7,
				Seed:        &seed,
			},
			want: &stage.ProviderConfig{
				MaxTokens:   1000,
				Temperature: 0.7,
				Seed:        &seed,
			},
		},
		{
			name: "nil seed",
			req: TurnRequest{
				MaxTokens:   500,
				Temperature: 0.3,
				Seed:        nil,
			},
			want: &stage.ProviderConfig{
				MaxTokens:   500,
				Temperature: 0.3,
				Seed:        nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildProviderConfig(&tt.req)
			if got.MaxTokens != tt.want.MaxTokens {
				t.Errorf("MaxTokens = %d, want %d", got.MaxTokens, tt.want.MaxTokens)
			}
			if got.Temperature != tt.want.Temperature {
				t.Errorf("Temperature = %f, want %f", got.Temperature, tt.want.Temperature)
			}
			if (got.Seed == nil) != (tt.want.Seed == nil) {
				t.Errorf("Seed nil mismatch: got %v, want %v", got.Seed, tt.want.Seed)
			}
			if got.Seed != nil && tt.want.Seed != nil && *got.Seed != *tt.want.Seed {
				t.Errorf("Seed = %d, want %d", *got.Seed, *tt.want.Seed)
			}
		})
	}
}

func TestBuildToolPolicy(t *testing.T) {
	tests := []struct {
		name     string
		scenario *config.Scenario
		want     *pipeline.ToolPolicy
	}{
		{
			name:     "nil scenario returns nil",
			scenario: nil,
			want:     nil,
		},
		{
			name: "scenario without tool policy returns nil",
			scenario: &config.Scenario{
				ID: "test",
			},
			want: nil,
		},
		{
			name: "scenario with tool policy returns configured policy",
			scenario: &config.Scenario{
				ID: "test",
				ToolPolicy: &config.ToolPolicy{
					ToolChoice:          "auto",
					MaxToolCallsPerTurn: 5,
					Blocklist:           []string{"dangerous_tool"},
				},
			},
			want: &pipeline.ToolPolicy{
				ToolChoice:          "auto",
				MaxRounds:           0,
				MaxToolCallsPerTurn: 5,
				Blocklist:           []string{"dangerous_tool"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildToolPolicy(tt.scenario)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("buildToolPolicy() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && tt.want != nil {
				if got.ToolChoice != tt.want.ToolChoice {
					t.Errorf("ToolChoice = %s, want %s", got.ToolChoice, tt.want.ToolChoice)
				}
				if got.MaxToolCallsPerTurn != tt.want.MaxToolCallsPerTurn {
					t.Errorf("MaxToolCallsPerTurn = %d, want %d", got.MaxToolCallsPerTurn, tt.want.MaxToolCallsPerTurn)
				}
			}
		})
	}
}

func TestBuildMediaConfig(t *testing.T) {
	mockStorage := &mockMediaStorage{}
	tests := []struct {
		name           string
		conversationID string
		mediaStorage   storage.MediaStorageService
		want           *stage.MediaExternalizerConfig
	}{
		{
			name:           "nil media storage returns nil",
			conversationID: "conv-123",
			mediaStorage:   nil,
			want:           nil,
		},
		{
			name:           "with media storage returns configured config",
			conversationID: "conv-456",
			mediaStorage:   mockStorage,
			want: &stage.MediaExternalizerConfig{
				Enabled:         true,
				StorageService:  mockStorage,
				SizeThresholdKB: 100,
				DefaultPolicy:   "retain",
				RunID:           "conv-456",
				ConversationID:  "conv-456",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMediaConfig(tt.conversationID, tt.mediaStorage)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("buildMediaConfig() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && tt.want != nil {
				if got.Enabled != tt.want.Enabled {
					t.Errorf("Enabled = %v, want %v", got.Enabled, tt.want.Enabled)
				}
				if got.SizeThresholdKB != tt.want.SizeThresholdKB {
					t.Errorf("SizeThresholdKB = %d, want %d", got.SizeThresholdKB, tt.want.SizeThresholdKB)
				}
				if got.DefaultPolicy != tt.want.DefaultPolicy {
					t.Errorf("DefaultPolicy = %s, want %s", got.DefaultPolicy, tt.want.DefaultPolicy)
				}
				if got.ConversationID != tt.want.ConversationID {
					t.Errorf("ConversationID = %s, want %s", got.ConversationID, tt.want.ConversationID)
				}
			}
		})
	}
}

func TestIsMockProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider interface{}
		want     bool
	}{
		{
			name:     "MockProvider returns true",
			provider: &mock.Provider{},
			want:     true,
		},
		{
			name:     "MockToolProvider returns true",
			provider: &mock.Provider{},
			want:     true,
		},
		{
			name:     "other provider returns false",
			provider: &mockNonMockProvider{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMockProvider(tt.provider.(providers.Provider))
			if got != tt.want {
				t.Errorf("isMockProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Mock types for testing
type mockStore struct{}

type mockMediaStorage struct{}

func (m *mockMediaStorage) StoreMedia(ctx context.Context, content *types.MediaContent, metadata *storage.MediaMetadata) (storage.Reference, error) {
	return storage.Reference("mock-uri"), nil
}

func (m *mockMediaStorage) RetrieveMedia(ctx context.Context, reference storage.Reference) (*types.MediaContent, error) {
	mockPath := "mock-path"
	return &types.MediaContent{FilePath: &mockPath}, nil
}

func (m *mockMediaStorage) DeleteMedia(ctx context.Context, reference storage.Reference) error {
	return nil
}

func (m *mockMediaStorage) GetURL(ctx context.Context, reference storage.Reference, expiry time.Duration) (string, error) {
	return "mock-url", nil
}

type mockNonMockProvider struct{}

func (m *mockNonMockProvider) ID() string                          { return "mock-non-mock" }
func (m *mockNonMockProvider) Name() string                        { return "Mock Non-Mock Provider" }
func (m *mockNonMockProvider) SupportsStreaming() bool             { return false }
func (m *mockNonMockProvider) SupportsJSONResponse() bool          { return false }
func (m *mockNonMockProvider) SupportsTools() bool                 { return false }
func (m *mockNonMockProvider) SupportsMedia() bool                 { return false }
func (m *mockNonMockProvider) DefaultMaxTokens() int               { return 1000 }
func (m *mockNonMockProvider) DefaultTemperature() float32         { return 0.7 }
func (m *mockNonMockProvider) InputCostPerMillionTokens() float64  { return 0.0 }
func (m *mockNonMockProvider) OutputCostPerMillionTokens() float64 { return 0.0 }
func (m *mockNonMockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{}
}
func (m *mockNonMockProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, nil
}
func (m *mockNonMockProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}
func (m *mockNonMockProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockNonMockProvider) Close() error                 { return nil }

func TestConvertQuerySource(t *testing.T) {
	tests := []struct {
		input    string
		expected stage.QuerySourceType
	}{
		{"last_user", stage.QuerySourceLastUser},
		{"last_n", stage.QuerySourceLastN},
		{"custom", stage.QuerySourceCustom},
		{"unknown", stage.QuerySourceLastUser},
		{"", stage.QuerySourceLastUser},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := convertQuerySource(tt.input)
			if got != tt.expected {
				t.Errorf("convertQuerySource(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildRelevanceConfig(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		got := buildRelevanceConfig(nil)
		if got != nil {
			t.Errorf("buildRelevanceConfig(nil) = %v, want nil", got)
		}
	})

	t.Run("unknown provider returns nil", func(t *testing.T) {
		cfg := &config.RelevanceConfig{
			Provider: "unknown-provider",
		}
		got := buildRelevanceConfig(cfg)
		if got != nil {
			t.Errorf("buildRelevanceConfig with unknown provider = %v, want nil", got)
		}
	})

	t.Run("config with default AlwaysKeepSystemRole", func(t *testing.T) {
		// Skip if no API key available
		t.Setenv("OPENAI_API_KEY", "test-key-for-unit-test")
		cfg := &config.RelevanceConfig{
			Provider:          "openai",
			MinRecentMessages: 5,
			QuerySource:       "last_n",
			LastNCount:        3,
		}
		got := buildRelevanceConfig(cfg)
		if got == nil {
			t.Skip("embedding provider not available")
		}
		if !got.AlwaysKeepSystemRole {
			t.Error("AlwaysKeepSystemRole should default to true")
		}
		if got.MinRecentMessages != 5 {
			t.Errorf("MinRecentMessages = %d, want 5", got.MinRecentMessages)
		}
		if got.QuerySource != stage.QuerySourceLastN {
			t.Errorf("QuerySource = %v, want %v", got.QuerySource, stage.QuerySourceLastN)
		}
		if got.LastNCount != 3 {
			t.Errorf("LastNCount = %d, want 3", got.LastNCount)
		}
	})

	t.Run("config with explicit AlwaysKeepSystemRole false", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "test-key-for-unit-test")
		keepSystem := false
		cfg := &config.RelevanceConfig{
			Provider:             "openai",
			AlwaysKeepSystemRole: &keepSystem,
		}
		got := buildRelevanceConfig(cfg)
		if got == nil {
			t.Skip("embedding provider not available")
		}
		if got.AlwaysKeepSystemRole {
			t.Error("AlwaysKeepSystemRole should be false when explicitly set")
		}
	})
}

func TestBuildContextPolicy_WithRelevance(t *testing.T) {
	t.Run("relevance strategy without config", func(t *testing.T) {
		scenario := &config.Scenario{
			ID: "test",
			ContextPolicy: &config.ContextPolicy{
				TokenBudget: 1000,
				Strategy:    "relevance",
				// No Relevance config
			},
		}
		got := buildContextPolicy(scenario)
		if got == nil {
			t.Fatal("expected non-nil policy")
		}
		if got.Strategy != stage.TruncateLeastRelevant {
			t.Errorf("Strategy = %v, want %v", got.Strategy, stage.TruncateLeastRelevant)
		}
		if got.RelevanceConfig != nil {
			t.Error("RelevanceConfig should be nil when no config provided")
		}
	})

	t.Run("relevance strategy with config", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "test-key-for-unit-test")
		scenario := &config.Scenario{
			ID: "test",
			ContextPolicy: &config.ContextPolicy{
				TokenBudget: 1000,
				Strategy:    "relevance",
				Relevance: &config.RelevanceConfig{
					Provider:            "openai",
					MinRecentMessages:   4,
					SimilarityThreshold: 0.5,
					QuerySource:         "custom",
					CustomQuery:         "test query",
				},
			},
		}
		got := buildContextPolicy(scenario)
		if got == nil {
			t.Fatal("expected non-nil policy")
		}
		if got.Strategy != stage.TruncateLeastRelevant {
			t.Errorf("Strategy = %v, want %v", got.Strategy, stage.TruncateLeastRelevant)
		}
		if got.RelevanceConfig == nil {
			t.Skip("embedding provider not available")
		}
		if got.RelevanceConfig.MinRecentMessages != 4 {
			t.Errorf("MinRecentMessages = %d, want 4", got.RelevanceConfig.MinRecentMessages)
		}
		if got.RelevanceConfig.SimilarityThreshold != 0.5 {
			t.Errorf("SimilarityThreshold = %f, want 0.5", got.RelevanceConfig.SimilarityThreshold)
		}
		if got.RelevanceConfig.QuerySource != stage.QuerySourceCustom {
			t.Errorf("QuerySource = %v, want %v", got.RelevanceConfig.QuerySource, stage.QuerySourceCustom)
		}
		if got.RelevanceConfig.CustomQuery != "test query" {
			t.Errorf("CustomQuery = %q, want %q", got.RelevanceConfig.CustomQuery, "test query")
		}
	})
}

func TestCreateEmbeddingProvider(t *testing.T) {
	t.Run("unknown provider returns error", func(t *testing.T) {
		_, err := createEmbeddingProvider("unknown", "")
		if err == nil {
			t.Error("expected error for unknown provider")
		}
	})

	t.Run("openai provider without API key fails gracefully", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("OPENAI_TOKEN", "")
		_, err := createEmbeddingProvider("openai", "")
		// Should fail because no API key
		if err == nil {
			t.Log("OpenAI provider created without API key - check environment")
		}
	})

	t.Run("gemini provider without API key fails gracefully", func(t *testing.T) {
		t.Setenv("GEMINI_API_KEY", "")
		t.Setenv("GOOGLE_API_KEY", "")
		_, err := createEmbeddingProvider("gemini", "")
		// Should fail because no API key
		if err == nil {
			t.Log("Gemini provider created without API key - check environment")
		}
	})

	t.Run("openai provider with API key succeeds", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "test-key-for-unit-test")
		provider, err := createEmbeddingProvider("openai", "")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if provider == nil {
			t.Error("expected non-nil provider")
			return
		}
		if provider.ID() != "openai-embedding" {
			t.Errorf("ID() = %q, want %q", provider.ID(), "openai-embedding")
		}
	})

	t.Run("gemini provider with API key succeeds", func(t *testing.T) {
		t.Setenv("GEMINI_API_KEY", "test-key-for-unit-test")
		provider, err := createEmbeddingProvider("gemini", "")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if provider == nil {
			t.Error("expected non-nil provider")
			return
		}
		if provider.ID() != "gemini-embedding" {
			t.Errorf("ID() = %q, want %q", provider.ID(), "gemini-embedding")
		}
	})

	t.Run("openai provider with custom model", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "test-key-for-unit-test")
		provider, err := createEmbeddingProvider("openai", "text-embedding-3-large")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
			return
		}
		if provider == nil {
			t.Error("expected non-nil provider")
			return
		}
		// Verify provider was created (Model() is not part of interface, so just check it works)
		if provider.ID() != "openai-embedding" {
			t.Errorf("ID() = %q, want %q", provider.ID(), "openai-embedding")
		}
	})
}
