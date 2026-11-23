package provider

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/replay"
	"github.com/AltairaLabs/PromptKit/runtime/v2/recording"
	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover #2713: on a turn whose provider was sent tools, a call to a
// registered tool the turn does not make eligible (the prompt's tools, the
// capability tools and the grants) is refused, after the blocklist and the
// tool hooks, with a tool result naming it.

const unofferedRefusalText = "Tool unlisted was not run: it is not available this turn"

// unofferedAllowed is the prompt's tools in the unoffered fixture.
var unofferedAllowed = []string{"offered", "fine"}

// unofferedFixture is a text turn's tool loop over a registry holding
// "offered" and "fine", which the prompt allows, and "unlisted", which it
// does not. A tool hook denies the tool named by deny.
type unofferedFixture struct {
	loop *toolLoop
	mu   sync.Mutex
	evs  []*events.Event
}

func newUnofferedFixture(t *testing.T, provider providers.Provider, deny string) *unofferedFixture {
	t.Helper()
	registry := tools.NewRegistry()
	for _, name := range []string{"offered", "fine", "unlisted"} {
		require.NoError(t, registry.Register(&tools.ToolDescriptor{
			Name: name, Description: name, InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
		}))
	}
	f := &unofferedFixture{}
	bus := events.NewEventBus()
	bus.SubscribeAll(func(e *events.Event) {
		switch e.Type {
		case events.EventToolCallStarted, events.EventToolCallFailed, events.EventToolCallCompleted:
			f.mu.Lock()
			f.evs = append(f.evs, e)
			f.mu.Unlock()
		}
	})
	hookReg := hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{blockedTool: deny, reason: "denied"}))
	stage := NewProviderStageWithHooks(provider, registry, nil, &ProviderConfig{},
		events.NewEmitter(bus, "run", "sess", "conv"), hookReg)
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: unofferedAllowed,
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	loop.MaxRounds = 10
	f.loop = loop
	return f
}

// toolEvents waits for n tool events and returns their types and tool names.
func (f *unofferedFixture) toolEvents(t *testing.T, n int) [][2]string {
	t.Helper()
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.evs) >= n
	}, 2*time.Second, time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][2]string, 0, len(f.evs))
	for _, e := range f.evs {
		d, _ := e.Data.(*events.ToolCallEventData)
		out = append(out, [2]string{string(e.Type), d.ToolName})
	}
	return out
}

func unofferedCall(id, name string) types.MessageToolCall {
	// Distinct arguments per call: no repeated-call nudge.
	return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(`{"n":"` + id + `"}`)}
}

// playRound plays one provider round of calls through the loop and returns
// the round's tool results.
func (f *unofferedFixture) playRound(t *testing.T, round int, calls ...types.MessageToolCall) []types.Message {
	t.Helper()
	before := len(f.loop.messages)
	response := types.Message{Role: "assistant", ToolCalls: calls}
	done, _, err := f.loop.afterRound(context.Background(), unofferedAllowed, &response, true, RoundRef{Round: round})
	require.NoError(t, err)
	require.False(t, done)
	return f.loop.messages[before+1:]
}

// A registered tool the prompt does not allow is refused, in call order with
// the round's other results, and the refusal is reported as a failed call.
func TestToolLoop_UnofferedRegisteredToolIsRefused(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "")

	got := f.playRound(t, 1, unofferedCall("c1", "unlisted"), unofferedCall("c2", "fine"))

	require.Len(t, got, 2)
	assert.Equal(t, policyRefusal("c1", "unlisted", unofferedRefusalText), got[0])
	assert.Equal(t, "c2", got[1].ToolResult.ID)
	assert.Empty(t, got[1].ToolResult.Error, "the allowed tool ran")
	evs := f.toolEvents(t, 4)
	assert.Contains(t, evs, [2]string{string(events.EventToolCallStarted), "unlisted"})
	assert.Contains(t, evs, [2]string{string(events.EventToolCallFailed), "unlisted"})
}

// The tool hooks run before the offer check: a hook's denial is what the
// model is told.
func TestToolLoop_HookDenialPrecedesTheOfferCheck(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "unlisted")

	got := f.playRound(t, 1, unofferedCall("c1", "unlisted"))

	assert.Equal(t, []types.Message{policyRefusal("c1", "unlisted", "Tool unlisted blocked by hook: denied")}, got)
}

// A name the registry does not hold keeps its not-found execution error.
func TestToolLoop_UnregisteredToolKeepsNotFound(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "")

	got := f.playRound(t, 1, unofferedCall("c1", "ghost"))

	require.Len(t, got, 1)
	assert.Equal(t, "Error: tool not found: ghost", got[0].ToolResult.GetTextContent())
	assert.Equal(t, types.ToolErrorExecution, got[0].ToolResult.ErrorType)
}

// A tool excluded for the turn after repeated rejections keeps its usual
// path (here the hook denies it again).
func TestToolLoop_ExcludedToolKeepsItsPath(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "offered")
	f.playRound(t, 1, unofferedCall("c1", "offered"))
	f.playRound(t, 2, unofferedCall("c2", "offered"))
	require.True(t, f.loop.Excluded["offered"])

	got := f.playRound(t, 3, unofferedCall("c3", "offered"))

	assert.Equal(t, []types.Message{policyRefusal("c3", "offered", "Tool offered blocked by hook: denied")}, got)
}

// Refusals of a tool the model was never offered are not rejections of an
// offered tool: repeated ones exclude nothing, and change no offer.
func TestToolLoop_UnofferedRefusalsAreNotRejections(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "")
	f.playRound(t, 1, unofferedCall("c1", "unlisted"))
	f.playRound(t, 2, unofferedCall("c2", "unlisted"))

	assert.Empty(t, f.loop.Excluded, "a tool never offered was excluded")
}

// firstOnlySelector selects only the first candidate.
type firstOnlySelector struct{}

func (firstOnlySelector) Name() string { return "first-only" }

func (firstOnlySelector) Init(selection.SelectorContext) error { return nil }

func (firstOnlySelector) Select(
	_ context.Context, _ selection.Query, candidates []selection.Candidate,
) ([]string, error) {
	return []string{candidates[0].ID}, nil
}

// A tool the ToolSelector left out of the tools array is still one the
// prompt allows: a call to it runs, it is not refused.
func TestToolLoop_SelectorMissIsNotRefused(t *testing.T) {
	registry := tools.NewRegistry()
	for _, name := range unofferedAllowed {
		require.NoError(t, registry.Register(&tools.ToolDescriptor{
			Name: name, Description: name, InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
		}))
	}
	stage := NewProviderStage(mock.NewToolProvider("test", "model", false, nil), registry, nil,
		&ProviderConfig{ToolSelector: firstOnlySelector{}})
	acc := &providerInput{
		allowedTools: unofferedAllowed,
		messages:     []types.Message{{Role: "user", Parts: []types.ContentPart{types.NewTextPart("hi")}}},
	}
	stage.applyToolSelector(context.Background(), acc)
	require.Equal(t, []string{"offered"}, acc.allowedTools, "the selector narrowed the array")
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)

	response := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{unofferedCall("c1", "fine")}}
	_, _, err = loop.afterRound(context.Background(), acc.allowedTools, &response, true, RoundRef{Round: 1})
	require.NoError(t, err)

	require.Len(t, loop.messages, 3)
	assert.Equal(t, "c1", loop.messages[2].ToolResult.ID)
	assert.Empty(t, loop.messages[2].ToolResult.Error, "the allowed tool ran")
}

// A provider that is never sent tools (replay, Predict only) has its
// recorded tool calls run as before: there is no offer to check them against.
func TestToolLoop_ProviderSentNoToolsIsNotGated(t *testing.T) {
	data, err := json.Marshal(events.MessageCreatedData{
		Role:      "assistant",
		ToolCalls: []events.MessageToolCall{{ID: "c1", Name: "unlisted", Args: `{}`}},
	})
	require.NoError(t, err)
	now := time.Now()
	rec := &recording.SessionRecording{
		Metadata: recording.Metadata{SessionID: "s", StartTime: now, EndTime: now.Add(time.Second), Version: "1.0"},
		Events: []recording.RecordedEvent{
			{Sequence: 1, Type: events.EventMessageCreated, Timestamp: now, SessionID: "s", Data: data},
		},
	}
	provider, err := replay.NewProvider(rec, nil)
	require.NoError(t, err)
	f := newUnofferedFixture(t, provider, "")
	resp, err := provider.Predict(context.Background(), providers.PredictionRequest{})
	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1)

	got := f.playRound(t, 1, resp.ToolCalls...)

	require.Len(t, got, 1)
	assert.Equal(t, "c1", got[0].ToolResult.ID)
	assert.Empty(t, got[0].ToolResult.Error, "the recorded call ran")
}

// scriptedRoundsProvider plays one round of tool calls per script entry, then
// answers in text, and records the tool results each round was sent and the
// tool names of each tools array built.
type scriptedRoundsProvider struct {
	*mock.ToolProvider
	script [][]types.MessageToolCall
	rounds int
	last   []types.Message
	builds [][]string
}

func (p *scriptedRoundsProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.Name)
	}
	p.builds = append(p.builds, names)
	return p.ToolProvider.BuildTooling(descriptors)
}

func (p *scriptedRoundsProvider) SupportsStreaming() bool { return false }

func (p *scriptedRoundsProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.last = req.Messages
	p.rounds++
	if p.rounds <= len(p.script) {
		calls := p.script[p.rounds-1]
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// A workflow transition mid-turn changes what the model may call from the
// next round on: the destination state's tool runs, and the origin state's
// is refused. It holds whether or not the destination's prompt differs: a
// state with the same prompt is not rebuilt, and its tools still apply.
func TestToolLoop_HandoffChangesTheEligibleTools(t *testing.T) {
	for name, destPrompt := range map[string]string{"new prompt": "DESTINATION PROMPT", "same prompt": "ORIGIN PROMPT"} {
		t.Run(name, func(t *testing.T) {
			results, builds := handoffRoundResults(t, destPrompt)

			require.NotEmpty(t, builds)
			assert.Equal(t, []string{"destination_tool", "workflow__transition"}, builds[len(builds)-1],
				"the model is offered the destination state's tools")
			require.Len(t, results, 2)
			assert.Equal(t, "c2", results[0].ID)
			assert.Empty(t, results[0].Error, "the destination state's tool ran")
			assert.Equal(t, "Tool origin_tool was not run: it is not available this turn", results[1].Error)
		})
	}
}

// handoffRoundResults plays a turn that moves from a state allowing
// origin_tool to one allowing destination_tool, with destPrompt, and returns
// the results of the round after the transition and the tools arrays built.
func handoffRoundResults(t *testing.T, destPrompt string) ([]*types.MessageToolResult, [][]string) {
	t.Helper()
	registry := tools.NewRegistry()
	for _, name := range []string{"workflow__transition", "origin_tool", "destination_tool"} {
		require.NoError(t, registry.Register(&tools.ToolDescriptor{
			Name: name, Description: name, InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
		}))
	}
	provider := &scriptedRoundsProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		script: [][]types.MessageToolCall{
			{unofferedCall("c1", "workflow__transition")},
			{unofferedCall("c2", "destination_tool"), unofferedCall("c3", "origin_tool")},
		},
	}
	turnState := core.NewTurnState()
	turnState.SystemPrompt = "ORIGIN PROMPT"
	turnState.AllowedTools = []string{"workflow__transition", "origin_tool"}
	stage := NewProviderStageWithTurnState(provider, registry, nil, &ProviderConfig{MaxTokens: 100}, nil, nil,
		turnState)
	stage.SetWorkflowStateResolver(&fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN PROMPT", AllowedTools: []string{"workflow__transition", "origin_tool"}},
		{Valid: true, SystemPrompt: destPrompt,
			AllowedTools: []string{"workflow__transition", "destination_tool"}},
	}})

	runHandoffTurn(t, stage)

	var results []*types.MessageToolResult
	for i := range provider.last {
		if tr := provider.last[i].ToolResult; tr != nil && tr.ID != "c1" {
			results = append(results, tr)
		}
	}
	return results, provider.builds
}

// loopFor returns a text turn's tool loop on a tool-calling provider over reg,
// with allowed as the prompt's tools, under policy and cfg.
func loopFor(
	t *testing.T, reg *tools.Registry, allowed []string, policy *pipeline.ToolPolicy, cfg *ProviderConfig,
) *toolLoop {
	t.Helper()
	stage := NewProviderStage(mock.NewToolProvider("test", "model", false, nil), reg, policy, cfg)
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: allowed,
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	loop.MaxRounds = 10
	return loop
}

// play runs one round of calls through loop and returns whether the turn
// ended and the round's tool results.
func play(t *testing.T, loop *toolLoop, allowed []string, round int, calls ...types.MessageToolCall) (bool, []types.Message) {
	t.Helper()
	before := len(loop.messages)
	response := types.Message{Role: "assistant", ToolCalls: calls}
	done, _, err := loop.afterRound(context.Background(), allowed, &response, true, RoundRef{Round: round})
	require.NoError(t, err)
	return done, loop.messages[before+1:]
}

// A tool granted by a call of a round is callable from the next round: the
// round's calls are checked against what was eligible when it began.
func TestToolLoop_GrantTakesEffectFromTheNextRound(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	allowed := []string{"weather", "activate"}
	// One call at a time, so activate has granted refund before refund is checked.
	loop := loopFor(t, reg, allowed, &pipeline.ToolPolicy{MaxParallelToolCalls: 1},
		&ProviderConfig{ToolGrants: exec.grants})

	_, first := play(t, loop, allowed, 1, unofferedCall("c1", "activate"), unofferedCall("c2", "refund"))
	_, second := play(t, loop, allowed, 2, unofferedCall("c3", "refund"))

	require.Len(t, first, 2)
	assert.Equal(t, policyRefusal("c2", "refund", "Tool refund was not run: it is not available this turn"), first[1])
	assert.Equal(t, []types.Message{okText("c3", "refund", "ok")}, second)
	assert.Equal(t, []string{"activate", "refund"}, exec.calls())
}

func okText(id, name, text string) types.Message {
	return types.NewToolResultMessage(types.NewTextToolResult(id, name, text))
}

// toolCallCounter is a workflow resolver that only counts the executed tool
// calls reported to it.
type toolCallCounter struct{ n int }

func (c *toolCallCounter) ResolveCurrentState(context.Context) (Handoff, error) {
	return Handoff{}, nil
}
func (c *toolCallCounter) CurrentStateMeta() map[string]any { return nil }
func (c *toolCallCounter) RecordToolCalls(n int)            { c.n += n }

// A refused call to an unavailable tool takes no place under the call
// budgets and does not fire termination.tool_called.
func TestToolLoop_UnavailableCallSpendsNoBudgetAndStopsNothing(t *testing.T) {
	reg := registryWithTools(t, "fine", "unlisted")
	reg.RegisterExecutor(&grantingExec{})
	allowed := []string{"fine"}
	stage := NewProviderStage(mock.NewToolProvider("test", "model", false, nil), reg,
		&pipeline.ToolPolicy{MaxToolCallsPerTurn: 1, StopOnTool: "unlisted"}, &ProviderConfig{})
	counter := &toolCallCounter{}
	stage.SetWorkflowStateResolver(counter)
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: allowed, messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	loop.MaxRounds = 10

	done, got := play(t, loop, allowed, 1, unofferedCall("c1", "unlisted"), unofferedCall("c2", "fine"))

	assert.False(t, done, "a refused call does not fire termination.tool_called")
	assert.Equal(t, []types.Message{
		policyRefusal("c1", "unlisted", unofferedRefusalText),
		okText("c2", "fine", "ok"),
	}, got, "the allowed call is within the budget of one")
	assert.Equal(t, 1, counter.n, "only the call that ran is recorded")
}

// A call to an unavailable tool that the blocklist refuses first is charged
// to no budget either: the round decides once that it is unavailable.
func TestToolLoop_BlocklistedUnavailableCallIsNotRecorded(t *testing.T) {
	reg := registryWithTools(t, "fine", "unlisted")
	reg.RegisterExecutor(&grantingExec{})
	allowed := []string{"fine"}
	stage := NewProviderStage(mock.NewToolProvider("test", "model", false, nil), reg,
		&pipeline.ToolPolicy{Blocklist: []string{"unlisted"}}, &ProviderConfig{})
	counter := &toolCallCounter{}
	stage.SetWorkflowStateResolver(counter)
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: allowed, messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	_, got := play(t, loop, allowed, 1, unofferedCall("c1", "unlisted"), unofferedCall("c2", "fine"))

	assert.Equal(t, []types.Message{
		policyRefusal("c1", "unlisted", "Tool unlisted is blocked by policy"),
		okText("c2", "fine", "ok"),
	}, got)
	assert.Equal(t, 1, counter.n, "only the call that ran is recorded")
}

// A tool DescribeTool withholds is not offered, and a call to it is refused.
func TestToolLoop_DescribeToolWithheldToolIsUnavailable(t *testing.T) {
	reg := registryWithTools(t, "fine", "unlisted")
	reg.RegisterExecutor(&grantingExec{})
	allowed := []string{"fine", "unlisted"}
	loop := loopFor(t, reg, allowed, nil, &ProviderConfig{
		DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			if d.Name == "unlisted" {
				return nil
			}
			return d
		},
	})

	_, got := play(t, loop, allowed, 1, unofferedCall("c1", "unlisted"))

	assert.Equal(t, []types.Message{policyRefusal("c1", "unlisted", unofferedRefusalText)}, got)
}

// With tool calling off for the turn (tool_choice: none) a tool-calling
// provider is offered nothing, and every call is refused.
func TestToolLoop_ToolChoiceNoneRefusesEveryCall(t *testing.T) {
	reg := registryWithTools(t, "fine", "unlisted")
	exec := &grantingExec{}
	reg.RegisterExecutor(exec)
	allowed := []string{"fine", "unlisted"}
	loop := loopFor(t, reg, allowed, &pipeline.ToolPolicy{ToolChoice: ToolChoiceNone}, &ProviderConfig{})

	_, got := play(t, loop, allowed, 1, unofferedCall("c1", "unlisted"))

	assert.Equal(t, []types.Message{policyRefusal("c1", "unlisted", unofferedRefusalText)}, got)
	assert.Empty(t, exec.calls())
}

// A name no tool is registered under fails as not found before the approval
// gate is asked about it.
func TestToolLoop_UnregisteredToolIsNotHeldForApproval(t *testing.T) {
	reg := registryWithTools(t, "fine")
	var asked []string
	loop := loopFor(t, reg, []string{"fine"}, nil, &ProviderConfig{
		ApprovalChecker: func(_ context.Context, _, name string, _ map[string]any) *tools.PendingToolInfo {
			asked = append(asked, name)
			return &tools.PendingToolInfo{Reason: "requires_approval"}
		},
	})

	_, got := play(t, loop, []string{"fine"}, 1, unofferedCall("c1", "ghost"))

	require.Len(t, got, 1)
	assert.Equal(t, "Error: tool not found: ghost", got[0].ToolResult.GetTextContent())
	assert.Empty(t, asked, "the approval gate was asked about a tool that does not exist")
}
