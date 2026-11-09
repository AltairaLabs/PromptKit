package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// convDrain reads a duplex conversation's responses for the life of the test.
func convDrain(t *testing.T, conv *Conversation) {
	t.Helper()
	ch, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range ch {
		}
	}()
}

// convWaitMessages waits until conv holds n messages and returns them.
func convWaitMessages(t *testing.T, conv *Conversation, n int) []string {
	t.Helper()
	var got []string
	require.Eventually(t, func() bool {
		got = got[:0]
		for _, m := range conv.Messages(context.Background()) {
			got = append(got, m.Role+":"+m.GetContent())
		}
		return len(got) >= n
	}, 5*time.Second, 5*time.Millisecond, "conversation holds %v", got)
	return got
}

// Forking a realtime conversation gives the fork its own provider session,
// seeded with the conversation so far, while the parent's session carries on
// alone: the fork's turns reach neither the parent's session nor its history.
func TestConv_DuplexForkContinuesInItsOwnSession(t *testing.T) {
	for name, store := range map[string]statestore.Store{"private store": nil, "caller's store": statestore.NewMemoryStore()} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("noted")
			opts := []Option{WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio)}
			if store != nil {
				opts = append(opts, WithStateStore(store))
			}
			parent, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat", opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = parent.Close() })
			convDrain(t, parent)
			parent.SetVar("name", "Ada")
			require.NoError(t, parent.SendText(ctx, "my name is Ada"))
			convWaitMessages(t, parent, 2)

			fork, err := parent.Fork()
			require.NoError(t, err)
			t.Cleanup(func() { _ = fork.Close() })
			assert.NotEqual(t, parent.ID(), fork.ID())
			got, _ := fork.GetVar("name")
			assert.Equal(t, "Ada", got, "the fork keeps the parent's variables")
			convDrain(t, fork)
			require.NoError(t, fork.SendText(ctx, "what is my name"))

			require.Eventually(t, func() bool { return len(prov.GetSessions()) == 2 }, 5*time.Second, 5*time.Millisecond,
				"the fork opens its own provider session")
			parentSession, forkSession := prov.GetSessions()[0], prov.GetSessions()[1]
			require.Eventually(t, func() bool { return len(forkSession.GetTexts()) > 0 }, 5*time.Second, 5*time.Millisecond)
			assert.Equal(t, []string{"what is my name"}, forkSession.GetTexts())
			assert.Equal(t, []providers.HistoryTurn{
				{Role: "user", Text: "my name is Ada"}, {Role: "assistant", Text: "noted"},
			}, forkSession.GetHistory(), "the fork's session starts with the conversation so far")
			assert.Equal(t, []string{"my name is Ada"}, parentSession.GetTexts(), "the fork's turn never reaches the parent's session")

			assert.Equal(t, []string{"user:my name is Ada", "assistant:noted", "user:what is my name", "assistant:noted"},
				convWaitMessages(t, fork, 4))
			assert.Equal(t, []string{"user:my name is Ada", "assistant:noted"}, convWaitMessages(t, parent, 2),
				"the parent's history is its own")
		})
	}
}

// A closed duplex conversation cannot be forked.
func TestConv_DuplexForkOfClosedConversation(t *testing.T) {
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat",
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio))
	require.NoError(t, err)
	require.NoError(t, conv.Close())
	_, err = conv.Fork()
	assert.ErrorIs(t, err, ErrConversationClosed)
}

// A realtime conversation persists each turn as it completes, with or without
// a MessageLog, so a session that is never closed (a crash, a pod disruption)
// can be resumed with its turns, and closing it adds no duplicates.
func TestConv_DuplexPersistsEachTurnMidSession(t *testing.T) {
	for name, withLog := range map[string]bool{"save stage": false, "with message log": true} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := statestore.NewMemoryStore()
			prov := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("noted")
			opts := []Option{WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
				WithStateStore(store), WithConversationID("live")}
			if withLog {
				opts = append(opts, WithMessageLog(store))
			}
			conv, err := OpenDuplex(createTestPackFile(t, convChatPack), "chat", opts...)
			require.NoError(t, err)
			convDrain(t, conv)
			require.NoError(t, conv.SendText(ctx, "first"))
			require.Equal(t, []string{"user:first", "assistant:noted"}, convWaitMessages(t, conv, 2))
			require.NoError(t, conv.SendText(ctx, "second"))
			convWaitMessages(t, conv, 4)

			// The process "dies" here: a new one resumes from the store.
			prov2 := mock.NewStreamingProvider("rt", "rt-model", false).WithAutoRespond("back")
			resumed, err := ResumeDuplex("live", createTestPackFile(t, convChatPack), "chat",
				WithProvider(prov2), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio), WithStateStore(store))
			require.NoError(t, err, "the turns were persisted while the session was live")
			t.Cleanup(func() { _ = resumed.Close() })

			require.NoError(t, conv.Close())
			state, err := store.Load(ctx, "live")
			require.NoError(t, err)
			var got []string
			for _, m := range state.Messages {
				got = append(got, m.Role+":"+m.GetContent())
			}
			assert.Equal(t, []string{"user:first", "assistant:noted", "user:second", "assistant:noted"}, got,
				"each turn is stored once")
		})
	}
}

// A VAD-mode voice conversation with no MessageLog persists each turn while
// the call is up, through its save stage.
func TestConv_VADModePersistsTurnsMidSessionWithoutMessageLog(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real VAD turn")
	}
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	provider := &turnRecordingProvider{}
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(provider), WithSkipSchemaValidation(), WithStateStore(store), WithConversationID("vad-live"),
		WithVADMode(newScriptedSTT("what is the capital of france"), newConvMockTTSService(), vadTestConfig()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	convDrain(t, conv)

	speakOneUtterance(t, conv)
	require.True(t, provider.waitForTurns(1, 8*time.Second), "the model fires during the session")
	require.Eventually(t, func() bool {
		state, err := store.Load(ctx, "vad-live")
		if err != nil {
			return false
		}
		for _, m := range state.Messages {
			if m.Role == "user" && m.GetContent() == "what is the capital of france" {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "the turn is stored while the call is still up")
}

// A unary conversation with a MessageLog stores each message once: the
// provider stage's write-through persists the turn, and the save stage does
// not repeat it.
func TestConv_UnaryWithMessageLogStoresEachMessageOnce(t *testing.T) {
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	w := newConvWire(t, convWireReply{Text: "pong"})
	conv := openConv(t, w, WithStateStore(store), WithMessageLog(store), WithConversationID("logged"))
	_, err := conv.Send(ctx, "ping")
	require.NoError(t, err)
	state, err := store.Load(ctx, "logged")
	require.NoError(t, err)
	var got []string
	for _, m := range state.Messages {
		got = append(got, m.Role+":"+m.GetContent())
	}
	assert.Equal(t, []string{"user:ping", "assistant:pong"}, got)
}
