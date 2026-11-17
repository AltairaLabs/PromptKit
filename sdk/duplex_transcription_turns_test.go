package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// Audio that starts a second spoken turn resets the transcription state the
// first turn captured, so the second turn's transcript is stored as its own
// user message. The audio arrives on the input goroutine while the response
// goroutine owns that state; run with -race, this also checks the two do not
// race on it.
func TestDuplexRealtime_SecondSpokenTurnKeepsItsTranscript(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	bt := newBusTranscript(bus)
	store := statestore.NewMemoryStore()
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithEventBus(bus), WithStateStore(store), WithConversationID("two-spoken"))
	require.NoError(t, err)
	convDrain(t, conv)

	speak := func() {
		require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
		}))
	}
	published := func(n int) func() bool {
		return func() bool {
			got, _ := bt.snapshot()
			return len(got) >= n
		}
	}

	speak()
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	sess := prov.GetSession()
	emitInputTranscript(sess, "first question", true)
	emitTurnText(sess, "first answer")
	require.Eventually(t, published(2), 5*time.Second, 5*time.Millisecond, "the first turn reaches the bus")

	speak()
	// The second turn's speech reaches the model before its transcript does.
	require.Eventually(t, func() bool { return len(sess.GetChunks()) >= 2 }, 5*time.Second, time.Millisecond)
	emitInputTranscript(sess, "second question", true)
	emitTurnText(sess, "second answer")
	require.Eventually(t, published(4), 5*time.Second, 5*time.Millisecond, "the second turn reaches the bus")
	require.NoError(t, conv.Close())

	want := map[int]string{
		0: "user:first question",
		1: "assistant:first answer",
		2: "user:second question",
		3: "assistant:second answer",
	}
	assert.Equal(t, want, persistedTranscript(t, store, "two-spoken"))
}
