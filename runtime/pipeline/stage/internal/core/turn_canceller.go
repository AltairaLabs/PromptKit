package core

import (
	"context"
	"sync"
	"time"
)

// InterruptWorkBuffer bounds the in-flight element queue between an input reader
// goroutine and a stage's processing loop in the barge-in pattern. An Interrupt
// cancels the in-flight turn out of band (see DrainCancelingOnInterrupt), so this
// buffer only needs to absorb a single turn's worth of elements — far below this
// bound in practice.
const InterruptWorkBuffer = 64

// TurnCanceller hands a stage's processing loop a cancelable child of the
// pipeline context and lets a barge-in cancel the in-flight turn from a separate
// reader goroutine. It is shared by the stages that must preempt a blocking call
// (provider generation, TTS synthesis) when an Interrupt arrives.
//
// Refresh rolls a fresh context for the next turn after an interrupt; the
// canceled context drops both the in-flight call and any work already queued
// behind it (their context is already done).
type TurnCanceller struct {
	parent context.Context
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	// cutAt is when an interrupt canceled the current turn context, for the
	// stages that record what the caller heard before it; zero when none has.
	cutAt time.Time
}

// NewTurnCanceller is a TurnCanceller with a live turn context, a child of
// parent.
func NewTurnCanceller(parent context.Context) *TurnCanceller {
	c := &TurnCanceller{parent: parent}
	c.Refresh()
	return c
}

// Refresh cancels the current turn context (if any) and starts a new one.
func (c *TurnCanceller) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.ctx, c.cancel = context.WithCancel(c.parent)
	c.cutAt = time.Time{}
}

// InterruptAt cancels the in-flight turn for an interrupt that arrived at t,
// recording t (the first, when several arrive before the next Refresh).
func (c *TurnCanceller) InterruptAt(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cutAt.IsZero() {
		c.cutAt = t
	}
	if c.cancel != nil {
		c.cancel()
	}
}

// InterruptedAt is when an interrupt canceled the current turn context, zero
// when none has since the last Refresh.
func (c *TurnCanceller) InterruptedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cutAt
}

// interrupt cancels the in-flight turn context without rolling a new one.
func (c *TurnCanceller) interrupt() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// Context returns the current turn Context.
func (c *TurnCanceller) Context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

// Stop releases the final context on shutdown.
func (c *TurnCanceller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// DrainCancelingOnInterrupt drains input into work, canceling the in-flight turn
// the instant an Interrupt arrives (out of band) so a barge-in preempts a
// blocking call (generation/synthesis) that would otherwise hold the loop. Order
// is preserved: the Interrupt is still forwarded through work so the loop can
// refresh the context and emit it downstream.
func DrainCancelingOnInterrupt(input <-chan StreamElement, work chan<- StreamElement, c *TurnCanceller) {
	defer close(work)
	for elem := range input {
		if elem.Interrupt {
			c.interrupt()
		}
		work <- elem
	}
}
