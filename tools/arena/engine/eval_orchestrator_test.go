package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEvalHandler is a minimal handler used to register a working eval type
// in tests without pulling in real handler implementations.
type testEvalHandler struct{}

func (h *testEvalHandler) Type() string { return "test_handler" }

func (h *testEvalHandler) Eval(_ context.Context, _ *evals.EvalContext, _ map[string]any) (*evals.EvalResult, error) {
	return &evals.EvalResult{
		EvalID: "test",
		Type:   "test_handler",
		Value:  true,
	}, nil
}

// newTestRegistry returns a registry with the testEvalHandler and wrapper handlers registered.
func newTestRegistry() *evals.EvalTypeRegistry {
	r := evals.NewEvalTypeRegistry()
	r.Register(&testEvalHandler{})
	return r
}

// sampleDefs returns a slice of EvalDef values useful for testing.
func sampleDefs() []evals.EvalDef {
	return []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
		{ID: "eval-2", Type: "other_handler", Trigger: evals.TriggerOnSessionComplete},
	}
}

func TestNewEvalOrchestrator_SkipEvalsTrue(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), sampleDefs(), true, nil, "chat")
	// When skipEvals is true, defs are still stored but runner is nil.
	// HasEvals reflects whether defs exist, not whether the runner is set.
	// RunTurnEvals/RunSessionEvals will still produce nil results via nil runner guard.
	assert.True(t, hook.HasEvals(), "HasEvals reflects stored defs even when skipEvals is true")

	// Verify that nil runner means no actual results are produced.
	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("hi"),
	}
	results := hook.RunTurnEvals(context.Background(), messages, 1, "session-1")
	assert.Empty(t, results, "nil runner should produce no results")
}

func TestNewEvalOrchestrator_EmptyDefs(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	assert.False(t, hook.HasEvals(), "HasEvals should be false when defs are empty")

	hook2 := NewEvalOrchestrator(newTestRegistry(), []evals.EvalDef{}, false, nil, "chat")
	assert.False(t, hook2.HasEvals(), "HasEvals should be false when defs slice is empty")
}

func TestNewEvalOrchestrator_ValidDefs(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")
	assert.True(t, hook.HasEvals(), "HasEvals should be true when valid defs are provided")
}

func TestFilterEvalDefs_EmptyFilter(t *testing.T) {
	defs := sampleDefs()
	result := filterEvalDefs(defs, nil)
	assert.Equal(t, defs, result, "empty filter should return all defs")

	result2 := filterEvalDefs(defs, []string{})
	assert.Equal(t, defs, result2, "empty slice filter should return all defs")
}

func TestFilterEvalDefs_WithFilter(t *testing.T) {
	defs := sampleDefs()
	result := filterEvalDefs(defs, []string{"test_handler"})
	require.Len(t, result, 1)
	assert.Equal(t, "eval-1", result[0].ID)
	assert.Equal(t, "test_handler", result[0].Type)
}

func TestFilterEvalDefs_NoMatch(t *testing.T) {
	defs := sampleDefs()
	result := filterEvalDefs(defs, []string{"nonexistent"})
	assert.Empty(t, result, "filter with no matches should return empty slice")
}

func TestRunTurnEvals_NoEvals(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	results := hook.RunTurnEvals(context.Background(), nil, 0, "session-1")
	assert.Nil(t, results, "RunTurnEvals should return nil when there are no evals")
}

func TestRunSessionEvals_NoEvals(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	results := hook.RunSessionEvals(context.Background(), nil, "session-1")
	assert.Nil(t, results, "RunSessionEvals should return nil when there are no evals")
}

func TestBuildEvalContext_ExtractsLastAssistantMessage(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("first reply"),
		types.NewUserMessage("follow up"),
		types.NewAssistantMessage("second reply"),
	}

	evalCtx := hook.buildEvalContext(messages, 3, "session-1")

	assert.Equal(t, "second reply", evalCtx.CurrentOutput, "should extract content from last assistant message")
	assert.Equal(t, 3, evalCtx.TurnIndex)
	assert.Equal(t, "session-1", evalCtx.SessionID)
	assert.Equal(t, "chat", evalCtx.PromptID)
	assert.Len(t, evalCtx.Messages, 4)
}

func TestBuildEvalContext_BridgesLatencyMs(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	messages := []types.Message{
		types.NewUserMessage("hello"),
		{Role: "assistant", Content: "reply", LatencyMs: 432},
	}

	evalCtx := hook.buildEvalContext(messages, 1, "session-1")

	require.NotNil(t, evalCtx.Metadata)
	assert.Equal(t, float64(432), evalCtx.Metadata["latency_ms"],
		"buildEvalContext should bridge latest assistant LatencyMs into metadata so latency_budget can read it")
}

type fakeCompositionMetaProvider struct{ md map[string]any }

func (f *fakeCompositionMetaProvider) CompositionMetadata() map[string]any { return f.md }

func TestBuildEvalContext_BridgesCompositionMetadata(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	hook.SetCompositionMetadataProvider(&fakeCompositionMetaProvider{md: map[string]any{
		"composition_step_outputs":    map[string]any{"classify": "x"},
		"composition_branch_taken":    map[string]any{"route": "paper"},
		"composition_parallel_status": map[string]any{"meta": "complete"},
	}})
	messages := []types.Message{types.NewAssistantMessage("done")}

	evalCtx := hook.buildEvalContext(messages, 1, "s")

	require.NotNil(t, evalCtx.Metadata)
	assert.NotNil(t, evalCtx.Metadata["composition_step_outputs"], "composition step outputs should be bridged")
	assert.Equal(t, "paper", evalCtx.Metadata["composition_branch_taken"].(map[string]any)["route"])
	assert.Equal(t, "complete", evalCtx.Metadata["composition_parallel_status"].(map[string]any)["meta"])
}

func TestEvalOrchestrator_CloneResetsCompositionProvider(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	hook.SetCompositionMetadataProvider(&fakeCompositionMetaProvider{md: map[string]any{
		"composition_step_outputs": map[string]any{"a": "b"},
	}})

	clone := hook.Clone()
	evalCtx := clone.buildEvalContext([]types.Message{types.NewAssistantMessage("x")}, 1, "s")

	_, present := evalCtx.Metadata["composition_step_outputs"]
	assert.False(t, present, "Clone must reset compositionMetaProvider for per-run isolation")
}

func TestBuildEvalContext_BridgesZeroLatency(t *testing.T) {
	// Sub-millisecond mock provider calls produce LatencyMs == 0 but the
	// metadata key should still be present — handlers may want to assert
	// "latency was measured" distinct from "latency was over budget".
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	messages := []types.Message{
		types.NewUserMessage("hello"),
		{Role: "assistant", Content: "reply", LatencyMs: 0},
	}

	evalCtx := hook.buildEvalContext(messages, 1, "session-1")

	require.NotNil(t, evalCtx.Metadata)
	assert.Equal(t, float64(0), evalCtx.Metadata["latency_ms"])
}

func TestBuildEvalContext_NoAssistantNoLatency(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	messages := []types.Message{types.NewUserMessage("hello")}

	evalCtx := hook.buildEvalContext(messages, 0, "session-1")

	if evalCtx.Metadata != nil {
		_, has := evalCtx.Metadata["latency_ms"]
		assert.False(t, has, "no latency should be reported when no assistant message exists")
	}
}

func TestLatestAssistantLatencyMs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		msgs    []types.Message
		wantMs  int64
		wantHas bool
	}{
		{name: "empty", msgs: nil, wantHas: false},
		{
			name: "no assistant",
			msgs: []types.Message{types.NewUserMessage("u")},
		},
		{
			name:    "single assistant",
			msgs:    []types.Message{{Role: "assistant", LatencyMs: 250}},
			wantMs:  250,
			wantHas: true,
		},
		{
			name: "uses latest assistant",
			msgs: []types.Message{
				{Role: "assistant", LatencyMs: 100},
				types.NewUserMessage("u"),
				{Role: "assistant", LatencyMs: 800},
			},
			wantMs:  800,
			wantHas: true,
		},
		{
			name:    "zero latency still reports has",
			msgs:    []types.Message{{Role: "assistant", LatencyMs: 0}},
			wantMs:  0,
			wantHas: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotMs, gotHas := latestAssistantLatencyMs(tc.msgs)
			assert.Equal(t, tc.wantMs, gotMs)
			assert.Equal(t, tc.wantHas, gotHas)
		})
	}
}

func TestBuildEvalContext_NoMessages(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	evalCtx := hook.buildEvalContext(nil, 0, "session-1")

	assert.Empty(t, evalCtx.CurrentOutput, "should have empty current output with no messages")
	assert.Empty(t, evalCtx.ToolCalls, "should have no tool calls with no messages")
	assert.Equal(t, 0, evalCtx.TurnIndex)
	assert.Equal(t, "session-1", evalCtx.SessionID)
}

func TestBuildEvalContext_ExtractsToolCalls(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("search for cats"),
		{
			Role:    "assistant",
			Content: "I found some results",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search", Args: []byte(`{"q":"cats"}`)},
				{ID: "tc-2", Name: "format", Args: []byte(`{}`)},
			},
		},
	}

	evalCtx := hook.buildEvalContext(messages, 1, "session-1")

	assert.Equal(t, "I found some results", evalCtx.CurrentOutput)
	require.Len(t, evalCtx.ToolCalls, 2)
	assert.Equal(t, "search", evalCtx.ToolCalls[0].ToolName)
	assert.Equal(t, 1, evalCtx.ToolCalls[0].TurnIndex)
	assert.Equal(t, "format", evalCtx.ToolCalls[1].ToolName)
}

func TestRunTurnEvals_WithValidHandler(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerEveryTurn},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("hi there"),
	}

	results := hook.RunTurnEvals(context.Background(), messages, 1, "session-1")
	require.NotNil(t, results, "should return results when evals are configured")
	assert.Len(t, results, 1)
}

func TestRunSessionEvals_WithValidHandler(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerOnSessionComplete},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("goodbye"),
	}

	results := hook.RunSessionEvals(context.Background(), messages, "session-1")
	require.NotNil(t, results, "should return results when session evals are configured")
	assert.Len(t, results, 1)
}

func TestRunConversationEvals_NoEvals(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), nil, false, nil, "chat")
	results := hook.RunConversationEvals(context.Background(), nil, "session-1")
	assert.Nil(t, results)
}

func TestRunConversationEvals_WithValidHandler(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerOnConversationComplete},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("goodbye"),
	}

	results := hook.RunConversationEvals(context.Background(), messages, "session-1")
	require.NotNil(t, results)
	assert.Len(t, results, 1)
}

func TestRunConversationEvals_EmptyMessages(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "eval-1", Type: "test_handler", Trigger: evals.TriggerOnConversationComplete},
	}
	hook := NewEvalOrchestrator(newTestRegistry(), defs, false, nil, "chat")

	results := hook.RunConversationEvals(context.Background(), []types.Message{}, "session-1")
	require.NotNil(t, results)
	assert.Len(t, results, 1)
}

func TestRunAssertionsAsEvals_EmptyAssertions(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), sampleDefs(), false, nil, "chat")
	results := hook.RunAssertionsAsEvals(
		context.Background(), nil, nil, 0, "session-1", evals.TriggerEveryTurn)
	assert.Nil(t, results)
}

func TestRunAssertionsAsEvals_ConversationTrigger(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), sampleDefs(), false, nil, "chat")

	cfgs := []assertions.AssertionConfig{
		{Type: "test_handler", Params: map[string]interface{}{}},
	}
	messages := []types.Message{
		types.NewUserMessage("hi"),
		types.NewAssistantMessage("hello"),
	}

	results := hook.RunAssertionsAsEvals(
		context.Background(), cfgs, messages, 1, "session-1",
		evals.TriggerOnConversationComplete)
	require.NotNil(t, results)
	assert.Len(t, results, 1)
	passed, ok := results[0].Value.(bool)
	assert.True(t, ok, "Value should be a bool")
	assert.True(t, passed, "assertion should have passed")
}

func TestRunAssertionsAsEvals_TurnTrigger(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), sampleDefs(), false, nil, "chat")

	cfgs := []assertions.AssertionConfig{
		{Type: "test_handler", Params: map[string]interface{}{}},
	}
	messages := []types.Message{
		types.NewAssistantMessage("hello"),
	}

	results := hook.RunAssertionsAsEvals(
		context.Background(), cfgs, messages, 0, "session-1",
		evals.TriggerEveryTurn)
	require.NotNil(t, results)
	assert.Len(t, results, 1)
}

func TestRunAssertionsAsConversationResults(t *testing.T) {
	hook := NewEvalOrchestrator(newTestRegistry(), sampleDefs(), false, nil, "chat")

	cfgs := []assertions.AssertionConfig{
		{Type: "test_handler", Params: map[string]interface{}{}},
	}
	messages := []types.Message{
		types.NewAssistantMessage("hello"),
	}

	results := hook.RunAssertionsAsConversationResults(
		context.Background(), cfgs, messages, 0, "session-1",
		evals.TriggerEveryTurn)
	require.NotNil(t, results)
	assert.Len(t, results, 1)
	assert.True(t, results[0].Passed)
}

func TestExtractToolCalls_WithResults(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("search"),
		{
			Role:    "assistant",
			Content: "searching...",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search", Args: []byte(`{"q":"cats"}`)},
			},
		},
		{
			Role:    "tool",
			Content: "found 3 results",
			ToolResult: &types.MessageToolResult{
				ID: "tc-1",
			},
		},
	}

	toolCalls := evals.ExtractToolCalls(messages)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "search", toolCalls[0].ToolName)
	assert.Equal(t, 1, toolCalls[0].TurnIndex)
	assert.Equal(t, "cats", toolCalls[0].Arguments["q"])
	assert.Equal(t, "found 3 results", toolCalls[0].Result)
}

func TestExtractToolCalls_WithError(t *testing.T) {
	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "calling tool",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "fail_tool", Args: []byte(`{}`)},
			},
		},
		{
			Role:    "tool",
			Content: "",
			ToolResult: &types.MessageToolResult{
				ID:    "tc-1",
				Error: "tool failed",
			},
		},
	}

	toolCalls := evals.ExtractToolCalls(messages)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "tool failed", toolCalls[0].Error)
}

func TestExtractToolCalls_NoMatchingResult(t *testing.T) {
	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "calling",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search"},
			},
		},
	}

	toolCalls := evals.ExtractToolCalls(messages)
	require.Len(t, toolCalls, 1)
	assert.Empty(t, toolCalls[0].Result)
}

// parseJSONArgs tests moved to runtime/evals/context_test.go

func TestExtractWorkflowExtras_AllFields(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			Meta: map[string]any{
				"_workflow_state":       "greeting",
				"_workflow_transitions": []string{"init", "greeting"},
				"_workflow_complete":    true,
			},
		},
	}

	extras := evals.ExtractWorkflowExtras(messages)
	require.NotNil(t, extras)
	assert.Equal(t, "greeting", extras["workflow_state"])
	assert.Equal(t, []string{"init", "greeting"}, extras["workflow_transitions"])
	assert.Equal(t, true, extras["workflow_complete"])
}

func TestExtractWorkflowExtras_NoWorkflowMeta(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Meta: map[string]any{"other": "data"}},
		{Role: "user"},
	}

	extras := evals.ExtractWorkflowExtras(messages)
	assert.Nil(t, extras)
}

func TestExtractWorkflowExtras_NilMeta(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant"},
	}

	extras := evals.ExtractWorkflowExtras(messages)
	assert.Nil(t, extras)
}

func TestEvalOrchestrator_Clone(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	orch := NewEvalOrchestrator(registry, nil, false, nil, "test")
	orch.SetMetadata(map[string]any{"key": "value"})

	clone := orch.Clone()
	require.NotNil(t, clone)
	assert.Equal(t, "value", clone.metadata["key"])

	// Mutating clone metadata doesn't affect original
	clone.metadata["key"] = "changed"
	assert.Equal(t, "value", orch.metadata["key"])

	// WorkflowMetadataProvider is independent
	clone.SetWorkflowMetadataProvider(nil)

	// Runner is independent (cloned, not shared)
	assert.NotSame(t, orch.runner, clone.runner)
	assert.Nil(t, clone.workflowMetaProvider)
}

func TestEvalOrchestrator_Clone_WithEventBus(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	orch := NewEvalOrchestrator(registry, nil, false, nil, "test")
	orch.SetEventBus(bus)

	clone := orch.Clone()
	require.NotNil(t, clone)
	assert.NotSame(t, orch.runner, clone.runner)
	// Event bus is shared (intentional — events go to same bus)
	assert.Equal(t, bus, clone.eventBus)
}

func TestEvalOrchestrator_Clone_Nil(t *testing.T) {
	var orch *EvalOrchestrator
	assert.Nil(t, orch.Clone())
}

// classifyObservingHandler captures whatever classify.Registry is reachable
// from the context at eval time. Used to verify the orchestrator threads the
// registry from SetClassifyRegistry through to handlers.
type classifyObservingHandler struct {
	seen *classify.Registry
}

func (h *classifyObservingHandler) Type() string { return "classify_probe" }

func (h *classifyObservingHandler) Eval(
	ctx context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	h.seen = classify.FromContext(ctx)
	return &evals.EvalResult{EvalID: "probe", Type: "classify_probe", Value: true}, nil
}

func TestEvalOrchestrator_ClassifyRegistryReachableFromHandlerContext(t *testing.T) {
	probe := &classifyObservingHandler{}
	reg := evals.NewEvalTypeRegistry()
	reg.Register(probe)

	defs := []evals.EvalDef{{ID: "probe", Type: "classify_probe", Trigger: evals.TriggerEveryTurn}}
	orch := NewEvalOrchestrator(reg, defs, false, nil, "test")

	want := classify.NewRegistry()
	orch.SetClassifyRegistry(want)

	results := orch.RunTurnEvals(context.Background(),
		[]types.Message{{Role: "assistant", Content: "hi"}}, 0, "s1")
	require.NotEmpty(t, results)
	assert.Same(t, want, probe.seen, "handler must see the registry the orchestrator was configured with")
}

func TestEvalOrchestrator_NoClassifyRegistryLeavesContextUnchanged(t *testing.T) {
	probe := &classifyObservingHandler{}
	reg := evals.NewEvalTypeRegistry()
	reg.Register(probe)

	defs := []evals.EvalDef{{ID: "probe", Type: "classify_probe", Trigger: evals.TriggerEveryTurn}}
	orch := NewEvalOrchestrator(reg, defs, false, nil, "test")
	// No SetClassifyRegistry call.

	orch.RunTurnEvals(context.Background(),
		[]types.Message{{Role: "assistant", Content: "hi"}}, 0, "s1")
	assert.Nil(t, probe.seen, "FromContext on an unconfigured orchestrator must return nil so handlers can produce a clean error")
}

func TestEvalOrchestrator_SetClassifyRegistry_NilReceiverSafe(t *testing.T) {
	var orch *EvalOrchestrator
	// Must not panic.
	orch.SetClassifyRegistry(classify.NewRegistry())
}

func TestEvalOrchestrator_NilReceiver(t *testing.T) {
	var hook *EvalOrchestrator
	ctx := context.Background()
	msgs := []types.Message{{Role: "assistant", Content: "hello"}}
	configs := []assertions.AssertionConfig{{Type: "contains", Params: map[string]interface{}{"value": "hello"}}}

	assert.False(t, hook.HasEvals())
	assert.Nil(t, hook.RunTurnEvals(ctx, msgs, 0, "s1"))
	assert.Nil(t, hook.RunSessionEvals(ctx, msgs, "s1"))
	assert.Nil(t, hook.RunConversationEvals(ctx, msgs, "s1"))
	assert.Nil(t, hook.RunAssertionsAsEvals(ctx, configs, msgs, 0, "s1", evals.TriggerEveryTurn))
	assert.Nil(t, hook.RunAssertionsAsConversationResults(ctx, configs, msgs, 0, "s1", evals.TriggerEveryTurn))
}
