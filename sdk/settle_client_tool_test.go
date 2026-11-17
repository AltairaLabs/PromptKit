package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// suspendOnClientSend opens a conversation suspended on one deferred
// send_message client call, with a call-rate limit and opts, and returns it
// with the call's ID.
func suspendOnClientSend(t *testing.T, limit int, opts ...Option) (*Conversation, string) {
	t.Helper()
	conv := openSendMessageConv(t, limit, 1, opts...)
	conv.DeferClientTool("send_message")
	resp, err := conv.Send(context.Background(), "send one")
	require.NoError(t, err)
	require.Len(t, resp.ClientTools(), 1)
	return conv, resp.ClientTools()[0].CallID
}

// A settled answer is prepared once: rewritten, admitted under the call-rate
// limit and given to the model as prepared, without being rewritten or
// counted again when the turn resumes.
func TestSettleClientTool_PreparesTheResultResumeGives(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, id := suspendOnClientSend(t, 2, WithToolHook(rw))
	ctx := context.Background()

	prepared, answered, err := conv.SettleClientTool(ctx, id, "ok", nil)
	require.NoError(t, err)
	require.True(t, answered)
	assert.Equal(t, `<redacted:"ok">`, prepared.GetTextContent())
	assert.Equal(t, "send_message", prepared.Name)

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	got := toolResultFor(t, conv, id)
	require.NotNil(t, got)
	assert.Equal(t, prepared.GetTextContent(), got.GetTextContent())
	assert.Len(t, rw.requests(), 1, "rewritten once")
	assert.True(t, roomFor(conv, 2), "the answer took one of the two slots, once")
	assert.False(t, roomFor(conv, 2))
}

// A second answer to a call is not answered: nothing is prepared or
// rewritten, and the first answer stands.
func TestSettleClientTool_IgnoresASecondAnswer(t *testing.T) {
	rw := &wrappingRewriter{}
	conv, id := suspendOnClientSend(t, 0, WithToolHook(rw))
	ctx := context.Background()
	require.NoError(t, conv.SendToolResult(ctx, id, "first"))

	prepared, answered, err := conv.SettleClientTool(ctx, id, "second", nil)
	require.NoError(t, err)
	assert.False(t, answered)
	assert.Equal(t, types.MessageToolResult{}, prepared)
	assert.Empty(t, rw.requests())

	_, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, `<redacted:"first">`, toolResultFor(t, conv, id).GetTextContent())
}

// A failure is prepared as Resume gives FailClientTool's: the partial output
// rewritten, then the error.
func TestSettleClientTool_PreparesAFailure(t *testing.T) {
	conv, id := suspendOnClientSend(t, 0, WithToolHook(&wrappingRewriter{}))
	prepared, answered, err := conv.SettleClientTool(context.Background(), id,
		map[string]any{"rows": 3}, errors.New("disk full"))
	require.NoError(t, err)
	require.True(t, answered)
	assert.Equal(t, "<redacted:{\"rows\":3}>\n\nTool error: disk full", prepared.GetTextContent())
	assert.Equal(t, "disk full", prepared.Error)
}

// A call the turn is not waiting on is refused, as SendToolResult refuses it.
func TestSettleClientTool_RefusesACallNotAwaited(t *testing.T) {
	conv, _ := suspendOnClientSend(t, 0)
	_, answered, err := conv.SettleClientTool(context.Background(), "nope", "x", nil)
	assert.ErrorIs(t, err, ErrToolCallNotAwaited)
	assert.False(t, answered)
}

// HasToolResultRewriters reports a tool hook that rewrites results.
func TestHasToolResultRewriters(t *testing.T) {
	with, _ := suspendOnClientSend(t, 0, WithToolHook(&wrappingRewriter{}))
	without, _ := suspendOnClientSend(t, 0)
	assert.True(t, with.HasToolResultRewriters())
	assert.False(t, without.HasToolResultRewriters())
}

// A prepared result whose answer Resume drops unused (a second answer to its
// call) is forgotten with it; the used one is kept.
func TestTakeSettlement_ForgetsThePreparedResultOfAnUnusedAnswer(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()
	first := &sdktools.ToolResolution{ID: "call_0", ResultJSON: []byte(`"a"`)}
	second := &sdktools.ToolResolution{ID: "call_0", ResultJSON: []byte(`"b"`)}
	conv.resolvedStore.Add(first)
	conv.resolvedStore.Add(second)
	conv.prepared = map[*sdktools.ToolResolution]types.MessageToolResult{
		first:  types.NewTextToolResult("call_0", "", "a"),
		second: types.NewTextToolResult("call_0", "", "b"),
	}
	history := []types.Message{{Role: roleAssistant, ToolCalls: []types.MessageToolCall{{ID: "call_0"}}}}

	kept := conv.takeSettlement(history)
	assert.Equal(t, []*sdktools.ToolResolution{first}, kept)
	assert.True(t, conv.isPrepared(first))
	assert.False(t, conv.isPrepared(second))
}
