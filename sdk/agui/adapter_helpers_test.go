package agui

import (
	"context"
	"encoding/json"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// A provider-answered call that failed part-way reports the partial output
// and the error, as the model is told.
func TestRunSend_ClientTools_ProviderReportsFailure(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "export"}, {CallID: "ct-2", ToolName: "export"}}
	sender := &mockSender{resp: sdk.NewResponseForTest("", nil, sdk.WithClientToolsForTest(pending))}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{
			{CallID: "ct-1", Result: "3 of 5 rows", Error: "disk full"},
			{CallID: "ct-2", Error: "disk full"},
		}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "export")
	require.NoError(t, err)

	require.Len(t, sender.sendToolResultCalls, 2)
	assert.Equal(t, "3 of 5 rows\n\nTool error: disk full", sender.sendToolResultCalls[0].result)
	assert.Equal(t, "Tool error: disk full", sender.sendToolResultCalls[1].result)
	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 2)
	assert.Equal(t, "3 of 5 rows\n\nTool error: disk full", results[0].Content)
}

func TestValueText(t *testing.T) {
	text := "hello"
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "plain", "plain"},
		{"raw JSON", json.RawMessage(`{"a":1}`), `{"a":1}`},
		{"parts", []types.ContentPart{{Type: types.ContentTypeText, Text: &text}, {Type: types.ContentTypeImage}}, "hello"},
		{"value", map[string]int{"n": 2}, `{"n":2}`},
		{"unmarshalable", func() {}, "0x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := valueText(tc.in)
			if tc.name == "unmarshalable" {
				assert.Contains(t, got, tc.want, "falls back to fmt")
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// A tool message without a result, and one whose call was answered by the
// run's input, add no event.
func TestRunSend_ToolMessagesWithoutResultsAreSkipped(t *testing.T) {
	turn := []types.Message{
		{Role: roleTool},
		toolMsg("", "orphan"),
		assistantMsg("ok"),
	}
	resp := sdk.NewResponseForTest("ok", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeToolCallResult)
}

// An event emitted after the run has ended is dropped rather than sent on a
// closed channel.
func TestEmitAfterClose(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{})
	_, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.NotPanics(t, func() { a.emit(context.Background(), aguievents.NewRunStartedEvent("t", "r")) })
}

func TestToolsToAGUI(t *testing.T) {
	descs := []tools.ToolDescriptor{
		{Name: "lookup", Description: "Look up", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "bare", Description: "No schema"},
		{Name: "broken", Description: "Bad schema", InputSchema: json.RawMessage(`not json`)},
	}

	got := ToolsToAGUI(descs)

	require.Len(t, got, 3)
	assert.Equal(t, "lookup", got[0].Name)
	assert.Equal(t, map[string]any{"type": "object"}, got[0].Parameters)
	assert.Nil(t, got[1].Parameters)
	assert.Nil(t, got[2].Parameters)
	assert.Nil(t, ToolsToAGUI(nil))
}
