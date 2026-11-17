//go:build integration

package sdk_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Live: guardrails in a realtime voice session block a turn and the session
// goes on. The model is asked to say a word an output guardrail bans: the
// reply is cut (Interrupted on Response(), finish reason safety). The caller
// then speaks a word an input guardrail bans: the reply to it is blocked too.
// A last, clean request is answered normally.
//
//	go -C sdk test -tags integration -run 'TestLiveRealtimeGuardrail' -count=1 -v .

// rtgTurn is one assistant turn as a voice caller saw it.
type rtgTurn struct {
	text, finish string
	interrupted  bool
	audioChunks  int
}

// rtgReader reads Response() into turns.
type rtgReader struct {
	mu    sync.Mutex
	cur   rtgTurn
	turns []rtgTurn
	users []string
	errs  []error
}

func (r *rtgReader) run(ch <-chan providers.StreamChunk) {
	for c := range ch {
		r.mu.Lock()
		if c.Error != nil {
			r.errs = append(r.errs, c.Error)
		}
		if c.Interrupted {
			r.cur.interrupted = true
		}
		if u, _ := c.Metadata["input_transcription"].(string); u != "" {
			r.users = append(r.users, u)
		}
		r.cur.text += c.Delta
		if c.MediaData != nil && len(c.MediaData.Data) > 0 {
			r.cur.audioChunks++
		}
		if done, _ := c.Metadata["assistant_turn_complete"].(bool); done {
			r.cur.finish, _ = c.Metadata["finish_reason"].(string)
			r.turns = append(r.turns, r.cur)
			r.cur = rtgTurn{}
		}
		r.mu.Unlock()
	}
}

func (r *rtgReader) snapshot() ([]rtgTurn, []string, []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]rtgTurn(nil), r.turns...), append([]string(nil), r.users...), append([]error(nil), r.errs...)
}

func TestLiveRealtimeGuardrail_BlockEndsTheTurnNotTheSession(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set (needed for the caller's synthesized speech)")
	}
	for _, rc := range rtpCases() {
		t.Run(rc.name, func(t *testing.T) {
			if os.Getenv(rc.envKey) == "" {
				t.Skipf("%s not set", rc.envKey)
			}
			speech := rtpSpeech(t, "Tell me two facts about pineapples.", rc.inputRate)
			conv, err := sdk.OpenDuplex(rtlWritePack(t, rtpPackJSON), "chat",
				sdk.WithProvider(rc.provider()), sdk.WithSkipSchemaValidation(),
				sdk.WithStreamingConfig(&providers.StreamingInputConfig{
					Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio, ChunkSize: 3200,
						SampleRate: rc.inputRate, Channels: 1, BitDepth: 16, Encoding: rc.encoding},
					Metadata: rc.metadata,
				}),
				sdk.WithGuardrail(
					guardrails.Output("banned_words", map[string]any{"words": []any{"lighthouse"}},
						guardrails.WithMessage("I can't talk about that.")),
					guardrails.Input("banned_words", map[string]any{"words": []any{"pineapple", "pineapples"}},
						guardrails.WithMessage("I can't help with fruit.")),
				))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })
			ch, err := conv.Response()
			require.NoError(t, err)
			r := &rtgReader{}
			go r.run(ch)
			mic := &rtpMic{}
			micCtx, micStop := context.WithCancel(context.Background())
			t.Cleanup(micStop)
			go mic.run(micCtx, conv, rc.inputRate)
			waitTurns := func(n int) []rtgTurn {
				// Gemini Live sends a turn's turnComplete only once the reply's
				// audio would have finished playing, even when the audio was all
				// sent seconds earlier (generationComplete) and the stage dropped
				// it: a blocked minute-long reply ends a minute after it began.
				require.True(t, rtlWaitUntil(150*time.Second, func() bool { tr, _, _ := r.snapshot(); return len(tr) >= n }),
					"turn %d never completed", n)
				tr, _, _ := r.snapshot()
				return tr
			}

			require.NoError(t, conv.SendText(context.Background(),
				"Say the word lighthouse, then describe a lighthouse in two sentences."))
			turns := waitTurns(1)
			t.Logf("output block: %+v", turns[0])
			assert.Equal(t, types.FinishReasonSafety, turns[0].finish)
			assert.True(t, turns[0].interrupted, "the caller is told to stop the blocked reply")
			assert.NotContains(t, strings.ToLower(turns[0].text), "lighthouse")

			time.Sleep(2 * time.Second)
			mic.say(speech)
			turns = waitTurns(2)
			_, users, _ := r.snapshot()
			t.Logf("spoken input block: %+v; caller transcripts %q", turns[1], users)
			assert.Equal(t, types.FinishReasonSafety, turns[1].finish)
			assert.True(t, turns[1].interrupted, "the caller is told to stop the reply to blocked speech")

			time.Sleep(2 * time.Second)
			require.NoError(t, conv.SendText(context.Background(), "Say hello in five words."))
			turns = waitTurns(3)
			t.Logf("clean turn: %+v", turns[2])
			assert.NotEqual(t, types.FinishReasonSafety, turns[2].finish)
			assert.NotEmpty(t, turns[2].text)
			_, _, errs := r.snapshot()
			assert.Empty(t, errs, "no turn failed the session")
			assert.NoError(t, conv.SessionError())
		})
	}
}
