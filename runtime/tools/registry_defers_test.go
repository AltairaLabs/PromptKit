package tools_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// declaredDeferrer defers a call without running it when defers is set
// (tools.DeferringExecutor); otherwise it runs it, returning pending from
// ExecuteAsync when pendingOnRun is set. runs counts the calls it ran.
type declaredDeferrer struct {
	defers       bool
	pendingOnRun bool
	runs         atomic.Int32
}

func (d *declaredDeferrer) Name() string { return "declared-deferrer" }

func (d *declaredDeferrer) Defer(*tools.ToolDescriptor, tools.ToolCall) (*tools.ToolExecutionResult, bool) {
	if !d.defers {
		return nil, false
	}
	return &tools.ToolExecutionResult{
		Status: tools.ToolStatusPending, PendingInfo: &tools.PendingToolInfo{Reason: "declared"},
	}, true
}

func (d *declaredDeferrer) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	d.runs.Add(1)
	return json.RawMessage(`{}`), nil
}

func (d *declaredDeferrer) ExecuteAsync(
	context.Context, *tools.ToolDescriptor, json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	d.runs.Add(1)
	if d.pendingOnRun {
		return &tools.ToolExecutionResult{Status: tools.ToolStatusPending}, nil
	}
	return &tools.ToolExecutionResult{Status: tools.ToolStatusComplete, Content: json.RawMessage(`{}`)}, nil
}

func deferRegistry(t *testing.T, exec *declaredDeferrer, opts ...tools.RegistryOption) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry(opts...)
	reg.RegisterExecutor(exec)
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name: "ask", Description: "ask", Mode: exec.Name(),
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{}`),
	}))
	return reg
}

// A call its executor defers is returned pending without running the
// executor, without asking the admission gate, and without counting under
// the registry's rate limit; any other call is run and asked about.
func TestRegistry_ExecuteAsyncAdmittedDefersWithoutRunningOrCounting(t *testing.T) {
	for _, defers := range []bool{true, false} {
		exec := &declaredDeferrer{defers: defers}
		reg := deferRegistry(t, exec, tools.WithRateLimit(1))
		asked := 0
		gate := func() error { asked++; return nil }
		res, err := reg.ExecuteAsyncAdmitted(context.Background(), "ask", json.RawMessage(`{}`), gate)
		require.NoError(t, err)
		second, err := reg.ExecuteAsyncAdmitted(context.Background(), "ask", json.RawMessage(`{}`), gate)
		require.NoError(t, err)
		if defers {
			assert.Equal(t, tools.ToolStatusPending, res.Status)
			assert.Equal(t, "declared", res.PendingInfo.Reason)
			assert.Equal(t, tools.ToolStatusPending, second.Status, "the registry's limit did not count the first")
			assert.Zero(t, asked, "a deferred call is admitted when it is settled")
			assert.Zero(t, exec.runs.Load(), "the executor does not run a deferred call")
		} else {
			assert.Equal(t, tools.ToolStatusComplete, res.Status)
			assert.Equal(t, tools.ToolStatusFailed, second.Status, "the registry's limit counted the first")
			assert.Equal(t, 1, asked)
			assert.EqualValues(t, 1, exec.runs.Load())
		}
	}
}

// A call an executor defers only once it runs is counted by the registry's
// rate limit when the host settles it, not at dispatch.
func TestRegistry_PendingResultIsNotCountedAtDispatch(t *testing.T) {
	exec := &declaredDeferrer{pendingOnRun: true}
	reg := deferRegistry(t, exec, tools.WithRateLimit(1))
	for range 2 {
		res, err := reg.ExecuteAsync(context.Background(), "ask", json.RawMessage(`{}`))
		require.NoError(t, err)
		assert.Equal(t, tools.ToolStatusPending, res.Status)
	}
	assert.EqualValues(t, 2, exec.runs.Load())
	require.NoError(t, reg.AdmitSettled("ask", nil))
	assert.ErrorIs(t, reg.AdmitSettled("ask", nil), tools.ErrRateLimitExceeded, "counted once, when settled")
}

// AdmitSettled counts a settled call under the registry's rate limit and the
// caller's admission; neither counts a call the other refuses.
func TestRegistry_AdmitSettled(t *testing.T) {
	reg := deferRegistry(t, &declaredDeferrer{}, tools.WithRateLimit(1))
	refuse := func() error { return assert.AnError }
	assert.ErrorIs(t, reg.AdmitSettled("ask", refuse), assert.AnError)
	asked := 0
	require.NoError(t, reg.AdmitSettled("ask", func() error { asked++; return nil }),
		"the refused call took no room")
	assert.Equal(t, 1, asked)
	assert.ErrorIs(t, reg.AdmitSettled("ask", nil), tools.ErrRateLimitExceeded)

	unlimited := deferRegistry(t, &declaredDeferrer{})
	assert.ErrorIs(t, unlimited.AdmitSettled("ask", refuse), assert.AnError, "admit decides without a limit")
	assert.NoError(t, unlimited.AdmitSettled("ask", nil))
}

// AdmitSettledRelease counts a settled call as AdmitSettled does, and its
// release takes that count back; a refused call gets no release.
func TestRegistry_AdmitSettledRelease(t *testing.T) {
	reg := deferRegistry(t, &declaredDeferrer{}, tools.WithRateLimit(1))
	release, err := reg.AdmitSettledRelease("ask", nil)
	require.NoError(t, err)
	refused, err := reg.AdmitSettledRelease("ask", nil)
	assert.ErrorIs(t, err, tools.ErrRateLimitExceeded)
	assert.Nil(t, refused)
	release()
	again, err := reg.AdmitSettledRelease("ask", nil)
	require.NoError(t, err, "the released call gave its room back")
	assert.NotNil(t, again)
}
