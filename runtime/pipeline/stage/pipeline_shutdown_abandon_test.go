package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitExecutionsDone reports whether every execution of p returned within d.
func waitExecutionsDone(p *StreamPipeline, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestShutdown_ReleasesExecutionWhoseReaderIsGone: a consumer that stops
// reading and never cancels holds the execution only until Shutdown's grace
// period ends, both while output is being forwarded and while the turn's
// error waits to be delivered.
func TestShutdown_ReleasesExecutionWhoseReaderIsGone(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.GracefulShutdownTimeout = 10 * time.Millisecond
	p, err := NewPipelineBuilderWithConfig(config).
		Chain(emitAndReturnStage("fails", "partial", errStageBoom)).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	close(in)
	_, err = p.Execute(context.Background(), in) // nobody ever reads the output
	require.NoError(t, err)

	require.ErrorIs(t, p.Shutdown(context.Background()), ErrShutdownTimeout)
	assert.True(t, waitExecutionsDone(p, 2*time.Second), "the abandoned execution must return once the grace period ends")
}

// TestShutdown_SlowReaderStillGetsTheError: a reader that is slow, but reads
// within the grace period, still gets every element and the turn's error.
func TestShutdown_SlowReaderStillGetsTheError(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ChannelBufferSize = 0
	config.GracefulShutdownTimeout = 2 * time.Second
	p, err := NewPipelineBuilderWithConfig(config).
		Chain(failingStage("fails", errStageBoom, false)).Build()
	require.NoError(t, err)

	in := make(chan StreamElement)
	close(in)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)

	shutdownErr := make(chan error, 1)
	go func() { shutdownErr <- p.Shutdown(context.Background()) }()
	time.Sleep(20 * time.Millisecond) // shutting down, not yet read
	errs := errorElements(out)

	require.Len(t, errs, 1)
	assert.ErrorIs(t, errs[0], errStageBoom)
	require.NoError(t, <-shutdownErr)
}
