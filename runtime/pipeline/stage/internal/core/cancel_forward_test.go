package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelForwarder_CanceledStillDeliversToAReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan StreamElement)
	got := make(chan StreamElement, 1)
	go func() { got <- <-out }()

	var fwd CancelForwarder
	text := "partial"
	fwd.Forward(ctx, out, StreamElement{Text: &text})

	assert.Equal(t, "partial", *(<-got).Text)
	assert.False(t, fwd.gone)
}

func TestCancelForwarder_StopsForwardingOnceConsumerIsGone(t *testing.T) {
	prev := CanceledForwardTimeout
	CanceledForwardTimeout = 5 * time.Millisecond
	t.Cleanup(func() { CanceledForwardTimeout = prev })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan StreamElement) // never read

	var fwd CancelForwarder
	fwd.Forward(ctx, out, StreamElement{})
	require.True(t, fwd.gone, "an unread send after cancellation must mark the consumer gone")

	// Later elements are dropped at once rather than waiting the timeout again.
	CanceledForwardTimeout = time.Hour
	done := make(chan struct{})
	go func() {
		fwd.Forward(ctx, out, StreamElement{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forward kept waiting after the consumer was known to be gone")
	}
}

func TestLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	assert.Equal(t, ctx, LiveContext(ctx), "a live context is used as-is")
	cancel()
	assert.NoError(t, LiveContext(ctx).Err(), "a canceled context is replaced by one that is not")
}

func TestCancelForwarder_LiveTurnSendsNormally(t *testing.T) {
	out := make(chan StreamElement, 1)
	var fwd CancelForwarder
	fwd.Forward(context.Background(), out, NewTextElement("live"))
	assert.Equal(t, "live", *(<-out).Text)
	assert.False(t, fwd.gone)
}

// canceledAfterCheck is a canceled context whose first Err reports it live:
// the turn is canceled after Forward checked it and while it sends.
type canceledAfterCheck struct {
	context.Context
	checked bool
}

func (c *canceledAfterCheck) Err() error {
	if !c.checked {
		c.checked = true
		return nil
	}
	return c.Context.Err()
}

func TestCancelForwarder_TurnCanceledMidSendStillOffersTheElement(t *testing.T) {
	prev := CanceledForwardTimeout
	CanceledForwardTimeout = 5 * time.Millisecond
	t.Cleanup(func() { CanceledForwardTimeout = prev })

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &canceledAfterCheck{Context: canceled}
	out := make(chan StreamElement) // nobody reads
	var fwd CancelForwarder
	fwd.Forward(ctx, out, NewTextElement("x"))

	assert.True(t, ctx.checked, "Forward checked the turn first")
	assert.True(t, fwd.gone, "the send fell through to the bounded one, which timed out")
}
