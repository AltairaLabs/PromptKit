package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/replay"
	"github.com/AltairaLabs/PromptKit/runtime/v2/recording"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// scriptedDuplexProvider is the real mock streaming provider answering each
// turn from a per-turn script.
func scriptedDuplexProvider(replies ...string) *mock.StreamingProvider {
	repo := newTestTurnRepository()
	for i, r := range replies {
		repo.addTurn("default", i+1, mock.Turn{Type: "text", Content: r})
	}
	return mock.NewStreamingProvider("duplex-mock", "duplex-model", false).
		WithAutoRespond("unused").
		WithMockResponses(repo, "default", "")
}

// duplexTurn sends text and reads the response channel until the chunk that
// closes the assistant turn, returning every chunk of the turn.
func duplexTurn(t *testing.T, conv *sdk.Conversation, ch <-chan providers.StreamChunk, text string) []providers.StreamChunk {
	t.Helper()
	require.NoError(t, conv.SendText(context.Background(), text))
	var got []providers.StreamChunk
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case c, ok := <-ch:
			require.True(t, ok, "response channel closed during the turn for %q", text)
			require.NoError(t, c.Error)
			got = append(got, c)
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				return got
			}
		case <-timer.C:
			t.Fatalf("no turn-complete chunk for %q; got %d chunks", text, len(got))
		}
	}
}

type transcriptEntry struct{ Role, Content string }

// recordedTranscript exports the session from the store and returns its
// message.created events in order.
func recordedTranscript(t *testing.T, store events.EventStore, sessionID string) (*recording.SessionRecording, []transcriptEntry) {
	t.Helper()
	rec, err := recording.Export(context.Background(), store, sessionID)
	require.NoError(t, err)
	evs, err := rec.ToTypedEvents()
	require.NoError(t, err)
	var out []transcriptEntry
	for _, e := range evs {
		if e.Type != events.EventMessageCreated {
			continue
		}
		d, ok := e.Data.(*events.MessageCreatedData)
		require.True(t, ok, "message.created data is %T", e.Data)
		out = append(out, transcriptEntry{d.Role, d.Content})
	}
	return rec, out
}

func openRecordedDuplex(t *testing.T, packJSON string, prov providers.Provider, opts ...sdk.Option) (*sdk.Conversation, events.EventStore) {
	t.Helper()
	store, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	all := append([]sdk.Option{
		sdk.WithProvider(prov),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventStore(store),
		sdk.WithRecording(nil),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}),
	}, opts...)
	conv, err := sdk.OpenDuplex(writePackFile(t, packJSON), "chat", all...)
	require.NoError(t, err)
	return conv, store
}

// TestDuplexRecording_TranscriptHasEachTurnOnceInOrder records a realtime
// session of three turns, then reads the recording back and replays it. The
// transcript holds every user and assistant turn exactly once, in order, each
// turn-complete chunk says why the turn ended, and the replay answers the
// same turns with the same replies.
func TestDuplexRecording_TranscriptHasEachTurnOnceInOrder(t *testing.T) {
	replies := []string{"reply one", "reply two", "reply three"}
	inputs := []string{"first", "second", "third"}
	stateStore := statestore.NewMemoryStore()
	conv, store := openRecordedDuplex(t, minimalPackJSON, scriptedDuplexProvider(replies...),
		sdk.WithStateStore(stateStore))

	ch, err := conv.Response()
	require.NoError(t, err)
	for i, in := range inputs {
		chunks := duplexTurn(t, conv, ch, in)
		last := chunks[len(chunks)-1]
		assert.Equal(t, "stop", last.Metadata["finish_reason"], "turn %d's complete chunk lacks finish_reason", i+1)
		var text string
		for _, c := range chunks {
			text += c.Delta
		}
		assert.Equal(t, replies[i], text, "turn %d streamed the wrong reply", i+1)
	}
	sessionID := conv.ID()
	require.NoError(t, conv.Close())

	// A realtime session's pipeline runs once, for the whole session, so its
	// messages are saved when it closes.
	state, err := stateStore.Load(context.Background(), sessionID)
	require.NoError(t, err)
	var history []transcriptEntry
	for _, m := range state.Messages {
		history = append(history, transcriptEntry{m.Role, m.GetContent()})
	}

	rec, transcript := recordedTranscript(t, store, sessionID)
	want := []transcriptEntry{
		{"user", "first"}, {"assistant", "reply one"},
		{"user", "second"}, {"assistant", "reply two"},
		{"user", "third"}, {"assistant", "reply three"},
	}
	assert.Equal(t, want, history, "the saved history is not each turn once, in order")
	assert.Equal(t, want, transcript, "the recorded transcript is not each turn once, in order")

	// Replay the recording as a provider and run the same turns.
	rp, err := replay.NewProvider(rec, nil)
	require.NoError(t, err)
	replayConv, err := sdk.Open(writePackFile(t, minimalPackJSON), "chat",
		sdk.WithProvider(rp), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = replayConv.Close() })
	for i, in := range inputs {
		resp, err := replayConv.Send(context.Background(), in)
		require.NoError(t, err)
		assert.Equal(t, replies[i], resp.Text(), "replayed turn %d", i+1)
	}
}
