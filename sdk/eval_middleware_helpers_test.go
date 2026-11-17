package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// dispatchTurnEvals dispatches turn-level evals as a completed turn does,
// loading the transcript itself. Nil-safe.
func (em *evalMiddleware) dispatchTurnEvals(ctx context.Context) {
	if em == nil {
		return
	}
	em.turnIndex.Add(1)
	em.dispatchAsync(ctx, "turn", em.runner.RunTurnEvals, nil)
}

// wait blocks until all in-flight turn eval goroutines have completed.
// Nil-safe.
func (em *evalMiddleware) wait() {
	if em == nil {
		return
	}
	em.wg.Wait()
}

// buildEvalContext builds the eval context for the current turn, loading the
// transcript itself.
func (em *evalMiddleware) buildEvalContext(ctx context.Context) *evals.EvalContext {
	return em.buildEvalContextWith(ctx, nil)
}

// dispatchSessionEvals runs the session evals over the stored transcript, as
// Close does.
func (em *evalMiddleware) dispatchSessionEvals(ctx context.Context) {
	em.dispatchSessionEvalsWith(ctx, em.closingSnapshot(ctx))
}

// IncrementTurn advances the turn counter by one. Safe for concurrent use.
func (d *sessionHookDispatcher) IncrementTurn() {
	if d == nil {
		return
	}
	d.turns.Add(1)
}

// lifecycleWait bounds how long a lifecycle test waits on Close, Complete or a
// turn: a regression that makes one hang fails the test in seconds instead of
// holding the suite (and the mutation gate) until its timeout.
const lifecycleWait = 5 * time.Second

// within runs fn and returns its error, failing the test if it has not
// returned within lifecycleWait.
func within(t *testing.T, what string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(lifecycleWait):
		t.Fatalf("%s did not return within %s", what, lifecycleWait)
		return nil
	}
}

// closeWithin closes conv, failing the test if Close hangs.
func closeWithin(t *testing.T, conv *Conversation) error {
	t.Helper()
	return within(t, "Close", conv.Close)
}

// A Close waiting on a turn completion that never finishes crashes the test
// run in seconds (see turnWaitLimit) rather than hanging every later Close.
func init() { turnWaitLimit = 10 * time.Second }
