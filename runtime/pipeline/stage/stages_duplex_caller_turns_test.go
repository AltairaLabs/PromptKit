package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// hear records speech (callerTurns.heardSpeech) and returns the turn it was
// placed in: the turn a transcript of it is judged at, while it is early.
func hear(c *callerTurns, input string, transcribed bool) int {
	c.heardSpeech(input, transcribed)
	return c.transcriptTurn(input, false)
}

// replies places n replies, each named by the input id it answers (""
// for none), and returns each reply's turn index.
func (c *callerTurns) replies(ids ...string) []int {
	var out []int
	for _, id := range ids {
		out = append(out, c.replyBegins(id).index)
		c.replyEnds()
	}
	return out
}

// A typed turn is answered by the next reply; typed turns queue in order.
func TestCallerTurns_TypedTurnsQueue(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	assert.Equal(t, 0, c.typed())
	assert.Equal(t, 1, c.typed())
	assert.Equal(t, 1, c.current(), "between replies, the newest turn waiting")
	assert.Equal(t, []int{0, 1}, c.replies("", ""))
	assert.Equal(t, 1, c.current(), "after the replies, the turn the latest answered")
}

// A reply naming the same input as the one before is a further reply in its
// turn, even with caller audio between (a live microphone sends silence); a
// new input is a new turn.
func TestCallerTurns_ProviderNamedInputs(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	c.audio()
	assert.Equal(t, []int{0}, c.replies("in-1"))
	c.audio()
	assert.Equal(t, []int{0, 1}, c.replies("in-1", "in-2"))
}

// Without named inputs, caller audio since the last reply began makes the
// next reply a new turn, unless tool results were sent since: that reply
// continues the tool round.
func TestCallerTurns_AudioAndToolRounds(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	c.audio()
	assert.Equal(t, []int{0}, c.replies(""))
	c.audio()
	c.toolResults()
	assert.Equal(t, []int{0}, c.replies(""), "the reply to a tool round")
	assert.Equal(t, []int{0}, c.replies(""), "a second reply with no new input")
	c.audio()
	assert.Equal(t, []int{1}, c.replies(""))
}

// A reply before any caller input is a turn of its own; a resumed session
// starts from the turns stored.
func TestCallerTurns_GreetingAndResume(t *testing.T) {
	c := &callerTurns{}
	c.start(3)
	c.start(9) // only the first start counts
	assert.Equal(t, 3, c.current(), "nothing yet: the first turn")
	assert.Equal(t, []int{3}, c.replies(""), "the greeting")
	assert.Equal(t, []int{3}, c.replies(""), "a further reply with no input")
	assert.Equal(t, 4, c.typed())
}

// A barge-in or a new utterance's transcript adds a spoken turn, once per
// utterance; a transcript while a reply is in progress is that reply's.
func TestCallerTurns_SpokenTurns(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	hear(c, "", false) // barge-in
	hear(c, "", true)  // its transcript: the same utterance
	assert.Equal(t, 0, c.current())
	turn := c.replyBegins("")
	assert.Equal(t, callerTurn{index: 0, heard: true, transcribed: true}, turn)
	hear(c, "", true) // a transcript that trails into the reply
	assert.Equal(t, 0, c.current(), "a reply in progress is the current turn")
	c.replyEnds()
	assert.True(t, c.reply.transcribed)

	hear(c, "", false)
	turn = c.replyBegins("")
	assert.Equal(t, callerTurn{index: 1, heard: true}, turn, "heard, no transcript yet")
	// A barge-in during that reply ends it and starts the caller's next turn,
	// even when the reply left nothing to mark ended.
	c.bargeIn()
	assert.Equal(t, callerTurn{index: 2, heard: true}, c.replyBegins(""))
	c.replyEnds()
}

// Which turns may have a transcript still to come: speech the provider
// reported may; a typed turn, a reply before any input, and audio alone not.
func TestCallerTurns_SpokenOrNot(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	assert.Equal(t, callerTurn{index: 0}, c.replyBegins(""), "a greeting")
	c.replyEnds()
	c.typed()
	assert.Equal(t, callerTurn{index: 1}, c.replyBegins(""), "a typed turn")
	c.replyEnds()
	c.audio()
	assert.Equal(t, callerTurn{index: 2}, c.replyBegins(""), "audio the provider named no turn for")
	c.replyEnds()
	assert.Equal(t, callerTurn{index: 3, heard: true, input: "in-9"}, c.replyBegins("in-9"), "a named input")
	c.replyEnds()
}

// A blocked typed turn is answered by its blocked message: it leaves the
// queue, is the turn answered latest, and the next reply answers the next
// waiting turn.
func TestCallerTurns_BlockedTurnIsAnswered(t *testing.T) {
	c := &callerTurns{}
	c.start(5)
	a, b := c.typed(), c.typed()
	c.answer(b)
	assert.Equal(t, a, c.current(), "a still waits for its reply")
	assert.Equal(t, []int{a}, c.replies(""))
	c.answer(7) // not waiting: nothing changes
	assert.Equal(t, a, c.current())

	blocked := c.typed()
	c.answer(blocked)
	assert.Equal(t, blocked, c.current(), "the blocked turn is the one answered latest")
}

// A realtime session records, on its TurnState, the caller turn of each turn
// it completes, and still counts its replies (SessionTurns): two replies to
// one typed input are one caller turn and two session turns.
func TestDuplexStage_RecordsTheCallerTurnOnTurnState(t *testing.T) {
	s, _, _ := duplexStageWithHooks(t, hooks.NewRegistry())
	_, ok := s.turnState.CallerTurn()
	assert.False(t, ok, "no turn completed yet")

	text := "A"
	require.False(t, s.handleInputElement(context.Background(), &StreamElement{Text: &text},
		make(chan StreamElement, 4), make(chan error, 1)))
	for range 2 {
		_, err := feedChunks(t, s, providers.StreamChunk{Content: "reply"}, stopChunk())
		require.NoError(t, err)
	}
	turn, ok := s.turnState.CallerTurn()
	assert.True(t, ok)
	assert.Equal(t, 0, turn)
	assert.Equal(t, 2, s.turnState.SessionTurns())

	var none *TurnState
	_, ok = none.CallerTurn()
	assert.False(t, ok, "a nil TurnState has none")
	assert.Equal(t, 0, none.SessionTurns())
}

// A stored conversation resumes from its user turns, or from one past the
// highest caller turn stored on its replies when that is more, in any numeric
// form a store returns it; a caller turn on anything but a reply is ignored.
func TestCallerTurns_ResumedFromTheTranscript(t *testing.T) {
	reply := func(turn any) types.Message {
		return types.Message{Role: core.RoleAssistant, Meta: map[string]interface{}{types.MetaCallerTurn: turn}}
	}
	assert.Equal(t, 2, resumedCallerTurns(nil, 2))
	assert.Equal(t, 2, resumedCallerTurns([]types.Message{{Role: core.RoleAssistant}}, 2), "no stored turn")
	assert.Equal(t, 5, resumedCallerTurns([]types.Message{reply(1), reply(4)}, 2))
	assert.Equal(t, 4, resumedCallerTurns([]types.Message{reply(int64(3))}, 0))
	assert.Equal(t, 7, resumedCallerTurns([]types.Message{reply(float64(6))}, 0), "as JSON decodes it")
	assert.Equal(t, 3, resumedCallerTurns([]types.Message{reply(1)}, 3), "fewer than the user turns")
	assert.Equal(t, 0, resumedCallerTurns([]types.Message{reply("9")}, 0), "not a number")
	user := types.Message{Role: core.RoleUser, Meta: map[string]interface{}{types.MetaCallerTurn: 8}}
	assert.Equal(t, 1, resumedCallerTurns([]types.Message{user}, 1), "only a reply's counts")
}

// Speech the provider names belongs to its input's turn wherever it lands:
// a transcript of a reply's own input is the reply's; one of another input
// while a reply plays is a new turn, which the reply naming it answers, ahead
// of turns queued before it; a late one is judged at its own turn.
func TestCallerTurns_NamedSpeech(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	c.audio()
	assert.Equal(t, 0, c.replyBegins("in-1").index)
	assert.Equal(t, 0, hear(c, "in-1", true), "the reply's own input")
	assert.Equal(t, 1, hear(c, "in-2", true), "speech over the reply: the next turn")
	assert.Equal(t, 0, c.current(), "the reply is still the current turn")
	c.replyEnds()

	typed := c.typed()
	assert.Equal(t, 2, typed)
	assert.Equal(t, callerTurn{index: 1, heard: true, transcribed: true, input: "in-2"}, c.replyBegins("in-2"))
	c.replyEnds()
	assert.Equal(t, typed, c.replyBegins("").index, "then the typed one")
	c.replyEnds()

	// Typed, then speech over the reply to it: the reply naming the speech
	// answers it, though the typed turn queued first.
	d := &callerTurns{}
	d.start(0)
	d.audio()
	d.replyBegins("in-1")
	first := d.typed()
	spoken := hear(d, "in-2", true)
	d.replyEnds()
	assert.Equal(t, spoken, d.replyBegins("in-2").index)
	d.replyEnds()
	assert.Equal(t, first, d.replyBegins("").index)
	d.replyEnds()

	assert.Equal(t, 1, c.transcriptTurn("in-2", true), "a named transcript, judged at its own turn")
	assert.Equal(t, typed, c.transcriptTurn("", true), "an unnamed late one, at the latest reply's")
	c.typed()
	assert.Equal(t, 3, c.transcriptTurn("", false), "an early one, at the current turn")
	assert.Equal(t, 0, hear(c, "in-1", true), "a named input already placed")
}

// An unnamed transcript joins the spoken turn waiting for its reply, which a
// named one then names; it does not join a typed turn.
func TestCallerTurns_UnnamedSpeechJoinsTheWaitingSpokenTurn(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	assert.Equal(t, 0, c.typed())
	assert.Equal(t, 1, hear(c, "", false), "speech is not the typed turn's")
	assert.Equal(t, 1, hear(c, "", true))
	assert.Equal(t, 1, hear(c, "in-5", true), "named now")
	assert.Equal(t, 1, c.transcriptTurn("in-5", false))
	assert.Equal(t, 2, hear(c, "in-6", true), "another named input: another turn")
}

// A reply cut short by a barge-in reported twice (the stage dropping it
// unheard, then the provider's Interrupted chunk), with a typed turn between
// the two, is one barge-in: the second adds no turn.
func TestCallerTurns_ABargeInReportedTwiceIsOneTurn(t *testing.T) {
	c := &callerTurns{}
	c.start(0)
	assert.Equal(t, 0, c.typed())
	assert.Equal(t, 0, c.replyBegins("").index)
	c.bargeIn() // the caller's spoken turn 1
	assert.Equal(t, 2, c.typed())
	c.bargeIn() // the same barge-in, reported again
	assert.Equal(t, []int{1, 2}, c.replies("", ""))
	assert.Equal(t, 3, c.typed(), "no turn was added between")
}
