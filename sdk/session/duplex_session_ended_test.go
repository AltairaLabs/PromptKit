package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// scriptedSession opens a session whose pipeline is the single stage fn.
func scriptedSession(
	t *testing.T, fn func(context.Context, <-chan stage.StreamElement, chan<- stage.StreamElement) error,
) DuplexSession {
	t.Helper()
	build := func(
		context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store,
	) (*stage.StreamPipeline, error) {
		cfg := stage.DefaultPipelineConfig()
		cfg.ExecutionTimeout = 0
		cfg.IdleTimeout = 0
		return stage.NewPipelineBuilderWithConfig(cfg).
			Chain(stage.NewStageFunc("scripted", stage.StageTypeTransform, fn)).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        mock.NewProvider("p", "m", false),
		PipelineBuilder: build,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// An error the pipeline reported and went on from is not the error it ended
// on: a pipeline that later ends without one leaves input refused with the
// session-closed error, and Error() nil.
func TestDuplexSession_ErrorThePipelineWentOnFromIsNotItsEnd(t *testing.T) {
	limit := errors.New("tool round limit reached")
	sess := scriptedSession(t, func(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
		defer close(out)
		<-in
		out <- stage.NewErrorElement(limit)
		text := "carrying on"
		out <- stage.StreamElement{Text: &text}
		return nil
	})
	resp := sess.Response()
	require.NoError(t, sess.SendText(context.Background(), "hi"))
	chunks := drainResponse(t, resp)
	require.Len(t, chunks, 2)
	assert.ErrorIs(t, chunks[0].Error, limit)
	assert.Equal(t, "carrying on", chunks[1].Content)
	requireClosedSoon(t, sess.Done(), "Done() did not close")

	assert.ErrorIs(t, sess.SendText(context.Background(), "again"), ErrSessionClosed)
	assert.NoError(t, sess.Error())
}

// A send the pipeline never takes is not accepted: once the pipeline stops
// reading and ends, a send waiting on it returns the error the pipeline ended
// on, never nil for input nobody will read. Every send that returned nil was
// taken by the pipeline.
func TestDuplexSession_SendThePipelineNeverTakesReturnsItsEndError(t *testing.T) {
	fatal := errors.New("provider connection lost")
	release := make(chan struct{})
	var taken atomic.Int32
	sess := scriptedSession(t, func(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
		defer close(out)
		<-in
		taken.Add(1)
		// Reads nothing more, then ends on an error.
		<-release
		out <- stage.NewErrorElement(fatal)
		return fatal
	})
	go func() {
		for range sess.Response() { //nolint:revive // drain
		}
	}()
	ctx := context.Background()
	require.NoError(t, sess.SendText(ctx, "first"))

	got := make(chan error, 1)
	go func() { got <- sess.SendText(ctx, "never read") }()
	select {
	case err := <-got:
		require.Errorf(t, err, "a send the pipeline never read returned nil")
		t.Fatalf("the send returned %v before the pipeline ended", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-got:
		assert.ErrorIs(t, err, fatal)
	case <-time.After(lifecycleWait):
		t.Fatal("the waiting send never returned after the pipeline ended")
	}
	assert.Equal(t, int32(1), taken.Load(), "the pipeline took only the first send")
	assert.ErrorIs(t, sess.Error(), fatal)
}

// Close ends the session, and Error() then reports only an error the
// pipeline ended on before Close: an error it reported and was going on from,
// still being delivered when Close cut the session off, is not one.
func TestDuplexSession_ErrorAfterCloseIsNotAnErrorBeingDelivered(t *testing.T) {
	limit := errors.New("tool round limit reached")
	emitted := make(chan struct{})
	sess := scriptedSession(t, func(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
		defer close(out)
		<-in
		// Fill the response buffer nobody reads yet, so the session is still
		// delivering the error below when Close comes.
		for i := 0; i < streamBufferSize; i++ {
			text := "filler"
			out <- stage.StreamElement{Text: &text}
		}
		out <- stage.NewErrorElement(limit)
		close(emitted)
		<-ctx.Done()
		return nil
	})
	resp := sess.Response()
	require.NoError(t, sess.SendText(context.Background(), "hi"))
	requireClosedSoon(t, emitted, "the stage never emitted its error")
	time.Sleep(50 * time.Millisecond) // the session reaches the blocked delivery
	require.NoError(t, sess.Close())
	drainResponse(t, resp)
	requireClosedSoon(t, sess.Done(), "Done() did not close")

	assert.NoError(t, sess.Error())
	assert.ErrorIs(t, sess.SendText(context.Background(), "again"), ErrSessionClosed)
}
