package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startOne starts a one-stage pipeline running fn on ctx, reading in.
func startOne(
	t *testing.T, ctx context.Context, in <-chan StreamElement,
	fn func(context.Context, <-chan StreamElement, chan<- StreamElement) error,
) *StreamExecution {
	t.Helper()
	cfg := DefaultPipelineConfig()
	cfg.ExecutionTimeout = 0
	cfg.IdleTimeout = 0
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(NewStageFunc("scripted", StageTypeTransform, fn)).Build()
	require.NoError(t, err)
	exec, err := p.Start(ctx, in)
	require.NoError(t, err)
	return exec
}

// startScripted starts a one-stage pipeline running fn on ctx, with input
// the caller feeds, and returns the execution after reading its output to the
// end, with the elements read.
func startScripted(
	t *testing.T, ctx context.Context,
	fn func(context.Context, <-chan StreamElement, chan<- StreamElement) error,
) (*StreamExecution, []StreamElement) {
	t.Helper()
	in := make(chan StreamElement)
	exec := startOne(t, ctx, in, fn)
	return exec, readToClose(t, exec, in)
}

// readToClose reads exec's output to its close, then closes in, and returns
// the elements read.
func readToClose(t *testing.T, exec *StreamExecution, in chan StreamElement) []StreamElement {
	t.Helper()
	var got []StreamElement
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-exec.Output():
			if !ok {
				close(in)
				return got
			}
			got = append(got, e)
		case <-timeout:
			t.Fatal("the execution's output never closed")
		}
	}
}

func textElem(s string) StreamElement { return StreamElement{Text: &s} }

// The stage's error ends the execution even when output follows its error
// element (a downstream stage flushing audio, say): Err is that error.
func TestStart_ErrIsTheFailureEvenWhenOutputFollowsIt(t *testing.T) {
	fatal := errors.New("provider connection lost")
	exec, got := startScripted(t, context.Background(),
		func(_ context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			out <- NewErrorElement(fatal)
			out <- textElem("flushed")
			return fatal
		})
	require.Len(t, got, 2)
	assert.ErrorIs(t, got[0].Error, fatal)
	assert.ErrorIs(t, exec.Err(), fatal)
}

// An error element the stage carried on from does not end the execution: a
// stage that then returns cleanly leaves Err nil.
func TestStart_ErrIgnoresAnErrorTheStageCarriedOnFrom(t *testing.T) {
	limit := errors.New("tool round limit reached")
	exec, got := startScripted(t, context.Background(),
		func(_ context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			out <- NewErrorElement(limit)
			return nil
		})
	require.Len(t, got, 1)
	assert.ErrorIs(t, got[0].Error, limit)
	assert.NoError(t, exec.Err())
}

// A stage that fails without emitting its error: the pipeline delivers it as
// the last element, and Err is it.
func TestStart_ErrIsAFailureThePipelineDelivered(t *testing.T) {
	fatal := errors.New("stage broke")
	exec, got := startScripted(t, context.Background(),
		func(_ context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			return fatal
		})
	require.NotEmpty(t, got)
	assert.ErrorIs(t, got[len(got)-1].Error, fatal)
	assert.ErrorIs(t, exec.Err(), fatal)
}

// Err is nil while the execution runs, even after the failing stage has
// emitted its error element: only the stage returning it ends the execution.
func TestStart_ErrIsNilWhileTheExecutionRuns(t *testing.T) {
	fatal := errors.New("provider connection lost")
	in := make(chan StreamElement)
	exec := startOne(t, context.Background(), in,
		func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			out <- NewErrorElement(fatal)
			for range in { //nolint:revive // drain until the caller closes the input
			}
			return fatal
		})
	select {
	case e := <-exec.Output():
		assert.ErrorIs(t, e.Error, fatal)
	case <-time.After(5 * time.Second):
		t.Fatal("the stage's error element never arrived")
	}
	assert.Equal(t, error(nil), exec.Err(), "the stage has not returned: the execution is running")
	close(in)
	for range exec.Output() { //nolint:revive // drain to the close
	}
	assert.ErrorIs(t, exec.Err(), fatal)
}

// Err is set when the execution ends, which can be before the reader has
// taken the elements still buffered on the output: those stay readable.
func TestStart_ErrIsSetBeforeTheBufferedOutputIsRead(t *testing.T) {
	fatal := errors.New("provider connection lost")
	in := make(chan StreamElement)
	defer close(in)
	exec := startOne(t, context.Background(), in,
		func(_ context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			out <- textElem("buffered")
			return fatal
		})
	require.Eventually(t, func() bool { return exec.Err() != nil }, 5*time.Second, time.Millisecond,
		"the execution never ended")
	assert.ErrorIs(t, exec.Err(), fatal)
	var texts []string
	for e := range exec.Output() {
		if e.Text != nil {
			texts = append(texts, *e.Text)
		}
	}
	assert.Equal(t, []string{"buffered"}, texts)
}

// The caller's own cancellation is not a failure: Err stays nil.
func TestStart_ErrIsNilAfterTheCallersCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exec, got := startScripted(t, ctx,
		func(ctx context.Context, _ <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			<-ctx.Done()
			return ctx.Err()
		})
	assert.Empty(t, got, "a canceled execution delivers no failure element")
	assert.Equal(t, error(nil), exec.Err())
}
