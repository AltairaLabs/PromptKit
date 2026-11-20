package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// busyStage stands for a stage held by a model or TTS call: it reads no
// input until its context is canceled, and records that it was.
type busyStage struct {
	stage.BaseStage
	canceled chan struct{}
}

func (s *busyStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)
	<-ctx.Done()
	close(s.canceled)
	for range input { //nolint:revive // drained so the session's sends never block
	}
	return ctx.Err()
}

// Drain on a session whose pipeline has stopped reading input (its input
// buffer full, as a live microphone fills it while a call blocks) closes the
// session within its playout grace, canceling the busy stage, instead of
// waiting on the end-of-stream send until the caller's deadline (#2847).
func TestDuplexSession_DrainOfABusyPipelineClosesWithinTheGrace(t *testing.T) {
	busy := &busyStage{BaseStage: stage.NewBaseStage("busy", stage.StageTypeTransform), canceled: make(chan struct{})}
	provider := mock.NewProvider("mock", "mock-model", false)
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		PipelineBuilder: func(context.Context, providers.Provider, providers.StreamInputSupport,
			*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
			return stage.NewPipelineBuilder().Chain(busy).Build()
		},
		Provider: provider,
	})
	require.NoError(t, err)

	// Fill the input the busy stage is not reading.
	fill, stopFill := context.WithCancel(context.Background())
	defer stopFill()
	filled := make(chan struct{})
	go func() {
		defer close(filled)
		for sess.SendText(fill, "more audio") == nil {
		}
	}()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, sess.Drain(ctx))
	assert.Less(t, time.Since(start), drainPlayoutGrace+time.Second,
		"Drain waited on the input of a pipeline that was not reading it")
	requireClosedSoon(t, busy.canceled, "the busy stage was not canceled")
	requireClosedSoon(t, sess.Done(), "Done() must close after Drain")
	stopFill()
	<-filled
}
