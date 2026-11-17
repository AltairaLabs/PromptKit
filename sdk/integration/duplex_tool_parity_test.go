package integration

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// A realtime (ASM) session's tool calls run through the same tool runner as a
// text turn's: tool policy, call-rate limit, tool hooks, result rewriting,
// events and the approval gate all apply (#2696, #2700).

// drainDuplexResponse reads the conversation's response channel in the
// background until it closes, so the session never stalls on its output. The
// returned channel is closed once the response channel is.
func drainDuplexResponse(t *testing.T, conv *sdk.Conversation) <-chan struct{} {
	t.Helper()
	responseCh, err := conv.Response()
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range responseCh { //nolint:revive // draining
		}
	}()
	return done
}

// firstToolResponses sends text and returns the tool responses the provider
// session is given for the model's tool calls.
func firstToolResponses(
	t *testing.T, conv *sdk.Conversation, provider *toolAwareStreamingProvider, text string,
) []providers.ToolResponse {
	t.Helper()
	got, _ := firstToolResponsesAndEnd(t, conv, provider, text)
	return got
}

// firstToolResponsesAndEnd is firstToolResponses that also returns a channel
// closed once the conversation's response channel closes: the mock session
// closes after its post-tool reply, so the turn is then over.
func firstToolResponsesAndEnd(
	t *testing.T, conv *sdk.Conversation, provider *toolAwareStreamingProvider, text string,
) ([]providers.ToolResponse, <-chan struct{}) {
	t.Helper()
	ended := drainDuplexResponse(t, conv)
	require.NoError(t, conv.SendText(context.Background(), text))
	require.Eventually(t, func() bool { return len(provider.getSessions()) == 1 }, 2*time.Second, 5*time.Millisecond)
	sess := provider.getSessions()[0]
	require.True(t, waitForToolResponses(sess, 1, 3*time.Second), "the provider was never given tool results")
	sess.responsesMu.Lock()
	defer sess.responsesMu.Unlock()
	return sess.receivedResponses[0], ended
}

// awaitEnd waits for a response channel drained by drainDuplexResponse to close.
func awaitEnd(t *testing.T, ended <-chan struct{}) {
	t.Helper()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the conversation's response channel never closed")
	}
}

// denyToolHook denies every call of one tool.
type denyToolHook struct {
	tool, reason string
}

func (h denyToolHook) Name() string { return "deny-" + h.tool }

func (h denyToolHook) BeforeExecution(_ context.Context, req hooks.ToolRequest) hooks.Decision {
	if req.Name == h.tool {
		return hooks.Deny(h.reason)
	}
	return hooks.Allow
}

func (h denyToolHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

// redactingHook rewrites every tool result to a fixed text.
type redactingHook struct{ replacement string }

func (h redactingHook) Name() string { return "redact" }

func (h redactingHook) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (h redactingHook) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (h redactingHook) RewriteResult(context.Context, hooks.ToolRequest, string) (string, error) {
	return h.replacement, nil
}

func refundCall(id string, amount int) types.MessageToolCall {
	args, _ := json.Marshal(map[string]any{"amount": amount, "order_id": "ORD-1"})
	return types.MessageToolCall{ID: id, Name: "process_refund", Args: args}
}

func TestDuplexToolParity_BeforeHookDenialReachesTheModel(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok",
		sdk.WithToolHook(denyToolHook{tool: "process_refund", reason: "refunds are frozen"}))
	var runs atomic.Int32
	conv.OnTool("process_refund", func(map[string]any) (any, error) {
		runs.Add(1)
		return map[string]any{"status": "refunded"}, nil
	})

	got := firstToolResponses(t, conv, provider, "refund it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{
		ToolCallID: "call_1",
		Result:     "Tool process_refund blocked by hook: refunds are frozen",
		IsError:    true,
	}, got[0])
	assert.Equal(t, int32(0), runs.Load(), "a denied call must not run")
}

func TestDuplexToolParity_BlocklistedToolIsRefused(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok",
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{Blocklist: []string{"process_refund"}}))
	var runs atomic.Int32
	conv.OnTool("process_refund", func(map[string]any) (any, error) {
		runs.Add(1)
		return map[string]any{"status": "refunded"}, nil
	})

	got := firstToolResponses(t, conv, provider, "refund it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{
		ToolCallID: "call_1", Result: "Tool process_refund is blocked by policy", IsError: true,
	}, got[0])
	assert.Equal(t, int32(0), runs.Load(), "a blocklisted call must not run")
}

func TestDuplexToolParity_MaxCallsPerMinuteRefusesTheCallOverTheLimit(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 10), refundCall("call_2", 20)}, "ok",
		sdk.WithToolPolicy(&rtpipeline.ToolPolicy{MaxCallsPerMinute: 1}))
	var runs atomic.Int32
	conv.OnTool("process_refund", func(map[string]any) (any, error) {
		runs.Add(1)
		return "refunded", nil
	})

	got := firstToolResponses(t, conv, provider, "refund both")

	require.Len(t, got, 2)
	const refusal = "Tool process_refund not executed: its limit of 1 calls per minute is reached"
	var refused, ran []string
	for _, r := range got {
		switch {
		case r.IsError && r.Result == refusal:
			refused = append(refused, r.ToolCallID)
		case !r.IsError && r.Result == "refunded":
			ran = append(ran, r.ToolCallID)
		default:
			t.Errorf("unexpected tool response %+v", r)
		}
	}
	assert.Len(t, refused, 1, "exactly one call is over the limit")
	assert.Len(t, ran, 1, "exactly one call is admitted")
	assert.Equal(t, int32(1), runs.Load(), "the call over the limit must not run")
}

func TestDuplexToolParity_ResultRewriterRedactsTheRealtimeResult(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok",
		sdk.WithToolHook(redactingHook{replacement: "[redacted]"}))
	conv.OnTool("process_refund", func(map[string]any) (any, error) {
		return map[string]any{"card": "4111-1111-1111-1111"}, nil
	})

	got := firstToolResponses(t, conv, provider, "refund it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{ToolCallID: "call_1", Result: "[redacted]"}, got[0])
}

func TestDuplexToolParity_ToolAndProviderEventsArePublished(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	ec := newEventCollector(bus)
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok", sdk.WithEventBus(bus))
	provider.toolCallCost = &types.CostInfo{InputTokens: 12, OutputTokens: 4, TotalCost: 0.0375}
	conv.OnTool("process_refund", func(map[string]any) (any, error) { return "refunded", nil })

	got, ended := firstToolResponsesAndEnd(t, conv, provider, "refund it")
	require.Len(t, got, 1)

	started := ec.waitForType(events.EventToolCallStarted, 2*time.Second)
	require.Len(t, started, 1)
	startedData, ok := started[0].Data.(*events.ToolCallEventData)
	require.True(t, ok)
	assert.Equal(t, "process_refund", startedData.ToolName)
	assert.Equal(t, "call_1", startedData.CallID)
	assert.Equal(t, map[string]any{"amount": float64(50), "order_id": "ORD-1"}, startedData.Args)
	assert.Equal(t, 1, startedData.Round)

	completed := ec.waitForType(events.EventToolCallCompleted, 2*time.Second)
	require.Len(t, completed, 1)
	completedData, ok := completed[0].Data.(*events.ToolCallEventData)
	require.True(t, ok)
	assert.Equal(t, "call_1", completedData.CallID)
	assert.Equal(t, "complete", completedData.Status)

	var toolTurn *events.ProviderCallCompletedData
	require.Eventually(t, func() bool {
		for _, e := range ec.ofType(events.EventProviderCallCompleted) {
			if d, ok := e.Data.(*events.ProviderCallCompletedData); ok && d.ToolCallCount == 1 {
				toolTurn = d
				return true
			}
		}
		return false
	}, 2*time.Second, 5*time.Millisecond, "no provider.call.completed for the turn that made the tool call")
	assert.Equal(t, 0.0375, toolTurn.Cost)
	assert.Equal(t, 12, toolTurn.InputTokens)
	assert.Equal(t, 4, toolTurn.OutputTokens)
	assert.Equal(t, 1, toolTurn.Round)
	assert.Equal(t, startedData.ProviderCallID, toolTurn.CallID,
		"the tool call names the provider call that made it")

	// The session closes after its post-tool reply: by then both rounds have
	// completed, each exactly once.
	awaitEnd(t, ended)
	perRound := func() map[int]int {
		counts := map[int]int{}
		for _, e := range ec.ofType(events.EventProviderCallCompleted) {
			if d, ok := e.Data.(*events.ProviderCallCompletedData); ok {
				counts[d.Round]++
			}
		}
		return counts
	}
	require.Eventually(t, func() bool { return perRound()[2] > 0 }, 2*time.Second, 5*time.Millisecond)
	assert.Equal(t, map[int]int{1: 1, 2: 1}, perRound(), "provider.call.completed per round")
}

func TestDuplexToolParity_CallExecutorReceivesTheConversationsCaller(t *testing.T) {
	caller := types.Caller{ID: "user-42", Claims: map[string]any{"tier": "gold"}}
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok", sdk.WithCaller(caller))
	exec := &recordingCallExecutor{}
	conv.OnToolExecutor("process_refund", exec)

	got := firstToolResponses(t, conv, provider, "refund it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{ToolCallID: "call_1", Result: "{\n  \"ok\": true\n}"}, got[0])
	calls := exec.recorded()
	require.Len(t, calls, 1)
	assert.Equal(t, "call_1", calls[0].ID)
	assert.Equal(t, caller, calls[0].Caller)
}

// recordingCallExecutor is a tools.CallExecutor that records each call.
type recordingCallExecutor struct {
	mu    sync.Mutex
	calls []tools.ToolCall
}

func (e *recordingCallExecutor) Name() string { return "recording" }

func (e *recordingCallExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

func (e *recordingCallExecutor) ExecuteCall(
	_ context.Context, _ *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, call)
	return json.RawMessage(`{"ok":true}`), nil, nil
}

func (e *recordingCallExecutor) recorded() []tools.ToolCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]tools.ToolCall(nil), e.calls...)
}

// An OnToolAsyncCall registered after OpenDuplex whose check lets the call
// through runs its execution handler with the provider's call ID and the
// conversation's caller.
func TestDuplexToolParity_OnToolAsyncCallAfterOpenRunsWithCallerAndCallID(t *testing.T) {
	caller := types.Caller{ID: "agent-7"}
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_9", 50)}, "ok", sdk.WithCaller(caller))
	var mu sync.Mutex
	var checked, executed []sdk.ToolCall
	conv.OnToolAsyncCall("process_refund",
		func(_ context.Context, call sdk.ToolCall) sdktools.PendingResult {
			mu.Lock()
			checked = append(checked, call)
			mu.Unlock()
			return sdktools.PendingResult{}
		},
		func(_ context.Context, call sdk.ToolCall) (any, error) {
			mu.Lock()
			executed = append(executed, call)
			mu.Unlock()
			return map[string]any{"status": "refunded"}, nil
		})

	got := firstToolResponses(t, conv, provider, "refund it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{ToolCallID: "call_9", Result: "{\n  \"status\": \"refunded\"\n}"}, got[0])
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, checked, 1)
	assert.Equal(t, "call_9", checked[0].CallID)
	assert.Equal(t, caller, checked[0].Caller)
	require.Len(t, executed, 1)
	assert.Equal(t, "call_9", executed[0].CallID)
	assert.Equal(t, caller, executed[0].Caller)
	assert.Equal(t, map[string]any{"amount": float64(50), "order_id": "ORD-1"}, executed[0].Args)
}

// hostToolStreamingConfig declares lookup_order on the session only: the pack
// does not define it.
func hostToolStreamingConfig() *providers.StreamingInputConfig {
	return &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		Tools: []providers.StreamingToolDefinition{{
			Name:        "lookup_order",
			Description: "Look up an order",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"order_id": map[string]any{"type": "string"}},
			},
		}},
	}
}

func lookupOrderCall(id string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "lookup_order", Args: json.RawMessage(`{"order_id":"ORD-5"}`)}
}

// A tool the host declares on the streaming config alone, with an OnToolAsync
// registered after OpenDuplex, runs when its check lets the call through.
func TestDuplexToolParity_HostDeclaredAsyncToolAfterOpenRuns(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON,
		[]types.MessageToolCall{lookupOrderCall("call_1")}, "ok",
		sdk.WithStreamingConfig(hostToolStreamingConfig()))
	conv.OnToolAsync("lookup_order",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		func(args map[string]any) (any, error) {
			return map[string]any{"order": args["order_id"], "state": "shipped"}, nil
		})

	got := firstToolResponses(t, conv, provider, "where is my order")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{
		ToolCallID: "call_1", Result: "{\n  \"order\": \"ORD-5\",\n  \"state\": \"shipped\"\n}",
	}, got[0])
}

// The same host-declared tool, held by its check: the hold is surfaced, and
// ResolveTool + ContinueDuplex run it and give the model its result.
func TestDuplexToolParity_HostDeclaredAsyncToolHeldThenResolved(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON,
		[]types.MessageToolCall{lookupOrderCall("call_1")}, "ok",
		sdk.WithStreamingConfig(hostToolStreamingConfig()))
	var runs atomic.Int32
	conv.OnToolAsync("lookup_order",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "pii", Message: "needs approval"}
		},
		func(map[string]any) (any, error) {
			runs.Add(1)
			return "shipped", nil
		})
	responseCh, err := conv.Response()
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, conv.SendText(ctx, "where is my order"))

	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	var pending []tools.PendingToolExecution
	for i := range chunks {
		pending = append(pending, chunks[i].PendingTools...)
	}
	require.Len(t, pending, 1)
	assert.Equal(t, "call_1", pending[0].CallID)
	assert.Equal(t, "lookup_order", pending[0].ToolName)
	require.NotNil(t, pending[0].PendingInfo)
	assert.Equal(t, "pii", pending[0].PendingInfo.Reason)
	assert.Equal(t, int32(0), runs.Load(), "a held call must not run before approval")

	_, err = conv.ResolveTool(ctx, "call_1")
	require.NoError(t, err)
	require.NoError(t, conv.ContinueDuplex(ctx))
	go func() {
		for range responseCh { //nolint:revive // draining
		}
	}()

	sess := provider.getSessions()[0]
	require.True(t, waitForToolResponses(sess, 1, 3*time.Second))
	sess.responsesMu.Lock()
	defer sess.responsesMu.Unlock()
	require.Len(t, sess.receivedResponses, 1, "only the approved result reaches the model")
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "call_1", Result: "shipped"}}, sess.receivedResponses[0],
		"an approved result reaches the model as a directly run one does")
	assert.Equal(t, int32(1), runs.Load())
}

// A call the realtime model makes runs once. The tool has both a call handler
// (OnToolCall) and an async registration whose check lets the call through:
// the session running the call itself as well as the pipeline would run one
// of them twice.
func TestDuplexToolParity_ACallRunsOnce(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok")
	var callRuns, asyncRuns atomic.Int32
	conv.OnToolCall("process_refund", func(context.Context, sdk.ToolCall) (any, error) {
		callRuns.Add(1)
		return "refunded", nil
	})
	conv.OnToolAsync("process_refund",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		func(map[string]any) (any, error) {
			asyncRuns.Add(1)
			return "refunded", nil
		})

	got, ended := firstToolResponsesAndEnd(t, conv, provider, "refund it")
	awaitEnd(t, ended)

	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "call_1", Result: "refunded"}}, got)
	assert.Equal(t, int32(1), callRuns.Load()+asyncRuns.Load(), "the call ran more than once")
	assert.Equal(t, int32(1), provider.getSessions()[0].toolResponsesReceived.Load(), "the provider is answered once")
}

// Ported from the deleted SDK duplex executor's unit tests: each case now runs
// through OpenDuplex and the pipeline's tool runner.

// Two client tools with no handler are both held for the caller, with their
// arguments, and the provider is answered for neither.
func TestDuplexToolParity_ClientToolsWithoutHandlersAreAllHeld(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "c1", Name: "get_location", Args: json.RawMessage(`{"accuracy":"fine"}`)},
		{ID: "c2", Name: "read_contacts", Args: json.RawMessage(`{"limit":3}`)},
	}
	conv, provider := openDuplexToolConv(t, duplexMultiToolsPackJSON, calls, "ok")
	patchToolMode(t, conv, "get_location", "client")
	patchToolMode(t, conv, "read_contacts", "client")
	responseCh, err := conv.Response()
	require.NoError(t, err)
	require.NoError(t, conv.SendText(context.Background(), "where and who"))

	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	held := map[string]tools.PendingToolExecution{}
	for i := range chunks {
		for _, p := range chunks[i].PendingTools {
			held[p.CallID] = p
		}
	}
	require.Len(t, held, 2)
	assert.Equal(t, "get_location", held["c1"].ToolName)
	assert.Equal(t, map[string]any{"accuracy": "fine"}, held["c1"].Args)
	assert.Equal(t, "read_contacts", held["c2"].ToolName)
	assert.Equal(t, map[string]any{"limit": float64(3)}, held["c2"].Args)
	assert.Equal(t, int32(0), provider.getSessions()[0].toolResponsesReceived.Load(),
		"held calls are answered by the caller, not the pipeline")
}

// A handler that fails gives the model an error result naming the failure.
func TestDuplexToolParity_HandlerErrorIsTheCallsResult(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{refundCall("call_1", 50)}, "ok")
	conv.OnTool("process_refund", func(map[string]any) (any, error) {
		return nil, errors.New("payments service unavailable")
	})

	got := firstToolResponses(t, conv, provider, "refund it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{ToolCallID: "call_1", Result: "Tool execution failed: payments service unavailable", IsError: true}, got[0])
}

// A call for a tool nobody declared gives the model an error result.
func TestDuplexToolParity_UnknownToolIsAnErrorResult(t *testing.T) {
	conv, provider := openDuplexToolConv(t, duplexHITLPackJSON,
		[]types.MessageToolCall{{ID: "call_1", Name: "nonexistent", Args: json.RawMessage(`{}`)}}, "ok")

	got := firstToolResponses(t, conv, provider, "do it")

	require.Len(t, got, 1)
	assert.Equal(t, providers.ToolResponse{ToolCallID: "call_1", Result: "Error: tool not found: nonexistent", IsError: true}, got[0])
}

// Two calls of a round both run, and the provider is answered for both in one
// batch.
func TestDuplexToolParity_EveryCallOfARoundIsAnswered(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "c1", Name: "get_status", Args: json.RawMessage(`{"order_id":"ORD-1"}`)},
		refundCall("c2", 10),
	}
	conv, provider := openDuplexToolConv(t, duplexHITLMultiPackJSON, calls, "ok")
	conv.OnTool("get_status", func(args map[string]any) (any, error) { return "shipped " + args["order_id"].(string), nil })
	conv.OnTool("process_refund", func(map[string]any) (any, error) { return "refunded", nil })

	got := firstToolResponses(t, conv, provider, "status and refund")

	assert.ElementsMatch(t, []providers.ToolResponse{
		{ToolCallID: "c1", Result: "shipped ORD-1"},
		{ToolCallID: "c2", Result: "refunded"},
	}, got)
}
