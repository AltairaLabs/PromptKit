package handlers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	rtypes "github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeExecScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

func TestExecEvalHandler_Type(t *testing.T) {
	h := NewExecEvalHandler(&ExecEvalConfig{TypeName: "sentiment_check"})
	assert.Equal(t, "sentiment_check", h.Type())
}

func TestExecEvalHandler_Eval_Success(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo '{"score": 0.85, "detail": "positive sentiment", "data": {"polarity": 0.85}}'
`)

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "sentiment",
		Command:  script,
	})

	evalCtx := &evals.EvalContext{
		CurrentOutput: "I love this product!",
		TurnIndex:     1,
		Messages: []rtypes.Message{
			{Role: "user", Content: "Review this product"},
			{Role: "assistant", Content: "I love this product!"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{"language": "en"})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "sentiment", result.Type)
	require.NotNil(t, result.Score)
	assert.InDelta(t, 0.85, *result.Score, 0.001)
	assert.Equal(t, "positive sentiment", result.Explanation)
	assert.NotNil(t, result.Details)
}

func TestExecEvalHandler_Eval_ProcessFailure(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo "process error" >&2
exit 1
`)

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "failing_eval",
		Command:  script,
	})

	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	require.NoError(t, err) // Should not return error — returns result with explanation
	require.NotNil(t, result)
	assert.Contains(t, result.Explanation, "exec eval failed")
	assert.Contains(t, result.Explanation, "process error")
	assert.Contains(t, result.Error, "process error", "no measurement was taken: an Error, not a score of 0")
}

func TestExecEvalHandler_Eval_InvalidJSON(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo "not valid json"
`)

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "bad_eval",
		Command:  script,
	})

	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Explanation, "invalid JSON")
	assert.NotEmpty(t, result.Error)
}

func TestExecEvalHandler_Eval_WithTimeout(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
sleep 10
echo '{"score": 1.0}'
`)

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName:  "slow_eval",
		Command:   script,
		TimeoutMs: 100,
	})

	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	// sleep is a child of the script's shell and holds its stdout open after
	// the shell is killed; the timeout must still end the call.
	start := time.Now()
	result, err := h.Eval(context.Background(), evalCtx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Explanation, "exec eval failed")
	assert.Contains(t, result.Error, "timed out after 100ms")
	assert.Less(t, time.Since(start), time.Second, "a script's child process must not outlive the timeout")
}

func TestExecEvalHandler_Eval_DefaultTimeout(t *testing.T) {
	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "test",
		Command:  "/bin/echo",
	})
	assert.Equal(t, defaultExecEvalTimeout, h.timeout())
}

func TestExecEvalHandler_Eval_CustomTimeout(t *testing.T) {
	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName:  "test",
		Command:   "/bin/echo",
		TimeoutMs: 5000,
	})
	assert.Equal(t, 5*time.Second, h.timeout())
}

func TestExecEvalHandler_Eval_WithEnv(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo "{\"score\": 1.0, \"detail\": \"$EXEC_EVAL_TEST_VAR\"}"
`)

	t.Setenv("EXEC_EVAL_TEST_VAR", "test_value")

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "env_eval",
		Command:  script,
		Env:      []string{"EXEC_EVAL_TEST_VAR"},
	})

	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "test_value", result.Explanation)
}

func TestExecEvalHandler_Eval_WithArgs(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo "{\"score\": 1.0, \"detail\": \"$1\"}"
`)

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "args_eval",
		Command:  script,
		Args:     []string{"extra-arg"},
	})

	evalCtx := &evals.EvalContext{CurrentOutput: "test"}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "extra-arg", result.Explanation)
}

func TestExecEvalHandler_Eval_ZeroScore(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo '{"score": 0.0, "detail": "completely negative"}'
`)

	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "zero_eval",
		Command:  script,
	})

	evalCtx := &evals.EvalContext{CurrentOutput: "terrible"}

	result, err := h.Eval(context.Background(), evalCtx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Score)
	assert.InDelta(t, 0.0, *result.Score, 0.001)
}

func TestExecEvalHandler_BuildRequest(t *testing.T) {
	h := NewExecEvalHandler(&ExecEvalConfig{
		TypeName: "test_eval",
		Command:  "/bin/true",
	})

	evalCtx := &evals.EvalContext{
		CurrentOutput: "hello world",
		TurnIndex:     2,
		SessionID:     "sess-123",
		PromptID:      "prompt-456",
		Variables:     map[string]any{"lang": "en"},
		Metadata:      map[string]any{"env": "test"},
		Messages: []rtypes.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello world"},
		},
	}
	params := map[string]any{"threshold": 0.5}

	req := h.buildRequest(evalCtx, params)
	assert.Equal(t, "test_eval", req.Type)
	assert.Equal(t, "hello world", req.Content)
	assert.Equal(t, params, req.Params)
	assert.Equal(t, 2, req.Context.TurnIndex)
	assert.Equal(t, "sess-123", req.Context.SessionID)
	assert.Equal(t, "prompt-456", req.Context.PromptID)
	assert.Len(t, req.Context.Messages, 2)
}

func TestNewExecEvalHandler(t *testing.T) {
	cfg := ExecEvalConfig{
		TypeName:  "my_eval",
		Command:   "/usr/local/bin/eval",
		Args:      []string{"--mode", "strict"},
		Env:       []string{"API_KEY"},
		TimeoutMs: 10000,
	}

	h := NewExecEvalHandler(&cfg)
	assert.Equal(t, "my_eval", h.typeName)
	assert.Equal(t, "/usr/local/bin/eval", h.command)
	assert.Equal(t, []string{"--mode", "strict"}, h.args)
	assert.Equal(t, []string{"API_KEY"}, h.env)
	assert.Equal(t, 10000, h.timeoutMs)
}

func TestExecEvalHandler_Eval_MetricsAndData(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo '{"score": 0.5, "metrics": {"functional_score": 0.75, "security_score": 0.25}, "data": {"cwe": ["CWE-89"]}}'
`)
	h := NewExecEvalHandler(&ExecEvalConfig{TypeName: "baxbench", Command: script})

	result, err := h.Eval(context.Background(), &evals.EvalContext{}, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Error)
	assert.InDelta(t, 0.5, *result.Score, 1e-9)
	require.NotNil(t, result.MetricValue)
	assert.InDelta(t, 0.5, *result.MetricValue, 1e-9)
	assert.Equal(t, map[string]float64{"functional_score": 0.75, "security_score": 0.25},
		result.Details["metrics"])
	assert.Equal(t, []any{"CWE-89"}, result.Details["cwe"])
}

func TestExecEvalHandler_Eval_NonNumericMetricIsError(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo '{"score": 1, "metrics": {"functional_score": "high"}}'
`)
	h := NewExecEvalHandler(&ExecEvalConfig{TypeName: "baxbench", Command: script})

	result, err := h.Eval(context.Background(), &evals.EvalContext{}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "invalid JSON")
}

func TestExecEvalHandler_Eval_ReportedErrorKeepsData(t *testing.T) {
	script := writeExecScript(t, "eval.sh", `#!/bin/sh
echo '{"score": 0, "error": "app did not start within 60s", "data": {"log": "EADDRINUSE"}}'
`)
	h := NewExecEvalHandler(&ExecEvalConfig{TypeName: "baxbench", Command: script})

	result, err := h.Eval(context.Background(), &evals.EvalContext{}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Error, "app did not start within 60s")
	assert.Equal(t, "EADDRINUSE", result.Details["log"], "the program's evidence survives the error")
	assert.Nil(t, result.MetricValue, "nothing was measured")
}

func TestExecEvalHandler_EvalTimeoutIsTheBindingTimeout(t *testing.T) {
	h := NewExecEvalHandler(&ExecEvalConfig{TypeName: "t", Command: "/bin/echo", TimeoutMs: 300000})
	assert.Equal(t, 300*time.Second, h.EvalTimeout(nil))
	assert.Equal(t, defaultExecEvalTimeout,
		NewExecEvalHandler(&ExecEvalConfig{TypeName: "t2", Command: "/bin/echo"}).EvalTimeout(nil))
}

func TestJSONSafeMetadata_DropsOnlyUnencodableEntries(t *testing.T) {
	in := map[string]any{
		"sandbox": map[string]any{"container_id": "abc"},
		"live":    func() {},
		"ch":      make(chan int),
	}
	out := jsonSafeMetadata(in)
	assert.Equal(t, map[string]any{"sandbox": map[string]any{"container_id": "abc"}}, out)
	assert.Len(t, in, 3, "the host's map is not mutated")
	assert.Nil(t, jsonSafeMetadata(nil))
}
