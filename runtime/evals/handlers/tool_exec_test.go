package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
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

// runToolExec executes tool_exec against a stub tool returning payload.
func runToolExec(t *testing.T, payload string, params map[string]any) *evals.EvalResult {
	t.Helper()
	registry := newStubRegistry(t, "run_tests", &stubExecutor{
		name:   "stub-score",
		result: json.RawMessage(payload),
	})
	params["tool"] = "run_tests"
	res, err := (&ToolExecHandler{}).Eval(context.Background(), evalCtxWithRegistry(registry), params)
	require.NoError(t, err)
	require.NotNil(t, res.Score)
	return res
}

func TestToolExec_ScorePathExtractsNumber(t *testing.T) {
	res := runToolExec(t, `{"pass_rate": 0.75, "total": 8}`, map[string]any{
		"score_path": "pass_rate",
	})
	assert.InDelta(t, 0.75, *res.Score, 1e-9)
	require.NotNil(t, res.MetricValue)
	assert.InDelta(t, 0.75, *res.MetricValue, 1e-9)
	assert.InDelta(t, 0.75, res.Details["raw_score"], 1e-9)
	assert.NotNil(t, res.Details["result"], "the parsed payload stays in details")
	assert.Empty(t, res.Error)
}

func TestToolExec_ScorePathReadsNestedJSONString(t *testing.T) {
	// Shell wrappers return stdout as a JSON string holding the JSON payload.
	res := runToolExec(t, `"{\"summary\":{\"passed\":3,\"rate\":60}}\n"`, map[string]any{
		"score_path":  "summary.rate",
		"score_scale": 100,
	})
	assert.InDelta(t, 0.6, *res.Score, 1e-9)
	assert.InDelta(t, 60.0, res.Details["raw_score"], 1e-9)
}

func TestToolExec_ScorePatternExtractsCapture(t *testing.T) {
	res := runToolExec(t, `"running...\nPASS_RATE=87.5\ndone\n"`, map[string]any{
		"score_pattern": `PASS_RATE=([0-9.]+)`,
		"score_scale":   100.0,
	})
	assert.InDelta(t, 0.875, *res.Score, 1e-9)
	assert.InDelta(t, 87.5, res.Details["raw_score"], 1e-9)
}

func TestToolExec_ScoreIsNotClampedOrThresholded(t *testing.T) {
	// A findings count is a raw measurement; the assertion wrapper's
	// max_score decides what is acceptable.
	res := runToolExec(t, `{"findings": 3}`, map[string]any{"score_path": "findings"})
	assert.InDelta(t, 3.0, *res.Score, 1e-9)
	assert.Nil(t, res.Passed)
}

func TestToolExec_ScoreExtractionFailureIsError(t *testing.T) {
	cases := map[string]struct {
		payload string
		params  map[string]any
		want    string
	}{
		"path missing":       {`{"other": 1}`, map[string]any{"score_path": "pass_rate"}, "no value"},
		"path boolean":       {`{"pass_rate": true}`, map[string]any{"score_path": "pass_rate"}, "not a number"},
		"path non-numeric":   {`{"pass_rate": "high"}`, map[string]any{"score_path": "pass_rate"}, "not a number"},
		"payload not JSON":   {`"plain text"`, map[string]any{"score_path": "pass_rate"}, "use score_pattern"},
		"pattern no match":   {`"nothing here"`, map[string]any{"score_pattern": `RATE=(\d+)`}, "did not match"},
		"capture non-number": {`"RATE=abc"`, map[string]any{"score_pattern": `RATE=(\w+)`}, "not a number"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res := runToolExec(t, tc.payload, tc.params)
			assert.Equal(t, 0.0, *res.Score)
			assert.Contains(t, res.Explanation, tc.want)
			assert.NotEmpty(t, res.Error, "a miss is no measurement; a 0 would pass a max_score gate")
		})
	}
}

func TestToolExec_ToolErrorScoresZeroWithScorePath(t *testing.T) {
	res := runToolExec(t, `"exit: 1\nPASS_RATE=90\n"`, map[string]any{
		"score_pattern":   `PASS_RATE=([0-9.]+)`,
		"failure_pattern": `exit: [1-9]`,
	})
	assert.Equal(t, 0.0, *res.Score)
	assert.Contains(t, res.Explanation, "failure_pattern")
}

func TestToolExec_ScoreConfigErrors(t *testing.T) {
	cases := map[string]struct {
		params map[string]any
		want   string
	}{
		"both set":       {map[string]any{"score_path": "a", "score_pattern": `a=(\d)`}, "not both"},
		"bad path":       {map[string]any{"score_path": "a[["}, "score_path"},
		"bad regex":      {map[string]any{"score_pattern": `[invalid`}, "score_pattern"},
		"no capture":     {map[string]any{"score_pattern": `RATE=\d+`}, "one capture group"},
		"two captures":   {map[string]any{"score_pattern": `(R)=(\d+)`}, "one capture group"},
		"zero scale":     {map[string]any{"score_path": "a", "score_scale": 0}, "score_scale"},
		"string scale":   {map[string]any{"score_path": "a", "score_scale": "100"}, "score_scale"},
		"scale alone":    {map[string]any{"score_scale": 100}, "score_scale"},
		"threshold here": {map[string]any{"score_path": "a", "min_score": 0.5}, "type: assertion"},
		"bad gate regex": {map[string]any{"score_path": "a", "success_pattern": `[bad`}, "success_pattern"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res := runToolExec(t, `{"a": 1}`, tc.params)
			assert.Equal(t, 0.0, *res.Score)
			assert.Contains(t, res.Error, tc.want)
		})
	}
}

func TestToolExec_ScoreConfigErrorSkipsToolCall(t *testing.T) {
	var captured json.RawMessage
	exec := &capturingExecutor{name: "stub-capture-score", captured: &captured}
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{Name: "run_tests", Mode: exec.Name()}))

	res, err := (&ToolExecHandler{}).Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
		"tool":          "run_tests",
		"score_pattern": `[invalid`,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Error)
	assert.Nil(t, captured, "a misconfigured check must not run the (possibly expensive) tool")
}

// TestToolExec_AssertionMinScoreGatesExtractedScore runs tool_exec through the
// real `type: assertion` wrapper: min_score is where the pass boundary lives,
// and the same 0.75 pass rate passes at 0.7 and fails at 0.8.
func TestToolExec_AssertionMinScoreGatesExtractedScore(t *testing.T) {
	registry := newStubRegistry(t, "run_tests", &stubExecutor{
		name:   "stub-assert",
		result: json.RawMessage(`{"pass_rate": 0.75}`),
	})
	wrapper, err := evals.NewEvalTypeRegistry().Get(evals.WrapperTypeAssertion)
	require.NoError(t, err)

	for _, tc := range []struct {
		min  float64
		pass bool
	}{{0.7, true}, {0.8, false}} {
		res, evalErr := wrapper.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
			"eval_type":   "tool_exec",
			"min_score":   tc.min,
			"eval_params": map[string]any{"tool": "run_tests", "score_path": "pass_rate"},
		})
		require.NoError(t, evalErr)
		require.NotNil(t, res.Passed)
		assert.Equal(t, tc.pass, *res.Passed, "min_score %v", tc.min)
		assert.InDelta(t, 0.75, *res.Score, 1e-9)
	}
}

func TestToolExec_ScorePathAcceptsNumericString(t *testing.T) {
	res := runToolExec(t, `{"rate": " 87.5 "}`, map[string]any{"score_path": "rate", "score_scale": 100})
	assert.InDelta(t, 0.875, *res.Score, 1e-9)
	assert.Empty(t, res.Error)
}

// In score mode a failed call took no measurement, so it is an Error; in
// binary mode the failed call is the measurement and stays a plain 0.
func TestToolExec_FailedCallIsErrorOnlyInScoreMode(t *testing.T) {
	payload := `"exit: 1\nPASS_RATE=90\n"`
	scored := runToolExec(t, payload, map[string]any{
		"score_pattern":   `PASS_RATE=([0-9.]+)`,
		"failure_pattern": `exit: [1-9]`,
	})
	assert.Equal(t, 0.0, *scored.Score)
	assert.Contains(t, scored.Error, "failure_pattern")

	binary := runToolExec(t, payload, map[string]any{"failure_pattern": `exit: [1-9]`})
	assert.Equal(t, 0.0, *binary.Score)
	assert.Empty(t, binary.Error)
}

// TestToolExec_AssertionMaxScoreFailsWhenNothingMeasured pins the fail-open
// case: a findings count gated by max_score must not pass when the tool
// broke and its placeholder score is 0.
func TestToolExec_AssertionMaxScoreFailsWhenNothingMeasured(t *testing.T) {
	wrapper, err := evals.NewEvalTypeRegistry().Get(evals.WrapperTypeAssertion)
	require.NoError(t, err)
	for name, payload := range map[string]string{
		"measured zero": `{"findings": 0}`,
		"no findings":   `{"error": "semgrep crashed"}`,
	} {
		registry := newStubRegistry(t, "run_semgrep", &stubExecutor{name: "stub-" + name, result: json.RawMessage(payload)})
		res, evalErr := wrapper.Eval(context.Background(), evalCtxWithRegistry(registry), map[string]any{
			"eval_type":   "tool_exec",
			"max_score":   0.0,
			"eval_params": map[string]any{"tool": "run_semgrep", "score_path": "findings"},
		})
		require.NoError(t, evalErr)
		require.NotNil(t, res.Passed)
		assert.Equal(t, name == "measured zero", *res.Passed, name)
	}
}

func TestToolExec_ValidateParams(t *testing.T) {
	h := &ToolExecHandler{}
	assert.NoError(t, h.ValidateParams(map[string]any{"tool": "t", "score_path": "a.b"}))
	assert.ErrorContains(t, h.ValidateParams(map[string]any{}), "'tool'")
	assert.ErrorContains(t, h.ValidateParams(map[string]any{"tool": "t", "score_path": "a[["}), "score_path")
	assert.ErrorContains(t, h.ValidateParams(map[string]any{"tool": "t", "failure_pattern": "[x"}), "failure_pattern")
	assert.ErrorContains(t, h.ValidateParams(map[string]any{"tool": "t", "max_score": 1}), "type: assertion")
}

func TestToolExec_EvalTimeoutIsTimeoutSeconds(t *testing.T) {
	h := &ToolExecHandler{}
	assert.Equal(t, defaultToolExecTimeout, h.EvalTimeout(map[string]any{}))
	assert.Equal(t, 300*time.Second, h.EvalTimeout(map[string]any{"timeout_seconds": 300}))
}
