package events

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitter_MessageCreatedCtx_PublishesOnTheBus pins that message.created has
// a real bus producer.
//
// #1865 asserted it did not — "nothing in this repo calls it, and calling it is
// probably a mistake" — on the strength of a grep of this repo alone. The
// caller was in promptarena, feeding a TUI conversation panel and a web SSE
// relay, and had been since before the repo split.
func TestEmitter_MessageCreatedCtx_PublishesOnTheBus(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)

	got := make(chan *Event, 4)
	bus.Subscribe(EventMessageCreated, func(e *Event) { got <- e })

	e := NewEmitter(bus, "run", "sess", "conv")
	e.MessageCreatedCtx(context.Background(), &MessageCreatedData{
		Role: "assistant", Content: "hello", Index: 3,
	})

	select {
	case evt := <-got:
		d, ok := evt.Data.(*MessageCreatedData)
		require.True(t, ok)
		assert.Equal(t, "hello", d.Content)
		assert.Equal(t, 3, d.Index)
		assert.Equal(t, "conv", evt.ConversationID)
		assert.Equal(t, "sess", evt.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("message.created never reached the bus")
	}
}

// TestEmitter_MessageCreatedCtx_NilDataIsNoOp matches the other Ctx helpers,
// which all guard nil rather than publishing an event with no payload.
func TestEmitter_MessageCreatedCtx_NilDataIsNoOp(t *testing.T) {
	bus := NewEventBus()
	t.Cleanup(bus.Close)

	got := make(chan *Event, 1)
	bus.Subscribe(EventMessageCreated, func(e *Event) { got <- e })

	NewEmitter(bus, "run", "sess", "conv").MessageCreatedCtx(context.Background(), nil)

	select {
	case <-got:
		t.Fatal("nil data must not publish")
	case <-time.After(200 * time.Millisecond):
	}
}
