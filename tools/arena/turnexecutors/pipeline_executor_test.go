package turnexecutors

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
)

func TestConvertTruncationStrategy(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		want     middleware.TruncationStrategy
	}{
		{
			name:     "truncate oldest",
			strategy: "truncate_oldest",
			want:     middleware.TruncateOldest,
		},
		{
			name:     "relevance",
			strategy: "relevance",
			want:     middleware.TruncateLeastRelevant,
		},
		{
			name:     "summarize",
			strategy: "summarize",
			want:     middleware.TruncateSummarize,
		},
		{
			name:     "fail",
			strategy: "fail",
			want:     middleware.TruncateFail,
		},
		{
			name:     "empty string defaults to oldest",
			strategy: "",
			want:     middleware.TruncateOldest,
		},
		{
			name:     "unknown strategy defaults to oldest",
			strategy: "unknown_strategy",
			want:     middleware.TruncateOldest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTruncationStrategy(tt.strategy)
			if got != tt.want {
				t.Errorf("convertTruncationStrategy(%q) = %v, want %v", tt.strategy, got, tt.want)
			}
		})
	}
}

func TestBuildBaseVariables(t *testing.T) {
	tests := []struct {
		name   string
		region string
		want   map[string]string
	}{
		{
			name:   "with region",
			region: "us-east-1",
			want:   map[string]string{"region": "us-east-1"},
		},
		{
			name:   "empty region returns empty map",
			region: "",
			want:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBaseVariables(tt.region)
			if len(got) != len(tt.want) {
				t.Errorf("buildBaseVariables() returned %d vars, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("buildBaseVariables()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestPipelineExecutor_BuildBaseVariables tests the buildBaseVariables method with PromptVars
func TestPipelineExecutor_BuildBaseVariables(t *testing.T) {
	executor := NewPipelineExecutor(nil)

	tests := []struct {
		name         string
		req          TurnRequest
		expectedVars map[string]string
	}{
		{
			name: "prompt vars only",
			req: TurnRequest{
				PromptVars: map[string]string{
					"restaurant_name": "Sushi Haven",
					"cuisine_type":    "Japanese",
				},
				Region: "",
			},
			expectedVars: map[string]string{
				"restaurant_name": "Sushi Haven",
				"cuisine_type":    "Japanese",
			},
		},
		{
			name: "region only",
			req: TurnRequest{
				PromptVars: nil,
				Region:     "us",
			},
			expectedVars: map[string]string{
				"region": "us",
			},
		},
		{
			name: "prompt vars and region - both included",
			req: TurnRequest{
				PromptVars: map[string]string{
					"restaurant_name": "Sushi Haven",
					"cuisine_type":    "Japanese",
				},
				Region: "us",
			},
			expectedVars: map[string]string{
				"restaurant_name": "Sushi Haven",
				"cuisine_type":    "Japanese",
				"region":          "us",
			},
		},
		{
			name: "prompt vars override region",
			req: TurnRequest{
				PromptVars: map[string]string{
					"restaurant_name": "Sushi Haven",
					"region":          "uk", // Override region
				},
				Region: "us", // Should not be used
			},
			expectedVars: map[string]string{
				"restaurant_name": "Sushi Haven",
				"region":          "uk", // PromptVars takes precedence
			},
		},
		{
			name: "empty request",
			req: TurnRequest{
				PromptVars: nil,
				Region:     "",
			},
			expectedVars: map[string]string{},
		},
		{
			name: "complex vars with special characters",
			req: TurnRequest{
				PromptVars: map[string]string{
					"restaurant_name": "Sushi Haven",
					"business_hours":  "12 PM - 11 PM, closed Mondays",
					"special_offer":   "10% off for seniors & students!",
				},
				Region: "us",
			},
			expectedVars: map[string]string{
				"restaurant_name": "Sushi Haven",
				"business_hours":  "12 PM - 11 PM, closed Mondays",
				"special_offer":   "10% off for seniors & students!",
				"region":          "us",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.buildBaseVariables(tt.req)

			if len(result) != len(tt.expectedVars) {
				t.Errorf("Expected %d variables, got %d", len(tt.expectedVars), len(result))
			}

			for key, expectedVal := range tt.expectedVars {
				if actualVal, ok := result[key]; !ok {
					t.Errorf("Missing variable %s", key)
				} else if actualVal != expectedVal {
					t.Errorf("Variable %s: expected %q, got %q", key, expectedVal, actualVal)
				}
			}
		})
	}
}
