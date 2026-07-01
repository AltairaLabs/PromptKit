package turnexecutors

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// TestLoadGuardrailHooks_EmptyTaskType covers composition/workflow entry states,
// which have no prompt_task: guardrail loading must return no hooks quietly,
// without reaching LoadTemplate (which would fail "prompt not found" on the empty
// task and log a misleading warning). A nil-repository registry is safe here
// precisely because the early return means the repository is never touched.
func TestLoadGuardrailHooks_EmptyTaskType(t *testing.T) {
	req := &TurnRequest{
		PromptRegistry: prompt.NewRegistryWithRepository(nil),
		TaskType:       "",
	}
	if got := loadGuardrailHooks(req, nil); got != nil {
		t.Fatalf("expected no guardrail hooks for empty task type, got %d", len(got))
	}
}

// TestLoadGuardrailHooks_NoRegistry covers the other quiet no-op path.
func TestLoadGuardrailHooks_NoRegistry(t *testing.T) {
	if got := loadGuardrailHooks(&TurnRequest{TaskType: "chat"}, nil); got != nil {
		t.Fatalf("expected no guardrail hooks without a registry, got %d", len(got))
	}
}

// Note: mock provider detection covered in existing helpers test file

// Merge with existing tests below in this file

func TestConvertTruncationStrategy(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		want     stage.TruncationStrategy
	}{
		{
			name:     "truncate oldest",
			strategy: "truncate_oldest",
			want:     stage.TruncateOldest,
		},
		{
			name:     "relevance",
			strategy: "relevance",
			want:     stage.TruncateLeastRelevant,
		},
		{
			name:     "summarize",
			strategy: "summarize",
			want:     stage.TruncateSummarize,
		},
		{
			name:     "fail",
			strategy: "fail",
			want:     stage.TruncateFail,
		},
		{
			name:     "empty string defaults to oldest",
			strategy: "",
			want:     stage.TruncateOldest,
		},
		{
			name:     "unknown strategy defaults to oldest",
			strategy: "unknown_strategy",
			want:     stage.TruncateOldest,
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

// TestPipelineExecutor_BuildBaseVariables tests the buildBaseVariables function with PromptVars
func TestPipelineExecutor_BuildBaseVariables(t *testing.T) {
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
			// Build base variables from region
			baseVars := buildBaseVariables(tt.req.Region)
			// Merge with PromptVars (PromptVars take precedence)
			result := make(map[string]string)
			for k, v := range baseVars {
				result[k] = v
			}
			for k, v := range tt.req.PromptVars {
				result[k] = v
			}

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
