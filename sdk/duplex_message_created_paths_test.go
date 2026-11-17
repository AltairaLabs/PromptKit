package sdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// busTranscript collects message.created by index, noting any index published
// twice.
type busTranscript struct {
	mu   sync.Mutex
	byIx map[int]string
	dups []int
}

func newBusTranscript(bus *events.EventBus) *busTranscript {
	bt := &busTranscript{byIx: map[int]string{}}
	bus.Subscribe(events.EventMessageCreated, func(e *events.Event) {
		d, ok := e.Data.(*events.MessageCreatedData)
		if !ok {
			return
		}
		bt.mu.Lock()
		defer bt.mu.Unlock()
		if _, seen := bt.byIx[d.Index]; seen {
			bt.dups = append(bt.dups, d.Index)
		}
		bt.byIx[d.Index] = d.Role + ":" + d.GetContent()
	})
	return bt
}

func (bt *busTranscript) snapshot() (map[int]string, []int) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	out := make(map[int]string, len(bt.byIx))
	for k, v := range bt.byIx {
		out[k] = v
	}
	return out, append([]int(nil), bt.dups...)
}

// persistedTranscript returns the stored messages by index.
func persistedTranscript(t *testing.T, store statestore.Store, id string) map[int]string {
	t.Helper()
	state, err := store.Load(context.Background(), id)
	require.NoError(t, err)
	out := map[int]string{}
	for i := range state.Messages {
		out[i] = state.Messages[i].Role + ":" + state.Messages[i].GetContent()
	}
	return out
}

// realtimePath drives one way a realtime session produces messages: it is
// given the session's mock (created by then) and the conversation.
type realtimePath struct {
	name string
	// opts are extra conversation options.
	opts []Option
	// metadata is added to the streaming config.
	metadata map[string]any
	// drive produces the messages; the conversation is closed after it.
	drive func(t *testing.T, conv *Conversation, sess *mock.MockStreamSession)
	// want is the transcript both routes must hold.
	want map[int]string
	// live is how many of want reach the bus before Close; 0 means all.
	live int
}

// emitTurnText emits an assistant reply's text and completes it.
func emitTurnText(sess *mock.MockStreamSession, text string) {
	stop := "stop"
	sess.EmitChunk(&providers.StreamChunk{Content: text, Delta: text})
	sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
}

// emitInputTranscript emits the user's transcribed speech.
func emitInputTranscript(sess *mock.MockStreamSession, text string, final bool) {
	meta := map[string]any{"type": "input_transcription", "transcription": text}
	if final {
		meta["transcription_final"] = true
	}
	sess.EmitChunk(&providers.StreamChunk{Metadata: meta})
}

// Every way a realtime session adds a message to the conversation publishes it
// on the bus once, at the index the persisted transcript holds it.
func TestDuplexRealtime_EveryMessagePathBroadcastsAtItsPersistedIndex(t *testing.T) {
	paths := []realtimePath{
		{
			name: "final transcript then reply",
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				emitInputTranscript(sess, "hi there", true)
				emitTurnText(sess, "hello")
			},
			want: map[int]string{0: "user:hi there", 1: "assistant:hello"},
		},
		{
			name: "transcript at the reply's start",
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				emitInputTranscript(sess, "hi there", false)
				emitTurnText(sess, "hello")
			},
			want: map[int]string{0: "user:hi there", 1: "assistant:hello"},
		},
		{
			name: "transcript at a content-less turn's end",
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				stop := "stop"
				emitInputTranscript(sess, "hi there", false)
				sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop,
					CostInfo: &types.CostInfo{InputTokens: 3, OutputTokens: 1}})
			},
			want: map[int]string{0: "user:hi there", 1: "assistant:"},
		},
		{
			name: "barge-in",
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				stop := "stop"
				emitInputTranscript(sess, "tell me", true)
				sess.EmitChunk(&providers.StreamChunk{Content: "once upon", Delta: "once upon"})
				sess.EmitChunk(&providers.StreamChunk{Interrupted: true})
				sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
				emitInputTranscript(sess, "stop", true)
				emitTurnText(sess, "ok")
			},
			want: map[int]string{0: "user:tell me", 1: "assistant:once upon", 2: "user:stop", 3: "assistant:ok"},
		},
		{
			name: "reply cut short at close",
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				emitInputTranscript(sess, "tell me", true)
				sess.EmitChunk(&providers.StreamChunk{Content: "half a", Delta: "half a"})
			},
			want: map[int]string{0: "user:tell me", 1: "assistant:half a"},
			live: 1,
		},
		{
			name: "provider error mid-reply",
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				emitInputTranscript(sess, "tell me", true)
				sess.EmitChunk(&providers.StreamChunk{Content: "half a", Delta: "half a"})
				sess.EmitChunk(&providers.StreamChunk{Error: errors.New("socket closed")})
			},
			want: map[int]string{0: "user:tell me", 1: "assistant:half a"},
		},
		{
			name: "blocked typed turn",
			opts: []Option{WithGuardrail(guardrails.Input("banned_words",
				map[string]any{"words": []any{"wire transfer"}},
				guardrails.WithMessage("I can't help with transfers.")))},
			drive: func(t *testing.T, conv *Conversation, _ *mock.MockStreamSession) {
				require.NoError(t, conv.SendText(context.Background(), "arrange a wire transfer"))
			},
			want: map[int]string{0: "user:arrange a wire transfer", 1: "assistant:I can't help with transfers."},
		},
		{
			name:     "late transcript, reordered (OpenAI Realtime)",
			metadata: map[string]any{"input_transcript_ordering": "reorder"},
			drive: func(_ *testing.T, _ *Conversation, sess *mock.MockStreamSession) {
				// The transcript lands while the reply streams.
				stop := "stop"
				sess.EmitChunk(&providers.StreamChunk{Content: "hello", Delta: "hello"})
				emitInputTranscript(sess, "hi there", true)
				sess.EmitChunk(&providers.StreamChunk{FinishReason: &stop})
			},
			want: map[int]string{0: "user:hi there", 1: "assistant:hello"},
		},
	}
	for i, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			id := fmt.Sprintf("paths-%d", i)
			bus := events.NewEventBus()
			t.Cleanup(bus.Close)
			bt := newBusTranscript(bus)
			store := statestore.NewMemoryStore()
			prov := mock.NewStreamingProvider("rt", "rt-model", false)
			cfg := *convDuplexAudio
			cfg.Metadata = p.metadata
			opts := append([]Option{
				WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(&cfg),
				WithEventBus(bus), WithStateStore(store), WithConversationID(id),
			}, p.opts...)
			conv, err := OpenDuplex(writeIngestionTestPack(t), "main", opts...)
			require.NoError(t, err)
			convDrain(t, conv)

			// Audio input opens the realtime session.
			require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
				MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
			}))
			require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
			p.drive(t, conv, prov.GetSession())
			live := p.live
			if live == 0 {
				live = len(p.want)
			}
			require.Eventually(t, func() bool {
				got, _ := bt.snapshot()
				return len(got) >= live
			}, 5*time.Second, 5*time.Millisecond, "the messages reach the bus")
			require.NoError(t, conv.Close())
			bus.Close() // delivers any stray publish

			got, dups := bt.snapshot()
			assert.Equal(t, p.want, got, "message.created by index")
			assert.Empty(t, dups, "a message was published more than once")
			assert.Equal(t, p.want, persistedTranscript(t, store, id), "the persisted transcript")
		})
	}
}
