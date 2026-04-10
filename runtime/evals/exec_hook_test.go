package evals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeTempScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exec hook smoke tests require a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body+"\n"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestExecEvalHook_PipesResultJSONOnStdin(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "captured.json")
	script := writeTempScript(t, `cat > "`+out+`"`)

	hook := NewExecEvalHook(&ExecEvalHookConfig{
		Name:    "capture",
		Command: script,
	})

	score := 0.75
	result := &EvalResult{EvalID: "e1", Type: "contains", Score: &score}
	hook.OnEvalResult(context.Background(), &EvalDef{ID: "e1"}, &EvalContext{SessionID: "s1"}, result)

	payload, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}

	var got EvalResult
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("captured payload was not valid JSON: %v\npayload=%s", err, string(payload))
	}
	if got.EvalID != "e1" || got.Type != "contains" {
		t.Errorf("unexpected payload: %+v", got)
	}
	if got.Score == nil || *got.Score != 0.75 {
		t.Errorf("score not round-tripped, got %+v", got.Score)
	}
}

func TestExecEvalHook_NonZeroExitIsSwallowed(t *testing.T) {
	// A hook that exits non-zero must not panic or crash the caller —
	// eval hooks are fire-and-forget.
	script := writeTempScript(t, "cat > /dev/null; exit 17")
	hook := NewExecEvalHook(&ExecEvalHookConfig{
		Name:    "failing",
		Command: script,
	})

	score := 1.0
	// Should not panic or propagate the error in any way observable here.
	hook.OnEvalResult(context.Background(),
		&EvalDef{ID: "e1"}, &EvalContext{}, &EvalResult{EvalID: "e1", Score: &score})
}

func TestExecEvalHook_TimeoutHonored(t *testing.T) {
	// A command that sleeps longer than the timeout should be killed and
	// the caller should return promptly.
	script := writeTempScript(t, "sleep 30")
	hook := NewExecEvalHook(&ExecEvalHookConfig{
		Name:      "slow",
		Command:   script,
		TimeoutMs: 100, // 100ms
	})

	done := make(chan struct{})
	go func() {
		score := 1.0
		hook.OnEvalResult(context.Background(),
			&EvalDef{ID: "e1"}, &EvalContext{}, &EvalResult{Score: &score})
		close(done)
	}()

	select {
	case <-done:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("ExecEvalHook did not return within 5s despite 100ms timeout")
	}
}

func TestExecEvalHook_Name(t *testing.T) {
	h := NewExecEvalHook(&ExecEvalHookConfig{Name: "foo", Command: "/bin/true"})
	if h.Name() != "foo" {
		t.Errorf("Name() = %q, want %q", h.Name(), "foo")
	}
}

func TestExecEvalHook_PassesArgsAndEnv(t *testing.T) {
	// The script writes $EVAL_HOOK_TEST_VAR and $1 to a file so the test
	// can verify that args and env were forwarded to the subprocess.
	dir := t.TempDir()
	out := filepath.Join(dir, "captured.txt")
	script := writeTempScript(t, `cat > /dev/null; printf 'arg=%s env=%s\n' "$1" "$EVAL_HOOK_TEST_VAR" > "`+out+`"`)

	if err := os.Setenv("EVAL_HOOK_TEST_VAR", "from-env"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("EVAL_HOOK_TEST_VAR") })

	hook := NewExecEvalHook(&ExecEvalHookConfig{
		Name:    "with-args-env",
		Command: script,
		Args:    []string{"hello-arg"},
		Env:     []string{"EVAL_HOOK_TEST_VAR"},
	})

	score := 1.0
	hook.OnEvalResult(context.Background(), &EvalDef{ID: "e1"}, &EvalContext{}, &EvalResult{Score: &score})

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	want := "arg=hello-arg env=from-env\n"
	if string(got) != want {
		t.Errorf("captured output = %q, want %q", string(got), want)
	}
}

func TestExecEvalHook_CommandNotFound(t *testing.T) {
	// Non-existent command — subprocess failure must be swallowed.
	hook := NewExecEvalHook(&ExecEvalHookConfig{
		Name:    "missing",
		Command: "/this/path/does/not/exist/definitely",
	})
	score := 1.0
	hook.OnEvalResult(context.Background(),
		&EvalDef{ID: "e1"}, &EvalContext{}, &EvalResult{Score: &score})
}
