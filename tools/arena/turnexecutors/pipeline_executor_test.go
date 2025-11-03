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
