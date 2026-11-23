package sdk

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These cover a round the repeated-identical-call breaker nudges: its looping
// calls are fed back, and its other calls run as any round's do, under the
// per-turn call budget, counted for rejection and changing the offer.

// nudgeScriptProvider plays a fixed script of tool-call rounds, then answers
// with text, recording the tool names it was offered and the messages it was
// given on every call.
type nudgeScriptProvider struct {
	*mock.ToolProvider

	mu      sync.Mutex
	script  [][]types.MessageToolCall
	offered [][]string
	choices []string
	seen    [][]types.Message
	active  [][]string          // PredictionRequest.ActiveTools, per call
	descs   []map[string]string // each offered tool's description, per call
}

func newNudgeScriptProvider(script ...[]types.MessageToolCall) *nudgeScriptProvider {
	return &nudgeScriptProvider{ToolProvider: mock.NewToolProvider("nudge", "nudge-model", false, nil), script: script}
}

func (p *nudgeScriptProvider) SupportsStreaming() bool { return false }

func (p *nudgeScriptProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, offered providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.choices = append(p.choices, choice)
	var names []string
	described := map[string]string{}
	if descs, ok := offered.([]*providers.ToolDescriptor); ok {
		for _, d := range descs {
			names = append(names, d.Name)
			described[d.Name] = d.Description
		}
	}
	sort.Strings(names)
	p.offered = append(p.offered, names)
	p.descs = append(p.descs, described)
	p.active = append(p.active, req.ActiveTools)
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	if len(p.script) == 0 {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	calls := p.script[0]
	p.script = p.script[1:]
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func (p *nudgeScriptProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	resp, _, err := p.PredictWithTools(ctx, req, nil, "")
	return resp, err
}

// offeredOn returns the tool names offered on the call-th model call (0-based).
func (p *nudgeScriptProvider) offeredOn(t *testing.T, call int) []string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Greater(t, len(p.offered), call)
	return p.offered[call]
}

// resultsOn returns the text of each tool result the model was given on the
// call-th model call (0-based), by call ID, and their errors.
func (p *nudgeScriptProvider) resultsOn(call int) (texts, errs map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	texts, errs = map[string]string{}, map[string]string{}
	for _, m := range p.seen[call] {
		if m.ToolResult != nil {
			texts[m.ToolResult.ID] = m.ToolResult.GetTextContent()
			errs[m.ToolResult.ID] = m.ToolResult.Error
		}
	}
	return texts, errs
}

func lookup(id, order string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "lookup_order", Args: []byte(`{"id":"` + order + `"}`)}
}

const nudgeRepeatedText = "Repeated call ignored: \"lookup_order\" has been called 2 times with identical " +
	"arguments and was NOT executed again — the result will not change. Stop repeating this exact call: " +
	"change the arguments, use a different tool, or finish the task."

// nudgePackJSON allows lookup_order and cancel_order; refund is reachable
// only through the refund-processing skill (writeToolGrantSkill).
const nudgePackJSON = `{
	"id": "nudge-round-test",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat", "name": "Chat",
			"system_template": "You are a support agent.",
			"tools": ["lookup_order", "cancel_order"],
			"tool_policy": {"max_rounds": 6}
		}
	},
	"tools": {
		"lookup_order": {"name": "lookup_order", "description": "Look up an order", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"cancel_order": {"name": "cancel_order", "description": "Cancel an order", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"refund": {"name": "refund", "description": "Issue a refund", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}
	}
}`

func openNudgeConv(
	t *testing.T, provider *nudgeScriptProvider, policy *pipeline.ToolPolicy, opts ...Option,
) (conv *Conversation, lookups *atomic.Int32) {
	t.Helper()
	return openNudgePack(t, nudgePackJSON, provider, policy, opts...)
}

func openNudgePack(
	t *testing.T, packJSON string, provider *nudgeScriptProvider, policy *pipeline.ToolPolicy, opts ...Option,
) (conv *Conversation, lookups *atomic.Int32) {
	t.Helper()
	opts = append([]Option{WithProvider(provider), WithSkipSchemaValidation(), WithToolPolicy(policy)}, opts...)
	conv, err := Open(writeWorkflowTestPack(t, packJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	lookups = &atomic.Int32{}
	conv.OnTool("lookup_order", func(map[string]any) (any, error) {
		lookups.Add(1)
		return "ok", nil
	})
	conv.OnTool("cancel_order", func(map[string]any) (any, error) {
		return nil, errors.New("orders cannot be cancelled")
	})
	conv.OnTool("refund", func(map[string]any) (any, error) { return "refunded", nil })
	return conv, lookups
}

// With max_tool_calls_per_turn spent, a nudged round's new call is refused as
// over the budget, not run.
func TestNudgeRound_NewCallOverTheSpentBudgetIsRefused(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), lookup("c3", "B")},
	)
	conv, lookups := openNudgeConv(t, provider,
		&pipeline.ToolPolicy{MaxToolCallsPerTurn: 1, MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "where are my orders?")
	require.NoError(t, err)

	assert.EqualValues(t, 1, lookups.Load(), "only the call within the budget ran")
	texts, errs := provider.resultsOn(2)
	assert.Equal(t, map[string]string{
		"c1": "ok",
		"c2": nudgeRepeatedText,
		"c3": "Tool lookup_order not executed: the tool call limit for this turn is reached",
	}, texts)
	assert.Equal(t, "Tool lookup_order not executed: the tool call limit for this turn is reached", errs["c3"])
}

// A nudged round's new calls count toward the budget: the next round's new
// call is over it.
func TestNudgeRound_NewCallsCountTowardTheBudget(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), lookup("c3", "B")},
		[]types.MessageToolCall{lookup("c4", "C")},
	)
	conv, lookups := openNudgeConv(t, provider,
		&pipeline.ToolPolicy{MaxToolCallsPerTurn: 2, MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "where are my orders?")
	require.NoError(t, err)

	assert.EqualValues(t, 2, lookups.Load(), "c1 and c3 ran; c4 is over the budget")
	assert.Nil(t, provider.offeredOn(t, 2), "the budget spent in the nudged round withdraws the tools")
	texts, _ := provider.resultsOn(3)
	assert.Equal(t, "Tool lookup_order not executed: the tool call limit for this turn is reached", texts["c4"])
}

// A skill__activate in a nudged round grants its tools from the next round.
func TestNudgeRound_SkillActivationExtendsTheNextRoundsTools(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), {
			ID: "c3", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`),
		}},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2},
		WithSkillsDir(writeToolGrantSkill(t)))

	_, err := conv.Send(context.Background(), "refund ORD-1")
	require.NoError(t, err)

	base := []string{"cancel_order", "lookup_order", "skill__activate", "skill__deactivate", "skill__read_resource"}
	assert.Equal(t, base, provider.offeredOn(t, 1))
	assert.Equal(t, []string{
		"cancel_order", "lookup_order", "refund", "skill__activate", "skill__deactivate", "skill__read_resource",
	}, provider.offeredOn(t, 2), "the round after the activation offers the granted tool")
}

// A nudged round's rejected call counts toward the rejection rule: rejected
// twice in the turn, the tool is excluded from the next round's tools.
func TestNudgeRound_RejectionCountsTowardExclusion(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A"), cancel("c2", "A")},
		[]types.MessageToolCall{lookup("c3", "A"), cancel("c4", "B")},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "cancel my orders")
	require.NoError(t, err)

	assert.Equal(t, []string{"cancel_order", "lookup_order"}, provider.offeredOn(t, 1), "one rejection does not exclude")
	assert.Equal(t, []string{"lookup_order"}, provider.offeredOn(t, 2),
		"the second rejection, in the nudged round, excludes")
}

// A call held for approval in a nudged round is surfaced pending, as in any
// round, not dropped without a result.
func TestNudgeRound_HeldCallIsSurfacedPending(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), {ID: "c3", Name: "cancel_order", Args: []byte(`{"id":"A"}`)}},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2})
	conv.OnToolAsync("cancel_order",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{Reason: "approval"} },
		func(map[string]any) (any, error) { return "cancelled", nil })

	resp, err := conv.Send(context.Background(), "cancel ORD-A")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	assert.Equal(t, "c3", resp.PendingTools()[0].ID)
	assert.Equal(t, "cancel_order", resp.PendingTools()[0].Name)
}

// termination.tool_called stops the turn when the named tool runs in a
// nudged round: the model is not called again.
func TestNudgeRound_TerminationToolStopsTheTurn(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), {ID: "c3", Name: "cancel_order", Args: []byte(`{"id":"A"}`)}},
		[]types.MessageToolCall{lookup("c4", "B")},
	)
	conv, lookups := openNudgeConv(t, provider,
		&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, StopOnTool: "cancel_order"})

	_, err := conv.Send(context.Background(), "cancel ORD-A")
	require.NoError(t, err)
	provider.mu.Lock()
	calls := len(provider.seen)
	provider.mu.Unlock()
	assert.Equal(t, 2, calls, "the turn stopped after the nudged round ran cancel_order")
	assert.EqualValues(t, 1, lookups.Load())
}

// A nudge in the turn's last allowed round still ends the turn at the round
// limit, with its error: there is no round left for the model's retry.
func TestNudgeRound_InTheLastRoundEndsAtTheRoundLimit(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), lookup("c3", "B")},
	)
	conv, lookups := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxRounds: 2, MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "where are my orders?")
	var maxRounds *stage.MaxRoundsError
	require.ErrorAs(t, err, &maxRounds)
	assert.Equal(t, 2, maxRounds.Rounds)
	assert.EqualValues(t, 2, lookups.Load(), "the nudged round's new call ran")
}

// choicesSeen returns the tool choice the model was given on each call.
func (p *nudgeScriptProvider) choicesSeen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.choices...)
}

func cancel(id, order string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "cancel_order", Args: []byte(`{"id":"` + order + `"}`)}
}

// A forced tool choice that still fits the offer a nudged round rebuilt is
// kept for the model's retry.
func TestNudgeRound_ForcedChoiceThatStillFitsIsKept(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A"), lookup("c2", "A"), {
			ID: "c3", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`),
		}},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: "cancel_order"},
		WithSkillsDir(writeToolGrantSkill(t)))

	_, err := conv.Send(context.Background(), "look up ORD-A")
	require.NoError(t, err)
	assert.Contains(t, provider.offeredOn(t, 1), "refund", "the nudged round rebuilt the offer")
	assert.Equal(t, []string{"cancel_order", "cancel_order"}, provider.choicesSeen())
}

// A forced choice of a tool the nudged round excluded falls back to auto, as
// after any round, instead of forcing a tool the model is no longer offered.
func TestNudgeRound_ForcedChoiceOfAnExcludedToolFallsBackToAuto(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A"), lookup("c2", "A"), cancel("c3", "A"), cancel("c4", "B")},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: "cancel_order"})

	_, err := conv.Send(context.Background(), "cancel my orders")
	require.NoError(t, err)
	assert.Equal(t, []string{"lookup_order"}, provider.offeredOn(t, 1))
	assert.Equal(t, []string{"cancel_order", "auto"}, provider.choicesSeen())
}

// cancelOnlyPackJSON offers cancel_order alone.
const cancelOnlyPackJSON = `{
	"id": "nudge-cancel-only",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat", "name": "Chat",
			"system_template": "You are a support agent.",
			"tools": ["cancel_order"],
			"tool_policy": {"max_rounds": 6}
		}
	},
	"tools": {
		"cancel_order": {"name": "cancel_order", "description": "Cancel an order", "mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}
	}
}`

// A required tool choice falls back to auto when the nudged round excluded
// every tool: no call can be forced from an empty offer.
func TestNudgeRound_RequiredChoiceWithNoToolsLeftFallsBackToAuto(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{cancel("c1", "A"), cancel("c2", "A"), cancel("c3", "B"), cancel("c4", "C")},
	)
	conv, _ := openNudgePack(t, cancelOnlyPackJSON, provider,
		&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: "required"})

	_, err := conv.Send(context.Background(), "cancel my orders")
	require.NoError(t, err)
	assert.Nil(t, provider.offeredOn(t, 1), "every tool is excluded")
	assert.Equal(t, []string{"required", "auto"}, provider.choicesSeen())
}

// A nudged round's results are in the order the model made the calls, not
// the nudges first.
func TestNudgeRound_ResultsAreInCallOrder(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "B"), lookup("c3", "A")},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "where are my orders?")
	require.NoError(t, err)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	var order, texts []string
	for _, m := range provider.seen[2] {
		if m.ToolResult != nil {
			order = append(order, m.ToolResult.ID)
			texts = append(texts, m.ToolResult.GetTextContent())
		}
	}
	assert.Equal(t, []string{"c1", "c2", "c3"}, order)
	assert.Equal(t, []string{"ok", "ok", nudgeRepeatedText}, texts)
}

// A call refused as over the budget did not run, so it does not count toward
// the repeated-call breaker: its one repeat is refused as over the budget
// again, not nudged.
func TestNudgeRound_OverBudgetCallDoesNotCountTowardTheBreaker(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A"), lookup("c2", "B")},
		[]types.MessageToolCall{lookup("c3", "B")},
	)
	conv, lookups := openNudgeConv(t, provider,
		&pipeline.ToolPolicy{MaxToolCallsPerTurn: 1, MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "where are my orders?")
	require.NoError(t, err)
	assert.EqualValues(t, 1, lookups.Load())
	texts, _ := provider.resultsOn(2)
	const overBudget = "Tool lookup_order not executed: the tool call limit for this turn is reached"
	assert.Equal(t, overBudget, texts["c2"])
	assert.Equal(t, overBudget, texts["c3"], "the repeat is not nudged")
}

// A looping call ahead of a new one keeps its place too.
func TestNudgeRound_LoopingCallKeepsItsPlace(t *testing.T) {
	provider := newNudgeScriptProvider(
		[]types.MessageToolCall{lookup("c1", "A")},
		[]types.MessageToolCall{lookup("c2", "A"), lookup("c3", "B")},
	)
	conv, _ := openNudgeConv(t, provider, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2})

	_, err := conv.Send(context.Background(), "where are my orders?")
	require.NoError(t, err)
	provider.mu.Lock()
	defer provider.mu.Unlock()
	var order []string
	for _, m := range provider.seen[2] {
		if m.ToolResult != nil {
			order = append(order, m.ToolResult.ID)
		}
	}
	assert.Equal(t, []string{"c1", "c2", "c3"}, order)
}
