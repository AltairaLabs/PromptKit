package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

