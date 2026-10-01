package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These run the adapter over real conversations (sdk.Open with a scripted
// mock provider), so they exercise the Response the SDK actually builds rather
// than a hand-made one.

// scriptedProvider answers each model call with the next scripted round.
type scriptedProvider struct {
	*mock.ToolProvider
	mu     sync.Mutex
	rounds []providers.PredictionResponse
	next   int
}

func newScriptedProvider(rounds ...providers.PredictionResponse) *scriptedProvider {
	return &scriptedProvider{ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil), rounds: rounds}
}

// Pin to the unary tool loop for determinism.
func (p *scriptedProvider) SupportsStreaming() bool { return false }

func (p *scriptedProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next >= len(p.rounds) {
		return providers.PredictionResponse{Content: "(no more script)"}, nil, nil
	}
	r := p.rounds[p.next]
	p.next++
	return r, r.ToolCalls, nil
}

func (p *scriptedProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	r, _, err := p.PredictWithTools(ctx, req, nil, "")
	return r, err
}

func say(text string, calls ...types.MessageToolCall) providers.PredictionResponse {
	return providers.PredictionResponse{Content: text, ToolCalls: calls}
}

func writePack(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agui.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const toolPackJSON = `{
	"id": "agui-e2e", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You help.", "tools": ["lookup", "get_location", "send_message"]}},
	"tools": {
		"lookup": {"name": "lookup", "description": "Look up an order",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}},
		"get_location": {"name": "get_location", "description": "The user's location",
			"parameters": {"type": "object", "properties": {}}},
		"send_message": {"name": "send_message", "description": "Send a message",
			"parameters": {"type": "object", "properties": {"body": {"type": "string"}}}}
	}
}`

func openConv(t *testing.T, provider providers.Provider) *sdk.Conversation {
	t.Helper()
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat", sdk.WithProvider(provider), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// bindClientTool makes name a client tool with no handler, so a call to it
// suspends the turn for the application to answer. This is what an AG-UI
// server does for the frontend tools a RunAgentInput advertises.
func bindClientTool(t *testing.T, conv *sdk.Conversation, name string) {
	t.Helper()
	desc, err := conv.ToolRegistry().GetTool(name)
	require.NoError(t, err)
	desc.Mode = ToolsFromAGUI([]aguitypes.Tool{{Name: name}})[0].Mode
}

// A model that called a tool and then answered: the run shows both messages,
// the call, and the tool's real result.
func TestE2E_ServerToolCallIsEmitted(t *testing.T) {
	provider := newScriptedProvider(
		say("Let me look.", call("call-1", "lookup", `{"id":"42"}`)),
		say("Found it."),
	)
	conv := openConv(t, provider)
	conv.OnTool("lookup", func(map[string]any) (any, error) { return map[string]any{"status": "shipped"}, nil })

	a := NewEventAdapter(conv)
	evts, err := sendAndCollect(t, a, "where is order 42?")
	require.NoError(t, err)

	var texts []string
	for _, e := range eventsOf[*aguievents.TextMessageContentEvent](evts) {
		texts = append(texts, e.Delta)
	}
	assert.Equal(t, []string{"Let me look.", "Found it."}, texts)
	starts := eventsOf[*aguievents.ToolCallStartEvent](evts)
	require.Len(t, starts, 1)
	assert.Equal(t, "lookup", starts[0].ToolCallName)
	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Content, "shipped")
}

// History from earlier turns is not replayed into the next run.
func TestE2E_SecondTurnEmitsOnlyItsOwnMessages(t *testing.T) {
	provider := newScriptedProvider(say("First."), say("Second."))
	conv := openConv(t, provider)

	_, err := sendAndCollect(t, NewEventAdapter(conv), "one")
	require.NoError(t, err)
	evts, err := sendAndCollect(t, NewEventAdapter(conv), "two")
	require.NoError(t, err)

	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.Len(t, texts, 1)
	assert.Equal(t, "Second.", texts[0].Delta)
}

// 25 tool calls overflow the buffer; a consumer that has not started reading
// still gets the whole run, ending in RUN_FINISHED.
func TestE2E_SlowConsumerGetsWholeRun(t *testing.T) {
	const n = 25
	calls := make([]types.MessageToolCall, n)
	for i := range calls {
		calls[i] = call(fmt.Sprintf("call-%d", i), "lookup", fmt.Sprintf(`{"id":"%d"}`, i))
	}
	conv := openConv(t, newScriptedProvider(say("Checking.", calls...), say("All done.")))
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "ok", nil })

	a := NewEventAdapter(conv)
	runDone := make(chan error, 1)
	go func() { runDone <- a.RunSend(context.Background(), userMsg("check all")) }()
	require.Eventually(t, func() bool { return len(a.events) == cap(a.events) }, collectTimeout, time.Millisecond)

	evts := collectEvents(a.Events())
	require.NoError(t, <-runDone)
	requireValidSequence(t, evts)
	assert.Len(t, eventsOf[*aguievents.ToolCallResultEvent](evts), n)
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[len(evts)-1].Type())
}

// The whole frontend-tool round trip over two runs: the first ends with the
// call unanswered, the second carries the application's answer.
func TestE2E_FrontendToolRoundTrip(t *testing.T) {
	provider := newScriptedProvider(
		say("Where are you?", call("call-1", "get_location", `{"q":"a"}`)),
		say("You are in Paris."),
	)
	conv := openConv(t, provider)
	bindClientTool(t, conv, "get_location")

	// Run 1: the call is emitted once, and left unanswered.
	evts, err := sendAndCollect(t, NewEventAdapter(conv), "where am I?")
	require.NoError(t, err)
	args := eventsOf[*aguievents.ToolCallArgsEvent](evts)
	require.Len(t, args, 1)
	assert.Equal(t, `{"q":"a"}`, args[0].Delta, "the call is emitted once, not twice")
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeToolCallResult)
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeCustom)

	// Run 2: the application's answer arrives as a tool message in the input.
	input := []aguitypes.Message{
		{ID: "u1", Role: aguitypes.RoleUser, Content: "where am I?"},
		{ID: "a1", Role: aguitypes.RoleAssistant, ToolCalls: []aguitypes.ToolCall{
			{ID: "call-1", Type: "function", Function: aguitypes.FunctionCall{Name: "get_location", Arguments: `{"q":"a"}`}},
		}},
		{ID: "t1", Role: aguitypes.RoleTool, ToolCallID: "call-1", Content: `{"city":"Paris"}`},
	}
	b := NewEventAdapter(conv)
	evts, err = runAndCollect(t, b, func(ctx context.Context) error {
		return b.RunResume(ctx, ToolResultsFromAGUI(input))
	})
	require.NoError(t, err)
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.Len(t, texts, 1)
	assert.Equal(t, "You are in Paris.", texts[0].Delta)
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeToolCallResult)

	// The model saw the application's answer.
	history := conv.Messages(context.Background())
	var toolContent string
	for i := range history {
		if history[i].ToolResult != nil && history[i].ToolResult.ID == "call-1" {
			toolContent = history[i].ToolResult.GetTextContent()
		}
	}
	assert.JSONEq(t, `{"city":"Paris"}`, toolContent)
}

// An approval-held call ends the run with an interrupt; once resolved, the
// continuing run reports the approved tool's result.
func TestE2E_ApprovalHoldInterruptsThenContinues(t *testing.T) {
	provider := newScriptedProvider(
		say("Sending.", call("call-1", "send_message", `{"body":"hi"}`)),
		say("Sent."),
	)
	conv := openConv(t, provider)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
		},
		func(map[string]any) (any, error) { return map[string]any{"sent": true}, nil },
	)

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "send hi")
	require.NoError(t, err)
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeToolCallResult, "no fabricated pending result")
	finished := evts[len(evts)-1].(*aguievents.RunFinishedEvent)
	require.NotNil(t, finished.Outcome)
	assert.Equal(t, aguievents.RunFinishedOutcomeTypeInterrupt, finished.Outcome.Type)
	require.Len(t, finished.Outcome.Interrupts, 1)
	assert.Equal(t, "call-1", finished.Outcome.Interrupts[0].ToolCallID)

	_, err = conv.ResolveTool(context.Background(), finished.Outcome.Interrupts[0].ID)
	require.NoError(t, err)

	b := NewEventAdapter(conv)
	evts, err = runAndCollect(t, b, b.RunContinue)
	require.NoError(t, err)
	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Equal(t, "call-1", results[0].ToolCallID)
	assert.Contains(t, results[0].Content, "sent")
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.Len(t, texts, 1)
	assert.Equal(t, "Sent.", texts[0].Delta)
}

const workflowPackJSON = `{
	"id": "agui-workflow", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"gather_info": {"id": "gather_info", "name": "Gather", "version": "1.0.0", "system_template": "You gather."},
		"process": {"id": "process", "name": "Process", "version": "1.0.0", "system_template": "You process."}
	},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {"prompt_task": "gather_info", "on_event": {"InfoComplete": "processing"}},
			"processing": {"prompt_task": "process", "control": "user"}
		}
	}
}`

// A workflow conversation works with the adapter, and its steps are balanced:
// the run opens the state it starts in, and a committed transition finishes
// that step and starts the next.
func TestE2E_WorkflowSteps(t *testing.T) {
	provider := newScriptedProvider(
		say("Got it.", call("call-1", "workflow__transition", `{"event":"InfoComplete","context":"done"}`)),
		say("Processing."),
	)
	wc, err := sdk.OpenWorkflow(writePack(t, workflowPackJSON), sdk.WithProvider(provider), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	evts, err := sendAndCollect(t, NewWorkflowEventAdapter(wc), "here is my info")
	require.NoError(t, err)

	assert.Equal(t, []stepEvent{
		{aguievents.EventTypeStepStarted, "intake"},
		{aguievents.EventTypeStepFinished, "intake"},
		{aguievents.EventTypeStepStarted, "processing"},
		{aguievents.EventTypeStepFinished, "processing"},
	}, stepEvents(evts))
	assert.Equal(t, "processing", wc.CurrentState())

	none, err := sendAndCollect(t, NewWorkflowEventAdapter(wc, WithWorkflowSteps(false)), "next")
	require.NoError(t, err)
	assert.Empty(t, stepEvents(none))
}

// The workflow adapter hands client-tool results and approvals to the
// conversation serving the current state.
func TestWorkflowSender_DelegatesToActiveConversation(t *testing.T) {
	provider := newScriptedProvider(say("hello"))
	wc, err := sdk.OpenWorkflow(writePack(t, workflowPackJSON), sdk.WithProvider(provider), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	ws := &workflowSender{wc: wc}
	ctx := context.Background()

	assert.Equal(t, "intake", ws.CurrentState())
	assert.NotNil(t, ws.EventBus())
	require.NoError(t, ws.SendToolResult(ctx, "c1", "x"))
	text := "y"
	require.NoError(t, ws.SendToolResultMultimodal(ctx, "c2", []types.ContentPart{{Type: types.ContentTypeText, Text: &text}}))
	ws.RejectClientTool(ctx, "c3", "no")
	_, err = ws.Resume(ctx)
	assert.NoError(t, err, "three resolutions are waiting")
	_, err = ws.Continue(ctx)
	assert.Error(t, err, "nothing is held for approval")
}

// ToolResultsFromAGUI's JSON pass-through keeps a JSON answer JSON for the
// model, rather than a quoted string.
func TestE2E_ToolResultJSONPassThrough(t *testing.T) {
	raw, err := json.Marshal(ToolResultsFromAGUI([]aguitypes.Message{
		{Role: aguitypes.RoleTool, ToolCallID: "c", Content: `{"a":1}`},
	})[0].Result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1}`, string(raw))
}
