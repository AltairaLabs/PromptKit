package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A host's WithBlockedMessage callback chooses the text of a blocked message:
// it is what the caller sees and hears, and what the conversation stores, on
// the realtime path and the text path alike.

const localizedBruno = "No hablamos de eso."

// blockedCalls records each BlockedMessage the callback is given and answers
// with reply.
type blockedCalls struct {
	reply string
	mu    sync.Mutex
	got   []BlockedMessage
}

func (b *blockedCalls) fn(_ context.Context, m BlockedMessage) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, m)
	return b.reply
}

func (b *blockedCalls) calls() []BlockedMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]BlockedMessage(nil), b.got...)
}

// storedAssistants is the content of each assistant message the session's
// store holds once the conversation is closed.
func (s *cueSession) storedAssistants(t *testing.T) []string {
	t.Helper()
	require.NoError(t, s.conv.Close())
	state, err := s.store.Load(context.Background(), "cue")
	require.NoError(t, err)
	var out []string
	for _, m := range state.Messages {
		if m.Role == "assistant" {
			out = append(out, m.Content)
		}
	}
	return out
}

// On a realtime session the host's text replaces the guardrail's message: the
// caller sees it, hears exactly the TTS service's audio for it, and the
// conversation stores it.
func TestWithBlockedMessage_RealtimeShowsSpeaksAndStoresTheHostText(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	host := &blockedCalls{reply: localizedBruno}
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(),
		WithCueVoice(speech, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(host.fn))

	want := speech.played(localizedBruno)
	r := s.cutBruno(t, len(want))

	assert.Equal(t, []string{
		"text:Let me tell you about ", "audio",
		"interrupted", "text:" + localizedBruno, "cue-audio:guardrail_message",
		"end:safety",
	}, r.trace)
	assert.Equal(t, want, r.cue, "the service's audio for the host's text")
	assert.Equal(t, 1, speech.calls(localizedBruno))
	assert.Equal(t, []string{localizedBruno}, s.storedAssistants(t))
}

// A callback that returns the empty string keeps the guardrail's message.
func TestWithBlockedMessage_EmptyKeepsTheMessage(t *testing.T) {
	speech := newCueTTS(cueRate / 4)
	host := &blockedCalls{}
	s := openCueSession(t, writeBrunoPack(t), spokenProvider(),
		WithCueVoice(speech, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(host.fn))

	want := speech.played(brunoMessage)
	r := s.cutBruno(t, len(want))

	assert.Contains(t, r.trace, "text:"+brunoMessage)
	assert.Equal(t, want, r.cue)
	assert.Equal(t, []string{brunoMessage}, s.storedAssistants(t))
	require.Len(t, host.calls(), 1)
	assert.Equal(t, brunoMessage, host.calls()[0].Text)
}

// brunoReplyProvider is a text provider whose every reply mentions Bruno.
func brunoReplyProvider() *mock.Provider {
	return mock.NewProviderWithRepository("m", "m", false,
		mock.NewInMemoryMockRepository("Let me tell you about Bruno."))
}

// On the text path the blocked message is the host's text too, for an output
// guardrail and for an input one.
func TestWithBlockedMessage_TextPathUsesTheHostText(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		host := &blockedCalls{reply: localizedBruno}
		conv, err := Open(writeBrunoPack(t), "main", WithProvider(brunoReplyProvider()),
			WithSkipSchemaValidation(), WithBlockedMessage(host.fn))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
		assert.Equal(t, localizedBruno, resp.Text())
		require.Len(t, host.calls(), 1)
		assert.Equal(t, brunoMessage, host.calls()[0].Text)
		assert.Equal(t, "output", host.calls()[0].Direction)
	})
	t.Run("input", func(t *testing.T) {
		host := &blockedCalls{reply: localizedBruno}
		conv, err := Open(writeIngestionTestPack(t), "main", WithProvider(brunoReplyProvider()),
			WithSkipSchemaValidation(), WithBlockedMessage(host.fn),
			WithGuardrail(guardrails.Input("banned_words", map[string]any{"words": []any{"Bruno"}},
				guardrails.WithMessage(brunoMessage))))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "tell me about Bruno")
		require.NoError(t, err)
		assert.Equal(t, localizedBruno, resp.Text())
		require.Len(t, host.calls(), 1)
		assert.Equal(t, "input", host.calls()[0].Direction)
	})
	t.Run("completed reply", func(t *testing.T) {
		host := &blockedCalls{reply: localizedBruno}
		conv, err := Open(writeValidatorPack(t, `{"type": "json_valid", "id": "json-only", "enabled": true,
			"message": "Only JSON here."}`), "main", WithProvider(brunoReplyProvider()),
			WithSkipSchemaValidation(), WithBlockedMessage(host.fn))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
		assert.Equal(t, localizedBruno, resp.Text())
		assert.Equal(t, []BlockedMessage{{ValidatorType: "json_valid", ValidatorID: "json-only", Direction: "output",
			Reason: "invalid JSON: invalid character 'L' looking for beginning of value", Text: "Only JSON here.",
			ConversationID: conv.ID()}}, host.calls())
	})
}

// writeValidatorPack writes a pack whose prompt "main" has the one validator
// given as JSON.
func writeValidatorPack(t *testing.T, validator string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "validator.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"name": "validator",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant.",
				"validators": [`+validator+`]
			}
		}
	}`), 0o600))
	return path
}

// The callback is given the firing exactly: the validator's type and pack id,
// the direction, the check's reason, the text the guardrail chose, the turn,
// the caller and the conversation, for a block found before the call (a typed
// input), on a streamed chunk and on the completed reply.
func TestWithBlockedMessage_ReceivesTheFiring(t *testing.T) {
	caller := types.Caller{ID: "caller-7"}
	cases := []struct {
		name      string
		validator string
		drive     func(t *testing.T, s *cueSession)
		want      BlockedMessage
		// stored is the assistant messages stored; nil is the blocked one only.
		stored []string
	}{
		{
			name: "BeforeCall",
			validator: `{"type": "banned_words", "id": "no-bruno-in", "enabled": true,
				"message": "We don't talk about Bruno.", "params": {"words": ["Bruno"], "direction": "input"}}`,
			drive: func(t *testing.T, s *cueSession) {
				require.NoError(t, s.conv.SendText(context.Background(), "tell me about Bruno"))
				s.read(t, &cueRead{}, (*cueRead).ended)
			},
			want: BlockedMessage{ValidatorType: "banned_words", ValidatorID: "no-bruno-in", Direction: "input",
				Reason: `forbidden content found: turn 0 contains "Bruno"`, Text: brunoMessage},
		},
		{
			name: "OnChunk",
			validator: `{"type": "banned_words", "id": "no-bruno-out", "enabled": true,
				"message": "We don't talk about Bruno.", "params": {"words": ["Bruno"]}}`,
			drive: func(t *testing.T, s *cueSession) {
				// A first, clean turn: the firing is on the second.
				s.sess.EmitChunk(replyChunk("Hello."))
				s.sess.EmitChunk(endChunk())
				s.read(t, &cueRead{}, (*cueRead).ended)
				s.speak(t) // the caller's second turn
				s.sess.EmitChunk(replyChunk("Bruno"))
				s.sess.EmitChunk(endChunk())
				s.read(t, &cueRead{}, (*cueRead).ended)
			},
			want: BlockedMessage{ValidatorType: "banned_words", ValidatorID: "no-bruno-out", Direction: "output",
				Reason: `forbidden content found in stream: "Bruno"`, Text: brunoMessage, TurnIndex: 1},
			stored: []string{"Hello.", localizedBruno},
		},
		{
			name: "AfterCall",
			validator: `{"type": "json_valid", "id": "json-only", "enabled": true,
				"message": "Only JSON here."}`,
			drive: func(t *testing.T, s *cueSession) {
				s.sess.EmitChunk(replyChunk("Let me"))
				s.sess.EmitChunk(endChunk())
				s.read(t, &cueRead{}, (*cueRead).ended)
			},
			want: BlockedMessage{ValidatorType: "json_valid", ValidatorID: "json-only", Direction: "output",
				Reason: "invalid JSON: invalid character 'L' looking for beginning of value", Text: "Only JSON here."},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := &blockedCalls{reply: localizedBruno}
			s := openCueSession(t, writeValidatorPack(t, tc.validator), spokenProvider(),
				WithCueVoice(nil, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(host.fn), WithCaller(caller))

			tc.drive(t, s)

			want := tc.want
			want.Caller = caller
			want.ConversationID = "cue"
			assert.Equal(t, []BlockedMessage{want}, host.calls())
			stored := tc.stored
			if stored == nil {
				stored = []string{localizedBruno}
			}
			assert.Equal(t, stored, s.storedAssistants(t))
		})
	}
}

// A callback that panics does not take the process down: the guardrail's
// message is the blocked turn's text.
func TestWithBlockedMessage_PanicKeepsTheMessage(t *testing.T) {
	conv, err := Open(writeBrunoPack(t), "main", WithProvider(brunoReplyProvider()), WithSkipSchemaValidation(),
		WithBlockedMessage(func(context.Context, BlockedMessage) string { panic("host bug") }))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "who is in the family?")
	require.NoError(t, err)
	assert.Equal(t, brunoMessage, resp.Text())
}

// turnIndexes is the TurnIndex of each call.
func turnIndexes(calls []BlockedMessage) []int {
	out := make([]int, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.TurnIndex)
	}
	return out
}

// The same firing on the same turns reports the same TurnIndex on the text
// path and on a realtime session: 0 for the first turn, 1 for the second.
func TestWithBlockedMessage_TurnIndexAgreesAcrossPaths(t *testing.T) {
	const jsonOnly = `{"type": "json_valid", "id": "json-only", "enabled": true, "message": "Only JSON here."}`

	text := &blockedCalls{reply: localizedBruno}
	conv, err := Open(writeValidatorPack(t, jsonOnly), "main", WithProvider(brunoReplyProvider()),
		WithSkipSchemaValidation(), WithStateStore(statestore.NewMemoryStore()), WithBlockedMessage(text.fn))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	for range 2 {
		_, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
	}

	live := &blockedCalls{reply: localizedBruno}
	s := openCueSession(t, writeValidatorPack(t, jsonOnly), spokenProvider(),
		WithCueVoice(nil, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(live.fn))
	for i := range 2 {
		if i > 0 {
			s.speak(t) // the caller's second turn
		}
		s.sess.EmitChunk(replyChunk("Let me"))
		s.sess.EmitChunk(endChunk())
		s.read(t, &cueRead{}, (*cueRead).ended)
	}

	assert.Equal(t, []int{0, 1}, turnIndexes(text.calls()), "text path")
	assert.Equal(t, []int{0, 1}, turnIndexes(live.calls()), "realtime path")
}

// The host's text is not judged again by the guardrail that fired, even when
// it names what the guardrail bans: it is shown and stored once, and the
// callback is called once per firing.
func TestWithBlockedMessage_HostTextIsNotJudgedAgain(t *testing.T) {
	const namesBruno = "De Bruno no se habla."

	t.Run("text", func(t *testing.T) {
		host := &blockedCalls{reply: namesBruno}
		conv, err := Open(writeBrunoPack(t), "main", WithProvider(brunoReplyProvider()),
			WithSkipSchemaValidation(), WithBlockedMessage(host.fn))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
		assert.Equal(t, namesBruno, resp.Text())
		assert.Len(t, host.calls(), 1)
		var assistants []string
		for _, m := range conv.Messages(context.Background()) {
			if m.Role == "assistant" {
				assistants = append(assistants, m.Content)
			}
		}
		assert.Equal(t, []string{namesBruno}, assistants)
	})
	t.Run("realtime", func(t *testing.T) {
		speech := newCueTTS(cueRate / 4)
		host := &blockedCalls{reply: namesBruno}
		s := openCueSession(t, writeBrunoPack(t), spokenProvider(),
			WithCueVoice(speech, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(host.fn))

		r := s.cutBruno(t, len(speech.played(namesBruno)))

		assert.Equal(t, []string{
			"text:Let me tell you about ", "audio",
			"interrupted", "text:" + namesBruno, "cue-audio:guardrail_message",
			"end:safety",
		}, r.trace)
		assert.Len(t, host.calls(), 1)
		assert.Equal(t, []string{namesBruno}, s.storedAssistants(t))
	})
}

// directions is the Direction of each call.
func directions(calls []BlockedMessage) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Direction)
	}
	return out
}

// A turn an output guardrail cut and an input guardrail then blocks too gets
// one message: the callback is asked once, for the first firing, and its text
// is what is shown, spoken and stored, whether the caller's transcript arrives
// mid-cut or after the cut reply ended.
func TestWithBlockedMessage_OncePerBlockedTurn(t *testing.T) {
	for name, late := range map[string]bool{"mid-cut": false, "after the reply": true} {
		t.Run(name, func(t *testing.T) {
			speech := newCueTTS(cueRate / 4)
			host := &blockedCalls{reply: localizedBruno}
			s := openCueSession(t, writeBrunoPack(t), spokenProvider(), inputBruno(),
				WithCueVoice(speech, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(host.fn))
			want := speech.played(localizedBruno)

			r := &cueRead{}
			s.sess.EmitChunk(replyChunk("Bruno is"))
			if !late {
				s.sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A"))
			}
			s.read(t, r, func(r *cueRead) bool { return len(r.cue) >= len(want) })
			s.sess.EmitChunk(endChunk())
			s.read(t, r, (*cueRead).ended)
			if late {
				s.sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A"))
				s.sess.EmitChunk(replyChunk("Hello again"))
				s.sess.EmitChunk(endChunk())
				s.read(t, r, func(r *cueRead) bool { return count(r.trace, "end:stop") == 1 })
			}

			assert.Equal(t, 1, count(r.trace, "text:"+localizedBruno), "%v", r.trace)
			assert.Equal(t, want, r.cue, "the host's text, spoken once")
			assert.Equal(t, []string{"output"}, directions(host.calls()), "asked once, for the first firing")
			stored := s.storedAssistants(t)
			require.NotEmpty(t, stored)
			assert.Equal(t, localizedBruno, stored[0])
		})
	}
}

// An input verdict judged off the response loop (a slow judge) that blocks
// the turn asks the callback once, and its text is the one shown and stored.
func TestWithBlockedMessage_AsyncInputVerdict(t *testing.T) {
	judge := newHeldInputHook("Bruno", brunoMessage)
	host := &blockedCalls{reply: localizedBruno}
	s := openCueSession(t, writeIngestionTestPack(t), spokenProvider(), WithProviderHook(judge),
		WithCueVoice(nil, CueClipCache(cueclip.NewMemoryCache(1<<20))), WithBlockedMessage(host.fn))

	r := &cueRead{}
	s.sess.EmitChunk(replyChunk("Sure, "))
	s.sess.EmitChunk(callerSaid("Tell me about Bruno.", "item_A"))
	require.Eventually(t, judge.started, 5*time.Second, time.Millisecond)
	s.sess.EmitChunk(replyChunk("he lives"))
	s.read(t, r, func(r *cueRead) bool { return count(r.trace, "text:he lives") == 1 })
	close(judge.release)
	s.read(t, r, func(r *cueRead) bool { return count(r.trace, "text:"+localizedBruno) == 1 })
	s.sess.EmitChunk(endChunk())
	s.read(t, r, (*cueRead).ended)

	require.Len(t, host.calls(), 1)
	got := host.calls()[0]
	assert.Equal(t, []string{"input", "held_input", "about Bruno", brunoMessage},
		[]string{got.Direction, got.ValidatorType, got.Reason, got.Text})
	assert.Equal(t, []string{localizedBruno}, s.storedAssistants(t))
}

// A composition step's guardrail blocks with the host's text too: the step's
// output, and so the composition's, is what WithBlockedMessage chose.
func TestWithBlockedMessage_CompositionStepUsesTheHostText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bruno-flow.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"name": "bruno-flow",
		"version": "v1",
		"prompts": {"main": {"system_template": "You are a helpful assistant."}},
		"compositions": {
			"flow": {"version": 1, "output": "s",
				"steps": [{"id": "s", "kind": "prompt", "prompt_task": "main", "input": "${input}"}]}
		}
	}`), 0o600))
	host := &blockedCalls{reply: localizedBruno}
	conv, err := OpenComposition(path, "flow", WithProvider(brunoReplyProvider()),
		WithSkipSchemaValidation(), WithBlockedMessage(host.fn),
		WithGuardrail(guardrails.Output("banned_words", map[string]any{"words": []any{"Bruno"}},
			guardrails.WithMessage(brunoMessage))))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "who is in the family?")
	require.NoError(t, err)
	assert.Equal(t, `"`+localizedBruno+`"`, resp.Text())
	require.Len(t, host.calls(), 1)
	assert.Equal(t, brunoMessage, host.calls()[0].Text)
	assert.Equal(t, "output", host.calls()[0].Direction)
}
