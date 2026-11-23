package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// thoughtful is an assistant reply that carries the model's reasoning.
func thoughtful() *types.Message {
	return &types.Message{
		Role: core.RoleAssistant, Content: "the banned answer",
		Reasoning: &types.ReasoningTrace{
			Text:   "reasoning toward the banned answer",
			Opaque: []types.OpaqueReasoning{{Provider: "claude", Kind: "thinking_signature", Data: "sig"}},
		},
	}
}

// A reply a guardrail blocked keeps none of the model's reasoning: the text
// path's enforcement leaves the guardrail's text and no reasoning, so the
// blocked reply's thinking is neither returned nor saved.
func TestOutputVerdictEnforce_DropsTheReasoning(t *testing.T) {
	for _, kind := range []verdictKind{verdictTruncate, verdictReplace, verdictDeny} {
		msg := thoughtful()
		(&OutputVerdict{kind: kind, Text: "blocked"}).Enforce(msg)
		assert.Equal(t, "blocked", msg.Content)
		assert.Nil(t, msg.Reasoning, "verdict %d", kind)
	}
}

// A call's cost names the provider and the model that served it, unless the
// provider named them itself.
func TestStampCostIdentity_NamesProviderAndModel(t *testing.T) {
	prov := mock.NewProvider("claude-main", "claude-model", false)

	stamped := &types.CostInfo{InputTokens: 3}
	stampCostIdentity(stamped, prov, 0)
	assert.Equal(t, "claude-main", stamped.ProviderName)
	assert.Equal(t, "claude-model", stamped.Model)

	own := &types.CostInfo{ProviderName: "own", Model: "own-model"}
	stampCostIdentity(own, prov, 0)
	assert.Equal(t, "own", own.ProviderName)
	assert.Equal(t, "own-model", own.Model)
}
