package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// End-to-end guardrails on a realtime duplex conversation (#1682), through
// OpenDuplex, SendText and Response, as a voice application uses them. The
// realtime provider stage used to run no hooks at all, so a pack's
// validators and WithGuardrail did nothing in duplex.

// duplexTurn is what a Response() reader receives for one assistant turn.
type duplexTurn struct {
	text         string
	finishReason string
}

// openGuardedDuplex opens a duplex conversation on a mock realtime provider
// that answers every input with reply.
func openGuardedDuplex(t *testing.T, packPath, reply string, opts ...Option) *Conversation {
	t.Helper()
	opts = append([]Option{
		WithProvider(mock.NewStreamingProvider("mock", "mock-model", false).WithAutoRespond(reply)),
		// A streaming config selects the realtime (ASM) path, DuplexProviderStage.
		WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}),
		WithSkipSchemaValidation(),
	}, opts...)
	conv, err := OpenDuplex(packPath, "main", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

// sendDuplexTurn sends text and reads Response() until the assistant turn
// completes.
func sendDuplexTurn(t *testing.T, conv *Conversation, text string) duplexTurn {
	t.Helper()
	ch, err := conv.Response()
	require.NoError(t, err)
	require.NoError(t, conv.SendText(context.Background(), text))

	var turn duplexTurn
	var b strings.Builder
	timeout := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-ch:
			require.True(t, ok, "the response channel closed before the turn completed")
			require.NoError(t, chunk.Error)
			b.WriteString(chunk.Delta)
			if done, _ := chunk.Metadata["assistant_turn_complete"].(bool); done {
				turn.text = b.String()
				turn.finishReason, _ = chunk.Metadata["finish_reason"].(string)
				return turn
			}
		case <-timeout:
			require.FailNow(t, "the assistant turn never completed")
		}
	}
}

// An input guardrail checks text before it reaches the realtime session. A
// blocked turn never reaches the model: the caller gets the guardrail's
// message, marked safety, as on the text path.
func TestDuplexGuardrail_InputBlocksTurn(t *testing.T) {
	conv := openGuardedDuplex(t, writeIngestionTestPack(t), "Sure, sending the money now.",
		WithGuardrail(guardrails.Input("banned_words", map[string]any{
			"words": []any{"wire transfer"},
		}, guardrails.WithMessage("I can't help with transfers."))),
	)

	turn := sendDuplexTurn(t, conv, "please arrange a wire transfer")

	assert.Equal(t, "I can't help with transfers.", turn.text)
	assert.Equal(t, types.FinishReasonSafety, turn.finishReason)
}

// An output guardrail checks the realtime reply as it streams. The banned
// text never reaches the caller, and the firing is reported on the
// conversation's event bus as a text turn's is.
func TestDuplexGuardrail_OutputBlocksReply(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	var mu sync.Mutex
	var failed []*events.ValidationEventData
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) {
		if d, ok := e.Data.(*events.ValidationEventData); ok {
			mu.Lock()
			failed = append(failed, d)
			mu.Unlock()
		}
	})
	conv := openGuardedDuplex(t, writeIngestionTestPack(t), "the code is forbidden-word",
		WithEventBus(bus),
		WithGuardrail(guardrails.Output("banned_words", map[string]any{
			"words": []any{"forbidden"},
		}, guardrails.WithMessage("I can't share that."))),
	)

	turn := sendDuplexTurn(t, conv, "hello")

	assert.NotContains(t, turn.text, "forbidden", "the banned text must not reach the caller")
	assert.Equal(t, types.FinishReasonSafety, turn.finishReason)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(failed) > 0
	}, 2*time.Second, 10*time.Millisecond, "the firing must be reported as validation.failed")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "output", failed[0].Direction)
}

// A pack's own validators run in duplex as they do in text.
func TestDuplexGuardrail_PackValidatorsRun(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "guarded.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(`{
		"name": "guarded",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant.",
				"validators": [
					{"type": "banned_words", "enabled": true, "params": {"words": ["forbidden"]}}
				]
			}
		}
	}`), 0o600))
	conv := openGuardedDuplex(t, packPath, "this is forbidden")

	turn := sendDuplexTurn(t, conv, "hello")

	assert.NotContains(t, turn.text, "forbidden")
	assert.Equal(t, types.FinishReasonSafety, turn.finishReason)
}

// The discriminating half: a clean turn passes through untouched and is not
// marked safety.
func TestDuplexGuardrail_CleanTurnPassesThrough(t *testing.T) {
	conv := openGuardedDuplex(t, writeIngestionTestPack(t), "Paris is the capital.",
		WithGuardrail(guardrails.Input("banned_words", map[string]any{"words": []any{"wire transfer"}})),
		WithGuardrail(guardrails.Output("banned_words", map[string]any{"words": []any{"forbidden"}})),
	)

	turn := sendDuplexTurn(t, conv, "what is the capital of France?")

	assert.Equal(t, "Paris is the capital.", turn.text)
	assert.NotEqual(t, types.FinishReasonSafety, turn.finishReason)
}

// realtimeGuardCut is what a voice caller saw of a realtime session in
// order: "user:<transcript>", "text:<delta>", "audio", "interrupted" and
// "end:<finish reason>".
func realtimeGuardCut(t *testing.T, ch <-chan providers.StreamChunk, turns int) []string {
	t.Helper()
	var got []string
	timeout := time.After(5 * time.Second)
	for turns > 0 {
		select {
		case c, ok := <-ch:
			require.True(t, ok, "the response channel closed: the session ended")
			require.NoError(t, c.Error, "a blocked turn must not fail the session")
			if c.Interrupted {
				got = append(got, "interrupted")
			}
			if u, _ := c.Metadata["input_transcription"].(string); u != "" {
				got = append(got, "user:"+u)
			}
			if c.Delta != "" {
				got = append(got, "text:"+c.Delta)
			}
			if c.MediaData != nil && len(c.MediaData.Data) > 0 {
				got = append(got, "audio")
			}
			if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
				fr, _ := c.Metadata["finish_reason"].(string)
				got = append(got, "end:"+fr)
				turns--
			}
		case <-timeout:
			require.FailNow(t, "the turns never completed", "%v", got)
		}
	}
	return got
}

// A realtime voice session whose guardrails block a reply keeps going. An
// output guardrail that trips mid-reply, and an input guardrail on the
// caller's spoken transcript, each end their turn: the provider is asked to
// stop the reply, the caller is told to stop playing it (chunk.Interrupted),
// the rest of the reply never arrives, and the turn is stored as the
// guardrail's message, marked safety. The next turn is answered normally, and
// every provider call that started completed.
func TestDuplexGuardrail_RealtimeBlockEndsTheTurnNotTheSession(t *testing.T) {
	bus := events.NewEventBus()
	bt := newBusTranscript(bus)
	var mu sync.Mutex
	calls := map[events.EventType]int{}
	for _, et := range []events.EventType{events.EventProviderCallStarted, events.EventProviderCallCompleted} {
		bus.Subscribe(et, func(e *events.Event) {
			mu.Lock()
			calls[e.Type]++
			mu.Unlock()
		})
	}
	store := statestore.NewMemoryStore()
	prov := mock.NewStreamingProvider("rt", "rt-model", false)
	conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
		WithProvider(prov), WithSkipSchemaValidation(), WithStreamingConfig(convDuplexAudio),
		WithEventBus(bus), WithStateStore(store), WithConversationID("guarded"),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{"words": []any{"wire transfer"}},
				guardrails.WithMessage("I can't help with transfers.")),
			guardrails.Output("banned_words", map[string]any{"words": []any{"forbidden"}},
				guardrails.WithMessage("I can't share that.")),
		),
	)
	require.NoError(t, err)
	ch, err := conv.Response()
	require.NoError(t, err)
	require.NoError(t, conv.SendChunk(context.Background(), &providers.StreamChunk{
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320)},
	}))
	require.Eventually(t, func() bool { return prov.GetSession() != nil }, 5*time.Second, time.Millisecond)
	sess := prov.GetSession()

	stop := "stop"
	said := func(text string) *providers.StreamChunk {
		return &providers.StreamChunk{Delta: text, Metadata: map[string]interface{}{"type": "output_transcription"},
			MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: make([]byte, 320), SampleRate: 24000}}
	}
	heard := func(text string) *providers.StreamChunk {
		return &providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": text, "transcription_final": true}}
	}
	end := &providers.StreamChunk{FinishReason: &stop}
	for _, c := range []*providers.StreamChunk{
		heard("what is the code"), said("the code is "), said("forbidden"), said(" 1234"), end,
		heard("now do a wire transfer"), said("Sure, "), said("sending it"), end,
		heard("thanks"), said("You're welcome."), end,
	} {
		sess.EmitChunk(c)
	}

	assert.Equal(t, []string{
		"user:what is the code", "text:the code is ", "audio", "interrupted", "end:safety",
		"user:now do a wire transfer", "interrupted", "end:safety",
		"user:thanks", "text:You're welcome.", "audio", "end:stop",
	}, realtimeGuardCut(t, ch, 3))
	assert.Equal(t, 2, sess.Interrupts(), "the provider is asked to stop each blocked reply")

	want := map[int]string{
		0: "user:what is the code", 1: "assistant:I can't share that.",
		2: "user:now do a wire transfer", 3: "assistant:I can't help with transfers.",
		4: "user:thanks", 5: "assistant:You're welcome.",
	}
	require.Eventually(t, func() bool {
		got, _ := bt.snapshot()
		return len(got) >= len(want)
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, conv.Close())
	bus.Close()
	got, dups := bt.snapshot()
	assert.Equal(t, want, got, "message.created")
	assert.Empty(t, dups)
	assert.Equal(t, want, persistedTranscript(t, store, "guarded"), "the stored transcript")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, calls[events.EventProviderCallStarted])
	assert.Equal(t, 3, calls[events.EventProviderCallCompleted], "every provider call pairs")
}
