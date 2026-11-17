package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerExecutor is a host CallExecutor that records the calls it is given.
type callerExecutor struct {
	stubExecutor
	calls []tools.ToolCall
}

func (e *callerExecutor) ExecuteCall(
	_ context.Context, _ *tools.ToolDescriptor, call tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	e.calls = append(e.calls, call)
	return json.RawMessage(`{"ok":true}`), nil, nil
}

func TestToolExec_HandsTheEvalContextsCallerToACallExecutor(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}
	exec := &callerExecutor{stubExecutor: stubExecutor{name: "host"}}
	registry := newStubRegistry(t, "run_tests", &exec.stubExecutor)
	registry.RegisterExecutor(exec)
	evalCtx := evalCtxWithRegistry(registry)
	evalCtx.Caller = member

	res, err := (&ToolExecHandler{}).Eval(context.Background(), evalCtx,
		map[string]any{"tool": "run_tests", "args": map[string]any{"suite": "unit"}})
	require.NoError(t, err)

	require.NotNil(t, res.Score)
	assert.Equal(t, 1.0, *res.Score)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, member, exec.calls[0].Caller)
	assert.Equal(t, "run_tests", exec.calls[0].Name)
	assert.JSONEq(t, `{"suite":"unit"}`, string(exec.calls[0].Args))
}
