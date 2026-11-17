package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// turnRunner returns a tool runner over a registry holding a "danger" tool,
// under policy.
func turnRunner(t *testing.T, policy *pipeline.ToolPolicy) *toolRunner {
	t.Helper()
	registry := tools.NewRegistry()
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "danger",
		Description: "a tool the policy refuses",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "mock",
	}))
	return &toolRunner{registry: registry, policy: func() *pipeline.ToolPolicy { return policy }}
}

func dangerCall(id string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "danger", Args: json.RawMessage(`{}`)}
}

// policyRefusal is a tool result refusing a call for a policy reason.
func policyRefusal(id, name, text string) types.Message {
	result := types.NewTextToolResult(id, name, text)
	result.Error = text
	result.ErrorType = types.ToolErrorApproval
	return types.NewToolResultMessage(result)
}

func TestToolTurn_RunRound_BlocklistedCallRefused(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{Blocklist: []string{"danger"}}).newTurn(nil, 10)

	out := turn.RunRound(context.Background(), []types.MessageToolCall{dangerCall("c1")}, roundRef{round: 1})

	assert.Equal(t, roundOutcome{
		Results: []types.Message{policyRefusal("c1", "danger", "Tool danger is blocked by policy")},
	}, out)
}

// The round's results are the ones ProviderStage appends for the same call.
func TestToolTurn_RunRound_BlocklistedMatchesProviderStage(t *testing.T) {
	policy := &pipeline.ToolPolicy{Blocklist: []string{"danger"}}
	runner := turnRunner(t, policy)
	stage := NewProviderStage(mock.NewToolProvider("test", "model", false, nil), runner.registry, policy,
		&ProviderConfig{})
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: []string{"danger"},
		messages:     []types.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)
	response := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{dangerCall("c1")}}
	_, _, err = loop.afterRound(context.Background(), []string{"danger"}, &response, true, roundRef{round: 1})
	require.NoError(t, err)

	out := runner.newTurn(nil, 10).RunRound(context.Background(), response.ToolCalls, roundRef{round: 1})

	assert.Equal(t, loop.messages[2:], out.Results)
}

func TestToolTurn_RunRound_PerTurnCallCapRefusesOverflow(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{
		Blocklist:           []string{"danger"},
		MaxToolCallsPerTurn: 1,
	}).newTurn(nil, 10)

	out := turn.RunRound(context.Background(),
		[]types.MessageToolCall{dangerCall("c1"), dangerCall("c2")}, roundRef{round: 1})

	assert.Equal(t, roundOutcome{
		Results: []types.Message{
			policyRefusal("c1", "danger", "Tool danger is blocked by policy"),
			policyRefusal("c2", "danger", "Tool danger not executed: the tool call limit for this turn is reached"),
		},
		// Both results are rejections of "danger", so it is excluded too.
		OfferChanged:   true,
		OfferExhausted: true,
	}, out)
}

// Without a registry, a round with calls fails the turn at once: there is
// nothing to run the calls on.
func TestToolTurn_RunRound_NilRegistryFailsTheTurn(t *testing.T) {
	runner := &toolRunner{policy: func() *pipeline.ToolPolicy { return nil }}

	out := runner.newTurn(nil, 10).RunRound(context.Background(), []types.MessageToolCall{dangerCall("c1")},
		roundRef{round: 1})

	require.Error(t, out.Err)
	assert.Equal(t, "provider stage: tool execution failed: tool registry not configured but tool calls present",
		out.Err.Error())
	assert.False(t, out.Stop)
	assert.Empty(t, out.Results)
}

// A runner its stage gave no offer to check against runs a registered tool.
func TestToolTurn_RunRound_RunnerWithoutEligibilityIsNotGated(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{}).newTurn(nil, 10)

	out := turn.RunRound(context.Background(), []types.MessageToolCall{dangerCall("c1")}, roundRef{round: 1})

	assert.Equal(t, []types.Message{
		types.NewToolResultMessage(types.NewTextToolResult("c1", "danger", "{\n  \"result\": \"mock response\"\n}")),
	}, out.Results, "the mock tool ran and returned its output")
}

func TestToolTurn_RunRound_IdenticalCallBreaker(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{
		Blocklist:             []string{"danger"},
		MaxIdenticalToolCalls: 2,
	}).newTurn(nil, 10)
	ctx := context.Background()

	first := turn.RunRound(ctx, []types.MessageToolCall{dangerCall("c1")}, roundRef{round: 1})
	assert.Equal(t, roundOutcome{
		Results: []types.Message{policyRefusal("c1", "danger", "Tool danger is blocked by policy")},
	}, first)

	nudge := turn.RunRound(ctx, []types.MessageToolCall{dangerCall("c2")}, roundRef{round: 2})
	assert.Equal(t, roundOutcome{
		Results: []types.Message{types.NewToolResultMessage(types.NewTextToolResult("c2", "danger",
			`Repeated call ignored: "danger" has been called 2 times with identical arguments and was NOT `+
				"executed again — the result will not change. Stop repeating this exact call: change the "+
				"arguments, use a different tool, or finish the task."))},
		Nudged: true,
	}, nudge)

	abort := turn.RunRound(ctx, []types.MessageToolCall{dangerCall("c3")}, roundRef{round: 3})
	require.Error(t, abort.Err)
	assert.Equal(t, "provider stage: tool loop persisted after a self-correction nudge: "+
		`"danger" called with identical arguments`, abort.Err.Error())
	assert.True(t, abort.Stop)
	assert.Empty(t, abort.Results)
}

func TestToolTurn_RunRound_MaxRoundsStops(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{Blocklist: []string{"danger"}}).newTurn(nil, 2)
	ctx := context.Background()

	first := turn.RunRound(ctx, []types.MessageToolCall{dangerCall("c1")}, roundRef{round: 1})
	assert.False(t, first.Stop)
	require.NoError(t, first.Err)

	last := turn.RunRound(ctx, []types.MessageToolCall{
		{ID: "c2", Name: "danger", Args: json.RawMessage(`{"n":2}`)},
	}, roundRef{round: 2})
	assert.Equal(t, roundOutcome{
		Results:      []types.Message{policyRefusal("c2", "danger", "Tool danger is blocked by policy")},
		Stop:         true,
		Err:          &MaxRoundsError{Rounds: 2},
		OfferChanged: true,
	}, last)
}

func TestToolTurn_RunRound_StopToolStopsWithoutError(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{Blocklist: []string{"danger"}, StopOnTool: "danger"}).
		newTurn(nil, 10)

	out := turn.RunRound(context.Background(), []types.MessageToolCall{dangerCall("c1")}, roundRef{round: 1})

	assert.Equal(t, roundOutcome{
		Results: []types.Message{policyRefusal("c1", "danger", "Tool danger is blocked by policy")},
		Stop:    true,
	}, out)
}

func TestToolTurn_AddCost_OverBudgetReturnsBudgetError(t *testing.T) {
	history := []types.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "prior", CostInfo: &types.CostInfo{TotalCost: 0.08}},
	}
	turn := turnRunner(t, &pipeline.ToolPolicy{MaxCostUSD: 0.10}).newTurn(history, 10)

	require.NoError(t, turn.AddCost(&types.CostInfo{TotalCost: 0.01}))
	require.NoError(t, turn.AddCost(nil))

	err := turn.AddCost(&types.CostInfo{TotalCost: 0.05})
	require.Error(t, err)
	assert.Equal(t, "provider stage: cost budget exceeded (0.1400 > 0.1000 USD)", err.Error())
}

func TestToolTurn_AddCost_NoBudgetNeverErrors(t *testing.T) {
	turn := turnRunner(t, &pipeline.ToolPolicy{}).newTurn(nil, 10)

	require.NoError(t, turn.AddCost(&types.CostInfo{TotalCost: 999}))
	assert.InDelta(t, 999.0, turn.cumulativeCost, 1e-9)
}
