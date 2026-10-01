package agui

import (
	"context"
	"testing"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// The provider answers client call_0; in the resumed turn a server tool
// reuses call_0. The provider answer's TOOL_CALL_RESULT must carry the
// provider answer, as the model saw it, not the server tool's output.
func TestRunSend_ProviderAnswerNotOverwrittenByReusedID(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "call_0", ToolName: "get_location"}}
	resumed := sdk.NewResponseForTest("Done.", nil, sdk.WithTurnMessagesForTest([]types.Message{
		toolMsg("call_0", `"Paris"`),
		assistantMsg("Checking the weather.", call("call_0", "weather", `{"city":"Paris"}`)),
		toolMsg("call_0", "SERVER"),
		assistantMsg("Done."),
	}))
	sender := &mockSender{
		resp:            sdk.NewResponseForTest("", nil, sdk.WithClientToolsForTest(pending)),
		resumeResponses: []*sdk.Response{resumed},
	}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "call_0", Result: "Paris"}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)
	assert.Equal(t, []string{`call_0="Paris"`, "call_0=SERVER"}, resultIDs(evts))
}

// A tool message whose every part is unusable (a provider file handle) has no
// parts left to send. It must not reach the conversation as an empty parts
// list, which the conversation rejects.
func TestToolResultsFromAGUI_AllPartsSkipped(t *testing.T) {
	msg := decodeMessage(t, `{"id":"t","role":"tool","toolCallId":"c1","content":[
		{"type":"document","source":{"type":"file","value":"file-abc","provider":"openai"}}]}`)

	results := ToolResultsFromAGUI([]aguitypes.Message{msg})

	require.Len(t, results, 1)
	assert.Nil(t, results[0].Result, "no parts and no text: no value")
}

func TestRunResume_EmptyPartsFallBack(t *testing.T) {
	mm := &multimodalSender{}
	a := newTestAdapter(mm, &mockEventBusProvider{})

	_, err := runAndCollect(t, a, func(ctx context.Context) error {
		return a.RunResume(ctx, []ToolResult{{CallID: "c1", Result: []types.ContentPart{}}})
	})
	require.NoError(t, err)

	assert.Empty(t, mm.parts, "an empty parts list is never sent")
	require.Len(t, mm.sendToolResultCalls, 1)
	assert.Nil(t, mm.sendToolResultCalls[0].result)
}
