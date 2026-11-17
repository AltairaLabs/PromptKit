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

// The wrapper never suspends a call: a base that could return pending runs
// its synchronous call entry point on the async path, as before the caller
// was forwarded.
func TestRepositoryToolExecutor_DoesNotExposeTheBasesAsyncEntryPoints(t *testing.T) {
	base := &asyncCallExecutor{}
	r := newRepositoryCallRegistry(t, base)

	res, err := r.ExecuteCallAsyncAdmitted(context.Background(), lookupCall(), nil)
	require.NoError(t, err)

	assert.Equal(t, ToolStatusComplete, res.Status)
	assert.JSONEq(t, `{"via":"call"}`, string(res.Content))
	require.Len(t, base.calls, 1)
	assert.Equal(t, "u1", base.calls[0].Caller.ID)
	assert.Empty(t, base.asyncCalls)
	assert.Empty(t, base.asyncArgs)
}

func TestRepositoryToolExecutor_RepositoryAnswerWins(t *testing.T) {
	base := &recordingCallExecutor{}
	r := newRepositoryCallRegistry(t, base)
	call := lookupCall()
	call.Args = json.RawMessage(`{"n":99}`)

	res, err := r.ExecuteCall(context.Background(), call)
	require.NoError(t, err)

	assert.JSONEq(t, `{"via":"repository"}`, string(res.Result))
	assert.Empty(t, base.calls)
	assert.Empty(t, base.plainArgs)
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

// A base that does not take the call runs Execute, as it always has.
func TestRepositoryToolExecutor_RunsExecuteOnABaseThatDoesNotTakeTheCall(t *testing.T) {
	r := newRepositoryCallRegistry(t, multimodalRepoBase{})

	res, err := r.ExecuteCall(context.Background(), lookupCall())
	require.NoError(t, err)

	assert.JSONEq(t, `{"via":"execute"}`, string(res.Result))
	assert.Empty(t, res.Parts)
}
