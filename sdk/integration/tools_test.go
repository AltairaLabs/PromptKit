package integration

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

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// toolsPackWithAllowedToolsJSON defines a pack where the prompt's tools field
// lists get_weather, enabling the pipeline to send it to the provider.
const toolsPackWithAllowedToolsJSON = `{
	"id": "integration-test-tools",
	"version": "1.0.0",
	"description": "Pack with tools for SDK integration tests",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant with tools.",
			"tools": ["get_weather"]
		}
	},
	"tools": {
		"get_weather": {
			"name": "get_weather",
			"description": "Get weather for a city",
			"mode": "local",
			"parameters": {
				"type": "object",
				"properties": {
					"city": {"type": "string"}
				},
				"required": ["city"]
			}
		}
	}
}`

// ---------------------------------------------------------------------------
// testLocalExecutor implements tools.Executor and dispatches to a handler map.
// This is used to re-register a "local" executor on the shared tool registry
// after OnTool() has been called, working around the fact that the SDK's
// internal localExecutor copies the handler map at pipeline build time.
// ---------------------------------------------------------------------------

type testLocalExecutor struct {
	mu       sync.RWMutex
	handlers map[string]func(args map[string]any) (any, error)
}

func newTestLocalExecutor() *testLocalExecutor {
	return &testLocalExecutor{
		handlers: make(map[string]func(args map[string]any) (any, error)),
	}
}

func (e *testLocalExecutor) Name() string { return "local" }

func (e *testLocalExecutor) addHandler(name string, handler func(args map[string]any) (any, error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers[name] = handler
}

func (e *testLocalExecutor) Execute(
	_ context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	e.mu.RLock()
	handler, ok := e.handlers[descriptor.Name]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no handler registered for tool: %s", descriptor.Name)
	}

	result, err := handler(argsMap)
	if err != nil {
		return nil, err
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool result: %w", err)
	}

	return resultJSON, nil
}

// openToolConv opens a conversation with a tool-aware mock provider and
// registers tool handlers via a testLocalExecutor that survives the pipeline
// build.
func openToolConv(
	t *testing.T,
	repo *testTurnRepository,
	handlers map[string]func(args map[string]any) (any, error),
	extraOpts ...sdk.Option,
) *sdk.Conversation {
	t.Helper()

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)
	packPath := writePackFile(t, toolsPackWithAllowedToolsJSON)

	opts := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	}
	opts = append(opts, extraOpts...)

	conv, err := sdk.Open(packPath, "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	// Register handlers via a testLocalExecutor that replaces the SDK's
	// internal localExecutor on the shared tool registry.
	exec := newTestLocalExecutor()
	for name, handler := range handlers {
		exec.addHandler(name, handler)
		// Also register with conv.OnTool for handler consistency
		conv.OnTool(name, handler)
	}
	conv.ToolRegistry().RegisterExecutor(exec)

	return conv
}

// ---------------------------------------------------------------------------
// testTurnRepository is a mock ResponseRepository that supports structured
// Turn responses including tool calls. Unlike InMemoryMockRepository (which
// only supports text), this implementation stores full mock.Turn objects
// keyed by scenario+turn number.
// ---------------------------------------------------------------------------

type testTurnRepository struct {
	mu    sync.RWMutex
	turns map[string]*mock.Turn // key: "scenario:turn"
}

func newTestTurnRepository() *testTurnRepository {
	return &testTurnRepository{
		turns: make(map[string]*mock.Turn),
	}
}

func (r *testTurnRepository) addTurn(scenario string, turnNum int, turn mock.Turn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%s:%d", scenario, turnNum)
	r.turns[key] = &turn
}

func (r *testTurnRepository) GetResponse(_ context.Context, params mock.ResponseParams) (string, error) {
	turn, err := r.GetTurn(context.Background(), params)
	if err != nil {
		return "", err
	}
	return turn.Content, nil
}

func (r *testTurnRepository) GetTurn(_ context.Context, params mock.ResponseParams) (*mock.Turn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	scenario := params.ScenarioID
	if scenario == "" {
		scenario = "default"
	}

	key := fmt.Sprintf("%s:%d", scenario, params.TurnNumber)
	if turn, ok := r.turns[key]; ok {
		return turn, nil
	}

	// Fallback: return generic text response
	return &mock.Turn{
		Type:    "text",
		Content: "Mock fallback response",
	}, nil
}

// ---------------------------------------------------------------------------
// 2.1 — Registered tool execution
// ---------------------------------------------------------------------------

func TestTools_RegisteredToolExecution(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check the weather",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "London"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "The weather in London is sunny.",
	})

	var handlerCalled atomic.Bool
	var receivedCity string
	var mu sync.Mutex

	handler := func(args map[string]any) (any, error) {
		handlerCalled.Store(true)
		mu.Lock()
		receivedCity, _ = args["city"].(string)
		mu.Unlock()
		return map[string]any{"temperature": 22, "condition": "sunny"}, nil
	}

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": handler,
	})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "What is the weather in London?")
	require.NoError(t, err)

	assert.True(t, handlerCalled.Load(), "tool handler should have been called")
	mu.Lock()
	assert.Equal(t, "London", receivedCity, "handler should receive correct city argument")
	mu.Unlock()
	assert.NotEmpty(t, resp.Text(), "response text should be non-empty")
}

// ---------------------------------------------------------------------------
// 2.2 — Multiple tool calls in a single turn
// ---------------------------------------------------------------------------

func TestTools_MultipleToolCallsInSingleTurn(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check both cities",
		ToolCalls: []mock.ToolCall{
			{
				Name:      "get_weather",
				Arguments: map[string]interface{}{"city": "London"},
			},
			{
				Name:      "get_weather",
				Arguments: map[string]interface{}{"city": "Paris"},
			},
		},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "London is sunny and Paris is cloudy.",
	})

	var callCount atomic.Int32
	var citiesMu sync.Mutex
	var cities []string

	handler := func(args map[string]any) (any, error) {
		callCount.Add(1)
		city, _ := args["city"].(string)
		citiesMu.Lock()
		cities = append(cities, city)
		citiesMu.Unlock()
		return map[string]any{"temperature": 20, "condition": "nice"}, nil
	}

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": handler,
	})

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Weather in London and Paris?")
	require.NoError(t, err)

	assert.Equal(t, int32(2), callCount.Load(), "handler should have been called twice")
	assert.NotEmpty(t, resp.Text(), "response text should be non-empty")

	citiesMu.Lock()
	assert.ElementsMatch(t, []string{"London", "Paris"}, cities, "both cities should be passed to handler")
	citiesMu.Unlock()
}

// ---------------------------------------------------------------------------
// 2.3 — HITL approval flow
// ---------------------------------------------------------------------------

func TestTools_HITLApproval(t *testing.T) {
	// HITL flow is tested at the Conversation API level using CheckPending,
	// ResolveTool, and Continue. The pipeline does not automatically gate
	// tool execution via async handlers; the gating is a caller-side concern.
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "text",
		Content: "I will process that for you.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)

	conv := openTestConvWithPack(t, toolsPackWithAllowedToolsJSON, "chat",
		sdk.WithProvider(provider),
	)

	var executed atomic.Bool

	conv.OnToolAsync(
		"get_weather",
		func(args map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{
				Reason:  "requires_approval",
				Message: "Weather lookup needs approval",
			}
		},
		func(args map[string]any) (any, error) {
			executed.Store(true)
			return map[string]any{"temperature": 22}, nil
		},
	)

	// Manually create a pending check (simulating what a caller would do
	// when intercepting a tool call before execution).
	pending, shouldWait := conv.CheckPending("get_weather", map[string]any{"city": "London"})
	require.True(t, shouldWait, "should indicate pending")
	require.NotNil(t, pending)
	assert.Equal(t, "requires_approval", pending.Reason)
	assert.Equal(t, "Weather lookup needs approval", pending.Message)

	// Verify the pending call is stored
	pendingList := conv.PendingTools()
	require.Len(t, pendingList, 1)
	assert.Equal(t, "get_weather", pendingList[0].Name)

	// Resolve the pending tool
	resolution, err := conv.ResolveTool(pending.ID)
	require.NoError(t, err)
	assert.NotNil(t, resolution)

	// The exec handler should have been called during resolve
	assert.True(t, executed.Load(), "exec handler should have been called after resolve")

	// Continue to send the tool result to the LLM
	ctx := context.Background()
	resp, err := conv.Continue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text(), "response after continue should have text")
}

// ---------------------------------------------------------------------------
// 2.4 — HITL rejection flow
// ---------------------------------------------------------------------------

func TestTools_HITLRejection(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "text",
		Content: "Understood, the action was rejected.",
	})

	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)

	conv := openTestConvWithPack(t, toolsPackWithAllowedToolsJSON, "chat",
		sdk.WithProvider(provider),
	)

	var executed atomic.Bool

	conv.OnToolAsync(
		"get_weather",
		func(args map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{
				Reason:  "requires_approval",
				Message: "Weather lookup needs approval",
			}
		},
		func(args map[string]any) (any, error) {
			executed.Store(true)
			return nil, nil
		},
	)

	// Create a pending check
	pending, shouldWait := conv.CheckPending("get_weather", map[string]any{"city": "London"})
	require.True(t, shouldWait)
	require.NotNil(t, pending)

	// Reject the pending tool
	resolution, err := conv.RejectTool(pending.ID, "not authorized")
	require.NoError(t, err)
	assert.True(t, resolution.Rejected)
	assert.Equal(t, "not authorized", resolution.RejectionReason)

	// Verify the exec handler was NOT called
	assert.False(t, executed.Load(), "exec handler should NOT be called when rejected")

	// Continue to send the rejection message to the LLM
	ctx := context.Background()
	resp, err := conv.Continue(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text(), "response after rejected continue should have text")
}

// ---------------------------------------------------------------------------
// 2.5 — Tool call events emitted
// ---------------------------------------------------------------------------

func TestTools_EventsEmitted(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type:    "tool_calls",
		Content: "Let me check",
		ToolCalls: []mock.ToolCall{{
			Name:      "get_weather",
			Arguments: map[string]interface{}{"city": "Tokyo"},
		}},
	})
	repo.addTurn("default", 2, mock.Turn{
		Type:    "text",
		Content: "It is warm in Tokyo.",
	})

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	handler := func(args map[string]any) (any, error) {
		return map[string]any{"temperature": 30}, nil
	}

	conv := openToolConv(t, repo, map[string]func(args map[string]any) (any, error){
		"get_weather": handler,
	}, sdk.WithEventBus(bus))

	ctx := context.Background()
	_, err := conv.Send(ctx, "Weather in Tokyo?")
	require.NoError(t, err)

	// Wait for PipelineCompleted to avoid race with bus.Close()
	require.True(t, ec.waitForEvent(events.EventPipelineCompleted, 5*time.Second),
		"should receive pipeline.completed event")

	// Verify tool call events
	assert.True(t, ec.hasType(events.EventToolCallStarted),
		"should emit tool.call.started")
	assert.True(t, ec.hasType(events.EventToolCallCompleted),
		"should emit tool.call.completed")

	// Verify tool.call.started has correct ToolName
	startedEvents := ec.ofType(events.EventToolCallStarted)
	require.NotEmpty(t, startedEvents)
	startedData, ok := startedEvents[0].Data.(*events.ToolCallStartedData)
	require.True(t, ok, "event data should be *ToolCallStartedData, got %T", startedEvents[0].Data)
	assert.Equal(t, "get_weather", startedData.ToolName)

	// Verify tool.call.completed has correct ToolName
	completedEvents := ec.ofType(events.EventToolCallCompleted)
	require.NotEmpty(t, completedEvents)
	completedData, ok := completedEvents[0].Data.(*events.ToolCallCompletedData)
	require.True(t, ok, "event data should be *ToolCallCompletedData, got %T", completedEvents[0].Data)
	assert.Equal(t, "get_weather", completedData.ToolName)
}
