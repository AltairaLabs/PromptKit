package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
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

// A rewriter's error takes the place of the result, as on a call the stage
// ran: the model is told the call failed and never sees the raw result.
func TestContinue_RewriterErrorReplacesTheResult(t *testing.T) {
	rw := &wrappingRewriter{err: errors.New("redactor offline")}
	got := heldCallContinued(t, "secret", approve, WithToolHook(rw))
	assert.Equal(t, "Tool error: rewriting the send_message result: redactor offline", got.GetTextContent())
	assert.Equal(t, "rewriting the send_message result: redactor offline", got.Error)
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
