package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// thoughtful is an assistant reply that carries the model's reasoning.
func thoughtful() *types.Message {
	return &types.Message{
		Role: roleAssistant, Content: "the banned answer",
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
		(&outputVerdict{kind: kind, text: "blocked"}).enforce(msg)
		assert.Equal(t, "blocked", msg.Content)
		assert.Nil(t, msg.Reasoning, "verdict %d", kind)
	}
}

// A realtime turn a guardrail blocked drops its reasoning too, whether it
// completed or was cut short after the block.
func TestDuplexEnforcedTurn_DropsTheReasoning(t *testing.T) {
	msg := thoughtful()
	markEnforcedTurn(msg, &StreamElement{})
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	assert.Nil(t, msg.Reasoning)

	s := newDuplexStageForUnit()
	s.accumulatedText.WriteString("the banned")
	s.accumulatedReasoning.WriteString("reasoning toward it")
	s.turnGuard.blocked = true
	elem := s.chunkToElement(&providers.StreamChunk{Interrupted: true})
	require.NotNil(t, elem.Message)
	assert.Equal(t, types.FinishReasonSafety, elem.Message.FinishReason)
	assert.Nil(t, elem.Message.Reasoning)

	unblocked := newDuplexStageForUnit()
	unblocked.accumulatedReasoning.WriteString("still thinking")
	elem = unblocked.chunkToElement(&providers.StreamChunk{Interrupted: true})
	require.NotNil(t, elem.Message)
	assert.Equal(t, &types.ReasoningTrace{Text: "still thinking"}, elem.Message.Reasoning)
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
