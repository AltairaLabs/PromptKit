package sdk

import (
	"context"
	"errors"
	"sync"
)

// ErrReentrantTurn is returned when a turn is started on a conversation from
// inside one of its own turns, such as a tool handler calling Send on the
// conversation that invoked it. The outer turn holds the conversation, so the
// inner call could only wait for itself.
//
// The nested call is recognized by its context, so a handler must pass on the
// context it was given (OnToolCtx). A call made with an unrelated context,
// such as from an OnTool handler, cannot be told apart from another caller's
// and waits for the running turn, which waits for the handler.
var ErrReentrantTurn = errors.New("sdk: a turn was started on a conversation from inside its own turn")

// turnGate lets one turn at a time run on a conversation (#2570). The
// conversation's pipeline, TurnState and history are shared by every turn, so
// two concurrent Sends would render, persist and read each other's state.
// Waiting for the gate is cancellable through the caller's context.
type turnGate struct {
	once sync.Once
	sem  chan struct{}
}

// turnOwnerKey marks a context as running inside a turn of a conversation.
type turnOwnerKey struct{ conv *Conversation }

func (g *turnGate) init() { g.once.Do(func() { g.sem = make(chan struct{}, 1) }) }

// acquire waits for the conversation's turn. It fails with ErrReentrantTurn
// when ctx is already inside a turn of conv, and with ctx's error when ctx
// ends first. The returned release must be called exactly once.
func (g *turnGate) acquire(ctx context.Context, conv *Conversation) (func(), error) {
	if ctx == nil { // tolerate callers that pass nil, as before the gate
		ctx = context.Background()
	}
	if ctx.Value(turnOwnerKey{conv: conv}) != nil {
		return nil, ErrReentrantTurn
	}
	g.init()
	select {
	case g.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() { once.Do(func() { <-g.sem }) }, nil
}

// mark returns ctx marked as running inside a turn of conv, so a nested
// attempt to start a turn on conv fails instead of deadlocking.
func (g *turnGate) mark(ctx context.Context, conv *Conversation) context.Context {
	if ctx == nil { // see acquire
		return nil
	}
	return context.WithValue(ctx, turnOwnerKey{conv: conv}, true)
}
