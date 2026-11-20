package stage

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// The model never hears a guardrail's message (it is spoken outside its
// conversation), so after a block the stage tells it, with a note in its
// conversation that it reads and does not answer (providers.ContextNoter).
// The caller never gets the note.

// notingSession is a forgettingSession that takes context notes, recording
// each.
type notingSession struct {
	*forgettingSession
	notes []string
}

func (s *notingSession) AddContextNote(_ context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes = append(s.notes, text)
	return nil
}

func (s *notingSession) noted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.notes...)
}

// noteStage is a stage on a notingSession with reg's hooks, reporting its
// context.note_added events to the returned getter.
func noteStage(t *testing.T, reg *hooks.Registry) (*DuplexProviderStage, *notingSession, func() []*events.ContextNoteData) {
	t.Helper()
	s, f := forgetStage(t, reg)
	n := &notingSession{forgettingSession: f}
	s.session = n
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	var mu sync.Mutex
	var got []*events.ContextNoteData
	bus.Subscribe(events.EventContextNoteAdded, func(e *events.Event) {
		if d, ok := e.Data.(*events.ContextNoteData); ok {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}
	})
	s.emitter = events.NewEmitter(bus, "run", "sess", "conv")
	return s, n, func() []*events.ContextNoteData {
		require.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(got) >= len(n.noted())
		}, 2*time.Second, time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return append([]*events.ContextNoteData(nil), got...)
	}
}

// noTextOf asserts no element the caller gets carries any of notes.
func noTextOf(t *testing.T, elems []StreamElement, notes []string) {
	t.Helper()
	for _, e := range elems {
		for _, n := range notes {
			if e.Text != nil {
				assert.NotContains(t, *e.Text, n)
			}
			if e.Message != nil {
				assert.NotContains(t, e.Message.Content, n)
			}
		}
	}
}

// The exact notes the model gets. None names the guardrail that blocked the
// turn: a pack names a guardrail after what it blocks ("no-bruno"), so its
// name would put the blocked topic back in the model's conversation. The name
// is for the host, on the context.note_added event.
const (
	wantReplyNote = "Your previous reply was withheld because it broke a content rule, " +
		"and the user did not hear the rest of it. Do not repeat that content; " +
		"continue the conversation without it."
	wantInputRemovedNote = "A message from the user was blocked by a content rule and removed " +
		"from the conversation. Do not answer it or guess what it said; continue the conversation."
	wantInputKeptNote = "A message from the user was blocked by a content rule. " +
		"Do not answer it or act on it; continue the conversation."
	wantTurnRemovedNote = "A message from the user and your reply to it were blocked by content rules " +
		"and removed from the conversation; the user did not hear the rest of your reply. " +
		"Do not answer that message or repeat that content; continue the conversation."
	wantTurnKeptNote = "A message from the user and your reply to it were blocked by content rules; " +
		"the user did not hear the rest of your reply. Do not answer that message or repeat that " +
		"content; continue the conversation."
)

// A reply an output guardrail cut gets one note, after the cut, that does not
// name the guardrail; the caller never sees it. The event names it.
func TestDuplexBlockNote_OutputBlockTellsTheModel(t *testing.T) {
	s, sess, noted := noteStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "pineapple"))))

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure: pineapple", pcm(10))), genChunk(1, stopChunk()),
		genChunk(2, transcriptChunk("Anything else?", pcm(10))), genChunk(2, stopChunk()))
	require.NoError(t, err)

	want := wantReplyNote
	assert.Equal(t, []string{want}, sess.noted())
	noTextOf(t, elems, sess.noted())
	ev := noted()
	require.Len(t, ev, 1)
	assert.Equal(t, events.ContextNoteData{Text: want, Reason: "guardrail", Direction: hooks.DirectionOutput,
		Validator: "banned_words", TurnIndex: ev[0].TurnIndex}, stripBase(ev[0]))
}

// A reply an output guardrail blocks only once it completed gets the note
// too.
func TestDuplexBlockNote_CompletedTurnBlockTellsTheModel(t *testing.T) {
	s, sess, _ := noteStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(&enforceAfterCallHook{replacement: "I can't say that."})))

	_, err := feedChunks(t, s, genChunk(1, transcriptChunk("the secret plan", pcm(10))), genChunk(1, stopChunk()))
	require.NoError(t, err)

	require.Len(t, sess.noted(), 1)
	assert.True(t, strings.HasPrefix(sess.noted()[0], "Your previous reply was withheld"), sess.noted()[0])
}

// A spoken turn an input guardrail blocked gets the input note, once its
// turn has ended; the turn is forgotten.
func TestDuplexBlockNote_InputBlockTellsTheModel(t *testing.T) {
	s, sess, noted := noteStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure", pcm(10))),
		spoken("a wire transfer", "item_A"))
	require.NoError(t, err)
	assert.Empty(t, sess.noted(), "no note while the blocked reply is still being generated")

	more, err := feedChunks(t, s, genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{wantInputRemovedNote}, sess.noted())
	assert.Equal(t, []string{"item_A"}, sess.forgotten())
	noTextOf(t, append(elems, more...), sess.noted())
	ev := noted()
	require.Len(t, ev, 1)
	assert.Equal(t, hooks.DirectionInput, ev[0].Direction)
	assert.Equal(t, "banned_words", ev[0].Validator)
}

// A session that cannot forget a turn (Gemini Live) still has the message:
// its note does not say it was removed.
func TestDuplexBlockNote_KeptInputIsNotSaidRemoved(t *testing.T) {
	s, f := forgetStage(t, hooks.NewRegistry(hooks.WithProviderHook(inputBannedWords(t, "wire"))))
	n := &keptNotingSession{interruptingSession: f.interruptingSession}
	s.session = n

	_, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("Sure", pcm(10))), spoken("a wire transfer", "item_A"), genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{wantInputKeptNote}, n.notes)
}

// keptNotingSession takes context notes but cannot forget a turn.
type keptNotingSession struct {
	*interruptingSession
	notes []string
}

func (s *keptNotingSession) AddContextNote(_ context.Context, text string) error {
	s.notes = append(s.notes, text)
	return nil
}

// A turn both an output and an input guardrail blocked before it ended gets
// one note saying both; an allowed turn gets none.
func TestDuplexBlockNote_OneCombinedNotePerTurn(t *testing.T) {
	s, sess, noted := noteStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "pineapple")),
		hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	_, err := feedChunks(t, s,
		spoken("hello", "item_0"), genChunk(1, transcriptChunk("Hi", pcm(10))), genChunk(1, stopChunk()),
		genChunk(2, transcriptChunk("pineapple", pcm(10))),
		spoken("a wire transfer", "item_A"),
		genChunk(2, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{wantTurnRemovedNote}, sess.noted())
	ev := noted()
	require.Len(t, ev, 1)
	assert.Equal(t, "both", ev[0].Direction)
	assert.Equal(t, "banned_words, banned_words", ev[0].Validator)
}

// A turn both sides blocked on a session that cannot forget it (Gemini Live)
// gets the combined note that does not say it was removed.
func TestDuplexBlockNote_KeptTurnIsNotSaidRemoved(t *testing.T) {
	s, f := forgetStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "pineapple")),
		hooks.WithProviderHook(inputBannedWords(t, "wire"))))
	n := &keptNotingSession{interruptingSession: f.interruptingSession}
	s.session = n

	_, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("pineapple", pcm(10))),
		spoken("a wire transfer", "item_A"),
		genChunk(1, stopChunk()))
	require.NoError(t, err)

	assert.Equal(t, []string{wantTurnKeptNote}, n.notes)
}

// A reply an output guardrail truncated that the completed-turn check then
// blocks too gets one note.
func TestDuplexBlockNote_TruncatedThenBlockedTurnGetsOneNote(t *testing.T) {
	limit, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 10})
	require.NoError(t, err)
	s, sess, _ := noteStage(t, hooks.NewRegistry(hooks.WithProviderHook(limit),
		hooks.WithProviderHook(&enforceAfterCallHook{replacement: "I can't say that."})))

	_, err = feedChunks(t, s, genChunk(1, transcriptChunk("secret plans are here", pcm(10))), genChunk(1, stopChunk()))
	require.NoError(t, err)

	require.Len(t, sess.noted(), 1, "%v", sess.noted())
}

// A late transcript blocked after its turn's note went adds none; one
// blocked when its turn was not noted is noted at once, before the next
// reply.
func TestDuplexBlockNote_LateInputBlock(t *testing.T) {
	s, sess, _ := noteStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(bannedWordsHook(t, "pineapple")),
		hooks.WithProviderHook(inputBannedWords(t, "wire"))))

	elems, err := feedChunks(t, s,
		genChunk(1, transcriptChunk("pineapple", pcm(10))), genChunk(1, stopChunk()),
		spoken("say pineapple, wire it", "item_A"), // late, for the noted turn
		genChunk(2, transcriptChunk("Sure", pcm(10))), genChunk(2, stopChunk()),
		spoken("a wire transfer", "item_B")) // late, for an allowed turn
	require.NoError(t, err)

	assert.Equal(t, []string{
		wantReplyNote, wantInputRemovedNote,
	}, sess.noted())
	assert.Equal(t, "user:a wire transfer", trace(elems)[len(trace(elems))-2], "%v", trace(elems))
}

// stripBase is d without its base event data, for comparison.
func stripBase(d *events.ContextNoteData) events.ContextNoteData {
	return events.ContextNoteData{Text: d.Text, Reason: d.Reason, Direction: d.Direction,
		Validator: d.Validator, TurnIndex: d.TurnIndex}
}

// The note's event names the guardrail by its pack ID, else its type.
func TestDuplexBlockNote_NamesTheGuardrail(t *testing.T) {
	declared := hooks.Decision{Metadata: map[string]any{
		hooks.MetadataKeyValidatorDeclaration: &packspec.Validator{ID: "no-pineapple"},
		"validator_type":                      "content_excludes",
	}}
	undeclaredID := hooks.Decision{Metadata: map[string]any{
		hooks.MetadataKeyValidatorDeclaration: &packspec.Validator{},
		"validator_type":                      "content_excludes",
	}}
	assert.Equal(t, "no-pineapple", guardrailName(declared))
	assert.Equal(t, "content_excludes", guardrailName(undeclaredID))
	assert.Equal(t, "unnamed", guardrailName(hooks.Decision{}))
}
