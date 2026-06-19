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

// TestToolExec_CapturesToolResultPayload verifies that the eval result's
// Details carries the parsed tool response under "result". This is what
// lets tool_exec serve as a non-gating measurement: emit a JSON metric
// from a tool (e.g. a diff-stats script in a sandbox), and the captured
// payload flows into the report for jq aggregation. Details is the
// right field because the AssertionEvalHandler wrapper overwrites Value
// with a boolean pass/fail when this eval is used as a conversation
// assertion — Details survives.
func TestToolExec_CapturesToolResultPayload(t *testing.T) {
	registry := newStubRegistry(t, "diff_stats", &stubExecutor{
		name:   "stub-metric",
		result: json.RawMessage(`{"loc_added": 12, "loc_removed": 3, "files_changed": 2}`),
	})
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "diff_stats",
	})
	require.NoError(t, err)
	require.NotNil(t, res.Details)
	payload, ok := res.Details["result"].(map[string]any)
	require.True(t, ok, "Details['result'] should be map[string]any, got %T", res.Details["result"])
	assert.Equal(t, float64(12), payload["loc_added"])
	assert.Equal(t, float64(3), payload["loc_removed"])
	assert.Equal(t, float64(2), payload["files_changed"])
}

// TestToolExec_DoublePassParsesNestedJSONString verifies that when
// the tool returns a JSON-encoded string whose contents are themselves
// valid JSON (the common Bash-stdout case for shell metric scripts),
// the captured payload surfaces as a structured map rather than an
// escaped string blob.
func TestToolExec_DoublePassParsesNestedJSONString(t *testing.T) {
	// Outer encode: the tool returns "{\"loc\":12}\n" (a JSON string).
	registry := newStubRegistry(t, "diff_stats", &stubExecutor{
		name:   "stub-bash",
		result: json.RawMessage(`"{\"loc\":12,\"files\":2}\n"`),
	})
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "diff_stats",
	})
	require.NoError(t, err)
	require.NotNil(t, res.Details)

	payload, ok := res.Details["result"].(map[string]any)
	require.True(t, ok, "expected double-decoded map, got %T", res.Details["result"])
	assert.Equal(t, float64(12), payload["loc"])
	assert.Equal(t, float64(2), payload["files"])
}

// TestToolExec_NonJSONStringStaysString verifies that a plain string
// payload (not JSON) flows through as the original string instead of
// being misclassified.
func TestToolExec_NonJSONStringStaysString(t *testing.T) {
	registry := newStubRegistry(t, "echo", &stubExecutor{
		name:   "stub-text",
		result: json.RawMessage(`"hello world"`),
	})
	h := &ToolExecHandler{}
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "echo",
	})
	require.NoError(t, err)
	require.Equal(t, "hello world", res.Details["result"])
}

// TestToolExec_SuccessPatternMatchesOutput verifies that when success_pattern
// is set, the gate passes only when the tool output matches the regex.
func TestToolExec_SuccessPatternMatchesOutput(t *testing.T) {
	// Stub returns a JSON string payload that contains the sentinel.
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-sentinel",
		result: json.RawMessage(`"all tests passed\n__GATE_OK__\n"`),
	})
	h := &ToolExecHandler{}

	// Pattern present in output → pass.
	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":            "Bash",
		"success_pattern": "__GATE_OK__",
		"args":            map[string]any{"command": "echo ok", "description": "smoke"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1.0, *res.Score, "output contains sentinel → should pass")
	assert.Contains(t, res.Explanation, "succeeded")
}

// TestToolExec_SuccessPatternMissingFromOutput verifies a gate fails when the
// sentinel is absent from the tool output.
func TestToolExec_SuccessPatternMissingFromOutput(t *testing.T) {
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-no-sentinel",
		result: json.RawMessage(`"exit: 1\nsome failure output\n"`),
	})
	h := &ToolExecHandler{}

	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":            "Bash",
		"success_pattern": "__GATE_OK__",
		"args":            map[string]any{"command": "false", "description": "will fail"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score, "sentinel absent → should fail")
	assert.Contains(t, res.Explanation, "success_pattern")
}

// TestToolExec_FailurePatternMatchesOutput verifies that failure_pattern causes
// a fail when the pattern is found in the output.
func TestToolExec_FailurePatternMatchesOutput(t *testing.T) {
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-fail-pattern",
		result: json.RawMessage(`"exit: 1\nERROR: something went wrong\n"`),
	})
	h := &ToolExecHandler{}

	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":            "Bash",
		"failure_pattern": `exit: [1-9]`,
		"args":            map[string]any{"command": "false", "description": "will fail"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score, "failure pattern matched → should fail")
	assert.Contains(t, res.Explanation, "failure_pattern")
}

// TestToolExec_FailurePatternAbsentFromOutput verifies the gate passes when
// failure_pattern does not match the output.
func TestToolExec_FailurePatternAbsentFromOutput(t *testing.T) {
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-clean-output",
		result: json.RawMessage(`"all good\n"`),
	})
	h := &ToolExecHandler{}

	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":            "Bash",
		"failure_pattern": `exit: [1-9]`,
		"args":            map[string]any{"command": "echo ok", "description": "smoke"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1.0, *res.Score, "failure pattern absent → should pass")
	assert.Contains(t, res.Explanation, "succeeded")
}

// TestToolExec_NeitherPatternSetUnchanged verifies baseline pass behavior is
// unaffected when neither success_pattern nor failure_pattern is configured.
func TestToolExec_NeitherPatternSetUnchanged(t *testing.T) {
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-plain",
		result: json.RawMessage(`"exit: 1\nfailed\n"`),
	})
	h := &ToolExecHandler{}

	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool": "Bash",
		"args": map[string]any{"command": "false", "description": "would fail in shell"},
	})
	require.NoError(t, err)
	// No pattern set → tool-level success is the only criterion → pass
	assert.Equal(t, 1.0, *res.Score, "no patterns set → unchanged pass behavior")
}

// TestToolExec_BadSuccessPatternFails verifies that an invalid regex in
// success_pattern fails the assertion with a clear message rather than panicking.
func TestToolExec_BadSuccessPatternFails(t *testing.T) {
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-bad-rx",
		result: json.RawMessage(`"ok"`),
	})
	h := &ToolExecHandler{}

	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":            "Bash",
		"success_pattern": `[invalid`,
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "success_pattern")
}

// TestToolExec_BadFailurePatternFails verifies that an invalid regex in
// failure_pattern fails the assertion with a clear message rather than panicking.
func TestToolExec_BadFailurePatternFails(t *testing.T) {
	registry := newStubRegistry(t, "Bash", &stubExecutor{
		name:   "stub-bad-rx2",
		result: json.RawMessage(`"ok"`),
	})
	h := &ToolExecHandler{}

	res, err := h.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":            "Bash",
		"failure_pattern": `[invalid`,
	})
	require.NoError(t, err)
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "failure_pattern")
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
