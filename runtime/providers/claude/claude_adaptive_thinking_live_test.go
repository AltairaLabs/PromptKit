//go:build integration

package claude

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestClaude_ThinkingShapePerGeneration_Live pins the split against the live
// API, in both directions.
//
// Sending the wrong shape is a hard 400: current models reject
// thinking.type.enabled, and 4.5-and-older reject adaptive. A unit test can
// only check what we build — only a live call proves the API accepts it.
func TestClaude_ThinkingShapePerGeneration_Live(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	cases := []struct {
		model     string
		wantShape string
	}{
		{"claude-sonnet-5", thinkingTypeAdaptive},
		{"claude-opus-5", thinkingTypeAdaptive},
		{"claude-sonnet-4-6", thinkingTypeAdaptive},
		{"claude-sonnet-4-5", thinkingTypeEnabled},
		{"claude-haiku-4-5", thinkingTypeEnabled},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			budget := 2048
			p := NewProvider(tc.model, tc.model, "https://api.anthropic.com/v1",
				providers.ProviderDefaults{MaxTokens: 4096}, false)
			p.thinkingBudget = &budget
			defer func() { _ = p.Close() }()

			require.Equal(t, tc.wantShape, p.claudeThinkingFor().Type,
				"%s must use the shape its generation accepts", tc.model)

			resp, err := p.Predict(context.Background(), providers.PredictionRequest{
				Messages: []types.Message{{
					Role:    "user",
					Content: "A bat and a ball cost $1.10; the bat is $1.00 more. Ball price?",
				}},
				MaxTokens: 4096,
			})
			require.NoErrorf(t, err,
				"%s rejected our thinking config; the shape for this generation changed", tc.model)
			assert.NotEmpty(t, resp.Content, "%s produced no answer", tc.model)
			t.Logf("%s: shape=%s reasoning=%v answer=%.50q",
				tc.model, tc.wantShape, resp.Reasoning != nil, strings.TrimSpace(resp.Content))
		})
	}
}
