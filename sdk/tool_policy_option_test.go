package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These cover WithToolPolicy (#2417): the caller tool policy, which carries
// the runtime-only fields the pack's tool_policy cannot, reaches the provider
// stage through Open. Each test proves one field takes effect.

func openWithToolPolicy(
	t *testing.T, packPolicy string, policy *pipeline.ToolPolicy, provider providers.Provider,
) (*Conversation, *atomic.Int32) {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, toolPolicyTestPack(packPolicy)), "chat",
		WithProvider(provider), WithSkipSchemaValidation(), WithToolPolicy(policy))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	executed := &atomic.Int32{}
	conv.OnTool("lookup_order", func(map[string]any) (any, error) {
		executed.Add(1)
		return "ok", nil
	})
	return conv, executed
}

func TestWithToolPolicy_MaxCallsPerMinute(t *testing.T) {
	conv, executed := openWithToolPolicy(t, `{"max_rounds": 4}`,
		&pipeline.ToolPolicy{MaxCallsPerMinute: 2}, newAlwaysCallProvider())

	_, _ = conv.Send(context.Background(), "where is my order?")
	assert.EqualValues(t, 2, executed.Load(), "calls past the per-minute limit are not run")
}

// End to end with the conversation's call-rate window (#2414): an approved
// held call takes a slot, so with a limit of 2 only one more call runs.
func TestWithToolPolicy_MaxCallsPerMinuteCountsApprovedCalls(t *testing.T) {
	conv, err := Open(writeWorkflowTestPack(t, toolPolicyTestPack(`{"max_rounds": 3}`)), "chat",
		WithProvider(newAlwaysCallProvider()), WithSkipSchemaValidation(),
		WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: 2}))
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()

	var hold atomic.Bool
	hold.Store(true)
	var executed atomic.Int32
	conv.OnToolAsync("lookup_order",
		func(map[string]any) sdktools.PendingResult {
			if hold.Load() {
				return sdktools.PendingResult{Reason: "requires_approval"}
			}
			return sdktools.PendingResult{}
		},
		func(map[string]any) (any, error) {
			executed.Add(1)
			return "ok", nil
		})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "look it up")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	_, err = conv.ResolveTool(ctx, resp.PendingTools()[0].ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, executed.Load())

	hold.Store(false)
	_, _ = conv.Send(ctx, "and again")
	assert.EqualValues(t, 2, executed.Load(), "the approved call used one of the two slots")
}

// costlyProvider is alwaysCallProvider with every round costing one dollar.
type costlyProvider struct{ *alwaysCallProvider }

func (p *costlyProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	resp, calls, err := p.alwaysCallProvider.PredictWithTools(ctx, req, tools, choice)
	resp.CostInfo = &types.CostInfo{TotalCost: 1}
	return resp, calls, err
}

func TestWithToolPolicy_MaxCostUSD(t *testing.T) {
	provider := &costlyProvider{newAlwaysCallProvider()}
	conv, _ := openWithToolPolicy(t, `{"max_rounds": 10}`, &pipeline.ToolPolicy{MaxCostUSD: 2.5}, provider)

	_, err := conv.Send(context.Background(), "where is my order?")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cost budget exceeded")
	assert.Equal(t, 3, provider.roundsSeen(), "the round that crosses the budget is the last")
}

// sameCallProvider calls lookup_order with the same arguments every round.
type sameCallProvider struct{ *alwaysCallProvider }

func (p *sameCallProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if tools == nil {
		return providers.PredictionResponse{Content: "answer"}, nil, nil
	}
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	calls := []types.MessageToolCall{{
		ID: fmt.Sprintf("call-%d", round), Name: "lookup_order", Args: []byte(`{"id":"ORD-1"}`),
	}}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func TestWithToolPolicy_MaxIdenticalToolCalls(t *testing.T) {
	provider := &sameCallProvider{newAlwaysCallProvider()}
	conv, executed := openWithToolPolicy(t, `{"max_rounds": 10}`,
		&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2}, provider)

	_, err := conv.Send(context.Background(), "where is my order?")
	require.Error(t, err, "the loop is aborted once the nudge is ignored")
	assert.EqualValues(t, 1, executed.Load(), "under the default of 3 it runs twice")
}

// parallelProvider asks for three lookups in one round, then answers.
type parallelProvider struct{ *alwaysCallProvider }

func (p *parallelProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	if tools == nil || round > 1 {
		return providers.PredictionResponse{Content: "answer"}, nil, nil
	}
	calls := make([]types.MessageToolCall, 3)
	for i := range calls {
		calls[i] = types.MessageToolCall{
			ID: fmt.Sprintf("call-%d", i), Name: "lookup_order", Args: []byte(fmt.Sprintf(`{"id":"ORD-%d"}`, i)),
		}
	}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func TestWithToolPolicy_MaxParallelToolCalls(t *testing.T) {
	conv, err := Open(writeWorkflowTestPack(t, toolPolicyTestPack(`{"max_rounds": 3}`)), "chat",
		WithProvider(&parallelProvider{newAlwaysCallProvider()}), WithSkipSchemaValidation(),
		WithToolPolicy(&pipeline.ToolPolicy{MaxParallelToolCalls: 1}))
	require.NoError(t, err)
	defer func() { _ = conv.Close() }()
	var mu sync.Mutex
	inFlight, peak := 0, 0
	conv.OnTool("lookup_order", func(map[string]any) (any, error) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		// Stay in flight until a second call joins, or 50ms pass. Unlimited,
		// the round's calls run together and the wait ends at once; limited
		// to one, no second call can start while this one runs.
		for deadline := time.Now().Add(50 * time.Millisecond); time.Now().Before(deadline); {
			mu.Lock()
			joined := inFlight >= 2
			mu.Unlock()
			if joined {
				break
			}
			time.Sleep(time.Millisecond)
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		return "ok", nil
	})

	_, err = conv.Send(context.Background(), "look up three orders")
	require.NoError(t, err)
	assert.Equal(t, 1, peak, "the round's calls run one at a time")
}

func TestWithToolPolicy_StopOnTool(t *testing.T) {
	provider := newAlwaysCallProvider()
	conv, executed := openWithToolPolicy(t, `{"max_rounds": 5}`,
		&pipeline.ToolPolicy{StopOnTool: "lookup_order"}, provider)

	_, err := conv.Send(context.Background(), "where is my order?")
	require.NoError(t, err)
	assert.Equal(t, 1, provider.roundsSeen(), "the loop stops after the round that called the stop tool")
	assert.EqualValues(t, 1, executed.Load())
}

// Where the option and the pack's tool_policy set the same field, the turn
// runs under pipeline.MergeToolPolicy: limits only narrow, either side can
// tighten one, and the blocklists combine.
func TestWithToolPolicy_PrecedenceWithPackToolPolicy(t *testing.T) {
	t.Run("the pack's lower max_rounds wins", func(t *testing.T) {
		provider := newAlwaysCallProvider()
		conv, _ := openWithToolPolicy(t, `{"max_rounds": 3}`, &pipeline.ToolPolicy{MaxRounds: 10}, provider)
		_, err := conv.Send(context.Background(), "go")
		require.Error(t, err)
		assert.Equal(t, 3, provider.roundsSeen())
	})
	t.Run("the option's lower max_rounds wins", func(t *testing.T) {
		provider := newAlwaysCallProvider()
		conv, _ := openWithToolPolicy(t, `{"max_rounds": 5}`, &pipeline.ToolPolicy{MaxRounds: 2}, provider)
		_, err := conv.Send(context.Background(), "go")
		require.Error(t, err)
		assert.Equal(t, 2, provider.roundsSeen())
	})
	t.Run("the option's blocklist adds to the pack's", func(t *testing.T) {
		conv, executed := openWithToolPolicy(t, `{"max_rounds": 2, "blocklist": ["other"]}`,
			&pipeline.ToolPolicy{Blocklist: []string{"lookup_order"}}, newAlwaysCallProvider())
		_, _ = conv.Send(context.Background(), "go")
		assert.Zero(t, executed.Load())
	})
}

// The option copies the policy: a host that changes its value afterwards
// does not change conversations already opened with it.
func TestWithToolPolicy_CopiesThePolicy(t *testing.T) {
	policy := &pipeline.ToolPolicy{MaxCallsPerMinute: 2, Blocklist: []string{"x"}}
	cfg := &config{}
	require.NoError(t, WithToolPolicy(policy)(cfg))
	policy.MaxCallsPerMinute = 99
	policy.Blocklist[0] = "changed"
	assert.Equal(t, 2, cfg.toolPolicy.MaxCallsPerMinute)
	assert.Equal(t, []string{"x"}, cfg.toolPolicy.Blocklist)

	require.NoError(t, WithToolPolicy(nil)(cfg))
	assert.Nil(t, cfg.toolPolicy, "nil clears it")
}

// Every workflow state's conversation runs under the option: with the
// transition tool as the stop tool, the turn ends after the transition round
// instead of handing off to the destination state in the same Send.
func TestWithToolPolicy_AppliesToWorkflowStates(t *testing.T) {
	provider := &handoffSDKProvider{ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil)}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, workflowPackJSON), WithProvider(provider),
		WithSkipSchemaValidation(), WithToolPolicy(&pipeline.ToolPolicy{StopOnTool: "workflow__transition"}))
	require.NoError(t, err)
	defer func() { require.NoError(t, wc.Close()) }()

	_, err = wc.Send(context.Background(), "I need help with my account")
	require.NoError(t, err)
	assert.Equal(t, "processing", wc.CurrentState(), "the transition still commits")
	assert.Len(t, provider.seenPrompts, 1, "the stop tool ended the loop before the destination's round")
}

// compositionAgentPack runs one agent step over the chat prompt, which offers
// lookup_order, with no termination of its own.
const compositionAgentPack = `{
	"id": "composition-policy", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You help.", "tools": ["lookup_order"]}},
	"tools": {"lookup_order": {"name": "lookup_order", "description": "Look up an order",
		"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}},
	"workflow": {"version": 1, "entry": "compose",
		"states": {"compose": {"orchestration": "composition", "composition": "flow", "terminal": true}}},
	"compositions": {"flow": {"version": 1,
		"steps": [{"id": "ag", "kind": "agent", "prompt_task": "chat", "input": "${input}", "tools": ["lookup_order"]}],
		"output": "ag"}}
}`

// A composition's agent steps run under the option too.
func TestWithToolPolicy_AppliesToCompositionSteps(t *testing.T) {
	provider := newAlwaysCallProvider()
	wc, err := OpenWorkflow(createTestPackFile(t, compositionAgentPack), WithProvider(provider),
		WithSkipSchemaValidation(), WithToolPolicy(&pipeline.ToolPolicy{StopOnTool: "lookup_order"}))
	require.NoError(t, err)
	defer func() { _ = wc.Close() }()

	_, _ = wc.Send(context.Background(), "find my order")
	assert.Equal(t, 1, provider.roundsSeen(), "the agent step stopped after the round that called the stop tool")
}

// countingLocalExecutor runs every "local" tool and counts the runs.
type countingLocalExecutor struct{ runs atomic.Int32 }

func (e *countingLocalExecutor) Name() string { return "local" }

func (e *countingLocalExecutor) Execute(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (json.RawMessage, error) {
	e.runs.Add(1)
	return json.RawMessage(`"ok"`), nil
}

// A composition's steps count in the conversation's call-rate window: each
// step execution builds its own provider stage, and without the shared
// window every one of them would start from an empty minute.
func TestWithToolPolicy_MaxCallsPerMinuteSpansCompositionSteps(t *testing.T) {
	provider := newAlwaysCallProvider()
	wc, err := OpenWorkflow(createTestPackFile(t, compositionAgentPack), WithProvider(provider),
		WithSkipSchemaValidation(),
		WithToolPolicy(&pipeline.ToolPolicy{MaxCallsPerMinute: 1, StopOnTool: "lookup_order"}))
	require.NoError(t, err)
	defer func() { _ = wc.Close() }()
	exec := &countingLocalExecutor{}
	wc.ActiveConversation().ToolRegistry().RegisterExecutor(exec)

	ctx := context.Background()
	_, _ = wc.Send(ctx, "find my order")
	_, _ = wc.Send(ctx, "find it again")
	assert.Equal(t, 2, provider.roundsSeen(), "one round per Send: each calls the stop tool")
	assert.EqualValues(t, 1, exec.runs.Load(), "the second step's call is over the conversation's limit")
}
