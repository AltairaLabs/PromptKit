package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

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
	c.spoken(false) // barge-in
	c.spoken(true)  // its transcript: the same utterance
	assert.Equal(t, 0, c.current())
	turn := c.replyBegins("")
	assert.Equal(t, callerTurn{index: 0, heard: true, transcribed: true}, turn)
	c.spoken(true) // a transcript that trails into the reply
	assert.Equal(t, 0, c.current(), "a reply in progress is the current turn")
	c.replyEnds()
	assert.True(t, c.reply.transcribed)

	c.spoken(false)
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
	assert.Equal(t, callerTurn{index: 3, heard: true}, c.replyBegins("in-9"), "a named input")
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
