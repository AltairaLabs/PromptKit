package stage

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A guardrail that blocks a realtime reply ends the turn, not the session. It
// cuts the reply where it can: the provider is asked to stop it and cut its own
// copy to what the caller heard (providers.ResponseInterrupter), and the
// caller is told to stop playing what it buffered, with the same Interrupt
// signal a barge-in gives. The next turn runs normally.

// interruptingSession is a recordingSession that also stops replies on
// request, recording each call.
type interruptingSession struct {
	*recordingSession
	interrupts int
}

func (s *interruptingSession) InterruptResponse(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupts++
	return nil
}

func (s *interruptingSession) interruptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interrupts
}

// cutStage is a duplex stage on an interruptingSession with reg's hooks, on
// the streaming path (no scenario turn ids).
func cutStage(t *testing.T, reg *hooks.Registry) (*DuplexProviderStage, *interruptingSession) {
	t.Helper()
	s, rec, _ := duplexStageWithHooks(t, reg)
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess
	return s, sess
}

// trace renders what the caller receives, in order: "text:<t>", "audio:<n>",
// "interrupt", "user:<t>", "end:<content>|<finish reason>", "error".
func trace(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		switch {
		case e.Error != nil:
			out = append(out, "error")
		case e.Interrupt:
			out = append(out, "interrupt")
		case e.Message != nil && e.Message.Role == roleUser:
			out = append(out, "user:"+e.Message.Content)
		case e.EndOfStream && e.Message != nil:
			out = append(out, "end:"+e.Message.Content+"|"+e.Message.FinishReason)
		}
		if e.Text != nil {
			out = append(out, "text:"+*e.Text)
		}
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			out = append(out, "audio:"+strconv.Itoa(len(e.Audio.Samples)))
		}
	}
	return out
}

func bannedWordsHook(t *testing.T, word string) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("banned_words", map[string]any{"words": []any{word}},
		guardrails.WithMessage("I can't say that."))
	require.NoError(t, err)
	return guard
}

// finalTranscript is a provider's final transcript of the caller's speech.
func finalTranscript(text string) providers.StreamChunk {
	return providers.StreamChunk{Metadata: map[string]interface{}{
		"type": "input_transcription", "transcription": text, "transcription_final": true,
	}}
}

// A streaming guardrail block cuts the reply: the provider is asked once to
// stop it, the caller gets the interrupt before anything else of the turn,
// and the second turn streams normally.
func TestDuplexGuardrailCut_StreamingBlockInterruptsAndSessionContinues(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))

	elems, err := feedChunks(t, s,
		transcriptChunk("this is ", pcm(10)),
		transcriptChunk("forbidden ", pcm(20)),
		transcriptChunk("and more", pcm(30)),
		stopChunk(),
		transcriptChunk("all fine", pcm(12)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:this is ", "audio:10",
		"interrupt",
		"end:I can't say that.|safety",
		"text:all fine", "audio:12",
		"end:all fine|stop",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount(), "the provider is asked once to stop the blocked reply")
}

// A streaming interceptor that denies without enforcing ends the turn the
// same way, with the default blocked message, not the session.
func TestDuplexGuardrailCut_StreamingDenyEndsTheTurnOnly(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&maxCharsChunkInterceptor{maxChars: 3})))

	elems, err := feedChunks(t, s,
		transcriptChunk("too long", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt",
		"end:" + prompt.DefaultBlockedMessage + "|safety",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
	assert.False(t, s.turnGuard.blocked, "the next turn starts unblocked")
}

// A completed-turn guardrail that enforces stops what the caller still has
// buffered and cuts the provider's copy, before the turn's end is emitted.
func TestDuplexGuardrailCut_AfterCallEnforceInterrupts(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&enforceAfterCallHook{replacement: "No."})))

	elems, err := feedChunks(t, s, transcriptChunk("the plan", pcm(10)), stopChunk())
	require.NoError(t, err)

	assert.Equal(t, []string{"text:the plan", "audio:10", "interrupt", "end:No.|safety"}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// A completed-turn hook that denies without enforcing ends the turn with the
// blocked message and its firing; the session goes on to the next turn.
func TestDuplexGuardrailCut_AfterCallDenyEndsTheTurnOnly(t *testing.T) {
	reg := hooks.NewRegistry(hooks.WithProviderHook(&allowBeforeDenyAfterHook{
		reason: "nope", metadata: map[string]any{"validator_type": "test_output"},
	}))
	s, sess := cutStage(t, reg)

	elems, err := feedChunks(t, s,
		transcriptChunk("hello", pcm(10)), stopChunk(),
		transcriptChunk("again", nil), stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:hello", "audio:10", "interrupt", "end:" + prompt.DefaultBlockedMessage + "|safety",
		"text:again", "interrupt", "end:" + prompt.DefaultBlockedMessage + "|safety",
	}, trace(elems))
	assert.Equal(t, 2, sess.interruptCount())
	msg, _ := turnMessage(t, elems)
	assert.Equal(t, []string{"test_output"}, validationTypes(msg.Validations))
	assert.Equal(t, map[string]interface{}{"validator_type": "test_output", finishReasonMetaKey: types.FinishReasonSafety},
		msg.Meta, "the blocked turn carries the decision's metadata, as a blocked typed turn does")
}

// wordInputHook blocks, before the call, a last user turn containing word.
type wordInputHook struct{ word string }

func (h *wordInputHook) Name() string { return "word_input" }

func (h *wordInputHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	var last string
	for _, m := range req.Messages {
		if m.Role == roleUser {
			last = m.GetContent()
		}
	}
	if !strings.Contains(last, h.word) {
		return hooks.Allow
	}
	req.Replacement = "I can't help with transfers."
	return hooks.Enforced("banned", map[string]any{"validator_type": "test_input"})
}

func (h *wordInputHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// The caller's spoken turn is checked once its transcript is final. A block
// after the reply began cuts the reply: the rest is dropped, the caller is
// interrupted, and the turn is the blocked message, marked with the input
// firing. The next spoken turn is answered normally.
func TestDuplexGuardrailCut_SpokenInputBlockCutsTheReply(t *testing.T) {
	s, rec, getEvents := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess

	elems, err := feedChunks(t, s,
		transcriptChunk("Sure, ", pcm(10)),
		finalTranscript("please arrange a wire transfer"),
		transcriptChunk("sending it", pcm(20)),
		stopChunk(),
		finalTranscript("what time is it"),
		transcriptChunk("noon", pcm(12)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure, ", "audio:10",
		"user:please arrange a wire transfer",
		"interrupt",
		"end:I can't help with transfers.|safety",
		"user:what time is it",
		"text:noon", "audio:12",
		"end:noon|stop",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())

	var blocked *types.Message
	for _, e := range elems {
		if e.EndOfStream && e.Message != nil && e.Message.FinishReason == types.FinishReasonSafety {
			blocked = e.Message
		}
	}
	require.NotNil(t, blocked)
	require.Len(t, blocked.Validations, 1)
	assert.Equal(t, "test_input", blocked.Validations[0].ValidatorType)
	assert.Equal(t, hooks.DirectionInput, blocked.Validations[0].Details["direction"])
	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionInput, got[0].Direction)
}

// When the caller's transcript is materialized as the reply begins (a provider
// with no final marker, as Gemini Live), the reply is blocked from its first
// chunk: none of it reaches the caller.
func TestDuplexGuardrailCut_SpokenInputBlockedAtReplyStart(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))

	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer please",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"user:a wire transfer please",
		"interrupt",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// A transcript that arrives after the reply to it completed can only stop
// what still plays and cut the provider's copy; the completed reply stays as
// it was emitted, and the firing is reported.
func TestDuplexGuardrailCut_LateSpokenInputBlockInterruptsOnly(t *testing.T) {
	s, rec, getEvents := duplexStageWithHooks(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	sess := &interruptingSession{recordingSession: rec}
	s.session = sess

	elems, err := feedChunks(t, s,
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
		finalTranscript("a wire transfer please"),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"text:Sure", "audio:10", "end:Sure|stop",
		"user:a wire transfer please",
		"interrupt",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
	got := getEvents()
	require.Len(t, got, 1)
	assert.Equal(t, hooks.DirectionInput, got[0].Direction)
}

// On the scenario path the caller's transcript arrives with the reply's end:
// it is checked there, and a block turns the reply into the blocked message.
func TestDuplexGuardrailCut_ScenarioTranscriptBlockedAtTurnEnd(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(&wordInputHook{word: "wire"})))
	turnID := "t1"
	user := StreamElement{Message: &types.Message{Role: roleUser}}
	user.Meta.TurnID = &turnID
	require.False(t, s.handleInputElement(context.Background(), &user, make(chan StreamElement, 4), make(chan error, 1)))

	elems, err := feedChunks(t, s,
		providers.StreamChunk{Metadata: map[string]interface{}{
			"type": "input_transcription", "transcription": "a wire transfer",
		}},
		transcriptChunk("Sure", pcm(10)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{"text:Sure", "audio:10", "interrupt", "end:I can't help with transfers.|safety"},
		trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}

// A reply already cut for its output is cut once: the caller's transcript
// blocking it too gives the turn the input block's message, with no second
// interruption.
func TestDuplexGuardrailCut_BlockedReplyIsCutOnce(t *testing.T) {
	s, sess := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "forbidden")),
		hooks.WithProviderHook(&wordInputHook{word: "wire"}),
	))

	elems, err := feedChunks(t, s,
		transcriptChunk("forbidden", pcm(10)),
		finalTranscript("a wire transfer"),
		transcriptChunk("more", pcm(20)),
		stopChunk(),
	)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"interrupt",
		"user:a wire transfer",
		"end:I can't help with transfers.|safety",
	}, trace(elems))
	assert.Equal(t, 1, sess.interruptCount())
}
