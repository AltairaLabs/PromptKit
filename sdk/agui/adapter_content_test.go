package agui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// A TOOL_CALL_RESULT never goes out with empty content: the AG-UI Go SDK's
// validator rejects it, so a Go consumer decoding the stream would fail.
// runAndCollect runs every event through that validator.

func TestRunSend_ResultContentIsNeverEmpty(t *testing.T) {
	img := "aGVsbG8="
	mediaOnly := types.Message{Role: roleTool, ToolResult: &types.MessageToolResult{ID: "c-img", Parts: []types.ContentPart{
		{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &img, MIMEType: "image/png"}},
		{Type: types.ContentTypeDocument, Media: &types.MediaContent{Data: &img, MIMEType: "application/pdf"}},
	}}}
	turn := []types.Message{
		assistantMsg("", call("c-img", "chart", `{}`), call("c-empty", "noop", `{}`)),
		mediaOnly,
		toolMsg("c-empty", ""),
		assistantMsg("Done."),
	}
	resp := sdk.NewResponseForTest("Done.", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Equal(t, "[image/png image] [application/pdf document]", resultContent(t, evts, "c-img"),
		"a media-only result says what it holds")
	assert.Equal(t, `""`, resultContent(t, evts, "c-empty"), "a tool that returned nothing: the empty string, as JSON")
}

// A ToolResultProvider that answers some calls with no value: the results it
// gave are emitted at once, with the value's JSON encoding.
func TestRunSend_ProviderAnswerWithoutValue(t *testing.T) {
	pending := []sdk.PendingClientTool{
		{CallID: "c-nil", ToolName: "a"}, {CallID: "c-empty", ToolName: "b"},
		{CallID: "c-media", ToolName: "d"}, {CallID: "c-left", ToolName: "c"},
	}
	sender := &mockSender{resp: sdk.NewResponseForTest("", nil, sdk.WithClientToolsForTest(pending))}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		shot := "aGVsbG8="
		return []ToolResult{
			{CallID: "c-nil"},
			{CallID: "c-empty", Result: ""},
			{CallID: "c-media", Result: []types.ContentPart{
				{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &shot, MIMEType: "image/png"}},
			}},
		}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Equal(t, "null", resultContent(t, evts, "c-nil"))
	assert.Equal(t, `""`, resultContent(t, evts, "c-empty"))
	assert.Equal(t, "[image/png image]", resultContent(t, evts, "c-media"))
}
