package claude

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Sending the wrong thinking shape is a hard 400, not a degraded response, so
// the split is pinned per generation. Verified live:
//
//	4.5 and older   enabled+budget accepted, adaptive 400
//	4.6             both accepted
//	5-series        adaptive accepted, enabled+budget 400

func thinkingFor(t *testing.T, model string, budget *int) *claudeThinking {
	t.Helper()
	p := &Provider{model: model, thinkingBudget: budget}
	return p.claudeThinkingFor()
}

func TestClaudeThinkingFor_AdaptiveForCurrentModels(t *testing.T) {
	budget := 2048
	for _, model := range []string{
		"claude-sonnet-5", "claude-opus-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-6",
		// An unrecognized model must get adaptive too: new releases should work
		// without waiting for a code change.
		"claude-sonnet-6", "claude-something-new",
	} {
		t.Run(model, func(t *testing.T) {
			th := thinkingFor(t, model, &budget)
			require.NotNil(t, th)
			assert.Equal(t, thinkingTypeAdaptive, th.Type)

			raw, err := json.Marshal(th)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "budget_tokens",
				"adaptive rejects budget_tokens, so it must not reach the wire")
		})
	}
}

func TestClaudeThinkingFor_LegacyShapeForOlderModels(t *testing.T) {
	budget := 2048
	for _, model := range []string{
		"claude-sonnet-4-5", "claude-haiku-4-5", "claude-opus-4-5",
		"claude-opus-4-1", "claude-3-5-sonnet-20241022",
	} {
		t.Run(model, func(t *testing.T) {
			th := thinkingFor(t, model, &budget)
			require.NotNil(t, th)
			assert.Equal(t, thinkingTypeEnabled, th.Type,
				"these models reject adaptive with a 400")
			assert.Equal(t, 2048, th.BudgetTokens)

			raw, err := json.Marshal(th)
			require.NoError(t, err)
			assert.Contains(t, string(raw), `"budget_tokens":2048`)
		})
	}
}

// TestClaudeThinkingFor_UnsetOmitsTheFieldEntirely pins the wire outcome: a
// caller who configured no budget must produce a request with no `thinking`
// field at all, not an empty one.
//
// Asserted on the marshaled request rather than a nil pointer, because a nil
// check alone would pass against a builder that never emits the field.
func TestClaudeThinkingFor_UnsetOmitsTheFieldEntirely(t *testing.T) {
	budget := 2048

	configured := &Provider{model: "claude-sonnet-5", thinkingBudget: &budget}
	raw, err := json.Marshal(configured.buildBaseRequest(providers.PredictionRequest{MaxTokens: 512}, nil))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"thinking"`, "a configured budget must reach the wire")
	assert.Contains(t, string(raw), thinkingTypeAdaptive)

	unset := &Provider{model: "claude-sonnet-5"}
	rawUnset, err := json.Marshal(unset.buildBaseRequest(providers.PredictionRequest{MaxTokens: 512}, nil))
	require.NoError(t, err)
	assert.NotContains(t, string(rawUnset), `"thinking"`,
		"no configured budget must omit the field, not send an empty one")
}

// TestBuildBaseRequest_AdaptiveDoesNotInflateMaxTokens pins a coupling that the
// legacy shape needs and adaptive must not inherit: max_tokens is raised to
// leave answer headroom above budget_tokens. With adaptive there is no budget,
// so the caller's max_tokens must be left exactly as configured.
func TestBuildBaseRequest_AdaptiveDoesNotInflateMaxTokens(t *testing.T) {
	budget := 2048

	adaptive := &Provider{model: "claude-sonnet-5", thinkingBudget: &budget}
	req := adaptive.buildBaseRequest(providers.PredictionRequest{MaxTokens: 512}, nil)
	assert.Equal(t, 512, req.MaxTokens, "adaptive has no budget to make room for")
	require.NotNil(t, req.Thinking)
	assert.Equal(t, thinkingTypeAdaptive, req.Thinking.Type)

	legacy := &Provider{model: "claude-sonnet-4-5", thinkingBudget: &budget}
	legacyReq := legacy.buildBaseRequest(providers.PredictionRequest{MaxTokens: 512}, nil)
	assert.Greater(t, legacyReq.MaxTokens, budget,
		"reasoning tokens count toward max_tokens, so the answer needs headroom")
}
