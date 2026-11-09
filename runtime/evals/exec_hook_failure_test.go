package evals

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// captureLogs routes the package logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return &buf
}

// TestExecEvalHook_SubprocessFailureIsLoggedNotRaised pins the
// fire-and-forget contract: a hook that exits non-zero, or whose command does
// not exist, returns normally and reports the failure as a warning naming the
// hook and the eval.
func TestExecEvalHook_SubprocessFailureIsLoggedNotRaised(t *testing.T) {
	tests := []struct {
		name    string
		command func(t *testing.T) string
	}{
		{"non-zero exit", func(t *testing.T) string { return writeTempScript(t, "cat > /dev/null; exit 17") }},
		{"command not found", func(*testing.T) string { return "/this/path/does/not/exist/definitely" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureLogs(t)
			hook := NewExecEvalHook(&ExecEvalHookConfig{Name: "failing-hook", Command: tt.command(t)})
			score := 1.0
			hook.OnEvalResult(context.Background(),
				&EvalDef{ID: "e1"}, &EvalContext{}, &EvalResult{EvalID: "e1", Score: &score})

			out := buf.String()
			for _, want := range []string{"exec eval hook: subprocess failed", "hook=failing-hook", "eval_id=e1"} {
				if !strings.Contains(out, want) {
					t.Errorf("log missing %q:\n%s", want, out)
				}
			}
		})
	}
}

// workerCountingHandler is an eval handler that counts how often it runs.
type workerCountingHandler struct{ calls atomic.Int32 }

func (h *workerCountingHandler) Type() string { return "counting" }

func (h *workerCountingHandler) Eval(context.Context, *EvalContext, map[string]any) (*EvalResult, error) {
	h.calls.Add(1)
	score := 1.0
	return &EvalResult{EvalID: "counting", Score: &score}, nil
}

// TestEvalWorker_NilResultWriter pins that a worker built without a result
// writer still runs the eval for each event and keeps its subscription open:
// Start ends only with the context, not with a handler error.
func TestEvalWorker_NilResultWriter(t *testing.T) {
	handler := &workerCountingHandler{}
	runner := NewEvalRunner(newTestRegistry(handler))
	payload := makePayload(t,
		[]EvalDef{{ID: "e1", Type: "counting", Trigger: TriggerEveryTurn}},
		&EvalContext{SessionID: "s1"},
	)
	sub := &mockSubscriber{turnEvents: [][]byte{payload, payload}}
	worker := NewEvalWorker(runner, sub, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := worker.Start(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start = %v, want the context deadline (no handler error)", err)
	}
	if got := handler.calls.Load(); got != 2 {
		t.Errorf("eval ran %d times, want 2 (once per turn event)", got)
	}
}
