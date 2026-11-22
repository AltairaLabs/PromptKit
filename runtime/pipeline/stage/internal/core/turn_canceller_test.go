package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnCanceller_InterruptCancelsTheTurnAndRefreshStartsANewOne(t *testing.T) {
	c := NewTurnCanceller(context.Background())
	first := c.Context()
	require.NoError(t, first.Err(), "a new canceller's turn context is live")
	assert.True(t, c.InterruptedAt().IsZero())

	at := time.Unix(100, 0)
	c.InterruptAt(at)
	assert.ErrorIs(t, first.Err(), context.Canceled, "an interrupt cancels the turn")
	assert.Equal(t, at, c.InterruptedAt())

	c.InterruptAt(time.Unix(200, 0))
	assert.Equal(t, at, c.InterruptedAt(), "the first interrupt before a refresh is kept")

	c.Refresh()
	second := c.Context()
	assert.NotSame(t, first, second)
	assert.NoError(t, second.Err(), "the refreshed turn context is live")
	assert.True(t, c.InterruptedAt().IsZero(), "a refresh clears the interrupt time")

	c.Stop()
	assert.ErrorIs(t, second.Err(), context.Canceled, "Stop cancels the final turn")
}

func TestTurnCanceller_RefreshCancelsTheTurnItReplaces(t *testing.T) {
	c := NewTurnCanceller(context.Background())
	first := c.Context()
	c.Refresh()
	assert.ErrorIs(t, first.Err(), context.Canceled)
}

func TestTurnCanceller_TurnsAreChildrenOfTheParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	c := NewTurnCanceller(parent)
	cancel()
	assert.ErrorIs(t, c.Context().Err(), context.Canceled)
}

func TestDrainCancelingOnInterrupt_CancelsOutOfBandAndKeepsOrder(t *testing.T) {
	c := NewTurnCanceller(context.Background())
	turn := c.Context()
	input := make(chan StreamElement, 3)
	work := make(chan StreamElement) // unbuffered: the loop is busy
	input <- NewTextElement("a")
	input <- NewInterruptElement()
	input <- NewTextElement("b")
	close(input)

	go DrainCancelingOnInterrupt(input, work, c)

	first := <-work
	assert.Equal(t, "a", *first.Text)
	// The loop has not taken the Interrupt yet; the reader already canceled.
	require.Eventually(t, func() bool { return turn.Err() != nil }, time.Second, time.Millisecond)
	assert.True(t, c.InterruptedAt().IsZero(), "an out-of-band interrupt records no time")

	second := <-work
	assert.True(t, second.Interrupt, "the Interrupt is still forwarded, in order")
	third := <-work
	assert.Equal(t, "b", *third.Text)
	_, open := <-work
	assert.False(t, open, "work closes once input does")
}
