package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newRepositoryCallRegistry registers base wrapped in a RepositoryToolExecutor
// whose repository answers "lookup" only for the arguments {"n": 99}.
func newRepositoryCallRegistry(t *testing.T, base Executor) *Registry {
	t.Helper()
	repo := NewFileToolResponseRepository("scenario", map[string][]MockToolResponseConfig{
		"lookup": {{CallArgs: map[string]any{"n": 99}, Result: map[string]any{"via": "repository"}}},
	})
	wrapped := NewRepositoryToolExecutor(base, repo, "scenario")
	r := NewRegistry()
	r.RegisterExecutor(wrapped)
	require.NoError(t, r.Register(&ToolDescriptor{
		Name: "lookup", Description: "lookup", Mode: wrapped.Name(),
		InputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`),
	}))
	return r
}

func TestRepositoryToolExecutor_ForwardsTheCallToACallExecutor(t *testing.T) {
	base := &recordingCallExecutor{}
	r := newRepositoryCallRegistry(t, base)

	res, err := r.ExecuteCall(context.Background(), lookupCall())
	require.NoError(t, err)

	assert.JSONEq(t, `{"via":"call"}`, string(res.Result))
	require.Len(t, base.calls, 1)
	assert.Equal(t, types.Caller{ID: "u1", Claims: map[string]any{"tenant": "acme"}}, base.calls[0].Caller)
	assert.Equal(t, "c1", base.calls[0].ID)
	assert.JSONEq(t, `{"n":5}`, string(base.calls[0].Args))
	assert.Empty(t, base.plainArgs)
}

func TestRepositoryToolExecutor_ForwardsTheCallToAnAsyncCallExecutor(t *testing.T) {
	base := &asyncCallExecutor{}
	r := newRepositoryCallRegistry(t, base)

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), nil)
	require.NoError(t, err)

	assert.Equal(t, ToolStatusComplete, res.Status)
	assert.JSONEq(t, `{"via":"call_async"}`, string(res.Content))
	require.Len(t, base.asyncCalls, 1)
	assert.Equal(t, types.Caller{ID: "u1", Claims: map[string]any{"tenant": "acme"}}, base.asyncCalls[0].Caller)
	assert.Equal(t, "c1", base.asyncCalls[0].ID)
}

// A base that can suspend a call but does not take the call keeps suspending
// it through the wrapper.
func TestRepositoryToolExecutor_KeepsAnAsyncExecutorsPendingResult(t *testing.T) {
	base := &asyncAndCallExecutor{}
	r := newRepositoryCallRegistry(t, base)

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), nil)
	require.NoError(t, err)

	assert.Equal(t, ToolStatusPending, res.Status)
	require.NotNil(t, res.PendingInfo)
	assert.Equal(t, "held", res.PendingInfo.Reason)
	assert.Equal(t, []string{`{"n":5}`}, base.asyncArgs)
}

// A synchronous base on the async path runs as on the synchronous one.
func TestRepositoryToolExecutor_RunsASyncBaseOnTheAsyncPath(t *testing.T) {
	base := &recordingCallExecutor{}
	r := newRepositoryCallRegistry(t, base)

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), nil)
	require.NoError(t, err)

	assert.Equal(t, ToolStatusComplete, res.Status)
	assert.JSONEq(t, `{"via":"call"}`, string(res.Content))
	require.Len(t, base.calls, 1)
	assert.Equal(t, "u1", base.calls[0].Caller.ID)
}

func TestRepositoryToolExecutor_RepositoryAnswerWinsOnEveryPath(t *testing.T) {
	base := &asyncCallExecutor{}
	r := newRepositoryCallRegistry(t, base)
	call := lookupCall()
	call.Args = json.RawMessage(`{"n":99}`)

	res, err := r.ExecuteCall(context.Background(), call)
	require.NoError(t, err)
	assert.JSONEq(t, `{"via":"repository"}`, string(res.Result))

	async, err := r.ExecuteCallAsyncAdmitted(context.Background(), call, nil)
	require.NoError(t, err)
	assert.Equal(t, ToolStatusComplete, async.Status)
	assert.JSONEq(t, `{"via":"repository"}`, string(async.Content))

	assert.Empty(t, base.calls)
	assert.Empty(t, base.asyncCalls)
	assert.Empty(t, base.asyncArgs)
}

// multimodalRepoBase returns content parts through ExecuteMultimodal.
type multimodalRepoBase struct{}

func (multimodalRepoBase) Name() string { return "mm" }
func (multimodalRepoBase) Execute(context.Context, *ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"via":"execute"}`), nil
}
func (multimodalRepoBase) ExecuteMultimodal(
	context.Context, *ToolDescriptor, json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	return json.RawMessage(`{"via":"multimodal"}`), []types.ContentPart{types.NewTextPart("part")}, nil
}

func TestRepositoryToolExecutor_ForwardsMultimodalParts(t *testing.T) {
	r := newRepositoryCallRegistry(t, multimodalRepoBase{})

	res, err := r.ExecuteCall(context.Background(), lookupCall())
	require.NoError(t, err)

	assert.JSONEq(t, `{"via":"multimodal"}`, string(res.Result))
	assert.Equal(t, []types.ContentPart{types.NewTextPart("part")}, res.Parts)
}
