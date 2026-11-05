package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
)

// stepEvalPackJSON is workflowPackJSON's workflow with pack-level evals: one
// on_workflow_step eval, one disabled on_workflow_step eval, and one
// every_turn eval. The step evals and the turn eval use separate recording
// stub types so each records only its own calls.
const stepEvalPackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "workflow-step-eval-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"evals": [
		{"id": "step", "type": "wf_step_stub", "trigger": "on_workflow_step"},
		{"id": "step_off", "type": "wf_step_stub", "trigger": "on_workflow_step", "enabled": false},
		{"id": "turn", "type": "wf_turn_stub", "trigger": "every_turn"}
	],
	"prompts": {
		"gather_info": {
			"id": "gather_info", "name": "Gather Info", "description": "Gather",
			"version": "1.0.0", "system_template": "You gather information."
		},
		"process": {
			"id": "process", "name": "Process", "description": "Process",
			"version": "1.0.0", "system_template": "You process requests."
		}
	},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {
				"prompt_task": "gather_info",
				"on_event": {"InfoComplete": "processing", "NeedMore": "intake"}
			},
			"processing": {
				"prompt_task": "process",
				"on_event": {"Done": "intake"}
			}
		}
	}
}`

// stepRecordingHandler records the transcript each call was handed, so a test
// can tell which turn an eval ran against.
type stepRecordingHandler struct {
	typeName string
	mu       sync.Mutex
	seen     [][]string
}

func (h *stepRecordingHandler) Type() string { return h.typeName }

func (h *stepRecordingHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	var texts []string
	for i := range evalCtx.Messages {
		texts = append(texts, evalCtx.Messages[i].GetContent())
	}
	h.mu.Lock()
	h.seen = append(h.seen, texts)
	h.mu.Unlock()
	score := 1.0
	return &evals.EvalResult{Score: &score}, nil
}

func (h *stepRecordingHandler) transcripts() [][]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([][]string(nil), h.seen...)
}

// stepEvent is one eval.completed event, reduced to what the tests assert.
type stepEvent struct {
	EvalID    string
	Trigger   string
	TurnIndex int
}

type stepEventCollector struct {
	mu     sync.Mutex
	events []stepEvent
}

func (c *stepEventCollector) byTrigger(trigger string) []stepEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []stepEvent
	for _, e := range c.events {
		if e.Trigger == trigger {
			out = append(out, e)
		}
	}
	return out
}

func newStepEvalHarness(
	t *testing.T,
) (*events.EventBus, *stepEventCollector, *evals.EvalTypeRegistry, *stepRecordingHandler) {
	t.Helper()
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	col := &stepEventCollector{}
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		if d, ok := e.Data.(*events.EvalEventData); ok {
			col.mu.Lock()
			col.events = append(col.events, stepEvent{EvalID: d.EvalID, Trigger: d.Trigger, TurnIndex: d.TurnIndex})
			col.mu.Unlock()
		}
	})
	registry := evals.NewEvalTypeRegistry()
	step := &stepRecordingHandler{typeName: "wf_step_stub"}
	registry.Register(step)
	registry.Register(&stepRecordingHandler{typeName: "wf_turn_stub"})
	return bus, col, registry, step
}

// waitStepEvals waits for n on_workflow_step results, then gives stragglers a
// moment to show up so an over-count is caught too.
func waitStepEvals(t *testing.T, col *stepEventCollector, n int) []stepEvent {
	t.Helper()
	require.Eventually(t, func() bool {
		return len(col.byTrigger(evals.TriggerOnWorkflowStep)) >= n
	}, 5*time.Second, 5*time.Millisecond, "expected %d on_workflow_step eval(s)", n)
	time.Sleep(20 * time.Millisecond)
	return col.byTrigger(evals.TriggerOnWorkflowStep)
}

// transitioningProvider calls workflow__transition with event on its first
// round, then answers plainly, so one Send spans a mid-turn transition.
type transitioningProvider struct {
	*mock.ToolProvider
	mu     sync.Mutex
	event  string
	rounds int
}

func (p *transitioningProvider) SupportsStreaming() bool { return false }

func (p *transitioningProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{Content: "plain"}, nil
}

func (p *transitioningProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rounds++
	if p.rounds == 1 && p.event != "" {
		calls := []types.MessageToolCall{{
			ID: "call-1", Name: "workflow__transition",
			Args: []byte(`{"event":"` + p.event + `","context":"moving on"}`),
		}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "reply after transition"}, nil, nil
}

// A manual Transition runs the on_workflow_step evals once, against the turn
// the workflow was on, and not the disabled one. A Send without a transition
// runs none.
func TestWorkflowStepEvals_ManualTransition(t *testing.T) {
	bus, col, registry, step := newStepEvalHarness(t)
	packPath := writeWorkflowTestPack(t, stepEvalPackJSON)

	wc, err := OpenWorkflow(packPath,
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	defer wc.Close()

	_, err = wc.Send(context.Background(), "hello")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) >= 1 },
		5*time.Second, 5*time.Millisecond)
	assert.Empty(t, col.byTrigger(evals.TriggerOnWorkflowStep), "no transition, no workflow-step eval")

	_, err = wc.Transition("InfoComplete")
	require.NoError(t, err)

	got := waitStepEvals(t, col, 1)
	require.Len(t, got, 1, "one transition runs the on_workflow_step evals once")
	assert.Equal(t, "step", got[0].EvalID, "the disabled eval must not run")
	assert.Equal(t, 1, got[0].TurnIndex, "the eval runs against the turn that preceded the transition")
	seen := step.transcripts()
	require.Len(t, seen, 1)
	assert.Contains(t, seen[0], "hello", "the eval sees the origin state's transcript")
}

// A transition the model makes mid-turn runs the on_workflow_step evals after
// that turn, with the turn's index — including a self-transition, which opens
// no new conversation.
func TestWorkflowStepEvals_MidTurnTransition(t *testing.T) {
	for _, tc := range []struct {
		name, event, wantState string
	}{
		{"to another state", "InfoComplete", "processing"},
		{"to the same state", "NeedMore", "intake"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus, col, registry, step := newStepEvalHarness(t)
			packPath := writeWorkflowTestPack(t, stepEvalPackJSON)
			provider := &transitioningProvider{
				ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
				event:        tc.event,
			}

			wc, err := OpenWorkflow(packPath,
				WithSkipSchemaValidation(),
				WithProvider(provider),
				WithEvalRegistry(registry),
				WithEventBus(bus),
			)
			require.NoError(t, err)
			defer wc.Close()

			_, err = wc.Send(context.Background(), "hello")
			require.NoError(t, err)
			require.Equal(t, tc.wantState, wc.CurrentState())

			got := waitStepEvals(t, col, 1)
			require.Len(t, got, 1)
			assert.Equal(t, "step", got[0].EvalID)
			assert.Equal(t, 1, got[0].TurnIndex)
			seen := step.transcripts()
			require.Len(t, seen, 1)
			assert.Contains(t, seen[0], "reply after transition",
				"the eval runs once the turn that transitioned has finished")

			// A later turn with no transition adds no workflow-step eval.
			_, err = wc.Send(context.Background(), "again")
			require.NoError(t, err)
			require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerEveryTurn)) >= 2 },
				5*time.Second, 5*time.Millisecond)
			assert.Len(t, col.byTrigger(evals.TriggerOnWorkflowStep), 1)
		})
	}
}

// A resumed workflow reports the transcript's turn on its workflow-step evals,
// not a count restarted in this process.
func TestWorkflowStepEvals_ResumedWorkflow(t *testing.T) {
	ctx := context.Background()
	bus, col, registry, step := newStepEvalHarness(t)
	packPath := writeWorkflowTestPack(t, stepEvalPackJSON)

	const workflowID = "wf-step-resume"
	store := statestore.NewMemoryStore()
	require.NoError(t, store.Save(ctx, &statestore.ConversationState{
		ID:       workflowID,
		Metadata: map[string]any{workflowCurrentKey: &workflow.Context{CurrentState: "intake"}},
	}))
	require.NoError(t, store.AppendMessages(ctx, workflowID, []types.Message{
		{Role: "user", Content: "one"}, {Role: "assistant", Content: "1"},
		{Role: "user", Content: "two"}, {Role: "assistant", Content: "2"},
	}))

	wc, err := ResumeWorkflow(workflowID, packPath,
		WithStateStore(store),
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	defer wc.Close()

	_, err = wc.Send(ctx, "three")
	require.NoError(t, err)
	_, err = wc.Transition("InfoComplete")
	require.NoError(t, err)

	got := waitStepEvals(t, col, 1)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].TurnIndex, "the resumed transcript is on turn 3")
	seen := step.transcripts()
	require.Len(t, seen, 1)
	assert.Contains(t, seen[0], "one", "the eval sees the resumed transcript")
	assert.Contains(t, seen[0], "three")
}

// suspendWorkflowPack with an on_workflow_step eval.
const stepEvalSuspendPack = `{
	"id": "workflow-step-suspend", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"evals": [{"id": "step", "type": "wf_step_stub", "trigger": "on_workflow_step"}],
	"prompts": {
		"gather_info": {"id": "gather_info", "name": "Gather", "version": "1.0.0",
			"system_template": "You gather.", "tools": ["locate"]},
		"process": {"id": "process", "name": "Process", "version": "1.0.0", "system_template": "You process."}
	},
	"tools": {"locate": {"name": "locate", "description": "Client tool",
		"parameters": {"type": "object", "properties": {}}}},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {"prompt_task": "gather_info", "on_event": {"InfoComplete": "processing"}},
			"processing": {"prompt_task": "process"}
		}
	}
}`

// A turn that transitions and suspends on a client tool commits the
// transition when it is resumed. Its on_workflow_step evals run once, against
// the resumed turn, even though the resume went to the conversation directly.
func TestWorkflowStepEvals_SuspendedTurnResumed(t *testing.T) {
	bus, col, registry, step := newStepEvalHarness(t)
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds: []providers.PredictionResponse{
			{Content: "Moving on.", ToolCalls: []types.MessageToolCall{
				{ID: "c-tr", Name: "workflow__transition", Args: []byte(`{"event":"InfoComplete","context":"located"}`)},
				{ID: "c-cli", Name: "locate", Args: []byte(`{}`)},
			}},
			{Content: "Processing."},
		},
	}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, stepEvalSuspendPack),
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	caller := wc.ActiveConversation()
	desc, err := caller.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"
	ctx := context.Background()

	resp, err := wc.Send(ctx, "hi")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())

	require.NoError(t, caller.SendToolResult(ctx, "c-cli", "Paris"))
	_, err = caller.Resume(ctx)
	require.NoError(t, err)
	require.Equal(t, "processing", wc.CurrentState())

	require.NoError(t, wc.Close())

	got := waitStepEvals(t, col, 1)
	require.Len(t, got, 1)
	seen := step.transcripts()
	require.Len(t, seen, 1)
	assert.Contains(t, seen[0], "Processing.", "the eval runs against the resumed turn")
}
