package stage

import (
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerTurns places a realtime session's replies against the caller's turns:
// it counts the caller's input turns and gives each reply the 0-based index of
// the caller turn it answers. That index is what the session's guardrails
// (ProviderRequest.TurnIndex) and its turn evals (ElementMetadata.CallerTurn)
// report, so both place a reply in the same turn, and it is stored on each
// reply (types.MetaCallerTurn), so a session that resumes the conversation
// goes on from it.
//
// A caller turn is input the caller gave: a typed message, or speech the
// provider took as a turn. Speech is known by the first of: the caller
// barging in, a transcript of a new utterance, the provider naming a new
// input the reply answers (providers.ReplyToInputKey), or, when the provider
// says neither, caller audio sent since the last reply began. A transcript is
// not needed: a spoken turn with none is still a turn. Speech the provider
// names as an input other than the reply in progress is a turn of its own,
// even while that reply plays on (a provider that does not cut a reply when
// the caller speaks over it).
//
// A reply with no new caller input since the last reply began answers the
// same turn: a tool round's further reply, or a second reply to the same
// words. A reply before the caller has said anything (a greeting) is a turn
// of its own, so the caller's first turn after it is the next. A session that
// opens on a stored conversation goes on from the caller turns stored
// (resumedCallerTurns).
//
// Safe for concurrent use: the input goroutine adds caller turns while the
// response goroutine places replies.
type callerTurns struct {
	mu      sync.Mutex
	started bool
	// next is the index the next caller turn gets.
	next int
	// queue is the caller turns no reply has answered yet, oldest first.
	queue []callerTurn
	// reply is the turn the latest reply answers, valid once answered is set.
	reply    callerTurn
	answered bool
	// replying is set while a reply is in progress (between replyBegins and
	// replyEnds).
	replying bool
	// replyInput is the provider's ID of the caller input the latest reply
	// answered, when it named one.
	replyInput string
	// byInput is the caller turn of each input the provider named.
	byInput map[string]int
	// audioSince and toolsSince record caller audio and tool results sent
	// since the latest reply began.
	audioSince, toolsSince bool
	// barged is set once a barge-in on the latest reply was recorded: one
	// reported again (by the stage dropping the reply, then by the
	// provider) is the same barge-in.
	barged bool
}

// callerTurn is one caller turn.
type callerTurn struct {
	index int
	// heard is set for a turn the provider reported as the caller's speech
	// (a barge-in, a transcript, an input it named), whose transcript may
	// still come. A typed turn, a reply before any input, and a turn known
	// only from the caller's audio have none to wait for: a provider that
	// takes speech as a turn names it.
	heard bool
	// transcribed is set once the turn's transcript has arrived.
	transcribed bool
	// input is the provider's ID for the turn's speech, when it named one.
	input string
}

// resumedCallerTurns is how many caller turns a stored conversation already
// holds: its user turns (userTurns), or, when more, one past the highest
// caller turn stored on its replies (types.MetaCallerTurn), which counts the
// spoken turns no transcript was stored for.
func resumedCallerTurns(prior []types.Message, userTurns int) int {
	turns := userTurns
	for i := range prior {
		if idx, ok := storedCallerTurn(&prior[i]); ok {
			turns = max(turns, idx+1)
		}
	}
	return turns
}

// storedCallerTurn reads the caller turn stored on m, in whatever numeric
// form the store gave it back.
func storedCallerTurn(m *types.Message) (int, bool) {
	if m.Role != roleAssistant {
		return 0, false
	}
	switch v := m.Meta[types.MetaCallerTurn].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

// start sets the caller turns the conversation held when the session opened,
// once.
func (c *callerTurns) start(prior int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return
	}
	c.started = true
	c.next = prior
}

// pushLocked adds a new caller turn and returns it.
func (c *callerTurns) pushLocked(t callerTurn) callerTurn {
	t.index = c.next
	c.next++
	c.queue = append(c.queue, t)
	c.nameLocked(t)
	return t
}

// nameLocked records the turn of t's input, when it has one.
func (c *callerTurns) nameLocked(t callerTurn) {
	if t.input == "" {
		return
	}
	if c.byInput == nil {
		c.byInput = map[string]int{}
	}
	c.byInput[t.input] = t.index
}

// typed adds a typed caller turn and returns its index.
func (c *callerTurns) typed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pushLocked(callerTurn{}).index
}

// heardSpeech records the caller's speech: a barge-in (not transcribed) or a
// transcript of it (transcribed), with input the provider's ID for it ("" when
// it names none). The speech belongs to:
//
//   - the turn of an input the provider named before;
//   - the reply in progress, for its transcript (the reply's own input, or,
//     unnamed, a transcript that trails into the reply);
//   - the newest spoken turn still waiting for its reply, for the same
//     utterance;
//   - else a new turn.
func (c *callerTurns) heardSpeech(input string, transcribed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx, ok := c.byInput[input]; ok {
		c.markLocked(idx, transcribed)
		return
	}
	if transcribed && c.replying && (input == "" || input == c.replyInput) {
		c.reply.transcribed = true
		c.reply.input = input
		c.nameLocked(c.reply)
		return
	}
	if n := len(c.queue); n > 0 && c.queue[n-1].heard && (c.queue[n-1].input == "" || input == "") {
		tail := &c.queue[n-1]
		tail.transcribed = tail.transcribed || transcribed
		if input != "" {
			tail.input = input
			c.nameLocked(*tail)
		}
		return
	}
	c.pushLocked(callerTurn{heard: true, transcribed: transcribed, input: input})
}

// markLocked records a transcript, when transcribed, for turn index.
func (c *callerTurns) markLocked(index int, transcribed bool) {
	if !transcribed {
		return
	}
	if c.reply.index == index {
		c.reply.transcribed = true
	}
	for i := range c.queue {
		if c.queue[i].index == index {
			c.queue[i].transcribed = true
		}
	}
}

// audio records caller audio sent to the session.
func (c *callerTurns) audio() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.audioSince = true
}

// toolResults records tool results sent to the session.
func (c *callerTurns) toolResults() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolsSince = true
}

// replyBegins places the reply that begins now and returns its turn. input is
// the provider's ID of the caller input it answers, "" when it names none.
// Calling it again before replyEnds returns the same turn.
func (c *callerTurns) replyBegins(input string) callerTurn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.replying {
		return c.reply
	}
	switch {
	case c.answered && input != "" && input == c.replyInput:
		// The same input again: a further reply in the turn.
	case c.takeNamedLocked(input):
	case len(c.queue) > 0:
		c.reply = c.queue[0]
		c.queue = c.queue[1:]
		if input != "" && c.reply.input == "" {
			c.reply.input = input
			c.nameLocked(c.reply)
		}
	case input != "":
		c.reply = callerTurn{index: c.next, heard: true, input: input}
		c.next++
		c.nameLocked(c.reply)
	case c.audioSince && !c.toolsSince:
		c.reply = callerTurn{index: c.next}
		c.next++
	case !c.answered:
		// A reply before any caller input: a turn of its own, unspoken.
		c.reply = callerTurn{index: c.next}
		c.next++
	}
	if input != "" {
		c.replyInput = input
	}
	c.answered = true
	c.replying = true
	c.barged = false
	c.audioSince, c.toolsSince = false, false
	return c.reply
}

// takeNamedLocked makes the waiting turn of input, when there is one, the
// reply's, and reports whether there was.
func (c *callerTurns) takeNamedLocked(input string) bool {
	if input == "" {
		return false
	}
	for i, t := range c.queue {
		if t.input == input {
			c.reply = t
			c.queue = append(c.queue[:i], c.queue[i+1:]...)
			return true
		}
	}
	return false
}

// bargeIn records that the caller barged in: the reply in progress ends, cut
// short, and the caller is speaking a turn of their own.
func (c *callerTurns) bargeIn() {
	c.mu.Lock()
	again := c.barged
	c.barged = true
	c.mu.Unlock()
	if again {
		return
	}
	c.replyEnds()
	c.heardSpeech("", false)
}

// replyEnds ends the reply in progress.
func (c *callerTurns) replyEnds() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.replying = false
}

// answer takes turn index out of the turns waiting for a reply: a blocked
// typed turn, answered by its blocked message rather than by a reply.
func (c *callerTurns) answer(index int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, t := range c.queue {
		if t.index == index {
			c.queue = append(c.queue[:i], c.queue[i+1:]...)
			c.reply, c.answered = t, true
			return
		}
	}
}

// transcriptTurn is the turn a caller transcript is judged at: the turn of
// the input the provider named for it, else, for a transcript that arrived
// after the reply to it (late), the turn the latest reply answered, else the
// current turn.
func (c *callerTurns) transcriptTurn(input string, late bool) int {
	c.mu.Lock()
	idx, named := c.byInput[input]
	answered, reply := c.answered, c.reply.index
	c.mu.Unlock()
	switch {
	case named:
		return idx
	case late && answered:
		return reply
	}
	return c.current()
}

// current is the index of the turn a guardrail check made now belongs to: the
// reply in progress, else the newest caller turn waiting for its reply, else
// the turn the latest reply answered, else the first.
func (c *callerTurns) current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.replying:
		return c.reply.index
	case len(c.queue) > 0:
		return c.queue[len(c.queue)-1].index
	case c.answered:
		return c.reply.index
	}
	return c.next
}
