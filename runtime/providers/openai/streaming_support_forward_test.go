package openai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// cuttingFakeSession is a fake session that stops replies and forgets turns,
// recording each request.
type cuttingFakeSession struct {
	*fakeStreamInputSession
	interrupted []int64
	forgot      []string
}

func (f *cuttingFakeSession) ForgetTurn(_ context.Context, inputID string) error {
	f.forgot = append(f.forgot, inputID)
	return nil
}

func (f *cuttingFakeSession) InterruptResponse(_ context.Context, gen int64) error {
	f.interrupted = append(f.interrupted, gen)
	return nil
}

// The bookkeeping wrapper CreateStreamSession returns must not hide the
// session's guardrail-cut support: the pipeline type-asserts the wrapper, so
// an unforwarded method leaves a blocked reply generating at the provider or
// the blocked turn in the model's memory.
func TestRealtimeSessionBookkeeping_ForwardsCutAndForget(t *testing.T) {
	inner := &cuttingFakeSession{fakeStreamInputSession: newFakeStreamSession()}
	var wrapped providers.StreamInputSession = &realtimeSessionBookkeeping{StreamInputSession: inner, release: func() {}}

	ri, ok := wrapped.(providers.ResponseInterrupter)
	require.True(t, ok, "the wrapper hides InterruptResponse")
	require.NoError(t, ri.InterruptResponse(context.Background(), 7))

	tf, ok := wrapped.(providers.TurnForgetter)
	require.True(t, ok, "the wrapper hides ForgetTurn")
	require.NoError(t, tf.ForgetTurn(context.Background(), "item_9"))

	assert.Equal(t, []int64{7}, inner.interrupted)
	assert.Equal(t, []string{"item_9"}, inner.forgot)
}
