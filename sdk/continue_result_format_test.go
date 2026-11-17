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
	t *testing.T, result any, settle func(ctx context.Context, conv *Conversation, id string) error, opts ...Option,
) *types.MessageToolResult {
	t.Helper()
	conv, provider := openSettlementConv(t, 1, []string{"Done."}, opts...)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(map[string]any) (any, error) { return result, nil },
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

var bigID = map[string]any{"id": int64(9007199254740993)}

// limitedTools is a host registry with a result size limit and the named local
// tools declared, as a host that supplies its own registry declares them.
func limitedTools(t *testing.T, maxSize int, names ...string) Option {
	t.Helper()
	reg := tools.NewRegistry(tools.WithMaxToolResultSize(maxSize))
	for _, name := range names {
		require.NoError(t, reg.Register(&tools.ToolDescriptor{
			Name: name, Description: name, Mode: "local", InputSchema: []byte(`{"type":"object"}`),
		}))
	}
	return WithToolRegistry(reg)
}

func approve(ctx context.Context, conv *Conversation, id string) error {
	_, err := conv.ResolveTool(ctx, id)
	return err
}

// An approved call's result reaches the model as a text turn formats a result
// it ran itself: indented, with a large integer exact.
func TestContinue_FormatsApprovedResultAsATextTurnDoes(t *testing.T) {
	got := heldCallContinued(t, bigID, approve)
	assert.Equal(t, "{\n  \"id\": 9007199254740993\n}", got.GetTextContent())
}

// A result over the registry's size limit is cut as the stage cuts one.
func TestContinue_TruncatesAnApprovedResultOverTheSizeLimit(t *testing.T) {
	got := heldCallContinued(t, bigID, approve, limitedTools(t, 10, "send_message"))
	assert.Equal(t, "{\n  \"id\": \n... [truncated, 28 bytes exceeded limit of 10 bytes]", got.GetTextContent())
}

// A multimodal resolution reaches the model as its parts, never as "<nil>".
func TestContinue_SendsAMultimodalResolutionAsItsParts(t *testing.T) {
	got := heldCallContinued(t, "unused", func(ctx context.Context, conv *Conversation, id string) error {
		return conv.SendToolResultMultimodal(ctx, id, []types.ContentPart{
			types.NewTextPart("a photo"), types.NewImagePartFromData("aGk=", "image/png", nil),
		})
	})
	require.Len(t, got.Parts, 2)
	assert.Equal(t, "a photo", *got.Parts[0].Text)
	assert.Equal(t, types.ContentTypeImage, got.Parts[1].Type)
	assert.Equal(t, "a photo", got.GetTextContent())
}

// Null and the empty string reach the model as themselves, not as "<nil>" or an
// empty text block.
func TestContinue_NullAndEmptyStringAreNotLost(t *testing.T) {
	assert.Equal(t, "null", heldCallContinued(t, nil, approve).GetTextContent())
	assert.Equal(t, `""`, heldCallContinued(t, "", approve).GetTextContent())
}

// A result given as a Go value only (no JSON) is formatted and capped like any
// other, not printed as Go syntax.
func TestContinue_ResultWithoutJSONIsFormattedAsJSON(t *testing.T) {
	conv := newTestConversation()
	conv.toolRegistry = tools.NewRegistry(tools.WithMaxToolResultSize(14))
	got := conv.modelResult("t", &sdktools.ToolResolution{Result: map[string]any{"a": "b"}})
	assert.Equal(t, "{\n  \"a\": \"b\"\n}", got.Text)
	got = conv.modelResult("t", &sdktools.ToolResolution{Result: map[string]any{"a": "bbbbbbbbbb"}})
	assert.Equal(t, "{\n  \"a\": \"bbbb\n... [truncated, 23 bytes exceeded limit of 14 bytes]", got.Text)
}

// The same oversized result reaches the model identically whether the pipeline
// ran the tool (the provider stage) or a reviewer approved it (Continue).
func TestContinue_MatchesTheStageForAnOversizedResult(t *testing.T) {
	result := map[string]any{"note": "日本語のテキスト, 😀 and more text beyond the limit"}
	limit := limitedTools(t, 23, "send_message")

	ran, provider := openSettlementConv(t, 1, []string{"Done."}, limit)
	ran.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{} },
		func(map[string]any) (any, error) { return result, nil })
	_, err := ran.Send(context.Background(), "message the customer")
	require.NoError(t, err)
	provider.mu.Lock()
	var viaStage string
	for _, m := range provider.seen[1] {
		if m.ToolResult != nil {
			viaStage = m.ToolResult.GetTextContent()
		}
	}
	provider.mu.Unlock()

	viaContinue := heldCallContinued(t, result, approve, limit).GetTextContent()
	assert.Contains(t, viaStage, "[truncated,")
	assert.Equal(t, viaStage, viaContinue)
}
