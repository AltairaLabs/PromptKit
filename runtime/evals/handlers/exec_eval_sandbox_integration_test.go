package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// sandboxEvalHelperEnv makes this test binary act as a host-side exec eval
// program instead of running tests. The integration test below points an
// exec binding at os.Args[0], so the program it runs is real Go reading the
// real request off stdin — the same contract a BaxBench adapter implements.
const sandboxEvalHelperEnv = "PROMPTKIT_SANDBOX_EVAL_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(sandboxEvalHelperEnv) == "1" {
		os.Exit(runSandboxEvalHelper())
	}
	os.Exit(m.Run())
}

// runSandboxEvalHelper is the eval program: it reads the sandbox block from
// the request metadata, probes the app on its mapped port and reports.
func runSandboxEvalHelper() int {
	var req struct {
		Params  map[string]any `json:"params"`
		Context struct {
			Metadata struct {
				Sandbox struct {
					ContainerID string         `json:"container_id"`
					Ports       map[string]int `json:"ports"`
				} `json:"sandbox"`
				Run map[string]any `json:"run"`
			} `json:"metadata"`
		} `json:"context"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		return 2
	}
	if ms, ok := req.Params["sleep_ms"].(float64); ok {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	switch req.Params["mode"] {
	case "crash":
		fmt.Fprintln(os.Stderr, "harness exploded")
		return 1
	case "error":
		return writeHelperResponse(map[string]any{"score": 0, "error": "app did not start"})
	}

	sandbox := req.Context.Metadata.Sandbox
	port, ok := sandbox.Ports["5000"]
	if !ok {
		return writeHelperResponse(map[string]any{"error": "no host port for container port 5000"})
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return writeHelperResponse(map[string]any{"error": "app unreachable: " + err.Error()})
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	return writeHelperResponse(map[string]any{
		"score":   0.75,
		"detail":  "3 of 4 functional tests passed",
		"metrics": map[string]float64{"functional_score": 0.75, "security_score": 1},
		"data": map[string]any{
			"container_id": sandbox.ContainerID,
			"app_body":     string(body),
			"run_id":       req.Context.Metadata.Run["run_id"],
		},
	})
}

func writeHelperResponse(v map[string]any) int {
	if err := json.NewEncoder(os.Stdout).Encode(v); err != nil {
		return 3
	}
	return 0
}

// TestExecEval_SandboxScoring_Integration runs a host-side exec eval end to
// end: real subprocess, real HTTP app on a mapped port, sandbox details in
// metadata next to live host objects, the eval runner with a timeout shorter
// than the program takes, and the assertion wrapper deciding pass/fail.
func TestExecEval_SandboxScoring_Integration(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "app ok")
	}))
	t.Cleanup(app.Close)
	appURL, err := url.Parse(app.URL)
	require.NoError(t, err)
	var appPort int
	_, err = fmt.Sscanf(appURL.Port(), "%d", &appPort)
	require.NoError(t, err)

	t.Setenv(sandboxEvalHelperEnv, "1")
	registry := evals.NewEvalTypeRegistry()
	registry.Register(NewExecEvalHandler(&ExecEvalConfig{
		TypeName:  "sandbox_probe",
		Command:   os.Args[0],
		TimeoutMs: 10000,
	}))
	// The runner's own timeout is shorter than the program's sleep: the
	// binding's timeout_ms must win, or every result below is an Error.
	runner := evals.NewEvalRunner(registry, evals.WithTimeout(100*time.Millisecond))

	evalCtx := &evals.EvalContext{
		SessionID: "run-1",
		Metadata: map[string]any{
			"sandbox": map[string]any{
				"container_id": "c0ffee",
				"ports":        map[string]any{"5000": appPort},
			},
			"run": map[string]any{"run_id": "run-1", "trial": 0},
			// Live host objects sit next to the data; neither may stop the program.
			metadataKeyToolRegistry: tools.NewRegistry(),
			"live":                  func() {},
		},
	}
	slow := map[string]any{"sleep_ms": 300}
	assertion := func(id string, params map[string]any, threshold string) evals.EvalDef {
		return evals.EvalDef{
			ID: id, Type: evals.WrapperTypeAssertion, Trigger: evals.TriggerOnSessionComplete,
			Params: map[string]any{"eval_type": "sandbox_probe", "eval_params": params, threshold: 0.0},
		}
	}
	measure := assertion("measure", slow, "min_score")
	measure.Params["min_score"] = 0.7
	defs := []evals.EvalDef{
		{ID: "plain", Type: "sandbox_probe", Trigger: evals.TriggerOnSessionComplete, Params: slow},
		measure,
		assertion("reported_error", map[string]any{"mode": "error"}, "max_score"),
		assertion("crashed", map[string]any{"mode": "crash"}, "max_score"),
	}

	got := map[string]evals.EvalResult{}
	for _, r := range runner.RunSessionEvals(context.Background(), defs, evalCtx) {
		got[r.EvalID] = r
	}
	require.Len(t, got, len(defs))

	plain := got["plain"]
	require.Empty(t, plain.Error, "the program ran past the runner timeout and saw its sandbox")
	assert.InDelta(t, 0.75, *plain.Score, 1e-9)
	assert.Nil(t, plain.Passed, "a plain eval states no verdict")
	assert.Equal(t, map[string]float64{"functional_score": 0.75, "security_score": 1}, plain.Metrics)
	assert.Equal(t, "c0ffee", plain.Details["container_id"])
	assert.Equal(t, "app ok", plain.Details["app_body"], "the program reached the app through the mapped port")
	assert.Equal(t, "run-1", plain.Details["run_id"])

	require.NotNil(t, got["measure"].Passed)
	assert.True(t, *got["measure"].Passed, "0.75 >= min_score 0.7")

	for _, id := range []string{"reported_error", "crashed"} {
		r := got[id]
		assert.NotEmpty(t, r.Error, id)
		require.NotNil(t, r.Passed, id)
		assert.False(t, *r.Passed, "%s: no measurement must not pass max_score: 0", id)
	}
	assert.Contains(t, got["reported_error"].Error, "app did not start")
	assert.Contains(t, got["crashed"].Error, "harness exploded")
}
