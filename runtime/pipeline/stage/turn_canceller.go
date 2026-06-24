package stage

import (
	"context"
	"sync"
)

// interruptWorkBuffer bounds the in-flight element queue between an input reader
// goroutine and a stage's processing loop in the barge-in pattern. An Interrupt
// cancels the in-flight turn out of band (see drainCancelingOnInterrupt), so this
// buffer only needs to absorb a single turn's worth of elements — far below this
// bound in practice.
const interruptWorkBuffer = 64

// turnCanceller hands a stage's processing loop a cancelable child of the
// pipeline context and lets a barge-in cancel the in-flight turn from a separate
// reader goroutine. It is shared by the stages that must preempt a blocking call
// (provider generation, TTS synthesis) when an Interrupt arrives.
//
// refresh() rolls a fresh context for the next turn after an interrupt; the
// canceled context drops both the in-flight call and any work already queued
// behind it (their context is already done).
type turnCanceller struct {
	parent context.Context
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func newTurnCanceller(parent context.Context) *turnCanceller {
	c := &turnCanceller{parent: parent}
	c.refresh()
	return c
}

// refresh cancels the current turn context (if any) and starts a new one.
func (c *turnCanceller) refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.ctx, c.cancel = context.WithCancel(c.parent)
}

// interrupt cancels the in-flight turn context without rolling a new one.
func (c *turnCanceller) interrupt() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// context returns the current turn context.
func (c *turnCanceller) context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

// stop releases the final context on shutdown.
func (c *turnCanceller) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// drainCancelingOnInterrupt drains input into work, canceling the in-flight turn
// the instant an Interrupt arrives (out of band) so a barge-in preempts a
// blocking call (generation/synthesis) that would otherwise hold the loop. Order
// is preserved: the Interrupt is still forwarded through work so the loop can
// refresh the context and emit it downstream.
func drainCancelingOnInterrupt(input <-chan StreamElement, work chan<- StreamElement, c *turnCanceller) {
	defer close(work)
	for elem := range input {
		if elem.Interrupt {
			c.interrupt()
		}
		work <- elem
	}
}
