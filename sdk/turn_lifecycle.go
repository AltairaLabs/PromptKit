package sdk

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// lifecycleWorker runs a conversation's per-turn lifecycle jobs one at a
// time, in the order they were queued, on a goroutine of its own: never on a
// duplex session's output goroutine, never inline in the pipeline. A job's
// session hook may therefore call back into the conversation (SendText, or
// Close: see Conversation.end).
type lifecycleWorker struct {
	mu   sync.Mutex
	cond *sync.Cond
	// queue holds the jobs not yet started; running is set while one runs.
	queue   []func()
	running bool
	started bool
	stopped bool
	// hooks counts the session hooks executing in a job.
	hooks int
}

func newLifecycleWorker() *lifecycleWorker {
	w := &lifecycleWorker{}
	w.cond = sync.NewCond(&w.mu)
	return w
}

// enqueue queues job, starting the worker on first use. Nil-safe; a stopped
// worker drops it.
func (w *lifecycleWorker) enqueue(job func()) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	w.queue = append(w.queue, job)
	if !w.started {
		w.started = true
		go w.loop()
	}
	w.cond.Broadcast()
}

// loop runs queued jobs until the worker stops.
func (w *lifecycleWorker) loop() {
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && !w.stopped {
			w.cond.Wait()
		}
		if len(w.queue) == 0 {
			w.mu.Unlock()
			return
		}
		job := w.queue[0]
		w.queue = w.queue[1:]
		w.running = true
		w.mu.Unlock()

		job()

		w.mu.Lock()
		w.running = false
		w.cond.Broadcast()
		w.mu.Unlock()
	}
}

// waitIdle waits until every job queued so far has run. Nil-safe.
func (w *lifecycleWorker) waitIdle() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(w.queue) > 0 || w.running {
		w.cond.Wait()
	}
}

// runQueued runs the jobs queued now, on the calling goroutine (the worker's
// own, from a job), until none is left.
func (w *lifecycleWorker) runQueued() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.mu.Unlock()
			return
		}
		job := w.queue[0]
		w.queue = w.queue[1:]
		w.mu.Unlock()
		job()
	}
}

// stop drops jobs queued from now on and lets the goroutine exit once idle.
// Nil-safe.
func (w *lifecycleWorker) stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	w.cond.Broadcast()
}

// hookRunning reports whether a session hook is executing in a job.
func (w *lifecycleWorker) hookRunning() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hooks > 0
}

// inHook counts a session hook executing while fn runs.
func (w *lifecycleWorker) inHook(fn func()) {
	w.mu.Lock()
	w.hooks++
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.hooks--
		w.mu.Unlock()
	}()
	fn()
}

// turnsStored is the save stage's report (stage.IncrementalSaveConfig
// .OnTurnsStored) of the completed turns a write just stored, by the
// transcript index of each one's completing reply: each is queued for its
// lifecycle, in order. A turn whose write failed is reported, and so runs,
// once a later write stores it.
func (c *Conversation) turnsStored(replyIndices []int) {
	for _, idx := range replyIndices {
		c.lifecycle.enqueue(func() { c.runTurnLifecycle(idx) })
	}
}

// awaitTurnLifecycles waits until the lifecycle of every turn stored so far
// has run: a text turn's runs before the caller sees the turn end (Send
// returns, Stream and ResumeStream send ChunkDone).
func (c *Conversation) awaitTurnLifecycles() {
	c.lifecycle.waitIdle()
}

// runTurnLifecycle is the work done once each completed turn is stored, on
// the lifecycle worker: the turn is numbered, OnSessionUpdate runs, and the
// per-turn evals are dispatched. A duplex session's lifecycle runs
// asynchronously, in order, with the session going on meanwhile.
//
// The stored transcript is loaded once, for the session hooks and the turn
// evals alike, on the conversation's lifetime context, and cut at the turn's
// reply (replyIndex; -1: the whole transcript), so a turn stored late sees
// itself, not a later turn. The turn is numbered from it (turnOrdinal). A
// load that fails skips the turn's lifecycle, logged. With no session hook and
// no eval nothing is loaded.
func (c *Conversation) runTurnLifecycle(replyIndex int) {
	if !c.sessionHooks.dispatches() && c.evalMW == nil {
		// Nothing reads the transcript. A text turn's number is the one its
		// pipeline derived from the store (the same CountUserTurns ordinal).
		if c.mode == UnaryMode {
			c.sessionHooks.SetTurn(c.turnState.TurnIndex())
		}
		return
	}
	ctx := c.lifecycleContext()
	base := c.session()
	if base == nil {
		return
	}
	messages, err := base.Messages(ctx)
	if err != nil {
		logger.Error("skipping a turn's session update and turn evals: loading its transcript failed",
			"conversation", base.ID(), "error", err)
		return
	}
	if replyIndex >= 0 && replyIndex < len(messages) {
		messages = messages[:replyIndex+1]
	}
	messages = append([]types.Message(nil), messages...)
	number := turnOrdinal(messages)
	c.sessionHooks.SetTurn(number)
	c.lifecycle.inHook(func() { c.sessionHooks.SessionUpdateWith(ctx, messages) })
	c.evalMW.dispatchTurnEvalsWith(ctx, messages, number) // nil-safe, no-op if middleware is nil
}

// lifecycleContext is the context the lifecycle's loads and hooks run on:
// tied to the conversation's lifetime (canceled once Close has ended the
// session), carrying the conversation's state.
func (c *Conversation) lifecycleContext() context.Context {
	c.lifeOnce.Do(func() {
		c.lifeCtx, c.lifeCancel = context.WithCancel(c.withConversationState(context.Background()))
	})
	return c.lifeCtx
}

// endLifecycle stops the worker and cancels the lifecycle context.
func (c *Conversation) endLifecycle() {
	c.lifecycle.stop()
	c.lifecycleContext()
	c.lifeCancel()
}
