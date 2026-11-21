package stage

import "sync"

// callerTurns places a realtime session's replies against the caller's turns:
// it counts the caller's input turns and gives each reply the 0-based index of
// the caller turn it answers. That index is what the session's guardrails
// (ProviderRequest.TurnIndex) and its turn evals (ElementMetadata.CallerTurn)
// report, so both place a reply in the same turn.
//
// A caller turn is input the caller gave: a typed message, or speech the
// provider took as a turn. Speech is known by the first of: the caller
// barging in, a transcript of a new utterance, the provider naming a new
// input the reply answers (providers.ReplyToInputKey), or, when the provider
// says neither, caller audio sent since the last reply began. A transcript is
// not needed: a spoken turn with none is still a turn.
//
// A reply with no new caller input since the last reply began answers the
// same turn: a tool round's further reply, or a second reply to the same
// words. A reply before the caller has said anything (a greeting) is a turn
// of its own, so the caller's first turn after it is the next. A session that
// opens on a stored conversation goes on from the caller turns stored
// (TurnState.TurnIndex when it opened).
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
	// audioSince and toolsSince record caller audio and tool results sent
	// since the latest reply began.
	audioSince, toolsSince bool
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
}

// start sets the caller turns the transcript held when the session opened,
// once.
func (c *callerTurns) start(prior int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startLocked(prior)
}

func (c *callerTurns) startLocked(prior int) {
	if c.started {
		return
	}
	c.started = true
	c.next = prior
}

// pushLocked adds a new caller turn and returns its index.
func (c *callerTurns) pushLocked(heard, transcribed bool) int {
	t := callerTurn{index: c.next, heard: heard, transcribed: transcribed}
	c.next++
	c.queue = append(c.queue, t)
	return t.index
}

// typed adds a typed caller turn and returns its index.
func (c *callerTurns) typed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pushLocked(false, false)
}

// spoken records that the caller is speaking a new turn (a barge-in, or the
// transcript of a new utterance, when transcribed): a turn is added unless
// one is already waiting for its reply. A transcript that arrives while a
// reply is in progress is that reply's turn's.
func (c *callerTurns) spoken(transcribed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if transcribed && c.replying {
		c.reply.transcribed = true
		return
	}
	if n := len(c.queue); n > 0 {
		if transcribed {
			c.queue[n-1].transcribed = true
		}
		return
	}
	c.pushLocked(true, transcribed)
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

// replyBegins places the reply that begins now and returns its turn. inputID
// is the provider's ID of the caller input it answers, "" when it names none.
// Calling it again before replyEnds returns the same turn.
func (c *callerTurns) replyBegins(inputID string) callerTurn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.replying {
		return c.reply
	}
	switch {
	case c.answered && inputID != "" && inputID == c.replyInput:
		// The same input again: a further reply in the turn.
	case len(c.queue) > 0:
		c.reply = c.queue[0]
		c.queue = c.queue[1:]
	case inputID != "":
		c.reply = callerTurn{index: c.next, heard: true}
		c.next++
	case c.audioSince && !c.toolsSince:
		c.reply = callerTurn{index: c.next}
		c.next++
	case !c.answered:
		// A reply before any caller input: a turn of its own, unspoken.
		c.reply = callerTurn{index: c.next}
		c.next++
	}
	if inputID != "" {
		c.replyInput = inputID
	}
	c.answered = true
	c.replying = true
	c.audioSince, c.toolsSince = false, false
	return c.reply
}

// bargeIn records that the caller barged in: the reply in progress ends, cut
// short, and the caller is speaking a turn of their own.
func (c *callerTurns) bargeIn() {
	c.replyEnds()
	c.spoken(false)
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
