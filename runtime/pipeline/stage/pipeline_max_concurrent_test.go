package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedPipeline builds a one-stage pipeline whose stage holds each execution
// open until its input closes.
func gatedPipeline(t *testing.T, maxConcurrent int) *StreamPipeline {
	t.Helper()
	hold := NewStageFunc("hold", StageTypeSink, func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		for range in { //nolint:revive // drain
		}
		return nil
	})
	cfg := DefaultPipelineConfig().WithMaxConcurrentPipelines(maxConcurrent)
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(hold).Build()
	require.NoError(t, err)
	return p
}

func drainOutput(out <-chan StreamElement) {
	for range out { //nolint:revive // drain
	}
}

func TestPipeline_MaxConcurrentPipelines_BoundsExecute(t *testing.T) {
	p := gatedPipeline(t, 1)

	first := make(chan StreamElement)
	out1, err := p.Execute(context.Background(), first)
	require.NoError(t, err)

	// The only slot is taken, so a second Execute waits until its context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = p.Execute(ctx, make(chan StreamElement))
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// Once the first execution finishes, its slot is free again.
	close(first)
	drainOutput(out1)
	third := make(chan StreamElement)
	close(third)
	out3, err := p.Execute(context.Background(), third)
	require.NoError(t, err)
	drainOutput(out3)
}

func TestPipeline_MaxConcurrentPipelines_WaiterRunsWhenSlotFrees(t *testing.T) {
	p := gatedPipeline(t, 1)

	first := make(chan StreamElement)
	out1, err := p.Execute(context.Background(), first)
	require.NoError(t, err)

	started := make(chan error, 1)
	second := make(chan StreamElement)
	close(second)
	go func() {
		out2, err := p.Execute(context.Background(), second)
		if err == nil {
			drainOutput(out2)
		}
		started <- err
	}()

	select {
	case <-started:
		t.Fatal("second Execute ran while the only slot was taken")
	case <-time.After(20 * time.Millisecond):
	}

	close(first)
	drainOutput(out1)
	select {
	case err := <-started:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second Execute never got the freed slot")
	}
}

func TestPipeline_MaxConcurrentPipelines_ZeroIsUnlimited(t *testing.T) {
	started := make(chan struct{}, 3)
	hold := NewStageFunc("hold", StageTypeSink, func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
		defer close(out)
		started <- struct{}{}
		for range in { //nolint:revive // drain
		}
		return nil
	})
	cfg := DefaultPipelineConfig().WithMaxConcurrentPipelines(0)
	p, err := NewPipelineBuilderWithConfig(cfg).Chain(hold).Build()
	require.NoError(t, err)

	inputs := make([]chan StreamElement, 3)
	outs := make([]<-chan StreamElement, 3)
	for i := range inputs {
		inputs[i] = make(chan StreamElement)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		out, err := p.Execute(ctx, inputs[i])
		cancel()
		require.NoError(t, err)
		outs[i] = out
	}

	// All three run at once: each stage starts while the others are still open.
	running := 0
	for running < 3 {
		select {
		case <-started:
			running++
		case <-time.After(time.Second):
			t.Fatalf("only %d of 3 executions started", running)
		}
	}
	assert.Equal(t, 3, running)
	for i := range inputs {
		close(inputs[i])
		drainOutput(outs[i])
	}
}

func TestPipeline_MaxConcurrentPipelines_ShutdownReleasesWaiter(t *testing.T) {
	p := gatedPipeline(t, 1)

	first := make(chan StreamElement)
	out1, err := p.Execute(context.Background(), first)
	require.NoError(t, err)

	waited := make(chan error, 1)
	go func() {
		_, err := p.Execute(context.Background(), make(chan StreamElement))
		waited <- err
	}()

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- p.Shutdown(context.Background()) }()

	select {
	case err := <-waited:
		assert.ErrorIs(t, err, ErrPipelineShuttingDown)
	case <-time.After(time.Second):
		t.Fatal("a waiting Execute was not released by Shutdown")
	}

	close(first)
	drainOutput(out1)
	require.NoError(t, <-shutdownDone)
}
