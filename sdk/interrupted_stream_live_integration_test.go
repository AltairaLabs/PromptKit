//go:build integration

package sdk_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// These tests cut real provider streams off partway and check what survives:
// the partial reply comes back with the error, the conversation saves it
// marked interrupted, and the next turn still works — the provider accepts the
// history, which it only does if the fragment was kept out of it.
//
// The hermetic suite proves the mechanics against a scripted provider. Only a
// live call proves each vendor's real stream ends the way the runtime now
// expects when the caller cancels mid-reasoning or mid-answer.
//
// Run (keys from the repo .env):
//
//	set -a; . ./.env; set +a
//	go test -tags integration ./sdk/ -run TestLive_InterruptedStream -v

const interruptedPackJSON = `{
	"id": "live-interrupted",
	"version": "1.0.0",
	"description": "Live interrupted-stream checks",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a careful assistant."
		}
	}
}`

// reasoningPrompt is hard enough that every reasoning model thinks before it
// answers, so there is reasoning in flight to interrupt.
const reasoningPrompt = "A bat and a ball cost $1.10 in total. The bat costs $1.00 more than the ball. " +
	"Three friends each buy one bat and two balls, then return one ball each. " +
	"How much did they spend in total? Work it out carefully."

// longAnswerPrompt produces a long visible answer, so there is text in flight
// to interrupt.
const longAnswerPrompt = "Write the numbers one to one hundred as English words, one per line. " +
	"Do not add anything else."

func openInterruptedConv(t *testing.T, lp liveReasoningProvider, bus *events.EventBus) *sdk.Conversation {
	t.Helper()
	provider, err := providers.CreateProviderFromSpec(lp.spec)
	require.NoErrorf(t, err, "%s: CreateProviderFromSpec", lp.name)

	packPath := t.TempDir() + "/interrupted.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(interruptedPackJSON), 0o644))

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithEventBus(bus),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}

func skipWithoutInterruptKey(t *testing.T, lp liveReasoningProvider) {
	t.Helper()
	for _, k := range lp.envKeys {
		if os.Getenv(k) != "" {
			return
		}
	}
	t.Skipf("none of %v set", lp.envKeys)
}

// assertSavedInterrupted checks the conversation saved the partial reply as the
// last message, marked interrupted, then that a fresh turn succeeds.
func assertSavedInterruptedThenRecovers(t *testing.T, conv *sdk.Conversation, name, wantContentPrefix string) {
	t.Helper()
	history := conv.Messages(context.Background())
	require.NotEmptyf(t, history, "%s: nothing saved for the interrupted turn", name)
	last := history[len(history)-1]
	assert.Equalf(t, "assistant", last.Role, "%s: last saved message", name)
	assert.Truef(t, last.IsInterrupted(), "%s: saved reply not marked interrupted (finish %q)",
		name, last.FinishReason)
	assert.NotEmptyf(t, last.Meta[types.MetaInterruptedCause], "%s: no interruption cause saved", name)
	if wantContentPrefix != "" {
		assert.Equalf(t, wantContentPrefix, last.Content, "%s: saved text differs from what streamed", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	resp, err := conv.Send(ctx, "Reply with just the word: ready")
	require.NoErrorf(t, err, "%s: the turn after an interrupted one must succeed — "+
		"a provider rejecting the history means the fragment was sent back", name)
	assert.NotEmptyf(t, strings.TrimSpace(resp.Text()), "%s: empty reply after the interrupted turn", name)
	assert.NotEqualf(t, types.FinishReasonInterrupted, resp.Message().FinishReason, "%s", name)
}

// TestLive_InterruptedStream_CancelDuringReasoning cancels a Send as soon as the
// model's first reasoning arrives, so the stream dies mid-thought.
func TestLive_InterruptedStream_CancelDuringReasoning(t *testing.T) {
	for _, lp := range liveReasoningProviders() {
		t.Run(lp.name, func(t *testing.T) {
			skipWithoutInterruptKey(t, lp)
			bus := events.NewEventBus()
			conv := openInterruptedConv(t, lp, bus)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var once sync.Once
			cut := make(chan struct{})
			unsub := bus.Subscribe(events.EventReasoningDelta, func(*events.Event) {
				once.Do(func() { close(cut); cancel() })
			})
			defer unsub()

			resp, err := conv.Send(ctx, reasoningPrompt)
			select {
			case <-cut:
			default:
				if !lp.reasonsOnToolRounds {
					// OpenAI supplies reasoning summaries only on some runs;
					// the answer-stream test covers its interruption.
					t.Skipf("%s: no reasoning streamed on this run, nothing to cut", lp.name)
				}
				t.Fatalf("%s: a reliably-reasoning model streamed no reasoning (err=%v)", lp.name, err)
			}
			require.Errorf(t, err, "%s: the canceled Send must report the cancellation", lp.name)
			require.Truef(t, errors.Is(err, context.Canceled), "%s: err = %v, want context.Canceled", lp.name, err)
			require.NotNilf(t, resp, "%s: no partial reply returned with the cancellation", lp.name)
			assert.Equalf(t, types.FinishReasonInterrupted, resp.Message().FinishReason, "%s", lp.name)

			reasoning := resp.Message().Reasoning
			t.Logf("%s: cut mid-reasoning; partial text %d chars, reasoning %d chars",
				lp.name, len(resp.Text()), len(reasoningText(reasoning)))
			require.NotNilf(t, reasoning, "%s: cut mid-reasoning but no reasoning came back", lp.name)
			assert.NotEmptyf(t, reasoning.Text, "%s: cut mid-reasoning but reasoning is empty", lp.name)

			assertSavedInterruptedThenRecovers(t, conv, lp.name, "")
		})
	}
}

// TestLive_InterruptedStream_CancelDuringAnswer cancels a Stream a few text
// chunks into a long answer.
func TestLive_InterruptedStream_CancelDuringAnswer(t *testing.T) {
	const cutAfterChunks = 3
	for _, lp := range liveReasoningProviders() {
		t.Run(lp.name, func(t *testing.T) {
			skipWithoutInterruptKey(t, lp)
			conv := openInterruptedConv(t, lp, events.NewEventBus())

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			var streamed strings.Builder
			var final sdk.StreamChunk
			textChunks := 0
			for chunk := range conv.Stream(ctx, longAnswerPrompt) {
				if chunk.Type == sdk.ChunkText && chunk.Error == nil {
					streamed.WriteString(chunk.Text)
					if textChunks++; textChunks == cutAfterChunks {
						cancel()
					}
				}
				final = chunk
			}

			require.GreaterOrEqualf(t, textChunks, cutAfterChunks,
				"%s: the answer finished before it could be cut (%d chunks)", lp.name, textChunks)
			require.Errorf(t, final.Error, "%s: the canceled stream must end on an error", lp.name)
			require.Truef(t, errors.Is(final.Error, context.Canceled),
				"%s: final error = %v, want context.Canceled", lp.name, final.Error)
			require.NotNilf(t, final.Message, "%s: the error chunk carries no partial reply", lp.name)
			assert.Equalf(t, types.FinishReasonInterrupted, final.Message.Message().FinishReason, "%s", lp.name)

			got := final.Message.Text()
			t.Logf("%s: streamed %d chars before the cut; partial reply %d chars", lp.name, streamed.Len(), len(got))
			require.NotEmptyf(t, got, "%s: partial reply has no text", lp.name)
			assert.Truef(t, strings.HasPrefix(got, streamed.String()),
				"%s: partial reply %q does not start with the text the caller saw %q", lp.name, got, streamed.String())

			assertSavedInterruptedThenRecovers(t, conv, lp.name, got)
		})
	}
}

func reasoningText(r *types.ReasoningTrace) string {
	if r == nil {
		return ""
	}
	return r.Text
}
