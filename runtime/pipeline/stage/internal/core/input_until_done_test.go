package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recvWithin reads one element from ch, failing after a second.
func recvWithin(t *testing.T, ch <-chan StreamElement) (StreamElement, bool) {
	t.Helper()
	select {
	case elem, ok := <-ch:
		return elem, ok
	case <-time.After(time.Second):
		t.Fatal("the relay neither delivered nor closed")
		return StreamElement{}, false
	}
}

func TestInputUntilDone_RelaysUntilInputCloses(t *testing.T) {
	input := make(chan StreamElement, 2)
	input <- NewTextElement("a")
	input <- NewTextElement("b")
	close(input)

	relay := InputUntilDone(context.Background(), input)
	for _, want := range []string{"a", "b"} {
		elem, ok := recvWithin(t, relay)
		require.True(t, ok)
		assert.Equal(t, want, *elem.Text)
	}
	_, ok := recvWithin(t, relay)
	assert.False(t, ok, "the relay closes when input does")
}

func TestInputUntilDone_ClosesWhenTheContextEndsWithInputOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	relay := InputUntilDone(ctx, make(chan StreamElement))
	cancel()
	_, ok := recvWithin(t, relay)
	assert.False(t, ok, "the relay closes on the context, not on the next element")
}

func TestInputUntilDone_ClosesWhenTheContextEndsWhileNobodyReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan StreamElement, 1)
	input <- NewTextElement("unread")
	relay := InputUntilDone(ctx, input)
	// Wait for the relay to take the element, so it is blocked handing it on.
	require.Eventually(t, func() bool { return len(input) == 0 }, time.Second, time.Millisecond)
	// cancel closes the context's Done channel before this goroutine reads, so
	// the relay's blocked send loses to it.
	cancel()

	_, ok := recvWithin(t, relay)
	assert.False(t, ok, "the relay gave up the send once the context ended")
}
