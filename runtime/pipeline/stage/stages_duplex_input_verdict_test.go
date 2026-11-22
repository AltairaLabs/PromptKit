package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// An input guardrail's verdict on the caller's spoken turn that takes longer
// than the stage waits in line for (a judge model) is acted on when it
// arrives; meanwhile the reply streams, except where the turn's outcome needs
// the verdict: before the first of a reply is forwarded and before a turn
// ends.

// slowInputHook is inputBannedWords that takes delay to decide.
type slowInputHook struct {
	hooks.ProviderHook
	delay time.Duration
}

func (h *slowInputHook) BeforeCall(ctx context.Context, req *hooks.ProviderRequest) hooks.Decision {
	time.Sleep(h.delay)
	return h.ProviderHook.BeforeCall(ctx, req)
}

// slowVerdictStage is a stage whose input guardrail bans "wire" and decides
// well after the stage stops waiting for it in line.
func slowVerdictStage(t *testing.T) (*DuplexProviderStage, *forgettingSession) {
	t.Helper()
	s, sess := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(
		&slowInputHook{ProviderHook: inputBannedWords(t, "wire"), delay: 30 * time.Millisecond})))
	s.inputVerdictInline = time.Millisecond
	return s, sess
}

// A turn blocked before its reply began: the reply's first chunk waits for the
// verdict, so none of the blocked reply is heard.
func TestDuplexInputVerdict_ReplyWaitsForAVerdictBeforeItBegins(t *testing.T) {
	s, sess := slowVerdictStage(t)

	elems, err := feedChunksLateVerdicts(t, s,
		spoken("a wire transfer", "item_A"),
		genChunk(1, transcriptChunk("Sure", pcm(10))),
		genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"user:a wire transfer",
		"interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, []string{"item_A"}, sess.forgotten())
}

// heldHook is an input guardrail whose verdicts wait for release, then allow.
type heldHook struct{ release chan struct{} }

func (*heldHook) Name() string { return "held" }

func (h *heldHook) BeforeCall(ctx context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	select {
	case <-h.release:
	case <-ctx.Done():
	}
	return hooks.Allow
}

func (*heldHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// The reply streaming when the caller's transcript arrives goes on while its
// verdict is out: its next chunk is forwarded at once.
func TestDuplexInputVerdict_StreamingReplyDoesNotWait(t *testing.T) {
	judge := &heldHook{release: make(chan struct{})}
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(judge)))
	s.inputVerdictInline = time.Millisecond
	ctx := context.Background()
	out := make(chan StreamElement, 16)
	for _, c := range []providers.StreamChunk{
		genChunk(1, transcriptChunk("Sure, ", pcm(10))), spoken("hello", "item_A"),
	} {
		require.NoError(t, s.handleResponseChunk(ctx, &c, out))
	}

	handled := make(chan error, 1)
	go func() {
		next := genChunk(1, transcriptChunk("once upon a time", pcm(10)))
		handled <- s.handleResponseChunk(ctx, &next, out)
	}()
	select {
	case err := <-handled:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		close(judge.release)
		require.FailNow(t, "the reply's next chunk waited for the verdict")
	}
	close(judge.release)
	close(out)
	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	assert.Equal(t, []string{"text:Sure, ", "audio:10", "user:hello", "text:once upon a time", "audio:10"},
		trace(elems))
}

// A reply that ends while its turn's verdict is out waits for it: the turn
// ends blocked, cut once.
func TestDuplexInputVerdict_TurnEndWaitsForTheVerdict(t *testing.T) {
	s, sess := slowVerdictStage(t)

	elems, err := feedChunksLateVerdicts(t, s,
		genChunk(1, transcriptChunk("Sure, ", pcm(10))),
		spoken("a wire transfer", "item_A"),
		genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure, ", "audio:10",
		"user:a wire transfer",
		"interrupt", "text:I can't help with transfers.",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, []int64{1}, sess.interruptedGens())
}

// A reply a barge-in ends while its turn's verdict is out keeps that verdict:
// the partial reply is the blocked turn's.
func TestDuplexInputVerdict_BargeInWaitsForTheVerdict(t *testing.T) {
	s, _ := slowVerdictStage(t)

	elems, err := feedChunksLateVerdicts(t, s,
		genChunk(1, transcriptChunk("Sure, ", pcm(10))),
		spoken("a wire transfer", "item_A"),
		providers.StreamChunk{Generation: 1, Interrupted: true})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure, ", "audio:10",
		"user:a wire transfer",
		"interrupt", "text:I can't help with transfers.",
		"partial:I can't help with transfers.|safety",
	}, verdictTrace(elems))
	assert.Empty(t, s.pendingVerdicts, "no verdict is left for the next turn")
}

// verdictTrace is trace with a reply cut short shown as
// "partial:<content>|<finish reason>".
func verdictTrace(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Meta.Interrupted && e.Message != nil && e.Message.Role == core.RoleAssistant {
			out = append(out, "partial:"+e.Message.Content+"|"+e.Message.FinishReason)
			continue
		}
		out = append(out, trace([]StreamElement{e})...)
	}
	return out
}

// A reply that opens with a tool call or reasoning, rather than text or
// audio, still waits for the verdict before anything of it goes out: the
// blocked turn's reply is not heard.
func TestDuplexInputVerdict_ReplyOpeningWithToolCallsOrReasoningWaits(t *testing.T) {
	openers := map[string]providers.StreamChunk{
		"tool calls": genChunk(1, providers.StreamChunk{ToolCalls: []types.MessageToolCall{weatherCall("c1")}}),
		"reasoning":  genChunk(1, providers.StreamChunk{Reasoning: "the caller wants a transfer"}),
	}
	for name, opener := range openers {
		t.Run(name, func(t *testing.T) {
			s, _ := slowVerdictStage(t)

			elems, err := feedChunksLateVerdicts(t, s,
				spoken("a wire transfer", "item_A"),
				opener,
				genChunk(1, transcriptChunk("Sure", pcm(10))),
				genChunk(1, stopChunk()))
			require.NoError(t, err)

			assert.Equal(t, []string{
				"user:a wire transfer",
				"interrupt", "text:I can't help with transfers.",
				"end:I can't help with transfers.|safety",
			}, trace(elems))
			msg, _ := turnMessage(t, elems)
			assert.Empty(t, msg.ToolCalls, "the blocked turn's tool calls are not run")
			for i, e := range elems {
				if e.Reasoning != nil {
					assert.Greater(t, i, 1, "reasoning goes out only after the verdict: %v", trace(elems))
				}
			}
		})
	}
}

// The next reply waits for the verdict before its first output whether the
// last reply ended (a provider that numbers no replies) or a new one
// supersedes the reply that was streaming.
func TestDuplexInputVerdict_NextReplyWaits(t *testing.T) {
	cases := map[string][]providers.StreamChunk{
		"after an ended reply": {
			spoken("hello", "item_0"), transcriptChunk("Hi", pcm(10)), stopChunk(),
			spoken("a wire transfer", "item_A"),
			transcriptChunk("Sure", pcm(10)), stopChunk(),
		},
		"superseding a streaming reply": {
			genChunk(1, transcriptChunk("Hi", pcm(10))),
			spoken("a wire transfer", "item_A"),
			genChunk(2, transcriptChunk("Sure", pcm(10))), genChunk(2, stopChunk()),
		},
	}
	for name, chunks := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := slowVerdictStage(t)

			elems, err := feedChunksLateVerdicts(t, s, chunks...)
			require.NoError(t, err)

			assert.NotContains(t, trace(elems), "text:Sure", "the blocked turn's reply is not heard")
			assert.Contains(t, trace(elems), "text:I can't help with transfers.")
		})
	}
}

// A transcript that arrives after its tool turn's reply, while the follow-up
// reply streams, is judged without holding that reply back; its user message
// goes out once the verdict is in.
func TestDuplexInputVerdict_LateTranscriptDoesNotStallTheReply(t *testing.T) {
	judge := &heldHook{release: make(chan struct{})}
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(judge)))
	s.inputVerdictInline = time.Hour // only a streaming reply goes on at once
	ctx := context.Background()
	out := make(chan StreamElement, 32)
	toolTurn := genChunk(1, toolCallTurn(weatherCall("c1"))[0])
	handled := make(chan error, 1)
	go func() {
		for _, c := range []providers.StreamChunk{
			toolTurn, genChunk(2, transcriptChunk("The weather ", pcm(10))), spoken("what's the weather", "item_A"),
			genChunk(2, transcriptChunk("is sunny", pcm(10))),
		} {
			if err := s.handleResponseChunk(ctx, &c, out); err != nil {
				handled <- err
				return
			}
		}
		handled <- nil
	}()
	select {
	case err := <-handled:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		close(judge.release)
		require.FailNow(t, "the reply's next chunk waited for the late transcript's verdict")
	}
	close(judge.release)
	require.NoError(t, s.awaitVerdicts(ctx, out))
	close(out)
	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	tr := trace(elems)
	require.NotEmpty(t, tr)
	assert.Equal(t, []string{"text:The weather ", "audio:10", "text:is sunny", "audio:10", "user:what's the weather"},
		tr[len(tr)-5:])
}

// A transcript assembled from incremental chunks is materialized when its
// turn ends (a provider that marks no final transcript); the turn's end waits
// for its verdict.
func TestDuplexInputVerdict_TurnEndWaitsForATranscriptMaterializedThere(t *testing.T) {
	s, _ := slowVerdictStage(t)

	elems, err := feedChunksLateVerdicts(t, s,
		genChunk(1, transcriptChunk("Sure", pcm(10))),
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer",
		}},
		genChunk(1, stopChunk()))
	require.NoError(t, err)

	msg, _ := turnMessage(t, elems)
	assert.Equal(t, "I can't help with transfers.", msg.Content)
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
}

// A verdict that allows the turn changes nothing: the reply streams and ends
// as it would have.
func TestDuplexInputVerdict_AllowedTurnStreamsAsItWould(t *testing.T) {
	s, sess := slowVerdictStage(t)

	elems, err := feedChunksLateVerdicts(t, s,
		spoken("hello", "item_A"),
		genChunk(1, transcriptChunk("Hi", pcm(10))),
		genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{"user:hello", "text:Hi", "audio:10", "end:Hi|stop"}, trace(elems))
	assert.Empty(t, sess.forgotten())
	assert.Empty(t, sess.interruptedGens())
}
