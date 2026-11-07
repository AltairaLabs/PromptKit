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
