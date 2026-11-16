package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordingCallExecutor is a host executor that records every call it is given
// through each entry point.
type recordingCallExecutor struct {
	calls     []ToolCall // via ExecuteCall
	plainArgs []string   // via Execute
}

func (e *recordingCallExecutor) Name() string { return "host" }

func (e *recordingCallExecutor) Execute(
	_ context.Context, _ *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	e.plainArgs = append(e.plainArgs, string(args))
	return json.RawMessage(`{"via":"execute"}`), nil
}

func (e *recordingCallExecutor) ExecuteCall(
	_ context.Context, _ *ToolDescriptor, call ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	e.calls = append(e.calls, call)
	return json.RawMessage(`{"via":"call"}`), nil, nil
}

// asyncAndCallExecutor implements AsyncToolExecutor and CallExecutor, but not
// AsyncCallExecutor.
type asyncAndCallExecutor struct {
	recordingCallExecutor
	asyncArgs []string
}

func (e *asyncAndCallExecutor) ExecuteAsync(
	_ context.Context, _ *ToolDescriptor, args json.RawMessage,
) (*ToolExecutionResult, error) {
	e.asyncArgs = append(e.asyncArgs, string(args))
	return &ToolExecutionResult{Status: ToolStatusPending, PendingInfo: &PendingToolInfo{Reason: "held"}}, nil
}

// asyncCallExecutor implements AsyncCallExecutor and AsyncToolExecutor.
type asyncCallExecutor struct {
	asyncAndCallExecutor
	asyncCalls []ToolCall
}

func (e *asyncCallExecutor) ExecuteCallAsync(
	_ context.Context, _ *ToolDescriptor, call ToolCall,
) (*ToolExecutionResult, error) {
	e.asyncCalls = append(e.asyncCalls, call)
	return &ToolExecutionResult{Status: ToolStatusComplete, Content: json.RawMessage(`{"via":"call_async"}`)}, nil
}

func newCallTestRegistry(t *testing.T, executor Executor) *Registry {
	t.Helper()
	r := NewRegistry()
	r.RegisterExecutor(executor)
	require.NoError(t, r.Register(&ToolDescriptor{
		Name: "lookup", Description: "lookup", Mode: "host",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`),
	}))
	return r
}

func lookupCall() ToolCall {
	return ToolCall{
		Name: "lookup", ID: "c1", Args: json.RawMessage(`{"n":"5"}`),
		Caller: types.Caller{ID: "u1", Claims: map[string]any{"tenant": "acme"}},
	}
}

func TestRegistry_ExecuteCall_GivesCallExecutorTheCall(t *testing.T) {
	exec := &recordingCallExecutor{}
	r := newCallTestRegistry(t, exec)

	res, err := r.ExecuteCall(context.Background(), lookupCall())
	require.NoError(t, err)

	require.Len(t, exec.calls, 1)
	got := exec.calls[0]
	assert.Equal(t, "lookup", got.Name)
	assert.Equal(t, "c1", got.ID)
	assert.Equal(t, types.Caller{ID: "u1", Claims: map[string]any{"tenant": "acme"}}, got.Caller)
	assert.JSONEq(t, `{"n":5}`, string(got.Args), "the executor receives the coerced args")
	assert.Empty(t, exec.plainArgs, "a CallExecutor is not given Execute")

	assert.Equal(t, "c1", res.ID)
	assert.Equal(t, "lookup", res.Name)
	assert.JSONEq(t, `{"via":"call"}`, string(res.Result))
	assert.Empty(t, res.Error)
}

// Execute builds a call with no ID and no caller, and a CallExecutor still
// takes it through ExecuteCall.
func TestRegistry_Execute_GivesCallExecutorACallWithoutCaller(t *testing.T) {
	exec := &recordingCallExecutor{}
	r := newCallTestRegistry(t, exec)

	res, err := r.Execute(context.Background(), "lookup", json.RawMessage(`{"n":"7"}`))
	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, ToolCall{Name: "lookup", Args: json.RawMessage(`{"n":7}`)}, exec.calls[0])
	assert.JSONEq(t, `{"via":"call"}`, string(res.Result))
}

func TestRegistry_ExecuteCallAsyncAdmitted_GivesCallExecutorTheCall(t *testing.T) {
	exec := &recordingCallExecutor{}
	r := newCallTestRegistry(t, exec)
	gate := &admitCounter{}

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), gate.admit)
	require.NoError(t, err)
	assert.Equal(t, 1, gate.calls)
	assert.Equal(t, ToolStatusComplete, res.Status)
	assert.JSONEq(t, `{"via":"call"}`, string(res.Content))

	require.Len(t, exec.calls, 1)
	assert.Equal(t, "c1", exec.calls[0].ID)
	assert.Equal(t, "u1", exec.calls[0].Caller.ID)
	assert.JSONEq(t, `{"n":5}`, string(exec.calls[0].Args))
}

func TestRegistry_ExecuteCallAsyncAdmitted_PrefersAsyncCallExecutor(t *testing.T) {
	exec := &asyncCallExecutor{}
	r := newCallTestRegistry(t, exec)

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), nil)
	require.NoError(t, err)
	assert.JSONEq(t, `{"via":"call_async"}`, string(res.Content))
	require.Len(t, exec.asyncCalls, 1)
	assert.Equal(t, "c1", exec.asyncCalls[0].ID)
	assert.Equal(t, "u1", exec.asyncCalls[0].Caller.ID)
	assert.JSONEq(t, `{"n":5}`, string(exec.asyncCalls[0].Args))
	assert.Empty(t, exec.asyncArgs, "ExecuteAsync is not used when ExecuteCallAsync exists")
	assert.Empty(t, exec.calls, "the sync entry point is not used on the async path")
}

// An executor that is AsyncToolExecutor and CallExecutor, but not
// AsyncCallExecutor, keeps suspending calls through ExecuteAsync on the async
// path, and takes ExecuteCall on the synchronous path.
func TestRegistry_AsyncAndCallExecutor_KeepsEachEntryPoint(t *testing.T) {
	exec := &asyncAndCallExecutor{}
	r := newCallTestRegistry(t, exec)

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), nil)
	require.NoError(t, err)
	assert.Equal(t, ToolStatusPending, res.Status)
	assert.Equal(t, []string{`{"n":5}`}, exec.asyncArgs)
	assert.Empty(t, exec.calls)

	syncRes, err := r.ExecuteCall(context.Background(), lookupCall())
	require.NoError(t, err)
	assert.JSONEq(t, `{"via":"call"}`, string(syncRes.Result))
	require.Len(t, exec.calls, 1)
	assert.Equal(t, "u1", exec.calls[0].Caller.ID)
}

// ToolCall's caller is never serialized.
func TestToolCall_CallerIsNotSerialized(t *testing.T) {
	b, err := json.Marshal(lookupCall())
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"lookup","args":{"n":"5"},"id":"c1"}`, string(b))
}
