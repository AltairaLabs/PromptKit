package openai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// cuttingFakeSession is a fake session that stops replies and speaks text,
// recording each request.
type cuttingFakeSession struct {
	*fakeStreamInputSession
	interrupted []int64
	announced   []string
	forgot      []int64
}

func (f *cuttingFakeSession) ForgetTurn(_ context.Context, gen int64) error {
	f.forgot = append(f.forgot, gen)
	return nil
}

func (f *cuttingFakeSession) InterruptResponse(_ context.Context, gen int64) error {
	f.interrupted = append(f.interrupted, gen)
	return nil
}

func (f *cuttingFakeSession) Announce(_ context.Context, text string) (bool, error) {
	f.announced = append(f.announced, text)
	return true, nil
}

// The bookkeeping wrapper CreateStreamSession returns must not hide the
// session's guardrail-cut support: the pipeline type-asserts the wrapper, so
// an unforwarded method leaves a blocked reply generating at the provider,
// the caller in silence, or the blocked turn in the model's memory.
func TestRealtimeSessionBookkeeping_ForwardsCutAnnounceAndForget(t *testing.T) {
	inner := &cuttingFakeSession{fakeStreamInputSession: newFakeStreamSession()}
	var wrapped providers.StreamInputSession = &realtimeSessionBookkeeping{StreamInputSession: inner, release: func() {}}

	ri, ok := wrapped.(providers.ResponseInterrupter)
	require.True(t, ok, "the wrapper hides InterruptResponse")
	require.NoError(t, ri.InterruptResponse(context.Background(), 7))

	a, ok := wrapped.(providers.Announcer)
	require.True(t, ok, "the wrapper hides Announce")
	spoken, err := a.Announce(context.Background(), "No.")
	require.NoError(t, err)

	tf, ok := wrapped.(providers.TurnForgetter)
	require.True(t, ok, "the wrapper hides ForgetTurn")
	require.NoError(t, tf.ForgetTurn(context.Background(), 9))

	assert.Equal(t, []int64{7}, inner.interrupted)
	assert.Equal(t, []int64{9}, inner.forgot)
	assert.Equal(t, []string{"No."}, inner.announced)
	assert.True(t, spoken)
}
