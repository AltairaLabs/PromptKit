package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// heldCallContinued holds the call, settles it with settle and returns the tool
// result the model saw on Continue.
func heldCallContinued(
	t *testing.T, settle func(ctx context.Context, conv *Conversation, id string) error, opts ...Option,
) *types.MessageToolResult {
	t.Helper()
	conv, provider := openSettlementConv(t, 1, []string{"Done."}, opts...)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{Reason: "requires_approval"} },
		func(map[string]any) (any, error) { return map[string]any{"id": int64(9007199254740993)}, nil },
	)
	ctx := context.Background()

	resp, err := conv.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	require.NoError(t, settle(ctx, conv, resp.PendingTools()[0].ID))
	_, err = conv.Continue(ctx)
	require.NoError(t, err)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	require.Len(t, provider.seen, 2)
	for _, m := range provider.seen[1] {
		if m.ToolResult != nil {
			return m.ToolResult
		}
	}
	t.Fatal("the model saw no tool result")
	return nil
}

func approve(ctx context.Context, conv *Conversation, id string) error {
	_, err := conv.ResolveTool(ctx, id)
	return err
}

// An approved call's result reaches the model as a text turn formats a result
// it ran itself: indented, with a large integer exact.
func TestContinue_FormatsApprovedResultAsATextTurnDoes(t *testing.T) {
	got := heldCallContinued(t, approve)
	assert.Equal(t, "{\n  \"id\": 9007199254740993\n}", got.GetTextContent())
}

// A result over the registry's size limit is cut as the stage cuts one.
func TestContinue_TruncatesAnApprovedResultOverTheSizeLimit(t *testing.T) {
	got := heldCallContinued(t, approve, WithToolRegistry(tools.NewRegistry(tools.WithMaxToolResultSize(10))))
	assert.Equal(t, "{\n  \"id\": \n... [truncated, 28 bytes exceeded limit of 10 bytes]", got.GetTextContent())
}

// A multimodal resolution reaches the model as its parts, never as "<nil>".
func TestContinue_SendsAMultimodalResolutionAsItsParts(t *testing.T) {
	got := heldCallContinued(t, func(ctx context.Context, conv *Conversation, id string) error {
		return conv.SendToolResultMultimodal(ctx, id, []types.ContentPart{
			types.NewTextPart("a photo"), types.NewImagePartFromData("aGk=", "image/png", nil),
		})
	})
	require.Len(t, got.Parts, 2)
	assert.Equal(t, "a photo", *got.Parts[0].Text)
	assert.Equal(t, types.ContentTypeImage, got.Parts[1].Type)
	assert.Equal(t, "a photo", got.GetTextContent())
}
