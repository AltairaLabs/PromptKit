package session

import (
	"context"
	"errors"
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

	assert.EqualError(t, sess.SendText(context.Background(), "again"), errSessionClosed)
	assert.NoError(t, sess.Error())
}

// A send already waiting on a full input when the pipeline ends returns the
// error it ended on instead of waiting for a reader that is gone.
func TestDuplexSession_SendWaitingOnFullInputReturnsWhenThePipelineEnds(t *testing.T) {
	fatal := errors.New("provider connection lost")
	release := make(chan struct{})
	sess := scriptedSession(t, func(ctx context.Context, _ <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
		defer close(out)
		// Never reads its input, so the session's input buffer fills.
		<-release
		out <- stage.NewErrorElement(fatal)
		return fatal
	})
	go func() {
		for range sess.Response() { //nolint:revive // drain
		}
	}()
	ctx := context.Background()
	for i := 0; i < streamBufferSize; i++ {
		require.NoError(t, sess.SendText(ctx, "fill"))
	}

	got := make(chan error, 1)
	go func() { got <- sess.SendText(ctx, "one too many") }()
	select {
	case err := <-got:
		t.Fatalf("test premise: the send past a full input should wait, returned %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-got:
		assert.ErrorIs(t, err, fatal)
	case <-time.After(lifecycleWait):
		t.Fatal("the waiting send never returned after the pipeline ended")
	}
	assert.ErrorIs(t, sess.Error(), fatal)
}
