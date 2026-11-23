package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover tool.offer.rebuilt: a provider call sent a tools array that
// differs from the previous call's is reported, with what changed it, what
// it gained and lost, and the round. Each turn is a whole Process run on one
// stage, as the SDK runs every Send of a conversation on one pipeline.

// offerScriptProvider plays a script of tool-call rounds, one entry per call,
// then answers with text, recording the tool names each call was offered.
type offerScriptProvider struct {
	*mock.ToolProvider

	mu      sync.Mutex
	script  [][]types.MessageToolCall
	offered [][]string
	caching bool
	stream  bool
}

func newOfferScriptProvider(id string, script ...[]types.MessageToolCall) *offerScriptProvider {
	return &offerScriptProvider{ToolProvider: mock.NewToolProvider(id, "offer-model", false, nil), script: script}
}

func (p *offerScriptProvider) SupportsStreaming() bool     { return p.stream }
func (p *offerScriptProvider) SupportsPromptCaching() bool { return p.caching }

// next records what the call was offered and returns its scripted calls.
func (p *offerScriptProvider) next(offered providers.ProviderTools) []types.MessageToolCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	var names []string
	if descs, ok := offered.([]*providers.ToolDescriptor); ok {
		for _, d := range descs {
			names = append(names, d.Name)
		}
	}
	sort.Strings(names)
	p.offered = append(p.offered, names)
	if len(p.script) == 0 {
		return nil
	}
	calls := p.script[0]
	p.script = p.script[1:]
	return calls
}

// queue appends rounds to the script, for a later turn.
func (p *offerScriptProvider) queue(rounds ...[]types.MessageToolCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.script = append(p.script, rounds...)
}

func (p *offerScriptProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, offered providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if calls := p.next(offered); len(calls) > 0 {
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

func (p *offerScriptProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	resp, _, err := p.PredictWithTools(ctx, req, nil, "")
	return resp, err
}

func (p *offerScriptProvider) PredictStreamWithTools(
	_ context.Context, _ providers.PredictionRequest, offered providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	calls := p.next(offered)
	ch := make(chan providers.StreamChunk, 1)
	stop := "stop"
	if len(calls) > 0 {
		ch <- providers.StreamChunk{ToolCalls: calls, FinishReason: &stop}
	} else {
		ch <- providers.StreamChunk{Content: "done", Delta: "done", FinishReason: &stop}
	}
	close(ch)
	return ch, nil
}

func (p *offerScriptProvider) offeredOn(t *testing.T, call int) []string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Greater(t, len(p.offered), call)
	return p.offered[call]
}

// offerCallExecutor runs the registry's "local" tools, calling onRun with the
// tool's name first.
type offerCallExecutor struct{ onRun func(name string) }

func (e *offerCallExecutor) Name() string { return "local" }

func (e *offerCallExecutor) Execute(
	_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	if e.onRun != nil {
		e.onRun(d.Name)
	}
	return json.RawMessage(`{"ok":true}`), nil
}

// offerEvents collects the tool.offer.rebuilt events a bus delivers.
type offerEvents struct {
	bus *events.EventBus
	mu  sync.Mutex
	got []events.ToolOfferRebuiltData
}

func newOfferEvents(t *testing.T) *offerEvents {
	t.Helper()
	o := &offerEvents{bus: events.NewEventBus()}
	t.Cleanup(o.bus.Close)
	o.bus.Subscribe(events.EventToolOfferRebuilt, func(e *events.Event) {
		d, ok := e.Data.(*events.ToolOfferRebuiltData)
		if !ok {
			return
		}
		o.mu.Lock()
		o.got = append(o.got, *d)
		o.mu.Unlock()
	})
	return o
}

func (o *offerEvents) emitter() *events.Emitter {
	return events.NewEmitter(o.bus, "run", "sess", "conv")
}

// all closes the bus, which delivers every event published so far, and
// returns them.
func (o *offerEvents) all() []events.ToolOfferRebuiltData {
	o.bus.Close()
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.got
}

type offerStageOpts struct {
	allowed  []string
	cfg      *ProviderConfig
	policy   *pipeline.ToolPolicy
	hooks    *hooks.Registry
	onRun    func(name string)
	resolver WorkflowStateResolver
}

func newOfferStage(
	t *testing.T, prov providers.Provider, reg *tools.Registry, ev *offerEvents, o offerStageOpts,
) *ProviderStage {
	t.Helper()
	reg.RegisterExecutor(&offerCallExecutor{onRun: o.onRun})
	ts := core.NewTurnState()
	ts.AllowedTools = o.allowed
	cfg := o.cfg
	if cfg == nil {
		cfg = &ProviderConfig{}
	}
	var em *events.Emitter
	if ev != nil {
		em = ev.emitter()
	}
	s := NewProviderStageWithTurnState(prov, reg, o.policy, cfg, em, o.hooks, ts)
	s.SetWorkflowStateResolver(o.resolver)
	return s
}

// runOfferTurn runs one whole turn on s.
func runOfferTurn(t *testing.T, s *ProviderStage, text string) {
	t.Helper()
	input := make(chan core.StreamElement, 2)
	msg := types.Message{Role: "user"}
	msg.AddTextPart(text)
	input <- core.NewMessageElement(&msg)
	close(input)
	output := make(chan core.StreamElement, 64)
	require.NoError(t, s.Process(context.Background(), input, output))
	for range output { //nolint:revive // draining
	}
}

func offerCall(id, name string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(`{"id":"` + id + `"}`)}
}

// grantOnActivate returns a ToolGrants accessor that grants refund once the
// activate tool has run, and the onRun hook that runs it.
func grantOnActivate() (grants func() []string, onRun func(string)) {
	var mu sync.Mutex
	var granted []string
	return func() []string {
			mu.Lock()
			defer mu.Unlock()
			return granted
		}, func(name string) {
			if name == "activate" {
				mu.Lock()
				granted = []string{"refund"}
				mu.Unlock()
			}
		}
}

// A grant made by a tool round is reported on the next round, the first sent
// the granted tool. A later turn sent the same array reports nothing.
func TestToolOfferRebuilt_GrantMidTurn(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "activate")})
	grants, onRun := grantOnActivate()
	s := newOfferStage(t, prov, registryWithTools(t, "activate", "lookup_order", "refund"), ev, offerStageOpts{
		allowed: []string{"activate", "lookup_order"},
		cfg:     &ProviderConfig{ToolGrants: grants},
		onRun:   onRun,
	})

	runOfferTurn(t, s, "refund my order")
	runOfferTurn(t, s, "thanks")

	assert.Equal(t, []string{"activate", "lookup_order", "refund"}, prov.offeredOn(t, 1))
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonSkillGrant}, Added: []string{"refund"}, Round: 2,
	}}, ev.all())
}

// The streaming tool loop reports a grant the same way.
func TestToolOfferRebuilt_GrantMidTurnStreaming(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "activate")})
	prov.stream = true
	prov.caching = true
	grants, onRun := grantOnActivate()
	s := newOfferStage(t, prov, registryWithTools(t, "activate", "lookup_order", "refund"), ev, offerStageOpts{
		allowed: []string{"activate", "lookup_order"},
		cfg:     &ProviderConfig{ToolGrants: grants},
		onRun:   onRun,
	})

	runOfferTurn(t, s, "refund my order")

	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonSkillGrant}, Added: []string{"refund"}, Round: 2,
		CachingProvider: true,
	}}, ev.all())
}

// A round that runs a tool and changes nothing, and a later turn offering the
// same tools, report nothing.
func TestToolOfferRebuilt_UnchangedReportsNothing(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "lookup_order")})
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order"), ev, offerStageOpts{
		allowed: []string{"lookup_order"},
	})

	runOfferTurn(t, s, "where is my order")
	prov.queue([]types.MessageToolCall{offerCall("c2", "lookup_order")})
	runOfferTurn(t, s, "and now?")

	assert.Equal(t, []string{"lookup_order"}, prov.offeredOn(t, 3), "the tool was offered on every call")
	assert.Empty(t, ev.all())
}

// A tool dropped for failing twice is reported removed on the next round,
// and offered again, reported as a turn reset, on the next turn.
func TestToolOfferRebuilt_ExclusionAndTurnReset(t *testing.T) {
	ev := newOfferEvents(t)
	blocked := func(id string) types.MessageToolCall {
		return types.MessageToolCall{ID: id, Name: "blocked_tool", Args: json.RawMessage(`{"n":"` + id + `"}`)}
	}
	prov := newOfferScriptProvider("p", []types.MessageToolCall{blocked("c1")}, []types.MessageToolCall{blocked("c2")})
	s := newOfferStage(t, prov, registryWithTools(t, "blocked_tool", "lookup_order"), ev, offerStageOpts{
		allowed: []string{"blocked_tool", "lookup_order"},
		hooks:   hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{blockedTool: "blocked_tool", reason: "no"})),
	})

	runOfferTurn(t, s, "do the thing")
	runOfferTurn(t, s, "try again")

	assert.Equal(t, []string{"lookup_order"}, prov.offeredOn(t, 2))
	assert.Equal(t, []events.ToolOfferRebuiltData{
		{Reasons: []string{events.ToolOfferReasonExcluded}, Removed: []string{"blocked_tool"}, Round: 3},
		{Reasons: []string{events.ToolOfferReasonTurnReset}, Added: []string{"blocked_tool"}, Round: 1},
	}, ev.all())
}

// Spending max_tool_calls_per_turn sends the next round no tools, reported
// as every tool removed; the next turn offers them again.
func TestToolOfferRebuilt_Exhausted(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "lookup_order")})
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "refund"), ev, offerStageOpts{
		allowed: []string{"lookup_order", "refund"},
		policy:  &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1},
	})

	runOfferTurn(t, s, "where is my order")
	runOfferTurn(t, s, "and now?")

	assert.Nil(t, prov.offeredOn(t, 1), "the round after the budget is spent is sent no tools")
	assert.Equal(t, []events.ToolOfferRebuiltData{
		{Reasons: []string{events.ToolOfferReasonExhausted}, Removed: []string{"lookup_order", "refund"}, Round: 2},
		{Reasons: []string{events.ToolOfferReasonTurnReset}, Added: []string{"lookup_order", "refund"}, Round: 1},
	}, ev.all())
}

// A ToolSelector choosing different tools for a turn is reported on that
// turn's first call.
func TestToolOfferRebuilt_Selector(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p")
	sel := &stubToolSelector{selected: []string{"a", "b"}}
	s := newOfferStage(t, prov, registryWithTools(t, "a", "b", "c"), ev, offerStageOpts{
		allowed: []string{"a", "b", "c"},
		cfg:     &ProviderConfig{ToolSelector: sel},
	})

	runOfferTurn(t, s, "first")
	sel.selected = []string{"b", "c"}
	runOfferTurn(t, s, "second")

	assert.Equal(t, []string{"b", "c"}, prov.offeredOn(t, 1))
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonSelector}, Added: []string{"c"}, Removed: []string{"a"}, Round: 1,
	}}, ev.all())
}

// Grants that change between turns, as a client's frontend tools do, are
// reported as client tools.
func TestToolOfferRebuilt_GrantsBetweenTurns(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p")
	var granted []string
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "show_map"), ev, offerStageOpts{
		allowed: []string{"lookup_order"},
		cfg:     &ProviderConfig{ToolGrants: func() []string { return granted }},
	})

	runOfferTurn(t, s, "first")
	granted = []string{"show_map"}
	runOfferTurn(t, s, "second")

	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonClientTools}, Added: []string{"show_map"}, Round: 1,
	}}, ev.all())
}

// A tool DescribeTool describes differently is reported with nothing added
// or removed.
func TestToolOfferRebuilt_Describe(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p")
	index := "skills: a"
	s := newOfferStage(t, prov, registryWithTools(t, "skill__activate"), ev, offerStageOpts{
		allowed: []string{"skill__activate"},
		cfg: &ProviderConfig{DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			c := *d
			c.Description = index
			return &c
		}},
	})

	runOfferTurn(t, s, "first")
	index = "skills: a, b"
	runOfferTurn(t, s, "second")

	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonDescribe}, Round: 1,
	}}, ev.all())
}

// MCP tools appearing between turns, as tools/list_changed brings them, are
// reported as an MCP list change.
func TestToolOfferRebuilt_MCPListChanged(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p")
	reg := registryWithTools(t, "lookup_order")
	s := newOfferStage(t, prov, reg, ev, offerStageOpts{allowed: []string{"lookup_order", "mcp__srv__*"}})

	runOfferTurn(t, s, "first")
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "mcp__srv__ping", Description: "ping", InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "local",
	}))
	runOfferTurn(t, s, "second")

	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonMCPListChanged}, Added: []string{"mcp__srv__ping"}, Round: 1,
	}}, ev.all())
}

// A tool the registry gains between turns, under no allowed list's control
// and not from MCP, is reported as a registry change.
func TestToolOfferRebuilt_RegistryChange(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p")
	reg := registryWithTools(t, "lookup_order")
	s := newOfferStage(t, prov, reg, ev, offerStageOpts{allowed: []string{"lookup_*"}})

	runOfferTurn(t, s, "first")
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "lookup_refund", Description: "x", InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "local",
	}))
	runOfferTurn(t, s, "second")

	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonRegistry}, Added: []string{"lookup_refund"}, Round: 1,
	}}, ev.all())
}

// A tool the registry describes differently between turns, with no
// DescribeTool, is reported as a registry change with nothing added or
// removed.
func TestToolOfferRebuilt_RegistryRedescribed(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p")
	reg := registryWithTools(t, "lookup_order")
	s := newOfferStage(t, prov, reg, ev, offerStageOpts{allowed: []string{"lookup_order"}})

	runOfferTurn(t, s, "first")
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "lookup_order", Description: "Look up an order by id", InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode: "local",
	}))
	runOfferTurn(t, s, "second")

	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonRegistry}, Round: 1,
	}}, ev.all())
}

// A workflow transition to a state with other tools is reported on the
// round that runs as the new state.
func TestToolOfferRebuilt_WorkflowState(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "workflow__transition")})
	resolver := &fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN", AllowedTools: []string{"lookup_order"}},
		{Valid: true, SystemPrompt: "DESTINATION", AllowedTools: []string{"escalate"}},
	}}
	s := newOfferStage(t, prov, registryWithTools(t, "workflow__transition", "lookup_order", "escalate"), ev,
		offerStageOpts{allowed: []string{"lookup_order"}, resolver: resolver})

	runOfferTurn(t, s, "escalate me")

	assert.Equal(t, []string{"escalate", "workflow__transition"}, prov.offeredOn(t, 1))
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonWorkflowState}, Added: []string{"escalate"},
		Removed: []string{"lookup_order"}, Round: 2,
	}}, ev.all())
}

// A workflow state whose prompt runs on another provider is reported as a
// provider switch, even with the same tools.
func TestToolOfferRebuilt_ProviderSwitch(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newOfferScriptProvider("first", []types.MessageToolCall{offerCall("c1", "workflow__transition")})
	other := newOfferScriptProvider("second")
	resolver := &fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN", AllowedTools: []string{"lookup_order"}},
		{Valid: true, SystemPrompt: "ORIGIN", AllowedTools: []string{"lookup_order"},
			Call: &HandoffCall{Provider: other}},
	}}
	s := newOfferStage(t, prov, registryWithTools(t, "workflow__transition", "lookup_order"), ev,
		offerStageOpts{allowed: []string{"lookup_order"}, resolver: resolver})

	runOfferTurn(t, s, "escalate me")

	assert.Equal(t, []string{"lookup_order", "workflow__transition"}, other.offeredOn(t, 0))
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonProviderSwitch}, Round: 2,
	}}, ev.all())
}

const offerRebuiltWarning = "tools array changed mid-conversation"

// A conversation on a caching provider logs the cache cost of a rebuild once,
// at its first rebuild.
func TestToolOfferRebuilt_WarnsOncePerConversation(t *testing.T) {
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })

	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "activate")})
	prov.caching = true
	grants, onRun := grantOnActivate()
	var client []string
	s := newOfferStage(t, prov, registryWithTools(t, "activate", "lookup_order", "refund", "show_map"), nil,
		offerStageOpts{
			allowed: []string{"activate", "lookup_order"},
			cfg:     &ProviderConfig{ToolGrants: func() []string { return append(grants(), client...) }},
			onRun:   onRun,
		})

	runOfferTurn(t, s, "refund my order")
	client = []string{"show_map"}
	runOfferTurn(t, s, "show me")

	assert.Equal(t, 1, strings.Count(buf.String(), offerRebuiltWarning), buf.String())
	assert.Contains(t, buf.String(), "reasons=[skill_grant]")
}

// A provider that does not cache the prompt is not warned about.
func TestToolOfferRebuilt_NoWarningWithoutCaching(t *testing.T) {
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })

	prov := newOfferScriptProvider("p", []types.MessageToolCall{offerCall("c1", "activate")})
	grants, onRun := grantOnActivate()
	s := newOfferStage(t, prov, registryWithTools(t, "activate", "refund"), nil, offerStageOpts{
		allowed: []string{"activate"},
		cfg:     &ProviderConfig{ToolGrants: grants},
		onRun:   onRun,
	})

	runOfferTurn(t, s, "refund my order")

	assert.Equal(t, []string{"activate", "refund"}, prov.offeredOn(t, 1))
	assert.NotContains(t, buf.String(), offerRebuiltWarning)
}
