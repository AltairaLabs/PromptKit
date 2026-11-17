package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The session records each call the model makes, its name and arguments,
// under its ID. An ID a later response reuses (Gemini numbers calls call_0,
// call_1, ... per response) names the later call.
func TestDuplexSession_ToolCallIsTheLatestCallUnderItsID(t *testing.T) {
	s := &duplexSession{}
	first := types.MessageToolCall{ID: "call_0", Name: "lookup_order", Args: []byte(`{"order_id":"ORD-1"}`)}
	s.rememberCalls([]types.MessageToolCall{first})
	got, ok := s.ToolCall("call_0")
	require.True(t, ok)
	assert.Equal(t, first, got)

	assert.Equal(t, "lookup_order", s.takeCallName("call_0"))
	_, ok = s.ToolCall("call_0")
	assert.False(t, ok, "an answered call is forgotten")

	later := types.MessageToolCall{ID: "call_0", Name: "get_location", Args: []byte(`{}`)}
	s.rememberCalls([]types.MessageToolCall{first})
	s.rememberCalls([]types.MessageToolCall{later})
	got, ok = s.ToolCall("call_0")
	require.True(t, ok)
	assert.Equal(t, later, got)
}

// replyingStage answers each input with a complete assistant reply.
type replyingStage struct{}

func (replyingStage) Name() string          { return "replying" }
func (replyingStage) Type() stage.StageType { return stage.StageTypeTransform }

func (replyingStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	for range in {
		reply := stage.NewMessageElement(&types.Message{Role: "assistant", Content: "done"})
		reply.EndOfStream = true
		select {
		case out <- reply:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Each complete reply ends a turn, and the session counts them.
func TestDuplexSession_CountsCompletedTurns(t *testing.T) {
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(replyingStage{}).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder: builder,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()
	ds := sess.(*duplexSession)
	assert.Zero(t, ds.TurnsCompleted())

	for i := 1; i <= 2; i++ {
		require.NoError(t, sess.SendChunk(context.Background(), &providers.StreamChunk{Content: "hi"}))
		select {
		case <-resp:
		case <-time.After(lifecycleWait):
			t.Fatal("no reply")
		}
		require.Eventually(t, func() bool { return ds.TurnsCompleted() == int64(i) }, lifecycleWait, time.Millisecond)
	}
}
