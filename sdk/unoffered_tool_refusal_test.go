package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover #2713: a call to a registered tool the model was not offered
// this turn is refused with a tool result, and its handler never runs.

// callScriptProvider plays one round per entry of script: the round's tool
// calls, or a text answer once the script is spent. It records the messages
// each round was sent.
type callScriptProvider struct {
	*mock.ToolProvider

	mu       sync.Mutex
	script   [][]types.MessageToolCall
	requests [][]types.Message
}

func newCallScriptProvider(script ...[]types.MessageToolCall) *callScriptProvider {
	return &callScriptProvider{
		ToolProvider: mock.NewToolProvider("script", "script-model", false, nil),
		script:       script,
	}
}

// Pin to the unary tool loop for determinism.
func (p *callScriptProvider) SupportsStreaming() bool { return false }

func (p *callScriptProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	round := len(p.requests)
	p.requests = append(p.requests, append([]types.Message(nil), req.Messages...))
	if round < len(p.script) {
		calls := p.script[round]
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// toolResults returns the tool results the last round was sent, in order.
func (p *callScriptProvider) toolResults() []types.MessageToolResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []types.MessageToolResult
	if len(p.requests) == 0 {
		return nil
	}
	for _, m := range p.requests[len(p.requests)-1] {
		if m.ToolResult != nil {
			out = append(out, *m.ToolResult)
		}
	}
	return out
}

// unofferedRefusal is the result of a refused call to a tool not offered.
func unofferedRefusal(id, name string) types.MessageToolResult {
	text := "Tool " + name + " was not run: it is not available this turn"
	result := types.NewTextToolResult(id, name, text)
	result.Error = text
	result.ErrorType = types.ToolErrorApproval
	return result
}

// handlerRuns counts the runs of each tool handler.
type handlerRuns struct {
	mu   sync.Mutex
	runs map[string]int
}

func (h *handlerRuns) handler(name string) func(map[string]any) (any, error) {
	return func(map[string]any) (any, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.runs == nil {
			h.runs = map[string]int{}
		}
		h.runs[name]++
		return name + " ran", nil
	}
}

func (h *handlerRuns) snapshot() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]int{}
	for k, v := range h.runs {
		out[k] = v
	}
	return out
}

// openUnofferedConv opens the tool-grant pack (lookup_order in the prompt,
// refund in the pack only, refund granted by the refund-processing skill)
// on provider, with a handler for both tools.
func openUnofferedConv(t *testing.T, provider *callScriptProvider, runs *handlerRuns) *Conversation {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, toolGrantPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithSkillsDir(writeToolGrantSkill(t)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", runs.handler("lookup_order"))
	conv.OnTool("refund", runs.handler("refund"))
	return conv
}

// A registered pack tool outside the prompt's tools is refused, with a tool
// result naming it, and its handler never runs; the offered tool in the same
// round runs.
func TestUnofferedTool_TextTurnRefusesTheCall(t *testing.T) {
	provider := newCallScriptProvider([]types.MessageToolCall{
		{ID: "c1", Name: "lookup_order", Args: []byte(`{"id":"ORD-1"}`)},
		{ID: "c2", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)},
	})
	runs := &handlerRuns{}
	conv := openUnofferedConv(t, provider, runs)

	_, err := conv.Send(context.Background(), "refund ORD-1")
	require.NoError(t, err)

	assert.Equal(t, map[string]int{"lookup_order": 1}, runs.snapshot(), "only the offered tool runs")
	assert.Equal(t, []types.MessageToolResult{
		types.NewTextToolResult("c1", "lookup_order", "lookup_order ran"),
		unofferedRefusal("c2", "refund"),
	}, provider.toolResults())
}

// A VAD duplex turn refuses the call as a text turn does.
func TestUnofferedTool_VADTurnRefusesTheCall(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	provider := newCallScriptProvider([]types.MessageToolCall{
		{ID: "c1", Name: "lookup_order", Args: []byte(`{"id":"ORD-1"}`)},
		{ID: "c2", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)},
	})
	runs := &handlerRuns{}
	conv, err := OpenDuplex(writeWorkflowTestPack(t, toolGrantPackJSON), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
		WithVADMode(newScriptedSTT("refund ORD-1"), newConvMockTTSService(), vadTestConfig()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", runs.handler("lookup_order"))
	conv.OnTool("refund", runs.handler("refund"))
	convDrain(t, conv)
	speakOneUtterance(t, conv)

	require.Eventually(t, func() bool { return len(provider.toolResults()) == 2 },
		8*time.Second, 5*time.Millisecond, "the round after the calls is sent both results")
	assert.Equal(t, map[string]int{"lookup_order": 1}, runs.snapshot(), "only the offered tool runs")
	assert.Equal(t, []types.MessageToolResult{
		types.NewTextToolResult("c1", "lookup_order", "lookup_order ran"),
		unofferedRefusal("c2", "refund"),
	}, provider.toolResults())
}

// A capability tool the conversation registers is offered without the prompt
// naming it, so a call to it runs: memory__remember stores the memory.
func TestUnofferedTool_CapabilityToolIsOffered(t *testing.T) {
	provider := newCallScriptProvider([]types.MessageToolCall{
		{ID: "c1", Name: memory.RememberToolName, Args: []byte(`{"content":"likes tea"}`)},
	})
	store := memory.NewInMemoryStore()
	scope := map[string]string{"user_id": "u1"}
	conv, err := Open(writeWorkflowTestPack(t, toolGrantPackJSON), "chat",
		WithProvider(provider), WithSkipSchemaValidation(), WithMemory(store, scope))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	_, err = conv.Send(context.Background(), "remember I like tea")
	require.NoError(t, err)

	results := provider.toolResults()
	require.Len(t, results, 1)
	assert.Empty(t, results[0].Error, "the memory tool ran")
	stored, err := store.List(context.Background(), scope, memory.ListOptions{})
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "likes tea", stored[0].Content)
}

// A skill's tool deactivated mid-turn is refused from the next round on.
func TestUnofferedTool_DeactivatedSkillToolIsRefusedMidTurn(t *testing.T) {
	provider := newCallScriptProvider(
		[]types.MessageToolCall{{ID: "c1", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`)}},
		[]types.MessageToolCall{{ID: "c2", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)}},
		[]types.MessageToolCall{{ID: "c3", Name: "skill__deactivate", Args: []byte(`{"name":"refund-processing"}`)}},
		[]types.MessageToolCall{{ID: "c4", Name: "refund", Args: []byte(`{"id":"ORD-2"}`)}},
	)
	runs := &handlerRuns{}
	conv := openUnofferedConv(t, provider, runs)

	_, err := conv.Send(context.Background(), "refund ORD-1 and ORD-2")
	require.NoError(t, err)

	assert.Equal(t, map[string]int{"refund": 1}, runs.snapshot(), "refund runs only while the skill is active")
	results := provider.toolResults()
	require.Len(t, results, 4)
	assert.Equal(t, types.NewTextToolResult("c2", "refund", "refund ran"), results[1])
	assert.Empty(t, results[2].Error, "skill__deactivate ran")
	assert.Equal(t, unofferedRefusal("c4", "refund"), results[3])
}

// A skill-gated tool is refused before skill__activate grants it, and runs
// once the skill is active (#1957).
func TestUnofferedTool_SkillGatedToolRunsOnlyAfterActivation(t *testing.T) {
	provider := newCallScriptProvider(
		[]types.MessageToolCall{{ID: "c1", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)}},
		[]types.MessageToolCall{{ID: "c2", Name: "skill__activate", Args: []byte(`{"name":"refund-processing"}`)}},
		[]types.MessageToolCall{{ID: "c3", Name: "refund", Args: []byte(`{"id":"ORD-1"}`)}},
	)
	runs := &handlerRuns{}
	conv := openUnofferedConv(t, provider, runs)

	_, err := conv.Send(context.Background(), "refund ORD-1")
	require.NoError(t, err)

	assert.Equal(t, map[string]int{"refund": 1}, runs.snapshot(), "refund runs once: after activation")
	results := provider.toolResults()
	require.Len(t, results, 3)
	assert.Equal(t, unofferedRefusal("c1", "refund"), results[0], "before activation the call is refused")
	assert.Equal(t, "skill__activate", results[1].Name)
	assert.Empty(t, results[1].Error)
	assert.Equal(t, types.NewTextToolResult("c3", "refund", "refund ran"), results[2])
}
