package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startScripted starts a one-stage pipeline running fn on ctx, with input
// the caller feeds, and returns the execution after reading its output to the
// end, with the elements read.
func startScripted(
	t *testing.T, ctx context.Context,
	fn func(context.Context, <-chan StreamElement, chan<- StreamElement) error,
) (*StreamExecution, []StreamElement) {
	t.Helper()
	cfg := DefaultPipelineConfig()
	cfg.ExecutionTimeout = 0
	cfg.IdleTimeout = 0
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(NewStageFunc("scripted", StageTypeTransform, fn)).Build()
	require.NoError(t, err)
	in := make(chan StreamElement)
	exec, err := p.Start(ctx, in)
	require.NoError(t, err)
	assert.Nil(t, exec.Err(), "Err is nil while the output is open")
	var got []StreamElement
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-exec.Output():
			if !ok {
				close(in)
				return exec, got
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
