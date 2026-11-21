package agui

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// tokens builds a TokenUsage entry the way the adapter reports one.
func tokens(provider string, in, out int64) aguievents.TokenUsage {
	return modelTokens(provider, "", in, out)
}

// modelTokens is tokens for a call whose model is known.
func modelTokens(provider, model string, in, out int64) aguievents.TokenUsage {
	return aguievents.TokenUsage{
		Provider:     provider,
		Model:        model,
		InputTokens:  aguievents.TokenCount(in),
		OutputTokens: aguievents.TokenCount(out),
		TotalTokens:  aguievents.TokenCount(in + out),
	}
}

func finishedEvent(t *testing.T, evts []aguievents.Event) *aguievents.RunFinishedEvent {
	t.Helper()
	finished, ok := evts[len(evts)-1].(*aguievents.RunFinishedEvent)
	require.True(t, ok, "the run ends with RUN_FINISHED: %v", describeRun(evts))
	return finished
}

// RUN_FINISHED reports the turn's token usage. The mock provider counts one
// token per 4 bytes of each message it is given (10 when that comes to none,
// as for "hi") and of its reply: 3 for "Hello, world.".
func TestRunSend_ReportsTheTurnsUsage(t *testing.T) {
	conv := openConv(t, streamingProvider(streamedText("Hello, ", "world.")))

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.TokenUsage{modelTokens("mock", "mock-model", 10, 3)}, finishedEvent(t, evts).Usage)
}

// A turn of several model calls to one provider reports them as one entry:
// the tool round's call (3 in for "where is 42?", 2 out for "Checking.") and
// the answer's (6 in for the question, "Checking." and "shipped"; 2 out for
// "Shipped.").
func TestRunSend_UsageSumsTheTurnsModelCalls(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("lookup", map[string]any{"id": "42"}, "Checking."),
		streamedText("Shipped."),
	))
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "where is 42?")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.TokenUsage{modelTokens("mock", "mock-model", 9, 4)}, finishedEvent(t, evts).Usage)
}

// Usage follows AG-UI's accounting: input counts every prompt token, cache
// reads and writes included; output counts reasoning; the cache and reasoning
// counts are parts of those totals. Each provider has its own entry, in the
// order the turn used them.
func TestRunSend_UsageFollowsAGUIAccounting(t *testing.T) {
	first := assistantMsg("Checking.", call("c1", "lookup", `{}`))
	first.CostInfo = &types.CostInfo{
		InputTokens: 100, CachedTokens: 20, OutputTokens: 30, ProviderName: "claude-main",
		Quantities: map[string]float64{"input_token": 100, "cache_read_token": 20, "cache_write_token": 5,
			"output_token": 30, "reasoning_token": 7},
	}
	second := assistantMsg("Done.")
	second.CostInfo = &types.CostInfo{InputTokens: 40, OutputTokens: 4, ProviderName: "openai-judge"}
	third := assistantMsg("Really done.")
	third.CostInfo = &types.CostInfo{InputTokens: 50, OutputTokens: 6, ProviderName: "claude-main"}
	turn := []types.Message{first, toolMsg("c1", "ok"), second, third}
	resp := sdk.NewResponseForTest("Really done.", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.TokenUsage{
		{
			Provider:              "claude-main",
			InputTokens:           aguievents.TokenCount(175),
			OutputTokens:          aguievents.TokenCount(43),
			TotalTokens:           aguievents.TokenCount(218),
			ReasoningTokens:       aguievents.TokenCount(7),
			CachedInputTokens:     aguievents.TokenCount(20),
			CacheWriteInputTokens: aguievents.TokenCount(5),
		},
		tokens("openai-judge", 40, 4),
	}, finishedEvent(t, evts).Usage)
}

// A turn that reports no usage carries none, rather than zero counts it has
// no data for.
func TestRunSend_NoUsageWithoutCostInfo(t *testing.T) {
	resp := sdk.NewResponseForTest("Hi.", nil, sdk.WithTurnMessagesForTest([]types.Message{
		assistantMsg("Hi."),
		{Role: roleAssistant, Content: "", CostInfo: &types.CostInfo{ProviderName: "silent"}},
	}))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	data, err := json.Marshal(finishedEvent(t, evts))
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(data, &wire))
	assert.Equal(t, []string{"runId", "threadId", "timestamp", "type"}, slices.Sorted(maps.Keys(wire)),
		"RUN_FINISHED carries no usage key")
}

// A turn with no turn messages reports the usage on its reply.
func TestRunSend_UsageFromTheReplyWithoutTurnMessages(t *testing.T) {
	resp := sdk.NewResponseForTest("Hi.", nil)
	resp.Message().CostInfo = &types.CostInfo{InputTokens: 8, OutputTokens: 2, ProviderName: "p"}
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.TokenUsage{tokens("p", 8, 2)}, finishedEvent(t, evts).Usage)
}

// A run that resumes a turn reports only the calls it made: the resumed
// reply's, 6 input tokens for the question, "Where are you?" and "Paris", and
// 4 output for "You are in Paris.". The run that left the call pending names
// it in its success outcome, with its own call's usage (2 in, 3 out).
func TestRunResume_ReportsOnlyItsOwnUsage(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("get_location", map[string]any{}, "Where are you?"),
		streamedText("You are ", "in Paris."),
	))
	bindClientTool(t, conv, "get_location")

	first, err := sendAndCollect(t, NewEventAdapter(conv), "where am I?")
	require.NoError(t, err)
	finished := finishedEvent(t, first)
	assert.Equal(t, &aguievents.RunFinishedOutcome{
		Type: aguievents.RunFinishedOutcomeTypeSuccess, PendingToolCallIDs: []string{"call_0_get_location"},
	}, finished.Outcome)
	assert.Equal(t, []aguievents.TokenUsage{modelTokens("mock", "mock-model", 2, 3)}, finished.Usage)

	input := []aguitypes.Message{{ID: "t1", Role: aguitypes.RoleTool, ToolCallID: "call_0_get_location", Content: "Paris"}}
	b := NewEventAdapter(conv)
	second, err := continueAndCollect(t, b, first, func(ctx context.Context) error {
		return b.RunResume(ctx, ToolResultsFromAGUI(input))
	})
	require.NoError(t, err)
	finished = finishedEvent(t, second)
	assert.Nil(t, finished.Outcome)
	assert.Equal(t, []aguievents.TokenUsage{modelTokens("mock", "mock-model", 6, 4)}, finished.Usage)
}

// A ToolResultProvider that answers some calls leaves the rest pending, and
// the success outcome names only those.
func TestRunSend_PendingToolCallIDsNameOnlyUnansweredCalls(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "a"}, {CallID: "ct-2", ToolName: "b"}}
	sender := &mockSender{resp: sdk.NewResponseForTest("", nil, sdk.WithClientToolsForTest(pending))}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "ct-1", Result: "done"}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Equal(t, &aguievents.RunFinishedOutcome{
		Type: aguievents.RunFinishedOutcomeTypeSuccess, PendingToolCallIDs: []string{"ct-2"},
	}, finishedEvent(t, evts).Outcome)
}

// A run that fails after a model call reports that call's usage on
// RUN_ERROR.
func TestRunSend_RunErrorReportsUsageSoFar(t *testing.T) {
	msg := assistantMsg("", call("ct-1", "a", `{}`))
	msg.CostInfo = &types.CostInfo{InputTokens: 12, OutputTokens: 5, ProviderName: "p"}
	resp := sdk.NewResponseForTest("", nil,
		sdk.WithTurnMessagesForTest([]types.Message{msg}),
		sdk.WithClientToolsForTest([]sdk.PendingClientTool{{CallID: "ct-1", ToolName: "a"}}))
	sender := &mockSender{resp: resp}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return nil, errors.New("tool backend down")
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.Error(t, err)

	runErr, ok := evts[len(evts)-1].(*aguievents.RunErrorEvent)
	require.True(t, ok, describeRun(evts))
	assert.Equal(t, "tool backend down", runErr.Message)
	assert.Equal(t, []aguievents.TokenUsage{tokens("p", 12, 5)}, runErr.Usage)
}

// Usage is reported per provider and model: one provider's calls to two
// models are two entries, and a call whose cost names no provider is
// labelled "unknown" rather than sent without one.
func TestRunSend_UsagePerProviderAndModel(t *testing.T) {
	cost := func(provider, model string, in, out int) *types.CostInfo {
		return &types.CostInfo{ProviderName: provider, Model: model, InputTokens: in, OutputTokens: out}
	}
	msgs := []types.Message{assistantMsg("a"), assistantMsg("b"), assistantMsg("c"), assistantMsg("d"), assistantMsg("e")}
	msgs[0].CostInfo = cost("openai", "big", 10, 1)
	msgs[1].CostInfo = cost("openai", "small", 20, 2)
	msgs[2].CostInfo = cost("", "", 30, 3)
	msgs[3].CostInfo = cost("openai", "big", 5, 1)
	msgs[4].CostInfo = cost("embed", "e1", 7, 0) // input alone is still usage
	resp := sdk.NewResponseForTest("d", nil, sdk.WithTurnMessagesForTest(msgs))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.TokenUsage{
		modelTokens("openai", "big", 15, 2),
		modelTokens("openai", "small", 20, 2),
		tokens("unknown", 30, 3),
		modelTokens("embed", "e1", 7, 0),
	}, finishedEvent(t, evts).Usage)
}
