package stage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupApprovalStage builds a ProviderStage with one tool and an ApprovalChecker.
func setupApprovalStage(t *testing.T, exec *delayedExecutor, checker tools.ApprovalChecker) *ProviderStage {
	t.Helper()
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name:        "risky",
		Description: "needs approval",
		Mode:        exec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}))
	return NewProviderStage(mock.NewProvider("t", "m", false), registry, nil,
		&ProviderConfig{ApprovalChecker: checker})
}

func TestProviderStage_ApprovalCheckerHoldsToolPending(t *testing.T) {
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}

	var checkedName string
	var checkedArgs map[string]any
	stage := setupApprovalStage(t, exec, func(_ context.Context, callID, name string, args map[string]any) *tools.PendingToolInfo {
		checkedName, checkedArgs = name, args
		return &tools.PendingToolInfo{Reason: "requires_approval", Message: "Approve?", ToolName: name}
	})

	_, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{"amount":150}`)}}, roundRef{round: 1})

	// The tool is HELD pending, not executed.
	var pendErr *tools.ErrToolsPending
	require.ErrorAs(t, err, &pendErr)
	require.Len(t, pendErr.Pending, 1)
	assert.Equal(t, "risky", pendErr.Pending[0].ToolName)
	assert.Equal(t, "c1", pendErr.Pending[0].CallID)
	require.NotNil(t, pendErr.Pending[0].PendingInfo)
	assert.Equal(t, "requires_approval", pendErr.Pending[0].PendingInfo.Reason)

	// The checker saw the call, and the executor never ran.
	assert.Equal(t, "risky", checkedName)
	assert.EqualValues(t, 150, checkedArgs["amount"])
	assert.EqualValues(t, 0, exec.callCount.Load(), "held tool must not execute")
}

func TestProviderStage_ApprovalCheckerNilExecutesNormally(t *testing.T) {
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}

	// Checker returns nil → no approval needed → executes.
	stage := setupApprovalStage(t, exec, func(_ context.Context, _, _ string, _ map[string]any) *tools.PendingToolInfo {
		return nil
	})

	results, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.EqualValues(t, 1, exec.callCount.Load(), "tool with no approval gate should execute")
}

func TestProviderStage_NoApprovalCheckerExecutesNormally(t *testing.T) {
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "risky", Mode: exec.Name(), InputSchema: []byte(`{"type":"object"}`),
	}))
	stage := NewProviderStage(mock.NewProvider("t", "m", false), registry, nil, nil) // nil config

	_, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})
	require.NoError(t, err)
	assert.EqualValues(t, 1, exec.callCount.Load())
}

// setupGateStage builds a ProviderStage with one tool and both an
// ApprovalChecker and an ApprovalGate.
func setupGateStage(
	t *testing.T, exec *delayedExecutor, checker tools.ApprovalChecker, gate tools.ApprovalGate,
) *ProviderStage {
	t.Helper()
	stage := setupApprovalStage(t, exec, checker)
	stage.config.ApprovalGate = gate
	return stage
}

// A gate's error refuses the call: the executor never runs, and the call's
// result is an approval error naming the cause.
func TestProviderStage_ApprovalGateRefusalIsTheCallsResult(t *testing.T) {
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}
	stage := setupGateStage(t, exec, nil,
		func(context.Context, string, string, map[string]any) (*tools.PendingToolInfo, error) {
			return nil, errors.New("hold store unavailable")
		})

	results, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})

	require.NoError(t, err, "a refusal is the call's result, not a failed round")
	require.Len(t, results, 1)
	require.NotNil(t, results[0].ToolResult)
	assert.Equal(t, "c1", results[0].ToolResult.ID)
	assert.Equal(t, types.ToolErrorApproval, results[0].ToolResult.ErrorType)
	assert.Equal(t,
		"Tool risky was not run: the approval gate refused it: hold store unavailable",
		results[0].ToolResult.Error)
	assert.EqualValues(t, 0, exec.callCount.Load(), "a refused call must not execute")
}

// When both are set the gate decides and the checker is not consulted.
func TestProviderStage_ApprovalGateTakesThePlaceOfTheChecker(t *testing.T) {
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}
	var checkerCalled bool
	stage := setupGateStage(t, exec,
		func(context.Context, string, string, map[string]any) *tools.PendingToolInfo {
			checkerCalled = true
			return nil
		},
		func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
			return &tools.PendingToolInfo{Reason: "referral", ToolName: name}, nil
		})

	_, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})

	var pendErr *tools.ErrToolsPending
	require.ErrorAs(t, err, &pendErr)
	require.Len(t, pendErr.Pending, 1)
	assert.Equal(t, "referral", pendErr.Pending[0].PendingInfo.Reason)
	assert.False(t, checkerCalled, "the checker is not consulted when a gate is set")
	assert.EqualValues(t, 0, exec.callCount.Load())
}

// A refused call is reported as a started call that failed, so a consumer
// that pairs the two events by call id sees both.
func TestProviderStage_ApprovalGateRefusalEmitsStartedThenFailed(t *testing.T) {
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "risky", Mode: exec.Name(), InputSchema: []byte(`{"type":"object"}`),
	}))
	bus := events.NewEventBus()
	var mu sync.Mutex
	var seen []string
	bus.SubscribeAll(func(e *events.Event) {
		data, ok := e.Data.(*events.ToolCallEventData)
		if !ok {
			return
		}
		mu.Lock()
		seen = append(seen, string(e.Type)+":"+data.CallID)
		mu.Unlock()
	})
	stage := NewProviderStageWithEmitter(mock.NewProvider("t", "m", false), registry, nil,
		&ProviderConfig{ApprovalGate: func(context.Context, string, string, map[string]any) (*tools.PendingToolInfo, error) {
			return nil, errors.New("refused")
		}},
		events.NewEmitter(bus, "run-1", "session-1", "conv-1"))

	_, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}}, roundRef{round: 1})
	require.NoError(t, err)
	bus.Close() // drains: every event published so far is delivered

	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{
		string(events.EventToolCallStarted) + ":c1",
		string(events.EventToolCallFailed) + ":c1",
	}, seen)
}
