package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// 1.1 — Full conversation round-trip
// ---------------------------------------------------------------------------

func TestConversation_SingleMessageRoundTrip(t *testing.T) {
	conv := openTestConv(t)

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Hello!")
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Text(), "response text should be non-empty")
	assert.Greater(t, resp.TokensUsed(), 0, "tokens used should be > 0")
	assert.Greater(t, resp.Cost(), 0.0, "cost should be > 0")
	assert.Greater(t, resp.Duration(), time.Duration(0), "duration should be > 0")
}

func TestConversation_ResponseHasMessage(t *testing.T) {
	conv := openTestConv(t)

	resp, err := conv.Send(context.Background(), "Hi")
	require.NoError(t, err)

	msg := resp.Message()
	require.NotNil(t, msg, "response should have an underlying message")
	assert.Equal(t, "assistant", msg.Role)
	assert.NotEmpty(t, msg.GetContent())
}

// ---------------------------------------------------------------------------
// 1.2 — Multi-turn state continuity
// ---------------------------------------------------------------------------

func TestConversation_MultiTurnHistory(t *testing.T) {
	conv := openTestConv(t)
	ctx := context.Background()

	_, err := conv.Send(ctx, "First message")
	require.NoError(t, err)

	_, err = conv.Send(ctx, "Second message")
	require.NoError(t, err)

	msgs := conv.Messages(ctx)
	// Expect at least: user1, assistant1, user2, assistant2
	assert.GreaterOrEqual(t, len(msgs), 4, "should have at least 4 messages after 2 turns")

	// Verify alternating roles
	roles := make([]string, len(msgs))
	for i, m := range msgs {
		roles[i] = m.Role
	}
	assert.Equal(t, "user", roles[0])
	assert.Equal(t, "assistant", roles[1])
	assert.Equal(t, "user", roles[2])
	assert.Equal(t, "assistant", roles[3])
}

// ---------------------------------------------------------------------------
// 1.3 — Event emission end-to-end (basic)
// ---------------------------------------------------------------------------

func TestConversation_EmitsEventsOnSend(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	conv := openTestConv(t, sdk.WithEventBus(bus))

	_, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)

	// Give async listeners a moment to process.
	ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second)

	assert.True(t, ec.hasType(events.EventPipelineStarted), "should emit pipeline.started")
	assert.True(t, ec.hasType(events.EventPipelineCompleted), "should emit pipeline.completed")
	assert.True(t, ec.hasType(events.EventProviderCallStarted), "should emit provider.call.started")
	assert.True(t, ec.hasType(events.EventProviderCallCompleted), "should emit provider.call.completed")
}

// ---------------------------------------------------------------------------
// 1.5 — Conversation close and cleanup
// ---------------------------------------------------------------------------

func TestConversation_CloseIsIdempotent(t *testing.T) {
	conv := openTestConv(t)

	err1 := conv.Close()
	assert.NoError(t, err1)

	err2 := conv.Close()
	assert.NoError(t, err2, "second Close should not error")
}

func TestConversation_SendAfterCloseReturnsError(t *testing.T) {
	conv := openTestConv(t)
	require.NoError(t, conv.Close())

	_, err := conv.Send(context.Background(), "Should fail")
	assert.ErrorIs(t, err, sdk.ErrConversationClosed)
}

func TestConversation_StreamAfterCloseReturnsError(t *testing.T) {
	conv := openTestConv(t)
	require.NoError(t, conv.Close())

	ch := conv.Stream(context.Background(), "Should fail")
	chunk, ok := <-ch
	if ok {
		assert.Error(t, chunk.Error, "stream chunk should carry an error after Close")
	}
}
