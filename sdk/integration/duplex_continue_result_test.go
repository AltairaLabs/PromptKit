package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// duplexContinued holds a realtime tool call, settles it with settle, runs
// ContinueDuplex and returns what the model session was given.
func duplexContinued(
	t *testing.T, result any, settle func(ctx context.Context, conv *sdk.Conversation) error, opts ...sdk.Option,
) providers.ToolResponse {
	got, _ := duplexContinuedIn(t, result, settle, opts...)
	return got
}

// duplexContinuedIn is duplexContinued that also returns the conversation.
func duplexContinuedIn(
	t *testing.T, result any, settle func(ctx context.Context, conv *sdk.Conversation) error, opts ...sdk.Option,
) (providers.ToolResponse, *sdk.Conversation) {
	t.Helper()
	opts = append([]sdk.Option{sdk.WithStreamingConfig(hostToolStreamingConfig())}, opts...)
	conv, provider := openDuplexToolConv(t, duplexToolsPackJSON,
		[]types.MessageToolCall{lookupOrderCall("call_1")}, "ok", opts...)
	conv.OnToolAsync("lookup_order",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{Reason: "pii"} },
		func(map[string]any) (any, error) { return result, nil })
	responseCh, err := conv.Response()
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, conv.SendText(ctx, "where is my order"))
	chunks := collectDuplexResponse(t, responseCh, 5*time.Second)
	var held int
	for i := range chunks {
		held += len(chunks[i].PendingTools)
	}
	require.Equal(t, 1, held)

	require.NoError(t, settle(ctx, conv))
	require.NoError(t, conv.ContinueDuplex(ctx))
	go func() {
		for range responseCh { //nolint:revive // draining
		}
	}()
	sess := provider.getSessions()[0]
	require.True(t, waitForToolResponses(sess, 1, 3*time.Second))
	sess.responsesMu.Lock()
	defer sess.responsesMu.Unlock()
	require.Len(t, sess.receivedResponses, 1)
	require.Len(t, sess.receivedResponses[0], 1)
	return sess.receivedResponses[0][0], conv
}

func approveCall(ctx context.Context, conv *sdk.Conversation) error {
	_, err := conv.ResolveTool(ctx, "call_1")
	return err
}

func limited(bytes int) sdk.Option {
	return sdk.WithToolRegistry(tools.NewRegistry(tools.WithMaxToolResultSize(bytes)))
}

// A multimodal resolution reaches the realtime session as its parts and its
// joined text, never as "<nil>".
func TestContinueDuplex_SendsAMultimodalResolutionAsItsParts(t *testing.T) {
	image := types.NewImagePartFromData("aGk=", "image/png", nil)
	got, conv := duplexContinuedIn(t, "unused", func(ctx context.Context, conv *sdk.Conversation) error {
		return conv.SendToolResultMultimodal(ctx, "call_1", []types.ContentPart{types.NewTextPart("a photo"), image})
	})
	// The history keeps the media the model was given.
	require.Eventually(t, func() bool {
		for _, m := range conv.Messages(context.Background()) {
			if m.ToolResult != nil && m.ToolResult.ID == "call_1" {
				return len(m.ToolResult.Parts) == 2 && m.ToolResult.Parts[1].Type == types.ContentTypeImage
			}
		}
		return false
	}, 3*time.Second, 20*time.Millisecond, "the history's tool result drops the image")
	assert.Equal(t, "a photo", got.Result)
	require.Len(t, got.Parts, 2)
	assert.Equal(t, "a photo", *got.Parts[0].Text)
	assert.Equal(t, types.ContentTypeImage, got.Parts[1].Type)
}

// The text parts of a multimodal resolution are cut at the size limit.
func TestContinueDuplex_CapsAMultimodalResolutionsTextParts(t *testing.T) {
	got := duplexContinued(t, "unused", func(ctx context.Context, conv *sdk.Conversation) error {
		return conv.SendToolResultMultimodal(ctx, "call_1", []types.ContentPart{types.NewTextPart("abcdefghij")})
	}, limited(4))
	assert.Equal(t, "abcd\n... [truncated, 10 bytes exceeded limit of 4 bytes]", got.Result)
	assert.Equal(t, got.Result, *got.Parts[0].Text)
}

// An approved result over the registry's size limit is cut, as a directly run
// one is.
func TestContinueDuplex_TruncatesAnApprovedResultOverTheSizeLimit(t *testing.T) {
	got := duplexContinued(t, "abcdefghij", approveCall, limited(4))
	assert.Equal(t, "abcd\n... [truncated, 10 bytes exceeded limit of 4 bytes]", got.Result)
}

// Null and the empty string reach the realtime session as themselves.
func TestContinueDuplex_NullAndEmptyStringAreNotLost(t *testing.T) {
	assert.Equal(t, "null", duplexContinued(t, nil, approveCall).Result)
	assert.Equal(t, `""`, duplexContinued(t, "", approveCall).Result)
}
