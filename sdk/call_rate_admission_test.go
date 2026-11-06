package sdk

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These cover count-at-admission for ToolPolicy.MaxCallsPerMinute (#2450): a
// call is checked and counted only when it is admitted to run, so nothing is
// reserved and nothing can be left behind.

// openSendMessageConv opens the gate pack under a call limit of limit, with a
// model whose first turn calls send_message calls times in one round and whose
// later turns answer.
func openSendMessageConv(t *testing.T, limit, calls int, opts ...Option) *Conversation {
	t.Helper()
	repo := newGateTurnRepo()
	toolCalls := make([]mock.ToolCall, calls)
	for i := range toolCalls {
		toolCalls[i] = mock.ToolCall{Name: "send_message", Arguments: map[string]any{"n": i}}
	}
	repo.add(1, mock.Turn{Type: "tool_calls", ToolCalls: toolCalls})
	repo.add(2, mock.Turn{Type: "text", Content: "done"})
	opts = append([]Option{
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(), WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: limit}),
	}, opts...)
	conv, err := Open(writeGatePack(t), "assist", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// holdEverySendMessage gates send_message on approval and counts the calls
// that reach the gate and the calls that run.
func holdEverySendMessage(conv *Conversation) (checked, ran *atomic.Int32) {
	checked, ran = &atomic.Int32{}, &atomic.Int32{}
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			checked.Add(1)
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(args map[string]any) (any, error) {
			ran.Add(1)
			return args, nil
		},
	)
	return checked, ran
}

// roomFor reports whether conv's window would admit one more send_message
// call under limit. It counts that call when it does.
func roomFor(conv *Conversation, limit int) bool {
	return conv.callRate.Admit(conv.ID(), "send_message", limit) == nil
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

// With a limit of 2, twenty approval-gated calls are all put to the human;
// the first two approvals run, and the third returns the limit error without
// running the call.
func TestCallRate_ThirdApprovalInTheWindowIsRefused(t *testing.T) {
	conv := openSendMessageConv(t, 2, 20)
	checked, ran := holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "send twenty")
	require.NoError(t, err)
	pending := resp.PendingTools()
	require.Len(t, pending, 20, "a held call is not counted until it is approved")
	assert.EqualValues(t, 20, checked.Load())

	for i := range 2 {
		res, resolveErr := conv.ResolveTool(ctx, pending[i].ID)
		require.NoError(t, resolveErr)
		require.NoError(t, res.Error, "approval %d is within the limit", i+1)
	}
	require.EqualValues(t, 2, ran.Load())

	res, err := conv.ResolveTool(ctx, pending[2].ID)
	require.NoError(t, err)
	var limitErr *stage.CallRateLimitError
	require.ErrorAs(t, res.Error, &limitErr, "the third approval returns the limit error")
	assert.Equal(t, "Tool send_message not executed: its limit of 2 calls per minute is reached",
		toolResultErrorText(res.Error))
	assert.EqualValues(t, 2, ran.Load(), "the refused call does not run")
}

// A rejected call never counts: neither a held call RejectTool rejects nor a
// client call RejectClientTool rejects.
func TestCallRate_RejectedCallNeverCounts(t *testing.T) {
	t.Run("held", func(t *testing.T) {
		conv := openSendMessageConv(t, 1, 2)
		_, ran := holdEverySendMessage(conv)
		ctx := context.Background()
		resp, err := conv.Send(ctx, "send two")
		require.NoError(t, err)
		require.Len(t, resp.PendingTools(), 2)

		_, err = conv.RejectTool(ctx, resp.PendingTools()[0].ID, "no")
		require.NoError(t, err)
		res, err := conv.ResolveTool(ctx, resp.PendingTools()[1].ID)
		require.NoError(t, err)
		assert.NoError(t, res.Error, "the rejection took none of the one slot")
		assert.EqualValues(t, 1, ran.Load())
	})
	t.Run("client", func(t *testing.T) {
		conv := openSendMessageConv(t, 1, 1)
		conv.DeferClientTool("send_message")
		ctx := context.Background()
		resp, err := conv.Send(ctx, "send one")
		require.NoError(t, err)
		require.Len(t, resp.ClientTools(), 1)

		conv.RejectClientTool(ctx, resp.ClientTools()[0].CallID, "not now")
		_, err = conv.Resume(ctx)
		require.NoError(t, err)
		assert.True(t, roomFor(conv, 1), "the rejected client call took no slot")
	})
}

// An approval is counted once: Continue using the approved result does not
// count it again.
func TestCallRate_ApprovalCountsOnce(t *testing.T) {
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

	assert.True(t, roomFor(conv, 2), "the approved call takes one of the two slots")
	assert.False(t, roomFor(conv, 2), "and only one")
}

// A turn the user abandons with a new Send leaves nothing in the window:
// neither its unapproved held calls nor its unused client results count.
func TestCallRate_AbandonedTurnLeavesNothingBehind(t *testing.T) {
	t.Run("held", func(t *testing.T) {
		conv := openSendMessageConv(t, 1, 3)
		holdEverySendMessage(conv)
		ctx := context.Background()
		resp, err := conv.Send(ctx, "send three")
		require.NoError(t, err)
		require.Len(t, resp.PendingTools(), 3)

		_, err = conv.Send(ctx, "never mind")
		require.NoError(t, err)
		assert.True(t, roomFor(conv, 1), "the abandoned held calls took no slot")
	})
	t.Run("client", func(t *testing.T) {
		conv := openSendMessageConv(t, 1, 1)
		conv.DeferClientTool("send_message")
		ctx := context.Background()
		resp, err := conv.Send(ctx, "send one")
		require.NoError(t, err)
		require.Len(t, resp.ClientTools(), 1)
		require.NoError(t, conv.SendToolResult(ctx, resp.ClientTools()[0].CallID, "sent"))

		_, err = conv.Send(ctx, "never mind")
		require.NoError(t, err)
		assert.True(t, roomFor(conv, 1), "the discarded result was never used, so it took no slot")
	})
}

// A held call made on one replica and approved on another, through a shared
// pending store, counts on the replica that approved it and leaves nothing on
// the one that held it.
func TestCallRate_ResolvingOnAnotherReplicaLeavesNothing(t *testing.T) {
	store := sdktools.NewMemoryPendingStore()
	t.Cleanup(func() { _ = store.Close() })
	shared := []Option{WithPendingStore(store), WithConversationID("conv-shared")}
	holder := openSendMessageConv(t, 1, 1, shared...)
	resolver := openSendMessageConv(t, 1, 1, shared...)
	holdEverySendMessage(holder)
	_, ran := holdEverySendMessage(resolver)
	ctx := context.Background()

	resp, err := holder.Send(ctx, "send one")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	res, err := resolver.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	require.EqualValues(t, 1, ran.Load())

	assert.True(t, roomFor(holder, 1), "the holding replica's window has nothing to leak")
	assert.False(t, roomFor(resolver, 1), "the approving replica counted the call")
}

// A deferred client result is admitted when Resume uses it; one that arrives
// after the window filled is refused, and the model is told so.
func TestCallRate_DeferredResultOverTheLimitIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		limit, refusals int
	}{
		"within the limit": {limit: 2, refusals: 0},
		"over the limit":   {limit: 1, refusals: 1},
	} {
		t.Run(name, func(t *testing.T) {
			conv := openSendMessageConv(t, tc.limit, 1)
			conv.DeferClientTool("send_message")
			ctx := context.Background()
			resp, err := conv.Send(ctx, "send one")
			require.NoError(t, err)
			require.Len(t, resp.ClientTools(), 1)

			// While the client works, another call is admitted (a held call
			// approved meanwhile), which fills a limit of one.
			require.True(t, roomFor(conv, tc.limit))
			require.NoError(t, conv.SendToolResult(ctx, resp.ClientTools()[0].CallID, "sent"))
			_, err = conv.Resume(ctx)
			require.NoError(t, err)
			assert.Equal(t, tc.refusals, limitRefusals(t, conv))
			assert.False(t, roomFor(conv, tc.limit), "the window is full")
		})
	}
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
	if event == "Next" {
		// The destination state's tool results are in its conversation's
		// history; a composition step's are not.
		require.Positive(t, limitRefusals(t, wc.ActiveConversation()),
			"the destination's call was refused for the limit, not for another reason")
	}
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
