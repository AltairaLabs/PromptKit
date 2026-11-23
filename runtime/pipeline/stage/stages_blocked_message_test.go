package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/cue"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// hostTexts is a BlockedMessageFunc answering reply, recording what it is given.
type hostTexts struct {
	reply string
	mu    sync.Mutex
	got   []BlockedMessage
}

func (h *hostTexts) fn(_ context.Context, b BlockedMessage) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.got = append(h.got, b)
	return h.reply
}

func (h *hostTexts) calls() []BlockedMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]BlockedMessage(nil), h.got...)
}

// A host text not known in advance renders on demand. A render past the
// budget plays the earcon at once, with the host's text shown; the clip it
// renders later is kept for the next cut.
func TestBlockedMessage_HostTextPastTheBudgetPlaysTheEarcon(t *testing.T) {
	const hostText = "Hablemos de otra cosa."
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	host := &hostTexts{reply: hostText}
	s.SetBlockedMessage(host.fn)
	speech := cueRamp(4800)
	svc := newFakeTTS(speech)
	svc.gate = make(chan struct{})
	cache := cueclip.NewMemoryCache(1 << 20)
	cs := newCueStageFor(s, cue.Config{TTS: svc, Cache: cache, OnDemandBudget: 20 * time.Millisecond})
	stored := storedTexts(cs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.cues.SessionStarted(ctx, spokenSession("alloy", cueTestRate), nil, nil)

	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)
	c := startChain(t, cs)
	for _, e := range elems {
		c.in <- e
	}
	got := c.finish(t)

	assert.Contains(t, cueTrace(got), "text:"+hostText)
	assert.Contains(t, cueTrace(got), "end:"+hostText+"|safety")
	assert.Equal(t, cue.EarconPCM(cueTestRate), cueAudioOf(got), "past the budget the earcon plays")

	close(svc.gate)
	assert.Equal(t, hostText, waitStored(t, stored))
	clip, ok, err := cache.Get(context.Background(), cueclip.KeyFor(hostText, "alloy", "tts:fake", pcm24k))
	require.NoError(t, err)
	require.True(t, ok, "the late clip is cached")
	assert.Equal(t, cueFaded(speech, cueTestRate), clip.PCM)
	require.Len(t, host.calls(), 1)
	assert.Equal(t, "I can't say that.", host.calls()[0].Text)
}

// A length guardrail that truncates a reply blocks it with no message: the
// function is not asked for one, and the truncation stands.
func TestBlockedMessage_TruncationDoesNotAsk(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 16})
	require.NoError(t, err)
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))
	host := &hostTexts{reply: "Never shown."}
	s.SetBlockedMessage(host.fn)

	elems, err := feedChunks(t, s,
		transcriptChunk("short start ", pcm(20)), transcriptChunk("then far too long", pcm(30)), stopChunk())
	require.NoError(t, err)

	assert.Empty(t, host.calls())
	assert.Contains(t, cueTrace(elems), "end:short start then|safety")
}

// A chunk interceptor that denies blocks the reply with the default message:
// the function is asked about it and its text is the turn's.
func TestBlockedMessage_DenyingChunkHookAsks(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&maxCharsChunkInterceptor{maxChars: 3})))
	host := &hostTexts{reply: "Host text."}
	s.SetBlockedMessage(host.fn)

	elems, err := feedChunks(t, s, transcriptChunk("too long", pcm(20)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"interrupt", "text:Host text.", "end:Host text.|safety"}, cueTrace(elems))
	assert.Equal(t, []BlockedMessage{{Direction: hooks.DirectionOutput, Reason: "content length 8 exceeds max 3",
		Text: prompt.DefaultBlockedMessage}}, host.calls())
}

// lengthGuard is the length guardrail at max characters: it truncates.
func lengthGuard(t *testing.T, max int) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": max})
	require.NoError(t, err)
	return guard
}

// textStage is a ProviderStage on the mock provider, streaming or not, with
// hook and fn.
func textStage(streaming bool, hook hooks.ProviderHook, fn BlockedMessageFunc) *ProviderStage {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false), streaming: streaming}
	return NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{MaxTokens: 100, BlockedMessage: fn},
		nil, hooks.NewRegistry(hooks.WithProviderHook(hook)))
}

// On the text path the function chooses a blocked turn's text, before the
// call, on a streamed chunk and on the completed reply, and is given the
// firing.
func TestBlockedMessage_TextPathTakesTheHostText(t *testing.T) {
	cases := []struct {
		name      string
		streaming bool
		hook      hooks.ProviderHook
		want      BlockedMessage
	}{
		{"before call", false, &enforceBeforeCallHook{reason: "input blocked", replacement: "Canned.",
			metadata: map[string]any{"validator_type": "test_input"}},
			BlockedMessage{ValidatorType: "test_input", Direction: hooks.DirectionInput, Reason: "input blocked",
				Text: "Canned."}},
		{"completed reply", false, &enforceAfterCallHook{replacement: "Replaced."},
			BlockedMessage{ValidatorType: "test_output", Direction: hooks.DirectionOutput, Reason: "output blocked",
				Text: "Replaced."}},
		{"streamed chunk", true, bannedWordsHook(t, "Mock"),
			BlockedMessage{ValidatorType: "banned_words", Direction: hooks.DirectionOutput,
				Reason: `forbidden content found in stream: "Mock"`, Text: "I can't say that."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := &hostTexts{reply: "Host text."}
			elems, err := runProviderStage(t, textStage(tc.streaming, tc.hook, host.fn), "hi")
			require.NoError(t, err)
			msgs := assistantMessages(elems)
			require.Len(t, msgs, 1)
			assert.Equal(t, "Host text.", msgs[0].Content)
			assert.Equal(t, []BlockedMessage{tc.want}, host.calls())
		})
	}
}

// A streamed reply a guardrail replaced is not judged again on completion:
// the host's text, though it names what the guardrail bans, is the turn's,
// and the function is asked once. The turn index is the 0-based index of the
// turn (the third turn is 2), as on the realtime path.
func TestBlockedMessage_TextPathReplacementIsNotJudgedAgain(t *testing.T) {
	host := &hostTexts{reply: "Mock is not discussed."}
	turn := NewTurnState()
	turn.SetTurnIndex(3)
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false), streaming: true}
	s := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{MaxTokens: 100, BlockedMessage: host.fn},
		nil, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "Mock"))), turn)

	elems, err := runProviderStage(t, s, "hi")
	require.NoError(t, err)

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Mock is not discussed.", msgs[0].Content)
	assert.Len(t, msgs[0].Validations, 1, "one firing")
	require.Len(t, host.calls(), 1)
	assert.Equal(t, 2, host.calls()[0].TurnIndex)
}

// On the text path a truncation, streamed or on the completed reply, does not
// ask the function.
func TestBlockedMessage_TextPathTruncationDoesNotAsk(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		host := &hostTexts{reply: "Never shown."}
		elems, err := runProviderStage(t, textStage(streaming, lengthGuard(t, 8), host.fn), "hi")
		require.NoError(t, err)
		msgs := assistantMessages(elems)
		require.Len(t, msgs, 1)
		assert.Equal(t, "Mock res", msgs[0].Content, "streaming=%v", streaming)
		assert.Empty(t, host.calls(), "streaming=%v", streaming)
	}
}

// A completed realtime reply a guardrail truncates does not ask the function.
func TestBlockedMessage_RealtimeCompletedTruncationDoesNotAsk(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&enforceAfterCallHook{replacement: "short"})))
	host := &hostTexts{reply: "Never shown."}
	s.SetBlockedMessage(host.fn)

	elems, err := feedChunks(t, s, transcriptChunk("short and long", pcm(20)), stopChunk())
	require.NoError(t, err)

	assert.Empty(t, host.calls())
	assert.Contains(t, cueTrace(elems), "end:short|safety")
}

// A spoken input an input guardrail blocks asks the function with the input
// direction.
func TestBlockedMessage_SpokenInputAsksWithInputDirection(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(
		&enforceBeforeCallHook{reason: "input blocked", metadata: map[string]any{"validator_type": "test_input"}})))
	host := &hostTexts{reply: "Host text."}
	s.SetBlockedMessage(host.fn)

	elems, err := feedChunks(t, s, finalTranscript("something disallowed"), transcriptChunk("a reply", pcm(20)),
		stopChunk())
	require.NoError(t, err)

	require.Len(t, host.calls(), 1)
	assert.Equal(t, hooks.DirectionInput, host.calls()[0].Direction)
	assert.Equal(t, prompt.DefaultBlockedMessage, host.calls()[0].Text)
	assert.Contains(t, cueTrace(elems), "end:Host text.|safety")
}

// A firing whose decision carries the pack's declaration names its id.
func TestBlockedMessage_ValidatorIDFromTheDeclaration(t *testing.T) {
	host := &hostTexts{}
	d := hooks.Enforced("why", map[string]any{"validator_type": "t",
		hooks.MetadataKeyValidatorDeclaration: &packspec.Validator{ID: "v-1", Type: "t"}})
	got := chooseBlockedText(context.Background(), host.fn, 0, blockedFiring{d: &d, direction: hooks.DirectionOutput,
		text: "Text.", turn: 2, caller: types.Caller{}})
	assert.Equal(t, "Text.", got, "an empty choice keeps the text")
	assert.Equal(t, []BlockedMessage{{ValidatorType: "t", ValidatorID: "v-1", Direction: hooks.DirectionOutput,
		Reason: "why", Text: "Text.", TurnIndex: 2}}, host.calls())
}

// Only text that is not a prefix of the original replaces it: a truncation and
// unchanged content keep the model's text.
func TestReplacedText(t *testing.T) {
	assert.False(t, replacedText("abcdef", "abc"), "a truncation")
	assert.False(t, replacedText("abc", "abc"), "unchanged")
	assert.True(t, replacedText("abcdef", "xyz"), "a replacement")
	assert.True(t, replacedText("ab", "abc"), "a longer text")
}

// panicking is a BlockedMessageFunc that panics.
func panicking(context.Context, BlockedMessage) string { panic("host bug") }

// A function that panics leaves the guardrail's message, on the text path and
// on a realtime session; the process goes on.
func TestBlockedMessage_PanicKeepsTheMessage(t *testing.T) {
	elems, err := runProviderStage(t, textStage(false, &enforceAfterCallHook{replacement: "Replaced."}, panicking), "hi")
	require.NoError(t, err)
	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Replaced.", msgs[0].Content)

	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	s.SetBlockedMessage(panicking)
	live, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)
	assert.Equal(t, []string{"interrupt", "text:I can't say that.", "end:I can't say that.|safety"}, cueTrace(live))
}

// A realtime function slower than the cue stage's on-demand budget is not
// waited for: the guardrail's message is used, and the function's context is
// canceled.
func TestBlockedMessage_RealtimeFunctionPastTheBudget(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	canceled := make(chan struct{})
	s.SetBlockedMessage(func(ctx context.Context, _ BlockedMessage) string {
		<-ctx.Done()
		close(canceled)
		return "Too late."
	})
	newCueStageFor(s, cue.Config{Cache: cueclip.NewMemoryCache(1 << 20), OnDemandBudget: 20 * time.Millisecond})

	start := time.Now()
	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)
	// The cue stage's 20ms budget, not the 1.5s default.
	assert.Less(t, time.Since(start), time.Second, "the function was waited for past the budget")

	assert.Equal(t, []string{"interrupt", "text:I can't say that.", "end:I can't say that.|safety"}, cueTrace(elems))
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "the function's context was never canceled")
	}
}

// An enforcing chunk interceptor that leaves the content as it was blocks
// nothing with a message: the function is not asked, on either path.
func TestBlockedMessage_UnchangedEnforcementDoesNotAsk(t *testing.T) {
	host := &hostTexts{reply: "Never shown."}
	meta := map[string]any{"validator_type": "unchanged"}
	elems, err := runProviderStage(t, textStage(true, &enforcingChunkInterceptor{meta: meta}, host.fn), "hi")
	require.NoError(t, err)
	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.NotEqual(t, "Never shown.", msgs[0].Content)
	assert.Empty(t, host.calls(), "text path")

	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&enforcingChunkInterceptor{meta: meta})))
	s.SetBlockedMessage(host.fn)
	_, err = feedChunks(t, s, transcriptChunk("kept as it is", pcm(20)), stopChunk())
	require.NoError(t, err)
	assert.Empty(t, host.calls(), "realtime path")
}

// blockedDetail is the BlockedMessageDetail of each failed validation.
func blockedDetail(vs []types.ValidationResult) []any {
	var out []any
	for _, v := range vs {
		if !v.Passed {
			out = append(out, v.Details[BlockedMessageDetail])
		}
	}
	return out
}

// The validation of a firing records the text the function chose; with no
// choice it records none.
func TestBlockedMessage_ValidationRecordsTheChoice(t *testing.T) {
	for name, tc := range map[string]struct {
		streaming bool
		hook      hooks.ProviderHook
	}{
		"before call":     {false, &enforceBeforeCallHook{reason: "r", replacement: "C.", metadata: map[string]any{"validator_type": "in"}}},
		"completed reply": {false, &enforceAfterCallHook{replacement: "Replaced."}},
		"streamed chunk":  {true, bannedWordsHook(t, "Mock")},
	} {
		host := &hostTexts{reply: "Host text."}
		elems, err := runProviderStage(t, textStage(tc.streaming, tc.hook, host.fn), "hi")
		require.NoError(t, err)
		msgs := assistantMessages(elems)
		require.Len(t, msgs, 1)
		assert.Equal(t, []any{"Host text."}, blockedDetail(msgs[0].Validations), name)

		quiet := &hostTexts{}
		elems, err = runProviderStage(t, textStage(tc.streaming, tc.hook, quiet.fn), "hi")
		require.NoError(t, err)
		assert.Equal(t, []any{nil}, blockedDetail(assistantMessages(elems)[0].Validations), name+": no choice")
	}

	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	s.SetBlockedMessage((&hostTexts{reply: "Host text."}).fn)
	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)
	var ends []types.ValidationResult
	for _, e := range elems {
		if e.EndOfStream && e.Message != nil {
			ends = append(ends, e.Message.Validations...)
		}
	}
	assert.Equal(t, []any{"Host text."}, blockedDetail(ends), "realtime")
}

// A turn an output guardrail cut, which an input guardrail then blocks too,
// asks the function once: the first firing's text stands.
func TestBlockedMessage_OncePerBlockedTurn(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "forbidden")),
		hooks.WithProviderHook(&enforceBeforeCallHook{reason: "input blocked",
			metadata: map[string]any{"validator_type": "test_input"}})))
	host := &hostTexts{reply: "Host text."}
	s.SetBlockedMessage(host.fn)

	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), finalTranscript("say it"), stopChunk())
	require.NoError(t, err)

	assert.Contains(t, cueTrace(elems), "end:Host text.|safety")
	require.Len(t, host.calls(), 1)
	assert.Equal(t, hooks.DirectionOutput, host.calls()[0].Direction)
}

// With no function, a blocked turn's text is the guardrail's.
func TestBlockedMessage_NoFunctionKeepsTheMessage(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
	s.SetBlockedMessage(nil)

	elems, err := feedChunks(t, s, transcriptChunk("forbidden", pcm(20)), stopChunk())
	require.NoError(t, err)
	assert.Equal(t, []string{"interrupt", "text:I can't say that.", "end:I can't say that.|safety"}, cueTrace(elems))
}
