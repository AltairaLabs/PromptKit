package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// stageWithPromptPolicy builds a provider stage whose TurnState carries a
// loaded prompt declaring promptPolicy, as PromptAssemblyStage leaves it.
func stageWithPromptPolicy(
	t *testing.T, caller *pipeline.ToolPolicy, promptPolicy *prompt.ToolPolicyPack, toolNames ...string,
) *ProviderStage {
	t.Helper()
	registry := tools.NewRegistry()
	for _, name := range toolNames {
		require.NoError(t, registry.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: "test",
			InputSchema: json.RawMessage(`{"type": "object"}`),
			Mode:        "mock",
		}))
	}
	ts := core.NewTurnState()
	ts.Template = &prompt.Template{TaskType: "builder", ToolPolicy: promptPolicy}
	ts.AllowedTools = toolNames
	stage := NewProviderStageWithTurnState(
		mock.NewToolProvider("test", "model", false, nil), registry, caller, &ProviderConfig{}, nil, nil, ts,
	)
	// A round run outside a turn (executeToolCalls) is checked against the
	// tools a turn on this prompt would allow.
	stage.offerState().setAllowed(toolNames)
	return stage
}

// The prompt's tool_policy reaches the stage through TurnState — the path the
// SDK depends on, since it passes no policy of its own (#2104).
func TestProviderStage_PromptToolPolicyMaxRounds(t *testing.T) {
	prompt200 := &prompt.ToolPolicyPack{MaxRounds: packspec.Ptr(200)}

	t.Run("prompt alone lifts the default", func(t *testing.T) {
		assert.Equal(t, 200, stageWithPromptPolicy(t, nil, prompt200).getMaxRounds())
	})
	t.Run("a lower caller limit (composition max_steps) narrows it", func(t *testing.T) {
		assert.Equal(t, 5, stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxRounds: 5}, prompt200).getMaxRounds())
	})
	t.Run("a higher caller limit cannot widen it", func(t *testing.T) {
		p10 := &prompt.ToolPolicyPack{MaxRounds: packspec.Ptr(10)}
		assert.Equal(t, 10, stageWithPromptPolicy(t, &pipeline.ToolPolicy{MaxRounds: 30}, p10).getMaxRounds())
	})
	t.Run("no policy anywhere is the default", func(t *testing.T) {
		assert.Equal(t, DefaultMaxRounds, stageWithPromptPolicy(t, nil, nil).getMaxRounds())
	})
}

func TestProviderStage_PromptToolPolicyBlocklist(t *testing.T) {
	stage := stageWithPromptPolicy(t, nil,
		&prompt.ToolPolicyPack{Blocklist: []string{"delete_kit"}}, "delete_kit", "read_kit")
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: []string{"delete_kit", "read_kit"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	response := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
		{ID: "c1", Name: "delete_kit", Args: json.RawMessage(`{}`)},
		{ID: "c2", Name: "read_kit", Args: json.RawMessage(`{}`)},
	}}
	_, _, err = loop.afterRound(context.Background(),
		[]string{"delete_kit", "read_kit"}, &response, true, RoundRef{Round: 1})
	require.NoError(t, err)

	results := toolResultsByID(loop.messages)
	require.Contains(t, results, "c1")
	assert.Contains(t, results["c1"].Error, "blocked by policy")
	assert.Empty(t, results["c2"].Error, "a tool not on the blocklist still runs")
}

func TestProviderStage_PromptToolChoiceNoneSendsNoTools(t *testing.T) {
	// Control: the same prompt with tool_choice required offers its tool.
	required := stageWithPromptPolicy(t, nil, &prompt.ToolPolicyPack{ToolChoice: packspec.Ptr("required")}, "read_kit")
	offered, choice, err := required.buildProviderTools([]string{"read_kit"}, map[string]bool{})
	require.NoError(t, err)
	require.NotNil(t, offered)
	assert.Equal(t, "required", choice, "the prompt's tool_choice is what the provider is asked for")

	none := stageWithPromptPolicy(t, nil, &prompt.ToolPolicyPack{ToolChoice: packspec.Ptr("none")}, "read_kit")
	offered, choice, err = none.buildProviderTools([]string{"read_kit"}, map[string]bool{})
	require.NoError(t, err)
	assert.Nil(t, offered, "tool_choice none sends no tool declarations")
	assert.Empty(t, choice)
}

func TestToolLoop_MaxToolCallsPerTurn(t *testing.T) {
	stage := stageWithPromptPolicy(t, nil, &prompt.ToolPolicyPack{MaxToolCallsPerTurn: packspec.Ptr(3)}, "read_kit")
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: []string{"read_kit"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.NotNil(t, loop.providerTools)

	call := func(id string) types.MessageToolCall {
		return types.MessageToolCall{ID: id, Name: "read_kit", Args: json.RawMessage(`{"n":"` + id + `"}`)}
	}

	// Round 1: two calls, both within the budget of three.
	r1 := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{call("a"), call("b")}}
	done, _, err := loop.afterRound(context.Background(), []string{"read_kit"}, &r1, true, RoundRef{Round: 1})
	require.NoError(t, err)
	require.False(t, done)
	assert.NotNil(t, loop.providerTools, "budget not yet spent: tools still offered")

	// Round 2: two more calls; only one fits. The other is answered with a
	// rejection so every call still has a result, and tools are withdrawn.
	r2 := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{call("c"), call("d")}}
	done, _, err = loop.afterRound(context.Background(), []string{"read_kit"}, &r2, true, RoundRef{Round: 2})
	require.NoError(t, err)
	require.False(t, done)

	results := toolResultsByID(loop.messages)
	for _, id := range []string{"a", "b", "c"} {
		require.Contains(t, results, id)
		assert.Empty(t, results[id].Error, "call %s was within budget", id)
	}
	require.Contains(t, results, "d", "an over-budget call still gets a result")
	assert.Contains(t, results["d"].Error, "tool call limit")
	assert.Nil(t, loop.providerTools, "budget spent: the next round offers no tools")
}

func toolResultsByID(msgs []types.Message) map[string]*types.MessageToolResult {
	out := map[string]*types.MessageToolResult{}
	for i := range msgs {
		if r := msgs[i].ToolResult; r != nil {
			out[r.ID] = r
		}
	}
	return out
}

func (p *loopingToolProvider) SupportsStreaming() bool { return false }

func (p *loopingToolProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, offered providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if offered == nil {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	n := p.rounds.Add(1)
	calls := []types.MessageToolCall{{
		ID: fmt.Sprintf("c%d", n), Name: "echo", Args: json.RawMessage(fmt.Sprintf(`{"n":%d}`, n)),
	}}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}
