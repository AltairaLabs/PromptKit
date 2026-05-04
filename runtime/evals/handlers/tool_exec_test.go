package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubExecutor is a minimal tools.Executor that returns a configured
// result and/or error. Lets tool_exec tests exercise the success/error
// paths without standing up a real MCP server or sandbox.
type stubExecutor struct {
	name      string
	result    json.RawMessage
	returnErr error
}

func (e *stubExecutor) Name() string { return e.name }

func (e *stubExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	if e.returnErr != nil {
		return nil, e.returnErr
	}
	if e.result != nil {
		return e.result, nil
	}
	return json.RawMessage(`{}`), nil
}

// newStubRegistry wires a registry where the named tool maps to a stub
// executor under a custom Mode. tool_exec dispatches by Mode → executor
// name, so registering the executor under a custom name and giving the
// descriptor the matching Mode is the cleanest test scaffold.
func newStubRegistry(t *testing.T, toolName string, exec *stubExecutor) *tools.Registry {
	t.Helper()
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	desc := &tools.ToolDescriptor{
		Name: toolName,
		Mode: exec.Name(),
		// The handler under test passes args verbatim; tool_exec doesn't
		// care about argument validation. Skip InputSchema to bypass.
	}
	require.NoError(t, registry.Register(desc))
	return registry
}

func evalCtxWithRegistry(registry *tools.Registry) *evals.EvalContext {
	return &evals.EvalContext{
		Metadata: map[string]any{"tool_registry": registry},
	}
}

func TestToolExec_Type(t *testing.T) {
	assert.Equal(t, "tool_exec", (&ToolExecHandler{}).Type())
}

func TestToolExec_RequiresToolParam(t *testing.T) {
	registry := newStubRegistry(t, "noop", &stubExecutor{name: "stub-pass"})
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, res.Score)
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "'tool' parameter is required")
}

func TestToolExec_NoRegistryConfigured(t *testing.T) {
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{"tool": "noop"})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "no metadata")
}

func TestToolExec_RegistryWrongType(t *testing.T) {
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(),
		&evals.EvalContext{Metadata: map[string]any{"tool_registry": "not a registry"}},
		map[string]any{"tool": "noop"},
	)
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "not a *tools.Registry")
}

func TestToolExec_SuccessPath(t *testing.T) {
	registry := newStubRegistry(t, "run_tests", &stubExecutor{
		name:   "stub-pass",
		result: json.RawMessage(`{"output": "PASS"}`),
	})
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "run_tests",
	})
	require.NoError(t, err)
	assert.Equal(t, 1.0, *res.Score)
	assert.Contains(t, res.Explanation, "succeeded")
}

func TestToolExec_ExecutorReturnsError(t *testing.T) {
	registry := newStubRegistry(t, "run_tests", &stubExecutor{
		name:      "stub-erroring",
		returnErr: errors.New("simulated tool error"),
	})
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "run_tests",
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score)
	// The handler treats Registry.Execute returning err *or* ToolResult.Error
	// as failure — both end up here. Either branch is acceptable; both are
	// covered by other tests.
	assert.Contains(t, res.Explanation, "run_tests")
}

func TestToolExec_PassesArgsToExecute(t *testing.T) {
	var capturedArgs json.RawMessage
	exec := &stubExecutor{
		name:   "capture",
		result: json.RawMessage(`{}`),
	}
	registry := tools.NewRegistry()
	registry.RegisterExecutor(&capturingExecutor{name: exec.name, captured: &capturedArgs})
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "Bash",
		Mode: exec.name,
	}))

	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "Bash",
		"args": map[string]any{"command": "echo hi", "description": "smoke"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1.0, *res.Score)

	var got map[string]any
	require.NoError(t, json.Unmarshal(capturedArgs, &got))
	assert.Equal(t, "echo hi", got["command"])
	assert.Equal(t, "smoke", got["description"])
}

// capturingExecutor records the args it received without erroring.
type capturingExecutor struct {
	name     string
	captured *json.RawMessage
}

func (e *capturingExecutor) Name() string { return e.name }

func (e *capturingExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	clone := make(json.RawMessage, len(args))
	copy(clone, args)
	*e.captured = clone
	return json.RawMessage(`{}`), nil
}
