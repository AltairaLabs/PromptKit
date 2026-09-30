package stage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestCancelForwarder_CanceledStillDeliversToAReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan StreamElement)
	got := make(chan StreamElement, 1)
	go func() { got <- <-out }()

	var fwd cancelForwarder
	text := "partial"
	fwd.forward(ctx, out, StreamElement{Text: &text})

	assert.Equal(t, "partial", *(<-got).Text)
	assert.False(t, fwd.gone)
}

func TestCancelForwarder_StopsForwardingOnceConsumerIsGone(t *testing.T) {
	prev := canceledForwardTimeout
	canceledForwardTimeout = 5 * time.Millisecond
	t.Cleanup(func() { canceledForwardTimeout = prev })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan StreamElement) // never read

	var fwd cancelForwarder
	fwd.forward(ctx, out, StreamElement{})
	require.True(t, fwd.gone, "an unread send after cancellation must mark the consumer gone")

	// Later elements are dropped at once rather than waiting the timeout again.
	canceledForwardTimeout = time.Hour
	done := make(chan struct{})
	go func() {
		fwd.forward(ctx, out, StreamElement{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forward kept waiting after the consumer was known to be gone")
	}
}

func TestCompleteToolCalls_DropsCallsWithHalfWrittenArgs(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "1", Name: "search", Args: json.RawMessage(`{"q":"weather"}`)},
		{ID: "2", Name: "lookup", Args: json.RawMessage(`{"city":"Bri`)},
		{ID: "3", Name: "ping"},
	}

	got := completeToolCalls(calls)

	require.Len(t, got, 2)
	assert.Equal(t, "1", got[0].ID)
	assert.Equal(t, "3", got[1].ID, "a call with no arguments yet is kept")
	_, err := json.Marshal(types.Message{Role: "assistant", ToolCalls: got})
	assert.NoError(t, err, "the kept calls must marshal")
}

func TestLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	assert.Equal(t, ctx, liveContext(ctx), "a live context is used as-is")
	cancel()
	assert.NoError(t, liveContext(ctx).Err(), "a canceled context is replaced by one that is not")
}
