package stage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runOnce runs p on one user message and returns the error elements the
// caller gets.
func runOnce(t *testing.T, p *StreamPipeline) []error {
	t.Helper()
	in := make(chan StreamElement, 1)
	in <- userElement("hi")
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)
	return errorElements(out)
}

// TestExecute_EmitThenReturnWrapDeliveredOnce: a stage that emits err and
// returns a wrap of it fails once, as far as the caller is concerned.
func TestExecute_EmitThenReturnWrapDeliveredOnce(t *testing.T) {
	wrapper := NewStageFunc("stt", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			out <- NewErrorElement(errStageBoom)
			return fmt.Errorf("stt: %w", errStageBoom)
		})
	p, err := NewPipelineBuilder().Chain(wrapper, NewPassthroughStage("sink")).Build()
	require.NoError(t, err)

	errs := runOnce(t, p)

	require.Len(t, errs, 1, "the failure reaches the caller once")
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecute_SameBareSentinelFromAnotherStageIsNotTheFailure: stage b fails
// with a bare io.EOF and stage c reports a bare io.EOF of its own. They are
// two errors from two stages; the caller gets both, in either order.
func TestExecute_SameBareSentinelFromAnotherStageIsNotTheFailure(t *testing.T) {
	failing := NewStageFunc("b", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			return io.EOF
		})
	reporter := func(before bool) *StageFunc {
		return NewStageFunc("c", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				if before {
					out <- NewErrorElement(io.EOF)
				}
				for e := range in {
					out <- e
				}
				if !before {
					out <- NewErrorElement(io.EOF)
				}
				return nil
			})
	}
	for _, before := range []bool{true, false} {
		p, err := NewPipelineBuilder().Chain(failing, reporter(before)).Build()
		require.NoError(t, err)

		errs := runOnce(t, p)

		require.Len(t, errs, 2, "report before the failure: %v", before)
		var stageErr *StageError
		failures := 0
		for _, e := range errs {
			if errors.As(e, &stageErr) && stageErr.StageName == "b" {
				failures++
			}
		}
		assert.Equal(t, 1, failures, "stage b's failure reaches the caller once (report before: %v)", before)
	}
}

// TestExecute_TimeoutForwardedThenReturnedIsOneFailure: under ExecutionTimeout
// the first stage times out, and a later stage forwards its signal and then
// returns its own context error. The turn failed once: one failure reaches
// the caller.
func TestExecute_TimeoutForwardedThenReturnedIsOneFailure(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = 5 * time.Millisecond
	config.IdleTimeout = 0
	forwardThenCtxErr := NewStageFunc("s2", StageTypeTransform,
		func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				out <- e
			}
			<-ctx.Done()
			return ctx.Err()
		})
	p, err := NewPipelineBuilderWithConfig(config).
		Chain(waitForCtxStage("s1"), forwardThenCtxErr, NewPassthroughStage("sink")).Build()
	require.NoError(t, err)

	for i := 0; i < 20; i++ {
		errs := runOnce(t, p)

		require.Len(t, errs, 1, "run %d: one failure", i)
		assert.ErrorIs(t, errs[0], ErrExecutionTimeout)
		var stageErr *StageError
		require.ErrorAs(t, errs[0], &stageErr)
		assert.Equal(t, "s1", stageErr.StageName, "run %d: the first stage's failure", i)
	}
}

// TestCompletionTotals_MatchesFailureBySourceStage: the delivered-once check
// pairs a failure with error elements from the stage that failed, not with
// any element carrying a related error.
func TestCompletionTotals_MatchesFailureBySourceStage(t *testing.T) {
	failure := NewStageError("b", StageTypeTransform, io.EOF)
	fromB := NewErrorElement(io.EOF)
	tagErrorSource(&fromB, "b")
	fromC := NewErrorElement(io.EOF)
	tagErrorSource(&fromC, "c")

	totals := &completionTotals{}
	require.True(t, totals.observe(&fromC))
	assert.False(t, totals.delivered(failure), "c's report is not b's failure")
	require.True(t, totals.observe(&fromB))
	assert.True(t, totals.delivered(failure), "b's own report is its failure")

	sig := newTurnFailureSignal(failure)
	tagErrorSource(&sig, "b")
	signalFirst := &completionTotals{}
	require.True(t, signalFirst.observe(&sig))
	c2 := fromC
	assert.True(t, signalFirst.observe(&c2), "c's report after b's signal still reaches the caller")
	b2 := fromB
	assert.False(t, signalFirst.observe(&b2), "b's own report after its signal is a repeat")
}

// TestExecute_BranchForwardedSignalStillReachesEveryReader: on a competitive
// fan-out, a forwarded signal goes to one reader only. When the fanning-out
// stage then fails too, every reader that did not get a signal still gets one.
func TestExecute_BranchForwardedSignalStillReachesEveryReader(t *testing.T) {
	forwardThenFail := NewStageFunc("a", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				out <- e
			}
			return errors.New("a failed too")
		})
	for i := 0; i < 20; i++ {
		got := make(chan string, 4)
		reader := func(name string) *StageFunc {
			return NewStageFunc(name, StageTypeTransform,
				func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
					defer close(out)
					saw := false
					for e := range in {
						saw = saw || isTurnFailureSignal(&e)
						out <- e
					}
					if saw {
						got <- name
					}
					return nil
				})
		}
		p, err := NewPipelineBuilder().
			AddStage(failingStage("x", errStageBoom, false)).AddStage(forwardThenFail).
			AddStage(reader("b1")).AddStage(reader("b2")).
			Connect("x", "a").Branch("a", "b1", "b2").Build()
		require.NoError(t, err)

		errs := runOnce(t, p)

		require.Len(t, got, 2, "run %d: both readers must be told the turn failed", i)
		require.Len(t, errs, 1, "run %d: the turn failed once", i)
		var stageErr *StageError
		require.ErrorAs(t, errs[0], &stageErr)
		assert.Equal(t, "x", stageErr.StageName, "run %d: with the first failure", i)
	}
}

// TestExecute_OverwrittenErrorIsAttributedToTheOverwritingStage: a stage that
// replaces an incoming element's error with its own, and fails with that
// error, owns it. The caller gets that failure once and not the error it
// replaced.
func TestExecute_OverwrittenErrorIsAttributedToTheOverwritingStage(t *testing.T) {
	errFrame := errors.New("frame 3 could not be decoded")
	reporter := NewStageFunc("a", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				out <- e
			}
			out <- NewErrorElement(errFrame)
			return nil
		})
	overwriter := NewStageFunc("b", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				if e.Error != nil {
					e.Error = errStageBoom
				}
				out <- e
			}
			return errStageBoom
		})
	p, err := NewPipelineBuilder().Chain(reporter, overwriter, NewPassthroughStage("sink")).Build()
	require.NoError(t, err)

	errs := runOnce(t, p)

	require.Len(t, errs, 1, "b's failure reaches the caller once")
	assert.ErrorIs(t, errs[0], errStageBoom)
}

// TestExecute_RecreatedSignalStillCountsAsDelivered: a leaf that re-creates
// every error element (NewErrorElement(e.Error)) drops the source tags. The
// signal still belongs to the stage whose failure it carries, so that failure
// is not delivered a second time at the end of the turn.
func TestExecute_RecreatedSignalStillCountsAsDelivered(t *testing.T) {
	recreate := NewStageFunc("b", StageTypeTransform,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for e := range in {
				if e.Error != nil {
					e = NewErrorElement(e.Error)
				}
				out <- e
			}
			return nil
		})
	p, err := NewPipelineBuilder().Chain(emitAndReturnStage("a", "", errStageBoom), recreate).Build()
	require.NoError(t, err)

	errs := runOnce(t, p)

	failures := 0
	for _, e := range errs {
		var stageErr *StageError
		if errors.As(e, &stageErr) && stageErr.StageName == "a" {
			failures++
		}
	}
	assert.Equal(t, 1, failures, "stage a's failure reaches the caller once")
}
