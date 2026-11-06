package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These cover the reserve / commit / release design of
// ToolPolicy.MaxCallsPerMinute: a call takes a slot when the model makes it,
// keeps it when it is admitted to run, and gives it back when it is refused.

// openSendMessageConv opens the gate pack under a call limit of limit, with a
// model whose first turn calls send_message calls times in one round and whose
// later turns answer.
func openSendMessageConv(t *testing.T, limit, calls int) *Conversation {
	t.Helper()
	repo := newGateTurnRepo()
	toolCalls := make([]mock.ToolCall, calls)
	for i := range toolCalls {
		toolCalls[i] = mock.ToolCall{Name: "send_message", Arguments: map[string]any{"n": i}}
	}
	repo.add(1, mock.Turn{Type: "tool_calls", ToolCalls: toolCalls})
	repo.add(2, mock.Turn{Type: "text", Content: "done"})
	conv, err := Open(writeGatePack(t), "assist",
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(), WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: limit}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// holdEverySendMessage gates send_message on approval and counts the calls
// that reach the gate.
func holdEverySendMessage(conv *Conversation) *atomic.Int32 {
	checked := &atomic.Int32{}
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			checked.Add(1)
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(args map[string]any) (any, error) { return args, nil },
	)
	return checked
}

// limitRefusals counts the tool results in conv's history that a call limit
// refused.
func limitRefusals(t *testing.T, conv *Conversation) int {
	t.Helper()
	n := 0
	for _, m := range conv.Messages(context.Background()) {
		if m.ToolResult != nil && strings.Contains(m.ToolResult.Error, "calls per minute is reached") {
			n++
		}
	}
	return n
}

func TestCallRate_ApprovalGatedCallsReserveSlots(t *testing.T) {
	conv := openSendMessageConv(t, 2, 20)
	checked := holdEverySendMessage(conv)

	resp, err := conv.Send(context.Background(), "send twenty")
	require.NoError(t, err)
	assert.Len(t, resp.PendingTools(), 2, "two calls are held for approval")
	assert.Equal(t, 18, limitRefusals(t, conv), "the other eighteen are refused at once")
	assert.EqualValues(t, 2, checked.Load(), "a refused call is never put to a human")
}

func TestCallRate_RejectedHeldCallFreesItsSlot(t *testing.T) {
	conv := openSendMessageConv(t, 2, 2)
	holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "send two")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 2)
	assert.Equal(t, 2, probeRefusals(t, conv, 2), "both held calls hold their slots")

	_, err = conv.RejectTool(ctx, resp.PendingTools()[0].ID, "no")
	require.NoError(t, err)
	assert.Equal(t, 1, probeRefusals(t, conv, 2), "the rejection gave one slot back")
}

// An approved held call commits one slot, once: Continue consuming the
// approval does not commit it a second time.
func TestCallRate_ApprovedHeldCallCommitsOnce(t *testing.T) {
	conv := openSendMessageConv(t, 2, 1)
	holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "send one")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	_, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, probeRefusals(t, conv, 2), "the approved call takes exactly one of the two slots")
}

func TestCallRate_RejectedClientToolFreesItsSlot(t *testing.T) {
	resumers := map[string]func(*Conversation, context.Context) error{
		"Resume":   func(c *Conversation, ctx context.Context) error { _, err := c.Resume(ctx); return err },
		"Continue": func(c *Conversation, ctx context.Context) error { _, err := c.Continue(ctx); return err },
	}
	for name, resume := range resumers {
		t.Run(name, func(t *testing.T) {
			conv := openSendMessageConv(t, 1, 1)
			conv.DeferClientTool("send_message")
			ctx := context.Background()

			resp, err := conv.Send(ctx, "send one")
			require.NoError(t, err)
			require.Len(t, resp.ClientTools(), 1)
			assert.Equal(t, 1, probeRefusals(t, conv, 1), "the deferred call holds its slot")

			conv.RejectClientTool(ctx, resp.ClientTools()[0].CallID, "not now")
			require.NoError(t, resume(conv, ctx))
			assert.Zero(t, probeRefusals(t, conv, 1), "the rejected call gave its slot back")
		})
	}
}

func TestCallRate_AnsweredClientToolKeepsItsSlot(t *testing.T) {
	conv := openSendMessageConv(t, 1, 1)
	conv.DeferClientTool("send_message")
	ctx := context.Background()

	resp, err := conv.Send(ctx, "send one")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1)

	callID := resp.ClientTools()[0].CallID
	require.NoError(t, conv.SendToolResult(ctx, callID, "sent"))
	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	assertCommitted(t, conv, callID)
}

// assertCommitted checks that callID's send_message slot is committed, not
// still reserved: a release, which frees a reservation, leaves a committed
// slot in place.
func assertCommitted(t *testing.T, conv *Conversation, callID string) {
	t.Helper()
	conv.callRate.Release(conv.ID(), "send_message", callID)
	assert.Equal(t, 1, probeRefusals(t, conv, 1), "the answered call ran, so its slot is committed")
}

// pendingLocalExecutor defers every "local" call, as an exec or async
// executor does, without being a client tool.
type pendingLocalExecutor struct{}

func (pendingLocalExecutor) Name() string { return "local" }

func (pendingLocalExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("asynchronous only")
}

func (pendingLocalExecutor) ExecuteAsync(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	return &tools.ToolExecutionResult{
		Status: tools.ToolStatusPending, PendingInfo: &tools.PendingToolInfo{Reason: "async", ToolName: d.Name},
	}, nil
}

// A call any executor defers resolves its slot when it is answered, not only a
// client tool's.
func TestCallRate_ExecutorDeferredCallResolvesItsSlot(t *testing.T) {
	t.Run("answered", func(t *testing.T) {
		conv := openSendMessageConv(t, 1, 1)
		conv.ToolRegistry().RegisterExecutor(pendingLocalExecutor{})
		ctx := context.Background()
		resp, err := conv.Send(ctx, "send one")
		require.NoError(t, err)
		require.Len(t, resp.ClientTools(), 1)
		callID := resp.ClientTools()[0].CallID
		require.NoError(t, conv.SendToolResult(ctx, callID, "sent"))
		_, err = conv.Resume(ctx)
		require.NoError(t, err)
		assertCommitted(t, conv, callID)
	})
	t.Run("rejected", func(t *testing.T) {
		conv := openSendMessageConv(t, 1, 1)
		conv.ToolRegistry().RegisterExecutor(pendingLocalExecutor{})
		ctx := context.Background()
		resp, err := conv.Send(ctx, "send one")
		require.NoError(t, err)
		require.Len(t, resp.ClientTools(), 1)
		conv.RejectClientTool(ctx, resp.ClientTools()[0].CallID, "no")
		_, err = conv.Resume(ctx)
		require.NoError(t, err)
		assert.Zero(t, probeRefusals(t, conv, 1), "the rejected call gave its slot back")
	})
}

// Answers stored for a turn the user abandons with a new Send still settle
// their calls' slots.
func TestCallRate_AbandonedTurnSettlesAnsweredSlots(t *testing.T) {
	conv := openSendMessageConv(t, 1, 1)
	conv.DeferClientTool("send_message")
	ctx := context.Background()
	resp, err := conv.Send(ctx, "send one")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1)

	conv.RejectClientTool(ctx, resp.ClientTools()[0].CallID, "no")
	conv.discardAbandonedResolutions()
	assert.Zero(t, probeRefusals(t, conv, 1), "the discarded rejection still gave its slot back")
}

// twoStateToolPack has two states that both offer lookup_order.
const twoStateToolPack = `{
	"id": "two-state-tools", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You help.", "tools": ["lookup_order"]}},
	"tools": {"lookup_order": {"name": "lookup_order", "description": "Look up an order", "mode": "local",
		"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}},
	"workflow": {"version": 1, "entry": "first",
		"states": {
			"first": {"prompt_task": "chat", "on_event": {"Next": "second", "Compose": "compose"}},
			"second": {"prompt_task": "chat"},
			"compose": {"orchestration": "composition", "composition": "flow", "terminal": true}}},
	"compositions": {"flow": {"version": 1,
		"steps": [{"id": "ag", "kind": "agent", "prompt_task": "chat", "input": "${input}", "tools": ["lookup_order"]}],
		"output": "ag"}}
}`

// sendInEachState sends one message in the workflow's entry state, makes the
// transition event, and sends one message in the destination state, with a
// model that calls lookup_order once per Send, under a limit of one call a
// minute. It returns how many lookups ran.
func sendInEachState(t *testing.T, event string) int32 {
	t.Helper()
	provider := newAlwaysCallProvider()
	wc, err := OpenWorkflow(createTestPackFile(t, twoStateToolPack), WithProvider(provider),
		WithSkipSchemaValidation(),
		WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: 1, StopOnTool: "lookup_order"}))
	require.NoError(t, err)
	defer func() { _ = wc.Close() }()
	exec := &countingLocalExecutor{}
	ctx := context.Background()

	wc.ActiveConversation().ToolRegistry().RegisterExecutor(exec)
	_, _ = wc.Send(ctx, "find my order")
	require.EqualValues(t, 1, exec.runs.Load(), "the first state's call runs")

	_, err = wc.Transition(event)
	require.NoError(t, err)
	wc.ActiveConversation().ToolRegistry().RegisterExecutor(exec)
	_, _ = wc.Send(ctx, "find it again")
	require.Equal(t, 2, provider.roundsSeen(), "the destination state's model made its call")
	return exec.runs.Load()
}

func TestCallRate_WindowSpansWorkflowTransition(t *testing.T) {
	assert.EqualValues(t, 1, sendInEachState(t, "Next"),
		"the destination state's call is over the workflow's limit")
}

func TestCallRate_WindowSpansCompositionStepsAndOuterTurns(t *testing.T) {
	assert.EqualValues(t, 1, sendInEachState(t, "Compose"),
		"the composition step's call is over the limit the outer turn used")
}
