package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/yaml"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// groupedEvals mixes judge- and classifier-backed checks, a wrapper around a
// judge, a custom long-running handler and one fast check. None sets groups.
const groupedEvals = `
	{"id": "judge", "type": "llm_judge", "trigger": "every_turn", "params": {"criteria": "polite"}},
	{"id": "tox", "type": "toxicity", "trigger": "every_turn"},
	{"id": "wrapped-judge", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "llm_judge", "eval_params": {"criteria": "polite"}, "min_score": 0.5}},
	{"id": "custom-long", "type": "e2e_strict", "trigger": "every_turn"},
	{"id": "fast", "type": "contains", "trigger": "every_turn", "params": {"patterns": ["hello"]}}`

// A host that runs only fast-running evals inline never blocks a turn on a
// judge; the long-running group holds every judge, the wrapper around one and
// a handler that declares itself long-running.
func TestE2E_Groups_JudgesAndClassifiersAreLongRunning(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	reg.Register(strictHandler{})

	judge := &countingJudge{}
	conv, log := openE2E(t, e2ePack("", groupedEvals, ""), "hello there",
		sdk.WithEvalRegistry(reg), sdk.WithJudgeProvider(judge), sdk.WithEvalGroups(evals.GroupFastRunning))
	sendAndClose(t, conv, "hi")
	assert.Equal(t, map[string]int{"fast": 1}, log.ids())
	assert.Zero(t, judge.calls, "a fast-running host called a judge")

	judge2 := &countingJudge{}
	conv2, log2 := openE2E(t, e2ePack("", groupedEvals, ""), "hello there",
		sdk.WithEvalRegistry(reg), sdk.WithJudgeProvider(judge2), sdk.WithEvalGroups(evals.GroupLongRunning))
	sendAndClose(t, conv2, "hi")
	assert.Equal(t, map[string]int{"judge": 1, "tox": 1, "wrapped-judge": 1, "custom-long": 1}, log2.ids())
	assert.Equal(t, 3, judge2.calls)
}

// lifecycleEvals has one eval per lifecycle trigger.
const lifecycleEvals = `
	{"id": "turn", "type": "min_length", "trigger": "every_turn", "params": {"min": 1}},
	{"id": "session", "type": "min_length", "trigger": "on_session_complete", "params": {"min": 1}},
	{"id": "conversation", "type": "min_length", "trigger": "on_conversation_complete", "params": {"min": 1}}`

// openStored opens (or, with resume, resumes) conversation id on store.
func openStored(
	t *testing.T, packPath, id string, store statestore.Store, resume bool, opts ...sdk.Option,
) (*sdk.Conversation, *resultLog) {
	t.Helper()
	log := &resultLog{}
	all := append([]sdk.Option{
		sdk.WithProvider(replyProvider("hello")),
		sdk.WithStateStore(store),
		sdk.WithEvalHook(log),
	}, opts...)
	var conv *sdk.Conversation
	var err error
	if resume {
		conv, err = sdk.Resume(id, packPath, "chat", all...)
	} else {
		conv, err = sdk.Open(packPath, "chat", append(all, sdk.WithConversationID(id))...)
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, log
}

// on_conversation_complete runs on Close over the whole transcript, reaches
// the event bus with its trigger, runs again after Resume over the reloaded
// history, and does not run for a conversation that said nothing.
func TestE2E_ConversationComplete_OnCloseAndAgainAfterResume(t *testing.T) {
	packPath := writePackFile(t, e2ePack("", lifecycleEvals, ""))
	store := statestore.NewMemoryStore()
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	ec := newEventCollector(bus)

	conv, log := openStored(t, packPath, "conv-1", store, false, sdk.WithEventBus(bus))
	sendAndClose(t, conv, "one", "two")
	assert.Equal(t, map[string]int{"turn": 2, "session": 1, "conversation": 1}, log.ids())
	first := log.messagesSeen("conversation")[0]
	assert.GreaterOrEqual(t, first, 4, "the conversation eval sees the whole transcript")

	// The bus delivers asynchronously: wait for the conversation eval's event.
	conversationTriggers := func() []string {
		var out []string
		for _, e := range ec.ofType(events.EventEvalCompleted) {
			if d, ok := e.Data.(*events.EvalEventData); ok && d.EvalID == "conversation" {
				out = append(out, d.Trigger)
			}
		}
		return out
	}
	require.Eventually(t, func() bool { return len(conversationTriggers()) > 0 }, 2*time.Second, 5*time.Millisecond,
		"the conversation eval's result never reached the event bus")
	assert.Equal(t, []string{evals.TriggerOnConversationComplete}, conversationTriggers())

	resumed, log2 := openStored(t, packPath, "conv-1", store, true)
	sendAndClose(t, resumed, "three")
	assert.Equal(t, map[string]int{"turn": 1, "session": 1, "conversation": 1}, log2.ids())
	assert.Greater(t, log2.messagesSeen("conversation")[0], first, "the resumed close scores the reloaded history")

	empty, log3 := openStored(t, packPath, "conv-empty", store, false)
	require.NoError(t, empty.Close())
	assert.Empty(t, log3.byID("conversation"), "a conversation that said nothing has no transcript to score")
}

// Eval histograms with a declared range get ten buckets across it; without
// one they keep prometheus.DefBuckets.
func TestE2E_HistogramBuckets(t *testing.T) {
	bus, collector, reg := testMetricsSetup(t)
	packJSON := e2ePack("", `
	{"id": "ranged", "type": "sentence_count", "trigger": "every_turn", "params": {"max": 1},
	 "metric": {"name": "reply_score", "type": "histogram", "range": {"min": 0, "max": 1}}},
	{"id": "unranged", "type": "sentence_count", "trigger": "every_turn", "params": {"max": 1},
	 "metric": {"name": "raw_score", "type": "histogram"}}`, "")
	conv, _ := openE2E(t, packJSON, twoSentences, sdk.WithEventBus(bus), sdk.WithMetrics(collector, nil))
	sendAndClose(t, conv, "count")

	bounds := func(suffix string) ([]float64, uint64) {
		families, err := reg.Gather()
		require.NoError(t, err)
		for _, f := range families {
			if !strings.HasSuffix(f.GetName(), suffix) {
				continue
			}
			h := f.GetMetric()[0].GetHistogram()
			var out []float64
			var atHalf uint64
			for _, b := range h.GetBucket() {
				out = append(out, b.GetUpperBound())
				if b.GetUpperBound() == 0.5 {
					atHalf = b.GetCumulativeCount()
				}
			}
			return out, atHalf
		}
		dumpRegistry(t, reg)
		t.Fatalf("no histogram named *%s", suffix)
		return nil, 0
	}
	ranged, atHalf := bounds("reply_score")
	assert.Equal(t, []float64{0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1}, ranged)
	assert.Equal(t, uint64(1), atHalf, "the 0.5 score falls in the le=0.5 bucket")
	unranged, _ := bounds("raw_score")
	assert.Equal(t, []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}, unranged)
}

// toolPack offers get_weather to the chat prompt.
func toolPack(packEvals string) string {
	return e2ePack(`,
			"tools": ["get_weather"]`, packEvals, `,
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get weather for a city",
			"parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
		}
	}`)
}

// twoWeatherCalls makes the model call get_weather twice, then answer.
const twoWeatherCalls = `
defaultResponse: unused
scenarios:
  e2e:
    turns:
      1:
        type: tool_calls
        tool_calls:
          - name: get_weather
            arguments: {city: Paris}
          - name: get_weather
            arguments: {city: Rome}
      2: Sunny in both.
`

// A turn exactly at tool_efficiency's max_calls scores 1/(max_calls+1), not 0,
// so an assertion with a low pass line passes it; one over the cap scores 0.
func TestE2E_ToolEfficiency_AtTheCap(t *testing.T) {
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, mockYAMLRepo(t, twoWeatherCalls))
	log := &resultLog{}
	conv, err := sdk.Open(writePackFile(t, toolPack(`
	{"id": "at-cap", "type": "tool_efficiency", "trigger": "every_turn", "params": {"max_calls": 2}},
	{"id": "at-cap-gate", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "tool_efficiency", "eval_params": {"max_calls": 2}, "min_score": 0.3}},
	{"id": "over-cap", "type": "tool_efficiency", "trigger": "every_turn", "params": {"max_calls": 1}}`)), "chat",
		sdk.WithProvider(provider), sdk.WithEvalHook(log))
	require.NoError(t, err)
	var calls int
	var mu sync.Mutex
	conv.OnTool("get_weather", func(map[string]any) (any, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return map[string]any{"forecast": "sunny"}, nil
	})
	resp := sendAndClose(t, conv, "weather?")[0]
	require.Equal(t, 2, calls, "the model's two calls ran")
	assert.Equal(t, "Sunny in both.", resp.Text())

	assert.InDelta(t, 1.0/3.0, score(t, log.only(t, "at-cap")), 1e-9)
	assert.Equal(t, "pass", passed(log.only(t, "at-cap-gate")))
	assert.InDelta(t, 0.0, score(t, log.only(t, "over-cap")), 1e-9)
}

// toolRecorder is the real mock tool provider, recording what each round sent.
type toolRecorder struct {
	*mock.ToolProvider
	mu   sync.Mutex
	seen [][]types.Message
}

func (p *toolRecorder) SupportsStreaming() bool { return false }

func (p *toolRecorder) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, offered providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	p.mu.Unlock()
	return p.ToolProvider.PredictWithTools(ctx, req, offered, choice)
}

// toolResultText returns the text of the tool result for callID in the last
// round the model received.
func (p *toolRecorder) toolResultText(t *testing.T, callID string) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.seen)
	last := p.seen[len(p.seen)-1]
	for i := range last {
		if r := last[i].ToolResult; r != nil && r.ID == callID {
			return r.GetTextContent()
		}
	}
	t.Fatalf("no result for %s reached the model in %d rounds", callID, len(p.seen))
	return ""
}

const locateCall = `
defaultResponse: unused
scenarios:
  e2e:
    turns:
      1:
        type: tool_calls
        tool_calls:
          - name: locate
            arguments: {}
      2: I will ask for your city instead.
`

// A host declares a client tool's decline_strategy through
// WithToolDescriptorOverride (a pack cannot carry it); RejectClientTool then
// tells the model what the strategy says.
func TestE2E_DeclineStrategy_FromDescriptorOverride(t *testing.T) {
	provider := &toolRecorder{ToolProvider: mock.NewToolProviderWithRepository(
		"mock", "mock-model", false, mockYAMLRepo(t, locateCall))}
	packJSON := e2ePack(`,
			"tools": ["locate"]`, "", `,
	"tools": {"locate": {"name": "locate", "description": "Locate the user",
	                      "parameters": {"type": "object", "properties": {}}}}`)
	conv, err := sdk.Open(writePackFile(t, packJSON), "chat",
		sdk.WithProvider(provider),
		sdk.WithToolDescriptorOverride("locate", func(d *tools.ToolDescriptor) {
			d.Mode = "client"
			d.ClientConfig = &tools.ClientConfig{Consent: &tools.ConsentConfig{
				Required: true, Message: "Share location?", DeclineStrategy: tools.DeclineStrategyFallback,
			}}
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.DeferClientTool("locate")

	ctx := context.Background()
	resp, err := conv.Send(ctx, "where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())
	callID := resp.ClientTools()[0].CallID
	conv.RejectClientTool(ctx, callID, "no thanks")
	final, err := conv.Resume(ctx)
	require.NoError(t, err)

	assert.Equal(t,
		"Tool locate unavailable: user declined consent. Use alternative approach. Reason: no thanks",
		provider.toolResultText(t, callID))
	assert.Equal(t, "I will ask for your city instead.", final.Text())
}

// workflowEvalPack is a two-step workflow with a pack-level step eval.
const workflowEvalPack = `{
	"id": "e2e-workflow-evals",
	"name": "E2E workflow evals",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {"prompt_task": "intake", "orchestration": "external", "on_event": {"Escalate": "specialist"}},
			"specialist": {"prompt_task": "specialist", "orchestration": "external", "on_event": {"Resolve": "closed"}},
			"closed": {"prompt_task": "closed", "terminal": true}
		}
	},
	"prompts": {
		"intake": {"id": "intake", "name": "Intake", "version": "1.0.0", "system_template": "Intake."},
		"specialist": {"id": "specialist", "name": "Specialist", "version": "1.0.0", "system_template": "Specialist."},
		"closed": {"id": "closed", "name": "Closed", "version": "1.0.0", "system_template": "Closed."}
	},
	"evals": [
		{"id": "step", "type": "min_length", "trigger": "on_workflow_step", "params": {"min": 1}},
		{"id": "turn", "type": "min_length", "trigger": "every_turn", "params": {"min": 1}}
	]
}`

// on_workflow_step runs once per committed transition, against the outgoing
// state's turn.
func TestE2E_WorkflowStepEvals_OncePerTransition(t *testing.T) {
	log := &resultLog{}
	wc, err := sdk.OpenWorkflow(writePackFile(t, workflowEvalPack),
		sdk.WithProvider(replyProvider("noted")), sdk.WithEvalHook(log))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	ctx := context.Background()
	_, err = wc.Send(ctx, "help")
	require.NoError(t, err)
	_, err = wc.Transition("Escalate")
	require.NoError(t, err)
	require.Len(t, log.byID("step"), 1)
	assert.GreaterOrEqual(t, log.messagesSeen("step")[0], 2, "the step eval sees the intake turn")

	_, err = wc.Send(ctx, "more")
	require.NoError(t, err)
	_, err = wc.Transition("Resolve")
	require.NoError(t, err)
	assert.Len(t, log.byID("step"), 2)
	assert.Len(t, log.byID("turn"), 2)
}

// judgeSystemPromptYAML is a judge prompt on disk with a fragment, the way
// Arena's judge_defaults.prompt names one.
const judgeSystemPromptYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: house-judge
spec:
  task_type: house-judge
  version: v1.0.0
  description: House judge
  system_template: "{{house_rules}} Grade against: {{criteria}}. Reply under review: {{response}}"
  variables:
    - name: criteria
      required: true
    - name: response
      required: true
  fragments:
    - name: house_rules
      path: frags/house_rules.yaml
      required: true
`

// judge_defaults.prompt through the real YAML repository and the real mock
// provider as judge: the system prompt the judge model receives is that
// prompt's rendered system template, fragment included.
func TestE2E_JudgeDefaultsPrompt_ReachesTheJudgeModel(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "prompts", "frags"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "prompts", "judge.yaml"), []byte(judgeSystemPromptYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(base, "prompts", "frags", "house_rules.yaml"),
		[]byte("fragment_type: text\nversion: v1\ncontent: HOUSE RULES APPLY.\n"), 0o600))
	reg := prompt.NewRegistryWithRepository(
		yaml.NewYAMLPromptRepository(base, map[string]string{"house-judge": "prompts/judge.yaml"}))

	judgeModel := &requestRecorder{Provider: replyProvider(`{"passed": true, "score": 0.8, "reasoning": "fine"}`)}
	runner := evals.NewEvalRunner(evals.NewEvalTypeRegistry())
	evalCtx := evals.BuildEvalContext([]types.Message{
		{Role: "user", Content: "Reset my device"},
		{Role: "assistant", Content: "Hold power for ten seconds."},
	}, 1, "s1", "chat", map[string]any{
		"judge_provider":                handlers.NewProviderJudge(judgeModel),
		handlers.MetadataJudgeDefaults:  map[string]any{"prompt": "house-judge"},
		handlers.MetadataPromptRegistry: reg,
	})
	results := runner.RunTurnEvals(context.Background(), []evals.EvalDef{
		{ID: "graded", Type: "llm_judge", Trigger: evals.TriggerEveryTurn, Params: map[string]any{"criteria": "actionable"}},
	}, evalCtx)
	require.Len(t, results, 1)
	require.Empty(t, results[0].Error)
	assert.InDelta(t, 0.8, *results[0].Score, 1e-9)

	reqs := judgeModel.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t,
		"HOUSE RULES APPLY. Grade against: actionable. Reply under review: Hold power for ten seconds.",
		reqs[0].System)
}
