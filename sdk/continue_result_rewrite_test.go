package sdk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// wrappingRewriter is a ToolResultRewriter that wraps every result's text in
// "<redacted:...>" (or fails with err) and records the requests it was given.
type wrappingRewriter struct {
	mu   sync.Mutex
	reqs []hooks.ToolRequest
	// ctxIDs holds the call ID each rewrite's context carried.
	ctxIDs []string
	err    error
}

func (h *wrappingRewriter) Name() string { return "wrap" }

func (h *wrappingRewriter) BeforeExecution(context.Context, hooks.ToolRequest) hooks.Decision {
	return hooks.Allow
}

func (h *wrappingRewriter) AfterExecution(context.Context, hooks.ToolRequest, hooks.ToolResponse) hooks.Decision {
	return hooks.Allow
}

func (h *wrappingRewriter) RewriteResult(ctx context.Context, req hooks.ToolRequest, content string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs = append(h.reqs, req)
	h.ctxIDs = append(h.ctxIDs, tools.CallIDFromContext(ctx))
	if h.err != nil {
		return "", h.err
	}
	return "<redacted:" + content + ">", nil
}

func (h *wrappingRewriter) requests() []hooks.ToolRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hooks.ToolRequest(nil), h.reqs...)
}

var rewriteCaller = types.Caller{ID: "user-7", Claims: map[string]any{"tier": "gold"}}

// An approved call's result reaches the model rewritten by the conversation's
// ToolResultRewriter, before the size limit cuts it, as the stage does.
func TestContinue_RewritesAnApprovedResultBeforeTruncating(t *testing.T) {
	rw := &wrappingRewriter{}
	got := heldCallContinued(t, "abcdefghij", approve,
		WithToolHook(rw), limitedTools(t, 15, "send_message"))
	assert.Equal(t, "<redacted:abcde\n... [truncated, 21 bytes exceeded limit of 15 bytes]", got.GetTextContent())
	assert.Empty(t, got.Error)
}

// A client tool's result answered through SendToolResult and delivered by
// Continue is rewritten too.
func TestContinue_RewritesAClientToolResult(t *testing.T) {
	rw := &wrappingRewriter{}
	got := heldCallContinued(t, "unused", func(ctx context.Context, conv *Conversation, id string) error {
		return conv.SendToolResult(ctx, id, map[string]any{"ok": true})
	}, WithToolHook(rw))
	assert.Equal(t, "<redacted:{\n  \"ok\": true\n}>", got.GetTextContent())
}

// failedEvents records the tool.call.failed events published on bus.
func failedEvents(bus *events.EventBus) func() []events.Event {
	var mu sync.Mutex
	var got []events.Event
	bus.Subscribe(events.EventToolCallFailed, func(e *events.Event) {
		mu.Lock()
		got = append(got, *e)
		mu.Unlock()
	})
	return func() []events.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]events.Event(nil), got...)
	}
}

// A rewriter's error takes the place of the result exactly as on a call the
// stage ran: the same text, error and error type, and a tool.call.failed
// event with the same session, conversation and user. The model never sees
// the raw result.
func TestContinue_RewriterErrorMatchesTheStage(t *testing.T) {
	const errText = "rewriting the send_message result: redactor offline"
	bus := events.NewEventBus()
	failed := failedEvents(bus)
	conv, provider := openSettlementConv(t, 1, []string{"Done.", "Sending.", "Done again."},
		WithToolHook(&wrappingRewriter{err: errors.New("redactor offline")}), WithEventBus(bus),
		WithConversationID("conv-42"), WithUserID("user-7"))
	provider.mu.Lock()
	// The second turn calls send_message again, and the call is held.
	provider.rounds[2].ToolCalls = []types.MessageToolCall{{ID: "call-2", Name: "send_message", Args: []byte(`{"body":"2"}`)}}
	provider.mu.Unlock()
	var hold atomic.Bool
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			if hold.Load() {
				return sdktools.PendingResult{Reason: "requires_approval"}
			}
			return sdktools.PendingResult{}
		},
		func(map[string]any) (any, error) { return "secret", nil })
	ctx := context.Background()

	_, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	hold.Store(true)
	resp, err := conv.Send(ctx, "and again")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	_, err = conv.ResolveTool(ctx, "call-2")
	require.NoError(t, err)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)

	provider.mu.Lock()
	results := map[string]types.MessageToolResult{}
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			results[m.ToolResult.ID] = *m.ToolResult
		}
	}
	provider.mu.Unlock()
	viaStage, viaContinue := results["call-1"], results["call-2"]
	assert.Equal(t, "Error: "+errText, viaContinue.GetTextContent())
	assert.Equal(t, errText, viaContinue.Error)
	assert.Equal(t, types.ToolErrorExecution, viaContinue.ErrorType)
	viaStage.ID, viaStage.Name = viaContinue.ID, viaContinue.Name
	assert.Equal(t, viaStage, viaContinue)

	require.Eventually(t, func() bool { return len(failed()) == 2 }, 2*time.Second, 5*time.Millisecond)
	stageEv, continueEv := failed()[0], failed()[1]
	for _, ev := range []events.Event{stageEv, continueEv} {
		assert.Equal(t, "conv-42", ev.ConversationID)
		assert.Equal(t, "conv-42", ev.SessionID)
		assert.Equal(t, "user-7", ev.UserID)
	}
	data := continueEv.Data.(*events.ToolCallEventData)
	assert.Equal(t, "send_message", data.ToolName)
	assert.Equal(t, "call-2", data.CallID)
	require.Error(t, data.Error)
	assert.Equal(t, errText, data.Error.Error())
}

// A call approved with edits is rewritten with the arguments it ran with.
func TestContinue_RewriterSeesTheEditedArguments(t *testing.T) {
	rw := &wrappingRewriter{}
	heldCallContinued(t, "sent", func(ctx context.Context, conv *Conversation, id string) error {
		_, err := conv.ResolveToolWithArgs(ctx, id, map[string]any{"body": "edited"})
		return err
	}, WithToolHook(rw))
	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.JSONEq(t, `{"body":"edited"}`, string(reqs[0].Args))
}

// The history holds the rewritten result the model was given, not the raw one.
func TestContinue_HistoryHoldsTheRewrittenResult(t *testing.T) {
	_, conv := heldCallContinuedIn(t, "secret", approve, WithToolHook(&wrappingRewriter{}))
	var stored []string
	for _, m := range conv.Messages(context.Background()) {
		if m.ToolResult != nil {
			stored = append(stored, m.ToolResult.GetTextContent())
		}
	}
	assert.Equal(t, []string{"<redacted:secret>"}, stored)
}

// A client tool's answer delivered with Resume is rewritten too, as the
// client sent it (not formatted or truncated), and the history holds the
// rewritten text.
func TestResume_RewritesAClientToolResult(t *testing.T) {
	calls := []types.MessageToolCall{{ID: "c-cli", Name: "locate", Args: []byte(`{"q":"x"}`)}}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Locating.", ToolCalls: calls}},
	}
	rw := &wrappingRewriter{}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider),
		WithSkipSchemaValidation(), WithToolHook(rw), WithCaller(rewriteCaller))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"

	ctx := context.Background()
	resp, err := conv.Send(ctx, "where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())
	require.NoError(t, conv.SendToolResult(ctx, "c-cli", map[string]any{"city": "Paris"}))
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	provider.mu.Lock()
	var seen []string
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			seen = append(seen, m.ToolResult.GetTextContent())
		}
	}
	provider.mu.Unlock()
	assert.Equal(t, []string{`<redacted:{"city":"Paris"}>`}, seen, "the client's JSON, rewritten, unformatted")
	var stored []string
	for _, m := range conv.Messages(ctx) {
		if m.ToolResult != nil {
			stored = append(stored, m.ToolResult.GetTextContent())
		}
	}
	assert.Equal(t, []string{`<redacted:{"city":"Paris"}>`}, stored)
	reqs := rw.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "locate", reqs[0].Name)
	assert.Equal(t, "c-cli", reqs[0].CallID)
	assert.JSONEq(t, `{"q":"x"}`, string(reqs[0].Args))
	assert.Equal(t, rewriteCaller, reqs[0].Caller)
}

// A rewriter's error on Resume replaces the client's answer with the stage's
// tool error: the raw answer is not delivered.
func TestResume_RewriterErrorReplacesTheResult(t *testing.T) {
	calls := []types.MessageToolCall{{ID: "c-cli", Name: "locate", Args: []byte(`{}`)}}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Locating.", ToolCalls: calls}},
	}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider),
		WithSkipSchemaValidation(), WithToolHook(&wrappingRewriter{err: errors.New("redactor offline")}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"

	ctx := context.Background()
	_, err = conv.Send(ctx, "where am I?")
	require.NoError(t, err)
	require.NoError(t, conv.SendToolResult(ctx, "c-cli", "Paris"))
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	var got *types.MessageToolResult
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			got = m.ToolResult
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, "Error: rewriting the locate result: redactor offline", got.GetTextContent())
	assert.Equal(t, "rewriting the locate result: redactor offline", got.Error)
	assert.Equal(t, types.ToolErrorExecution, got.ErrorType)
}

// The request for a resolution is taken from the call the suspended turn
// made, else from the held call the conversation admitted it from.
func TestRewriteCall_FallsBackToTheAdmittedCall(t *testing.T) {
	conv := newTestConversation()
	none := func() map[string]types.MessageToolCall { return map[string]types.MessageToolCall{} }
	conv.recordAdmittedRaw("c9", "send_message", nil, map[string]any{"body": "held"})

	call, ok := conv.callFor(none, &sdktools.ToolResolution{ID: "c9"})
	require.True(t, ok)
	assert.Equal(t, "send_message", call.Name)
	assert.Equal(t, "c9", call.ID)
	assert.JSONEq(t, `{"body":"held"}`, string(call.Args))

	_, ok = conv.callFor(none, &sdktools.ToolResolution{ID: "unknown"})
	assert.False(t, ok, "neither the history nor a held call names it")
}

// The rewriter is given the same request for an approved call as the stage
// gives it for the same call run in the turn: name, call ID, the model's
// arguments, the caller and the tool's declaration.
func TestContinue_RewriterSeesTheRequestTheStageGives(t *testing.T) {
	viaStage := &wrappingRewriter{}
	ran, _ := openSettlementConv(t, 1, []string{"Done."}, WithToolHook(viaStage), WithCaller(rewriteCaller))
	ran.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		func(map[string]any) (any, error) { return "sent", nil })
	_, err := ran.Send(context.Background(), "message the customer")
	require.NoError(t, err)

	viaContinue := &wrappingRewriter{}
	heldCallContinued(t, "sent", approve, WithToolHook(viaContinue), WithCaller(rewriteCaller))

	stageReqs, continueReqs := viaStage.requests(), viaContinue.requests()
	require.Len(t, stageReqs, 1)
	require.Len(t, continueReqs, 1)
	assert.Equal(t, "send_message", continueReqs[0].Name)
	assert.Equal(t, "call-1", continueReqs[0].CallID)
	assert.JSONEq(t, `{"body":"1"}`, string(continueReqs[0].Args))
	assert.Equal(t, rewriteCaller, continueReqs[0].Caller)
	require.NotNil(t, continueReqs[0].Declaration)
	assert.Equal(t, "send_message", continueReqs[0].Declaration.Name)
	assert.Equal(t, stageReqs[0], continueReqs[0])
	assert.Equal(t, []string{"call-1"}, viaContinue.ctxIDs, "the rewrite's context carries the call ID")
	assert.Equal(t, viaStage.ctxIDs, viaContinue.ctxIDs)
}

// A rejected call's text is not a tool result, so no rewriter sees it.
func TestContinue_RejectionIsNotRewritten(t *testing.T) {
	rw := &wrappingRewriter{}
	got := heldCallContinued(t, "unused", func(ctx context.Context, conv *Conversation, id string) error {
		_, err := conv.RejectTool(ctx, id, "no")
		return err
	}, WithToolHook(rw))
	assert.Equal(t, "Tool call rejected: no", got.GetTextContent())
	assert.Empty(t, rw.requests())
}

// A multimodal client answer delivered with Resume has its text parts
// rewritten and its media kept where they are.
func TestResume_RewritesAMultimodalAnswersTextParts(t *testing.T) {
	calls := []types.MessageToolCall{{ID: "c-cli", Name: "locate", Args: []byte(`{}`)}}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Locating.", ToolCalls: calls}},
	}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider),
		WithSkipSchemaValidation(), WithToolHook(&wrappingRewriter{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"

	ctx := context.Background()
	_, err = conv.Send(ctx, "where am I?")
	require.NoError(t, err)
	image := types.NewImagePartFromData("aGk=", "image/png", nil)
	require.NoError(t, conv.SendToolResultMultimodal(ctx, "c-cli", []types.ContentPart{types.NewTextPart("a photo"), image}))
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	var got *types.MessageToolResult
	for _, m := range provider.seen[len(provider.seen)-1] {
		if m.ToolResult != nil {
			got = m.ToolResult
		}
	}
	require.NotNil(t, got)
	require.Len(t, got.Parts, 2)
	assert.Equal(t, "<redacted:a photo>", *got.Parts[0].Text)
	assert.Equal(t, image, got.Parts[1])
}
