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

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// sendMessageOutputSchema requires a send_message result to be an object with
// an integer "sent" of at most 2^53.
const sendMessageOutputSchema = `{"type":"object","required":["sent"],` +
	`"properties":{"sent":{"type":"integer","maximum":9007199254740992}}}`

// sendMessageResult runs send_message, which returns result, and returns the
// tool result the model was given for it, with the conversation's
// ToolResolution when held is set: the call is then held, approved with
// ResolveTool and delivered by Continue; otherwise the turn runs it.
func sendMessageResult(
	t *testing.T, result any, held bool, opts ...Option,
) (*types.MessageToolResult, *sdktools.ToolResolution) {
	t.Helper()
	return sendMessageRun(t, func(map[string]any) (any, error) { return result, nil }, held, opts...)
}

// sendMessageRun is sendMessageResult for a send_message run by handler.
func sendMessageRun(
	t *testing.T, handler func(map[string]any) (any, error), held bool, opts ...Option,
) (*types.MessageToolResult, *sdktools.ToolResolution) {
	t.Helper()
	conv, provider := openSettlementConv(t, 1, []string{"Done."}, opts...)
	desc, err := conv.ToolRegistry().GetTool("send_message")
	require.NoError(t, err)
	desc.OutputSchema = json.RawMessage(sendMessageOutputSchema)
	desc.Labels = map[string]string{"team": "comms"}
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			if held {
				return sdktools.PendingResult{Reason: "requires_approval"}
			}
			return sdktools.PendingResult{}
		},
		handler,
	)
	ctx := context.Background()
	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	var resolution *sdktools.ToolResolution
	if held {
		require.Len(t, resp.PendingTools(), 1)
		resolution, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
		require.NoError(t, err)
		_, err = conv.Continue(ctx)
		require.NoError(t, err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Len(t, provider.seen, 2)
	for _, m := range provider.seen[1] {
		if m.ToolResult != nil {
			return m.ToolResult, resolution
		}
	}
	t.Fatal("the model saw no tool result")
	return nil, nil
}

// An approved call whose result does not match the tool's output schema
// reaches the model as the failure the turn reports for a call it runs: the
// same validation error and error type, the raw result withheld. The host
// that approved it is told why in the resolution's error.
func TestResolveTool_ResultViolatingTheOutputSchemaFailsAsInTurn(t *testing.T) {
	invalid := map[string]any{"sent": "yes", "secret": "4111"}
	inTurn, _ := sendMessageResult(t, invalid, false)
	approved, resolution := sendMessageResult(t, invalid, true)

	require.Contains(t, inTurn.Error, "result validation failed: ")
	assert.Equal(t, inTurn.Error, approved.Error)
	assert.Equal(t, inTurn.ErrorType, approved.ErrorType)
	assert.Equal(t, "Tool execution failed: "+inTurn.Error, approved.GetTextContent())
	assert.Equal(t, inTurn.GetTextContent(), approved.GetTextContent())

	var toolErr *ToolError
	require.ErrorAs(t, resolution.Error, &toolErr)
	assert.Equal(t, "send_message", toolErr.ToolName)
	assert.Equal(t, inTurn.Error, toolErr.Cause.Error())
	assert.Nil(t, resolution.ResultJSON)
	assert.Nil(t, resolution.Result)
}

// A valid approved result reaches the model as the turn gives a call's
// result, through the same coercion: 2^53+1 is over the schema's maximum as
// written, and passes once coercion normalizes it through a float to 2^53.
func TestResolveTool_ValidResultIsCoercedAsInTurn(t *testing.T) {
	valid := json.RawMessage(`{"sent":9007199254740993}`)
	inTurn, _ := sendMessageResult(t, valid, false)
	approved, resolution := sendMessageResult(t, valid, true)

	assert.Equal(t, "{\n  \"sent\": 9007199254740992\n}", inTurn.GetTextContent())
	assert.Equal(t, inTurn.GetTextContent(), approved.GetTextContent())
	assert.Empty(t, approved.Error)
	assert.NoError(t, resolution.Error)
	assert.Equal(t, `{"sent":9007199254740992}`, string(resolution.ResultJSON))
	assert.Equal(t, map[string]any{"sent": float64(9007199254740992)}, resolution.Result,
		"Result carries the coerced value too")
}

// A ToolResultRewriter is given the approved result after validation: a
// valid one as coerced, and never an invalid one.
func TestResolveTool_RewriterSeesTheValidatedResult(t *testing.T) {
	rw := &wrappingRewriter{}
	approved, _ := sendMessageResult(t, json.RawMessage(`{"sent":9007199254740993}`), true, WithToolHook(rw))
	assert.Equal(t, "<redacted:{\n  \"sent\": 9007199254740992\n}>", approved.GetTextContent())
	require.Len(t, rw.requests(), 1)

	rw = &wrappingRewriter{}
	approved, _ = sendMessageResult(t, map[string]any{"sent": "yes"}, true, WithToolHook(rw))
	assert.Contains(t, approved.Error, "result validation failed: ")
	assert.NotContains(t, approved.GetTextContent(), "redacted")
	assert.Empty(t, rw.requests(), "an invalid result is never given to the rewriter")
}

// A handler's own failure is reported as it was: validation does not replace
// it.
func TestResolveTool_HandlerFailureIsNotValidated(t *testing.T) {
	conv, _ := openSettlementConv(t, 1, []string{"Done."})
	desc, err := conv.ToolRegistry().GetTool("send_message")
	require.NoError(t, err)
	desc.OutputSchema = json.RawMessage(sendMessageOutputSchema)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(map[string]any) (any, error) { return nil, errors.New("gateway down") },
	)
	ctx := context.Background()
	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	resolution, err := conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)
	assert.EqualError(t, resolution.Error, "tool send_message failed: gateway down")
}

// An approval the call-rate limit refuses reaches the model as the stage
// tells it of a call the limit refused: the limit's message as the result,
// classified as an approval error.
func TestResolveTool_RateLimitedApprovalIsWordedAsTheStageRefusal(t *testing.T) {
	conv := openSendMessageConv(t, 1, 2)
	holdEverySendMessage(conv)
	ctx := context.Background()
	resp, err := conv.Send(ctx, "send two")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 2)
	_, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)
	refusedID := resp.PendingTools()[1].ID
	_, err = conv.ResolveTool(ctx, refusedID)
	require.NoError(t, err)
	_, err = conv.Continue(ctx)
	require.NoError(t, err)

	got := toolResultFor(t, conv, refusedID)
	require.NotNil(t, got)
	const limitText = "Tool send_message not executed: its limit of 1 calls per minute is reached"
	assert.Equal(t, limitText, got.GetTextContent())
	assert.Equal(t, limitText, got.Error)
	assert.Equal(t, types.ToolErrorApproval, got.ErrorType)
}

// Client calls of one round that the conversation defers are never refused
// for each other under ToolPolicy.MaxCallsPerMinute: each counts when it is
// answered, not when the model makes it.
func TestCallRate_DeferredClientCallsOfOneRoundAreNotRefused(t *testing.T) {
	conv := openSendMessageConv(t, 1, 20)
	conv.DeferClientTool("send_message")
	resp, err := conv.Send(context.Background(), "send twenty")
	require.NoError(t, err)
	assert.Len(t, resp.ClientTools(), 20, "every call waits for the client")
	assert.True(t, roomFor(conv, 1), "none of them counts yet")
}

// A client tool the conversation runs with a handler is admitted as any call
// it runs: with a limit of one, one of three calls in a round runs and the
// others are refused for the limit. Only a call the client executor defers
// skips admission at dispatch, and it is never run there.
func TestCallRate_ClientCallsWithAHandlerAreAdmittedWhenTheyRun(t *testing.T) {
	conv := openSendMessageConv(t, 1, 3)
	var ran atomic.Int32
	conv.OnClientTool("send_message", func(context.Context, ClientToolRequest) (any, error) {
		ran.Add(1)
		return "sent", nil
	})
	_, err := conv.Send(context.Background(), "send three")
	require.NoError(t, err)
	assert.EqualValues(t, 1, ran.Load(), "only one call runs")
	assert.Equal(t, 2, limitRefusals(t, conv), "the others are refused for the limit")
}

// sendMessageRegistry is a host registry with send_message declared, under a
// registry rate limit of limit calls per minute.
func sendMessageRegistry(t *testing.T, limit int) Option {
	t.Helper()
	reg := tools.NewRegistry(tools.WithRateLimit(limit))
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "send_message", Description: "send", Mode: "local",
		InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{}`),
	}))
	return WithToolRegistry(reg)
}

// Deferred client calls skip the tool registry's own rate limit when the
// model makes them, and are counted by it once each when they are answered:
// three calls under a registry limit of two all wait for the client, and of
// the three answers two are used and one is refused for the limit.
func TestRegistryRateLimit_CountsADeferredCallOnceWhenAnswered(t *testing.T) {
	conv := openSendMessageConv(t, 0, 3, sendMessageRegistry(t, 2))
	conv.DeferClientTool("send_message")
	ctx := context.Background()
	resp, err := conv.Send(ctx, "send three")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 3, "none is refused when the model makes it")
	for _, ct := range resp.ClientTools() {
		require.NoError(t, conv.SendToolResult(ctx, ct.CallID, "sent"))
	}
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	var used, refused int
	for _, ct := range resp.ClientTools() {
		got := toolResultFor(t, conv, ct.CallID)
		require.NotNil(t, got)
		switch {
		case got.Error == "":
			used++
		case strings.Contains(got.Error, "tool rate limit exceeded"):
			refused++
		}
	}
	assert.Equal(t, 2, used, "each answer counted once")
	assert.Equal(t, 1, refused)
}

// An approved result reaches the model the same whether Continue or Resume
// gives it: Resume does not check it again against validate_output, which
// the approval's own check (Registry.CheckResult, which leaves an mcp-mode
// tool's result as it is) already settled.
func TestResolveTool_ContinueAndResumeGiveTheSameApprovedResult(t *testing.T) {
	given := func(resume bool) string {
		conv, provider := openSettlementConv(t, 1, []string{"Done."})
		conv.OnToolAsync("send_message",
			func(map[string]any) sdktools.PendingResult {
				return sdktools.PendingResult{Reason: "requires_approval"}
			},
			func(map[string]any) (any, error) { return map[string]any{"sent": "yes"}, nil },
		)
		ctx := context.Background()
		resp, err := conv.Send(ctx, "message the customer")
		require.NoError(t, err)
		require.Len(t, resp.PendingTools(), 1)
		desc, err := conv.ToolRegistry().GetTool("send_message")
		require.NoError(t, err)
		desc.Mode = "mcp"
		desc.OutputSchema = json.RawMessage(sendMessageOutputSchema)
		desc.ClientConfig = &tools.ClientConfig{ValidateOutput: true}
		_, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
		require.NoError(t, err)
		if resume {
			_, err = conv.Resume(ctx)
		} else {
			_, err = conv.Continue(ctx)
		}
		require.NoError(t, err)
		provider.mu.Lock()
		defer provider.mu.Unlock()
		for _, m := range provider.seen[len(provider.seen)-1] {
			if m.ToolResult != nil {
				return m.ToolResult.GetTextContent()
			}
		}
		t.Fatal("the model saw no tool result")
		return ""
	}
	continued := given(false)
	assert.Equal(t, "{\n  \"sent\": \"yes\"\n}", continued)
	assert.Equal(t, continued, given(true))
}
