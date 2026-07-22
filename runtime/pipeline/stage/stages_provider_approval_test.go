package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
	stage := setupApprovalStage(t, exec, func(callID, name string, args map[string]any) *tools.PendingToolInfo {
		checkedName, checkedArgs = name, args
		return &tools.PendingToolInfo{Reason: "requires_approval", Message: "Approve?", ToolName: name}
	})

	_, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{"amount":150}`)}})

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
	stage := setupApprovalStage(t, exec, func(_, _ string, _ map[string]any) *tools.PendingToolInfo {
		return nil
	})

	results, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}})
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
		[]types.MessageToolCall{{ID: "c1", Name: "risky", Args: json.RawMessage(`{}`)}})
	require.NoError(t, err)
	assert.EqualValues(t, 1, exec.callCount.Load())
}
