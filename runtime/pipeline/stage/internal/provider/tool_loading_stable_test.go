package provider

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover tool_loading mode stable: every call of a conversation is sent
// the same tools array, and what the model may call in a round is given
// beside it (PredictionRequest.ActiveTools) and enforced by refusing a call
// outside it.

// stableCall is what one provider call was sent.
type stableCall struct {
	tools   []string // the tools array's names, sorted
	active  []string // PredictionRequest.ActiveTools
	choice  string
	results map[string]string // tool results the call was given, by call ID
}

// stableScriptProvider plays a script of tool-call rounds, one entry per
// call, then answers with text, recording what each call was sent.
type stableScriptProvider struct {
	*mock.ToolProvider

	mu      sync.Mutex
	script  [][]types.MessageToolCall
	calls   []stableCall
	caching bool
	stream  bool
}

func newStableScriptProvider(script ...[]types.MessageToolCall) *stableScriptProvider {
	return &stableScriptProvider{ToolProvider: mock.NewToolProvider("p", "stable-model", false, nil), script: script}
}

func (p *stableScriptProvider) SupportsStreaming() bool     { return p.stream }
func (p *stableScriptProvider) SupportsPromptCaching() bool { return p.caching }

func (p *stableScriptProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, offered providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	call := stableCall{active: req.ActiveTools, choice: choice, results: map[string]string{}}
	if descs, ok := offered.([]*providers.ToolDescriptor); ok {
		for _, d := range descs {
			call.tools = append(call.tools, d.Name)
		}
	}
	sort.Strings(call.tools)
	for i := range req.Messages {
		if tr := req.Messages[i].ToolResult; tr != nil {
			call.results[tr.ID] = tr.GetTextContent()
		}
	}
	p.calls = append(p.calls, call)
	if len(p.script) == 0 {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	calls := p.script[0]
	p.script = p.script[1:]
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func (p *stableScriptProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	resp, _, err := p.PredictWithTools(ctx, req, nil, "")
	return resp, err
}

func (p *stableScriptProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, offered providers.ProviderTools, choice string,
) (<-chan providers.StreamChunk, error) {
	resp, calls, err := p.PredictWithTools(ctx, req, offered, choice)
	if err != nil {
		return nil, err
	}
	ch := make(chan providers.StreamChunk, 1)
	stop := "stop"
	if len(calls) > 0 {
		ch <- providers.StreamChunk{ToolCalls: calls, FinishReason: &stop}
	} else {
		ch <- providers.StreamChunk{Content: resp.Content, Delta: resp.Content, FinishReason: &stop}
	}
	close(ch)
	return ch, nil
}

func (p *stableScriptProvider) queue(rounds ...[]types.MessageToolCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.script = append(p.script, rounds...)
}

func (p *stableScriptProvider) sent(t *testing.T) []stableCall {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]stableCall(nil), p.calls...)
}

func stableLoading(catalog ...string) ToolLoading {
	l := ToolLoading{Mode: ToolLoadingStable}
	if catalog != nil {
		l.Catalog = func() []string { return catalog }
	}
	return l
}

// A ToolSelector choosing different tools each turn narrows what the model
// may call, not the tools array: every call is sent all three tools, and
// nothing is reported as rebuilt.
func TestStableTools_SelectorNarrowsActiveNotTheArray(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newStableScriptProvider()
	sel := &stubToolSelector{selected: []string{"a", "b"}}
	s := newOfferStage(t, prov, registryWithTools(t, "a", "b", "c"), ev, offerStageOpts{
		allowed: []string{"a", "b", "c"},
		cfg:     &ProviderConfig{ToolSelector: sel, ToolLoading: stableLoading()},
	})

	runOfferTurn(t, s, "first")
	sel.selected = []string{"b", "c"}
	runOfferTurn(t, s, "second")
	sel.selected = []string{"a", "b", "c"}
	runOfferTurn(t, s, "third")

	calls := prov.sent(t)
	require.Len(t, calls, 3)
	for i, c := range calls {
		assert.Equal(t, []string{"a", "b", "c"}, c.tools, "call %d is sent every tool", i)
	}
	assert.Equal(t, []string{"a", "b"}, calls[0].active)
	assert.Equal(t, []string{"b", "c"}, calls[1].active)
	assert.Nil(t, calls[2].active, "a turn that may call every tool sends no restriction")
	assert.Empty(t, ev.all())
}

// The same selector in rebuild mode, the default, narrows the array itself.
func TestStableTools_RebuildModeNarrowsTheArray(t *testing.T) {
	prov := newStableScriptProvider()
	sel := &stubToolSelector{selected: []string{"a", "b"}}
	s := newOfferStage(t, prov, registryWithTools(t, "a", "b", "c"), nil, offerStageOpts{
		allowed: []string{"a", "b", "c"},
		cfg:     &ProviderConfig{ToolSelector: sel},
	})

	runOfferTurn(t, s, "first")

	calls := prov.sent(t)
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"a", "b"}, calls[0].tools)
	assert.Nil(t, calls[0].active)
}

// A skill grant within one turn: the granted tool is in the array from the
// first call, because the catalog names it; it becomes callable on the round
// after the activation, and the array never changes.
func TestStableTools_GrantWidensActiveNotTheArray(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newStableScriptProvider(
		[]types.MessageToolCall{offerCall("c1", "activate")},
		[]types.MessageToolCall{offerCall("c2", "refund")},
	)
	prov.caching = true
	grants, onRun := grantOnActivate()
	var ran []string
	s := newOfferStage(t, prov, registryWithTools(t, "activate", "lookup_order", "refund"), ev, offerStageOpts{
		allowed: []string{"activate", "lookup_order"},
		cfg:     &ProviderConfig{ToolGrants: grants, ToolLoading: stableLoading("refund")},
		onRun: func(name string) {
			ran = append(ran, name)
			onRun(name)
		},
	})

	runOfferTurn(t, s, "refund my order")

	calls := prov.sent(t)
	require.Len(t, calls, 3)
	for i, c := range calls {
		assert.Equal(t, []string{"activate", "lookup_order", "refund"}, c.tools, "call %d", i)
	}
	assert.Equal(t, []string{"activate", "lookup_order"}, calls[0].active)
	assert.Nil(t, calls[1].active, "once granted, every tool sent may be called")
	assert.Equal(t, []string{"activate", "refund"}, ran)
	assert.Empty(t, ev.all())
}

// A call to a tool in the array that the round does not let the model call is
// refused with PromptKit's refusal, and the tool does not run.
func TestStableTools_CallOutsideActiveIsRefused(t *testing.T) {
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "refund")})
	var ran []string
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "refund"), nil, offerStageOpts{
		allowed: []string{"lookup_order"},
		cfg:     &ProviderConfig{ToolLoading: stableLoading("refund")},
		onRun:   func(name string) { ran = append(ran, name) },
	})

	runOfferTurn(t, s, "refund it")

	calls := prov.sent(t)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"lookup_order", "refund"}, calls[0].tools)
	assert.Equal(t, []string{"lookup_order"}, calls[0].active)
	assert.Equal(t, map[string]string{"c1": NotAvailableRefusal("refund")}, calls[1].results)
	assert.Empty(t, ran)
}

// A selector's narrowing is enforced too: a call to a tool the selector left
// out of the round is refused, although the prompt allows it.
func TestStableTools_CallToToolTheSelectorLeftOutIsRefused(t *testing.T) {
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "c")})
	var ran []string
	s := newOfferStage(t, prov, registryWithTools(t, "a", "b", "c"), nil, offerStageOpts{
		allowed: []string{"a", "b", "c"},
		cfg: &ProviderConfig{
			ToolSelector: &stubToolSelector{selected: []string{"a"}}, ToolLoading: stableLoading(),
		},
		onRun: func(name string) { ran = append(ran, name) },
	})

	runOfferTurn(t, s, "do it")

	calls := prov.sent(t)
	require.Len(t, calls, 2)
	assert.Equal(t, map[string]string{"c1": NotAvailableRefusal("c")}, calls[1].results)
	assert.Empty(t, ran)
}

// With max_tool_calls_per_turn spent, the last round keeps the tools array
// and is sent tool choice none, instead of being sent no tools.
func TestStableTools_ExhaustedSendsChoiceNone(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "lookup_order")})
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "refund"), ev, offerStageOpts{
		allowed: []string{"lookup_order", "refund"},
		cfg:     &ProviderConfig{ToolLoading: stableLoading()},
		policy:  &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1},
	})

	runOfferTurn(t, s, "where is my order")
	runOfferTurn(t, s, "and now?")

	calls := prov.sent(t)
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"lookup_order", "refund"}, calls[1].tools)
	assert.Equal(t, ToolChoiceNone, calls[1].choice)
	assert.Equal(t, "auto", calls[2].choice, "the next turn chooses as usual")
	assert.Empty(t, ev.all())
}

// A tool excluded after repeated rejection stays in the array; the model may
// no longer call it.
func TestStableTools_ExclusionShrinksActive(t *testing.T) {
	prov := newStableScriptProvider(
		[]types.MessageToolCall{offerCall("c1", "flaky")},
		[]types.MessageToolCall{offerCall("c2", "flaky")},
	)
	s := newOfferStage(t, prov, registryWithTools(t, "flaky", "lookup_order"), nil, offerStageOpts{
		allowed: []string{"flaky", "lookup_order"},
		cfg:     &ProviderConfig{ToolLoading: stableLoading()},
		hooks:   hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{blockedTool: "flaky", reason: "no"})),
	})

	runOfferTurn(t, s, "go")

	calls := prov.sent(t)
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"flaky", "lookup_order"}, calls[2].tools)
	assert.Equal(t, []string{"lookup_order"}, calls[2].active)
}

// A grant the catalog does not hold, such as a client's frontend tool, is a
// real change to the array: it is rebuilt and reported.
func TestStableTools_GrantOutsideCatalogRebuilds(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "activate")})
	grants, onRun := grantOnActivate()
	s := newOfferStage(t, prov, registryWithTools(t, "activate", "lookup_order", "refund"), ev, offerStageOpts{
		allowed: []string{"activate", "lookup_order"},
		cfg:     &ProviderConfig{ToolGrants: grants, ToolLoading: stableLoading()},
		onRun:   onRun,
	})

	runOfferTurn(t, s, "refund my order")

	calls := prov.sent(t)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"activate", "lookup_order"}, calls[0].tools)
	assert.Equal(t, []string{"activate", "lookup_order", "refund"}, calls[1].tools)
	assert.Nil(t, calls[1].active)
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonSkillGrant}, Added: []string{"refund"}, Round: 2,
	}}, ev.all())
}

// A forced tool the round does not let the model call falls back to auto; a
// forced tool it does is kept, alongside the round's restriction.
func TestStableTools_ForcedChoiceFollowsActive(t *testing.T) {
	prov := newStableScriptProvider()
	s := newOfferStage(t, prov, registryWithTools(t, "a", "b"), nil, offerStageOpts{
		allowed: []string{"a", "b"},
		cfg: &ProviderConfig{
			ToolSelector: &stubToolSelector{selected: []string{"a"}}, ToolLoading: stableLoading(),
		},
		policy: &pipeline.ToolPolicy{ToolChoice: "b"},
	})
	runOfferTurn(t, s, "go")

	prov2 := newStableScriptProvider()
	s2 := newOfferStage(t, prov2, registryWithTools(t, "a", "b"), nil, offerStageOpts{
		allowed: []string{"a", "b"},
		cfg: &ProviderConfig{
			ToolSelector: &stubToolSelector{selected: []string{"a"}}, ToolLoading: stableLoading(),
		},
		policy: &pipeline.ToolPolicy{ToolChoice: "required"},
	})
	runOfferTurn(t, s2, "go")

	assert.Equal(t, "auto", prov.sent(t)[0].choice)
	assert.Equal(t, "required", prov2.sent(t)[0].choice)
	assert.Equal(t, []string{"a"}, prov2.sent(t)[0].active)
}

// The streaming tool loop sends the same array and restriction, and refuses
// a call outside it the same way.
func TestStableTools_Streaming(t *testing.T) {
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "refund")})
	prov.stream = true
	var ran []string
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "refund"), nil, offerStageOpts{
		allowed: []string{"lookup_order"},
		cfg:     &ProviderConfig{ToolLoading: stableLoading("refund")},
		onRun:   func(name string) { ran = append(ran, name) },
	})

	runOfferTurn(t, s, "refund it")

	calls := prov.sent(t)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"lookup_order", "refund"}, calls[1].tools)
	assert.Equal(t, []string{"lookup_order"}, calls[1].active)
	assert.Equal(t, map[string]string{"c1": NotAvailableRefusal("refund")}, calls[1].results)
	assert.Empty(t, ran)
}

// A workflow handoff mid-turn to a state whose tools the catalog holds keeps
// the array: what the model may call follows the state, and nothing is
// reported. One to a state naming a tool outside the catalog rebuilds it, as
// a workflow state change.
func TestStableTools_WorkflowHandoff(t *testing.T) {
	run := func(catalog ...string) (*stableScriptProvider, []events.ToolOfferRebuiltData, *ProviderStage) {
		ev := newOfferEvents(t)
		prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "lookup_order")})
		resolver := &fakeResolver{sequence: []Handoff{
			{Valid: true, SystemPrompt: "ORDERS", AllowedTools: []string{"lookup_order"}},
			{Valid: true, SystemPrompt: "REFUNDS", AllowedTools: []string{"refund"}},
		}}
		s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "refund"), ev, offerStageOpts{
			allowed:  []string{"lookup_order"},
			resolver: resolver,
			cfg:      &ProviderConfig{ToolLoading: stableLoading(catalog...)},
		})
		runOfferTurn(t, s, "refund my order")
		return prov, ev.all(), s
	}

	prov, got, s := run("lookup_order", "refund")
	calls := prov.sent(t)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"lookup_order", "refund"}, calls[0].tools)
	assert.Equal(t, []string{"lookup_order", "refund"}, calls[1].tools)
	assert.Equal(t, []string{"lookup_order"}, calls[0].active)
	assert.Equal(t, []string{"refund"}, calls[1].active)
	assert.Empty(t, got)
	assert.Equal(t, "REFUNDS", s.turnState.SystemPrompt, "the turn runs as the state it handed off to")
	assert.Equal(t, []string{"refund"}, s.turnState.AllowedTools)

	prov, got, _ = run()
	calls = prov.sent(t)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"lookup_order"}, calls[0].tools)
	assert.Equal(t, []string{"refund"}, calls[1].tools)
	assert.Nil(t, calls[1].active)
	assert.Equal(t, []events.ToolOfferRebuiltData{{
		Reasons: []string{events.ToolOfferReasonWorkflowState}, Added: []string{"refund"},
		Removed: []string{"lookup_order"}, Round: 2,
	}}, got)
}

// A tool round that makes DescribeTool describe a tool differently rebuilds
// the array, and is reported as a describe change.
func TestStableTools_DescribeChangeRebuilds(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "lookup_order")})
	desc := "first"
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "skill__activate"), ev, offerStageOpts{
		allowed: []string{"lookup_order"},
		onRun:   func(string) { desc = "second" },
		cfg: &ProviderConfig{ToolLoading: stableLoading(), DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			c := *d
			if c.Name == "skill__activate" {
				c.Description = desc
			}
			return &c
		}},
	})

	runOfferTurn(t, s, "go")

	assert.Equal(t, []events.ToolOfferRebuiltData{{Reasons: []string{events.ToolOfferReasonDescribe}, Round: 2}},
		ev.all())
}

// A call sent no tools array is sent no tool choice, even when the history's
// tool calls put it on the tool path.
func TestStableTools_NoArrayNoChoice(t *testing.T) {
	prov := newStableScriptProvider()
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order"), nil, offerStageOpts{
		allowed: []string{},
		cfg:     &ProviderConfig{ToolLoading: stableLoading()},
	})
	history := []types.Message{
		{Role: "user", Content: "where is ORD-1"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{offerCall("c0", "lookup_order")}},
		types.NewToolResultMessage(types.NewTextToolResult("c0", "lookup_order", "shipped")),
		{Role: "user", Content: "thanks"},
	}
	input := make(chan core.StreamElement, len(history))
	for i := range history {
		input <- core.NewMessageElement(&history[i])
	}
	close(input)
	output := make(chan core.StreamElement, 64)
	require.NoError(t, s.Process(context.Background(), input, output))
	for range output { //nolint:revive // draining
	}

	calls := prov.sent(t)
	require.Len(t, calls, 1)
	assert.Nil(t, calls[0].tools)
	assert.Equal(t, "", calls[0].choice)
	assert.Nil(t, calls[0].active)
}

// A catalog entry ending in "*" is a pattern, expanded against the registry
// as an allowed_tools entry is: a handoff to a state that allows an MCP
// server's tools by pattern keeps the array.
func TestStableTools_WildcardCatalogEntryIsExpanded(t *testing.T) {
	ev := newOfferEvents(t)
	prov := newStableScriptProvider([]types.MessageToolCall{offerCall("c1", "lookup_order")})
	resolver := &fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORDERS", AllowedTools: []string{"lookup_order"}},
		{Valid: true, SystemPrompt: "CRM", AllowedTools: []string{"mcp__crm__*"}},
	}}
	s := newOfferStage(t, prov, registryWithTools(t, "lookup_order", "mcp__crm__find", "mcp__crm__update"), ev,
		offerStageOpts{
			allowed:  []string{"lookup_order"},
			resolver: resolver,
			cfg:      &ProviderConfig{ToolLoading: stableLoading("lookup_order", "mcp__crm__*")},
		})

	runOfferTurn(t, s, "update the customer")

	calls := prov.sent(t)
	require.Len(t, calls, 2)
	all := []string{"lookup_order", "mcp__crm__find", "mcp__crm__update"}
	assert.Equal(t, all, calls[0].tools)
	assert.Equal(t, all, calls[1].tools)
	assert.Equal(t, []string{"mcp__crm__find", "mcp__crm__update"}, calls[1].active)
	assert.Empty(t, ev.all())
}
