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

// recordingResolver is a WorkflowStateResolver that also implements
// ToolCallRecorder, standing in for a consumer that forwards the count to its
// workflow context.
type recordingResolver struct {
	fakeResolver
	recorded []int
}

func (r *recordingResolver) RecordToolCalls(n int) {
	r.recorded = append(r.recorded, n)
}

func (r *recordingResolver) total() int {
	sum := 0
	for _, n := range r.recorded {
		sum += n
	}
	return sum
}

// budgetStage builds a ProviderStage with one always-succeeding tool.
func budgetStage(t *testing.T, checker tools.ApprovalChecker) (*ProviderStage, *delayedExecutor) {
	t.Helper()
	exec := &delayedExecutor{name: "local-exec", status: tools.ToolStatusComplete, content: []byte(`{"ok":true}`)}
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "work", Mode: exec.Name(), InputSchema: []byte(`{"type":"object"}`),
	}))
	stage := NewProviderStage(mock.NewProvider("t", "m", false), registry, nil,
		&ProviderConfig{ApprovalChecker: checker})
	return stage, exec
}

func call(id string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: "work", Args: json.RawMessage(`{}`)}
}

// The whole point of RFC 0009's max_tool_calls: something has to count. The
// runtime tool loop is the only place that sees every call on every path, so it
// reports the count and the consumer forwards it to the workflow context.
func TestProviderStage_RecordsExecutedToolCalls(t *testing.T) {
	stage, exec := budgetStage(t, nil)
	rec := &recordingResolver{}
	stage.SetWorkflowStateResolver(rec)

	results, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{call("c1"), call("c2"), call("c3")})
	require.NoError(t, err)
	require.Len(t, results, 3)

	assert.EqualValues(t, 3, exec.callCount.Load())
	assert.Equal(t, 3, rec.total(), "every executed tool call must be reported to the recorder")
}

// A pending call has NOT run — executeSingleToolCall surfaces it instead of
// executing, and it executes for real on resume. Counting it here would charge
// approval-gated calls twice and exhaust the budget at half the true call count.
func TestProviderStage_DoesNotRecordPendingToolCalls(t *testing.T) {
	held := map[string]bool{"c2": true}
	stage, exec := budgetStage(t, func(_ context.Context, callID, name string, _ map[string]any) *tools.PendingToolInfo {
		if held[callID] {
			return &tools.PendingToolInfo{Reason: "requires_approval", ToolName: name}
		}
		return nil
	})
	rec := &recordingResolver{}
	stage.SetWorkflowStateResolver(rec)

	_, err := stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{call("c1"), call("c2")})
	var pendErr *tools.ErrToolsPending
	require.ErrorAs(t, err, &pendErr)

	assert.EqualValues(t, 1, exec.callCount.Load(), "only the unheld call executes")
	assert.Equal(t, 1, rec.total(), "the held call must not be counted — it runs on resume")
}

// The resolver is optional, and a resolver that does not implement
// ToolCallRecorder is the normal case for non-workflow conversations.
func TestProviderStage_RecordToolCallsIsOptional(t *testing.T) {
	stage, _ := budgetStage(t, nil)
	stage.SetWorkflowStateResolver(&fakeResolver{}) // no RecordToolCalls method

	require.NotPanics(t, func() {
		_, err := stage.executeToolCalls(context.Background(), []types.MessageToolCall{call("c1")})
		require.NoError(t, err)
	})
}
