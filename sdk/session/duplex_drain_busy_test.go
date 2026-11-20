package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// chattyStage answers every input with many output elements, as a voice
// turn's audio does, and records the end of its input.
type chattyStage struct {
	stage.BaseStage
	perInput int
	inputs   atomic.Int64
	sawEnd   atomic.Bool
}

func (s *chattyStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)
	for elem := range input {
		if elem.EndOfStream {
			s.sawEnd.Store(true)
			return nil
		}
		s.inputs.Add(1)
		for i := 0; i < s.perInput; i++ {
			text := "audio"
			select {
			case output <- stage.StreamElement{Text: &text}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// Drain on a session whose caller took Response() and then stopped reading
// it, with the input backed up behind the stalled output, finishes the work
// queued and ends the pipeline on the drain signal, promptly. It used to wait
// out the drain timeout: the output blocked the pipeline, the pipeline left
// its input unread, and the drain signal waited on that input (#2847).
func TestDuplexSession_DrainFinishesWhenTheReaderHasStopped(t *testing.T) {
	chatty := &chattyStage{BaseStage: stage.NewBaseStage("chatty", stage.StageTypeTransform), perInput: 50}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		PipelineBuilder: func(context.Context, providers.Provider, providers.StreamInputSupport,
			*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
			return stage.NewPipelineBuilder().Chain(chatty).Build()
		},
		Provider: mock.NewProvider("mock", "mock-model", false),
	})
	require.NoError(t, err)
	_ = sess.Response() // taken, never read

	// Fill the session's input behind the stalled output, until a send waits.
	var sent int64
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := sess.SendText(ctx, "more audio")
		cancel()
		if err != nil {
			require.ErrorIs(t, err, context.DeadlineExceeded)
			break
		}
		sent++
	}
	require.Greater(t, sent, int64(streamBufferSize), "the input backs up behind the stalled output")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, sess.Drain(ctx))
	assert.Less(t, time.Since(start), 2*time.Second, "Drain waited on a reader that had stopped reading")
	assert.True(t, chatty.sawEnd.Load(), "the pipeline must end on the drain signal, not be cut off")
	assert.Equal(t, sent, chatty.inputs.Load(), "the input queued before the drain is all processed")
	assert.NoError(t, sess.Error())
}
