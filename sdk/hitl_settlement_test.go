package sdk

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// settlementCtxKey carries a caller identity on the Send context.
type settlementCtxKey struct{}

// openSettlementConv opens the gate pack on a model that makes the given
// number of send_message calls (call-1, call-2, ...) in its first round and
// then replies with each of replies in turn. The provider records what the
// model saw on every call.
func openSettlementConv(t *testing.T, calls int, replies []string, opts ...Option) (*Conversation, *roundsProvider) {
	t.Helper()
	toolCalls := make([]types.MessageToolCall, calls)
	for i := range toolCalls {
		toolCalls[i] = types.MessageToolCall{
			ID:   "call-" + strconv.Itoa(i+1),
			Name: "send_message",
			Args: []byte(`{"body":"` + strconv.Itoa(i+1) + `"}`),
		}
	}
	rounds := []providers.PredictionResponse{{Content: "Sending.", ToolCalls: toolCalls}}
	for _, reply := range replies {
		rounds = append(rounds, providers.PredictionResponse{Content: reply})
	}
	provider := &roundsProvider{ToolProvider: mock.NewToolProvider("x", "m", false, nil), rounds: rounds}
	opts = append([]Option{WithProvider(provider), WithSkipSchemaValidation()}, opts...)
	conv, err := Open(writeGatePack(t), "assist", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, provider
}

// toolResultsSeen returns the ids of the tool results the model saw on its
// call-th call (0-based).
func toolResultsSeen(p *roundsProvider, call int) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var ids []string
	for _, m := range p.seen[call] {
		if m.ToolResult != nil {
			ids = append(ids, m.ToolResult.ID)
		}
	}
	return ids
}

// modelCalls reports how many times the model was called.
func modelCalls(p *roundsProvider) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.seen)
}

// toolResultFor returns the result the history holds for call id, or nil.
func toolResultFor(t *testing.T, conv *Conversation, id string) *types.MessageToolResult {
	t.Helper()
	var found *types.MessageToolResult
	for _, m := range conv.Messages(context.Background()) {
		if m.ToolResult != nil && m.ToolResult.ID == id {
			found = m.ToolResult
		}
	}
	return found
}

// addFailingPendingStore is a pending store whose Add always fails, as a
// durable store does during an outage.
type addFailingPendingStore struct {
	sdktools.PendingStore
	err error
}

func (s *addFailingPendingStore) Add(context.Context, *sdktools.PendingToolCall) error { return s.err }

// Once every held call is settled, Continue gives the model every result in
// one request.
func TestContinue_SendsEverySettledResultInOneRequest(t *testing.T) {
	conv, provider := openSettlementConv(t, 2, []string{"Sent both."})
	_, ran := holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message two customers")
	require.NoError(t, err)
	held := resp.PendingTools()
	require.Len(t, held, 2)
	first, second := held[0].ID, held[1].ID
	for _, id := range []string{first, second} {
		_, err = conv.ResolveTool(ctx, id)
		require.NoError(t, err)
	}

	final, err := conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Sent both.", final.Text())
	assert.EqualValues(t, 2, ran.Load())
	require.Equal(t, 2, modelCalls(provider), "one request for the turn, one for the continuation")
	assert.Equal(t, []string{first, second}, toolResultsSeen(provider, 1), "the model saw every result in one call")
	assert.Empty(t, toolResultFor(t, conv, first).Error)
	assert.Empty(t, toolResultFor(t, conv, second).Error)
}

// A Continue with only some held calls settled carries on with those. The
// round it continued no longer awaits the others: approving one afterwards is
// refused and its handler does not run, rather than running a tool whose
// result the model would never see.
func TestContinue_PartialSettlementLeavesTheRestUnawaited(t *testing.T) {
	conv, provider := openSettlementConv(t, 2, []string{"Sent the first."})
	_, ran := holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message two customers")
	require.NoError(t, err)
	held := resp.PendingTools()
	require.Len(t, held, 2)
	first, second := held[0].ID, held[1].ID

	_, err = conv.ResolveTool(ctx, first)
	require.NoError(t, err)
	final, err := conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Sent the first.", final.Text())
	assert.Equal(t, []string{first}, toolResultsSeen(provider, 1))

	_, err = conv.ResolveTool(ctx, second)
	require.ErrorIs(t, err, ErrToolCallNotAwaited)
	assert.EqualValues(t, 1, ran.Load(), "the second call's handler must not run")
}

// A held call whose turn was abandoned (Send started a new one) is no longer
// awaited: approving it is refused, its handler does not run, and it leaves
// the pending store.
func TestResolveTool_RefusesACallTheTurnNoLongerAwaits(t *testing.T) {
	conv, _ := openSettlementConv(t, 1, []string{"Never mind, then."})
	_, ran := holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	id := resp.PendingTools()[0].ID
	_, err = conv.Send(ctx, "actually, don't")
	require.NoError(t, err)

	_, err = conv.ResolveTool(ctx, id)
	require.ErrorIs(t, err, ErrToolCallNotAwaited)
	assert.EqualValues(t, 0, ran.Load(), "the handler of a call nobody awaits must not run")
	stored, err := conv.PendingTools(ctx)
	require.NoError(t, err)
	assert.Empty(t, stored, "the stale hold is removed")
}

// Rejecting a call that is no longer awaited is refused the same way.
func TestRejectTool_RefusesACallTheTurnNoLongerAwaits(t *testing.T) {
	conv, _ := openSettlementConv(t, 1, []string{"Never mind, then."})
	holdEverySendMessage(conv)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	id := resp.PendingTools()[0].ID
	_, err = conv.Send(ctx, "actually, don't")
	require.NoError(t, err)

	_, err = conv.RejectTool(ctx, id, "too late")
	require.ErrorIs(t, err, ErrToolCallNotAwaited)
	stored, err := conv.PendingTools(ctx)
	require.NoError(t, err)
	assert.Empty(t, stored)
}

// When the pending store cannot record a hold, the call fails closed: it does
// not run, and the model is told why in the call's result.
func TestApprovalHold_FailsClosedWhenTheStoreCannotRecordIt(t *testing.T) {
	store := &addFailingPendingStore{PendingStore: sdktools.NewMemoryPendingStore(), err: errors.New("redis down")}
	conv, _ := openSettlementConv(t, 1, []string{"I could not send it."}, WithPendingStore(store))
	_, ran := holdEverySendMessage(conv)

	resp, err := conv.Send(context.Background(), "message the customer")
	require.NoError(t, err)

	assert.EqualValues(t, 0, ran.Load(), "a call that needed approval must not run unapproved")
	assert.Empty(t, resp.PendingTools())
	assert.Equal(t, "I could not send it.", resp.Text())
	calls := callsOfLastAssistantWithTools(t, conv)
	require.Len(t, calls, 1)
	result := toolResultFor(t, conv, calls[0])
	require.NotNil(t, result)
	assert.Contains(t, result.Error, "the call needs approval and its hold could not be recorded")
	assert.NotContains(t, result.Error, "redis down", "the store's error, which can name internal hosts, stays out of the model's input")
	assert.Equal(t, types.ToolErrorApproval, result.ErrorType)
}

// callsOfLastAssistantWithTools returns the ids of the calls the last
// assistant message that made any calls made.
func callsOfLastAssistantWithTools(t *testing.T, conv *Conversation) []string {
	t.Helper()
	history := conv.Messages(context.Background())
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == roleAssistant && len(history[i].ToolCalls) > 0 {
			ids := make([]string, 0, len(history[i].ToolCalls))
			for _, tc := range history[i].ToolCalls {
				ids = append(ids, tc.ID)
			}
			return ids
		}
	}
	return nil
}

// A gate supplied with WithApprovalGate sees the caller's context and the
// call id, and a call it holds is the host's to answer: it surfaces as a
// client tool, its handler does not run, and SendToolResult then Resume
// gives the model the host's result.
func TestWithApprovalGate_HoldsACallForTheHost(t *testing.T) {
	var sawCaller, sawCallID atomic.Value
	gate := func(ctx context.Context, callID, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		sawCaller.Store(ctx.Value(settlementCtxKey{}))
		sawCallID.Store(callID)
		return &tools.PendingToolInfo{Reason: "referral", ToolName: name}, nil
	}
	conv, _ := openSettlementConv(t, 1, []string{"Sent after review."}, WithApprovalGate(gate))
	var ran atomic.Int32
	conv.OnTool("send_message", func(args map[string]any) (any, error) {
		ran.Add(1)
		return args, nil
	})
	ctx := context.WithValue(context.Background(), settlementCtxKey{}, "agent-7")

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1, "a host-owned hold is answered by the host")
	id := resp.ClientTools()[0].CallID
	assert.Equal(t, "agent-7", sawCaller.Load(), "the gate sees the caller's context")
	assert.Equal(t, id, sawCallID.Load(), "the gate sees the provider's call id")
	assert.EqualValues(t, 0, ran.Load())

	require.NoError(t, conv.SendToolResult(ctx, id, map[string]any{"sent": true}))
	final, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Sent after review.", final.Text())
	result := toolResultFor(t, conv, id)
	require.NotNil(t, result)
	assert.JSONEq(t, `{"sent":true}`, result.GetTextContent())
}

// A gate's error refuses the call: it does not run, and the model is given
// the error as the call's result.
func TestWithApprovalGate_RefusalReachesTheModel(t *testing.T) {
	gate := func(context.Context, string, string, map[string]any) (*tools.PendingToolInfo, error) {
		return nil, errors.New("policy service unreachable")
	}
	conv, _ := openSettlementConv(t, 1, []string{"I could not send it."}, WithApprovalGate(gate))
	var ran atomic.Int32
	conv.OnTool("send_message", func(args map[string]any) (any, error) {
		ran.Add(1)
		return args, nil
	})

	resp, err := conv.Send(context.Background(), "message the customer")
	require.NoError(t, err)
	assert.EqualValues(t, 0, ran.Load())
	assert.Equal(t, "I could not send it.", resp.Text())
	calls := callsOfLastAssistantWithTools(t, conv)
	require.Len(t, calls, 1)
	result := toolResultFor(t, conv, calls[0])
	require.NotNil(t, result)
	assert.Contains(t, result.Error, "policy service unreachable")
	assert.Equal(t, types.ToolErrorApproval, result.ErrorType)
}

// A gate that makes no decision leaves it to the OnToolAsync check.
func TestWithApprovalGate_NoDecisionLeavesItToOnToolAsync(t *testing.T) {
	gate := func(context.Context, string, string, map[string]any) (*tools.PendingToolInfo, error) {
		return nil, nil
	}
	conv, _ := openSettlementConv(t, 1, []string{"Done."}, WithApprovalGate(gate))
	_, ran := holdEverySendMessage(conv)

	resp, err := conv.Send(context.Background(), "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1, "the OnToolAsync check still holds the call")
	assert.Equal(t, "requires_approval", resp.PendingTools()[0].Reason)
	assert.EqualValues(t, 0, ran.Load())
}

// An OnToolAsyncWithContext check sees the caller's context and the call id,
// and holds and resolves like OnToolAsync.
func TestOnToolAsyncWithContext_CheckSeesCallerAndCallID(t *testing.T) {
	conv, _ := openSettlementConv(t, 1, []string{"Sent."})
	var sawCaller, sawCallID atomic.Value
	var ran atomic.Int32
	conv.OnToolAsyncWithContext("send_message",
		func(ctx context.Context, callID string, _ map[string]any) sdktools.PendingResult {
			sawCaller.Store(ctx.Value(settlementCtxKey{}))
			sawCallID.Store(callID)
			return sdktools.PendingResult{Reason: "referral"}
		},
		func(args map[string]any) (any, error) {
			ran.Add(1)
			return args, nil
		})
	ctx := context.WithValue(context.Background(), settlementCtxKey{}, "agent-7")

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	id := resp.PendingTools()[0].ID
	assert.Equal(t, "agent-7", sawCaller.Load())
	assert.Equal(t, id, sawCallID.Load())
	assert.Equal(t, "referral", resp.PendingTools()[0].Reason)

	_, err = conv.ResolveTool(ctx, id)
	require.NoError(t, err)
	final, err := conv.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Sent.", final.Text())
	assert.EqualValues(t, 1, ran.Load())
}

// SendToolResult for a call the conversation no longer awaits is refused.
func TestSendToolResult_RefusesACallTheTurnNoLongerAwaits(t *testing.T) {
	gate := func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		return &tools.PendingToolInfo{Reason: "referral", ToolName: name}, nil
	}
	conv, _ := openSettlementConv(t, 1, []string{"Never mind, then."}, WithApprovalGate(gate))
	conv.OnTool("send_message", func(args map[string]any) (any, error) { return args, nil })
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1)
	id := resp.ClientTools()[0].CallID
	_, err = conv.Send(ctx, "actually, don't")
	require.NoError(t, err)

	assert.ErrorIs(t, conv.SendToolResult(ctx, id, map[string]any{"sent": true}), ErrToolCallNotAwaited)
}

// A Resume with only some host-held calls answered carries on with those, and
// a later answer for the others is refused.
func TestResume_PartialAnswersLeaveTheRestUnawaited(t *testing.T) {
	gate := func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		return &tools.PendingToolInfo{Reason: "referral", ToolName: name}, nil
	}
	conv, provider := openSettlementConv(t, 2, []string{"Reviewed the first."}, WithApprovalGate(gate))
	conv.OnTool("send_message", func(args map[string]any) (any, error) { return args, nil })
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message two customers")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 2)
	first, second := resp.ClientTools()[0].CallID, resp.ClientTools()[1].CallID

	require.NoError(t, conv.SendToolResult(ctx, first, "sent"))
	final, err := conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Reviewed the first.", final.Text())
	assert.Equal(t, []string{first}, toolResultsSeen(provider, 1))

	assert.ErrorIs(t, conv.SendToolResult(ctx, second, "sent"), ErrToolCallNotAwaited)
}

// A call the host's gate holds can be answered by another process: the
// conversation is closed, reopened with sdk.Resume on the same state store,
// answered with SendToolResult and resumed. Nothing about the hold lives in
// the process that made it.
func TestWithApprovalGate_HostHeldCallIsAnsweredInAnotherProcess(t *testing.T) {
	gate := func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		return &tools.PendingToolInfo{Reason: "referral", ToolName: name}, nil
	}
	state := statestore.NewMemoryStore()
	held, provider := openSettlementConv(t, 1, []string{"Sent after review."},
		WithApprovalGate(gate), WithStateStore(state), WithConversationID("referral-1"))
	ctx := context.Background()

	resp, err := held.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1)
	id := resp.ClientTools()[0].CallID
	require.NoError(t, held.Close())

	resumed, err := Resume("referral-1", writeGatePack(t), "assist",
		WithProvider(provider), WithSkipSchemaValidation(), WithApprovalGate(gate), WithStateStore(state))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resumed.Close() })
	require.NoError(t, resumed.SendToolResult(ctx, id, map[string]any{"sent": true}))
	final, err := resumed.Resume(ctx)
	require.NoError(t, err)

	assert.Equal(t, "Sent after review.", final.Text())
	assert.Equal(t, []string{id}, toolResultsSeen(provider, 1), "the model got the host's answer")
	assert.JSONEq(t, `{"sent":true}`, toolResultFor(t, resumed, id).GetTextContent())
}
