// Package safe provides panic-safe goroutine helpers. A panic in a worker
// goroutine (e.g. parsing a malformed provider stream, an index-out-of-range on
// an empty API response) is not recoverable by the goroutine that spawned it and
// crashes the entire process. These helpers recover and log the panic instead,
// and let the caller signal downstream (close a channel, emit an error) so
// consumers don't hang — turning "one bad response kills the process" into "one
// failed turn".
package safe

import (
	"runtime/debug"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// Run executes fn with panic recovery. On panic it logs the value and stack,
// then calls onPanic (if non-nil) with the recovered value so the caller can
// signal downstream (e.g. send an error chunk before a deferred close). Use this
// inside an existing goroutine body.
func Run(name string, fn func(), onPanic func(recovered any)) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("recovered panic in goroutine",
				"name", name, "panic", r, "stack", string(debug.Stack()))
			if onPanic != nil {
				onPanic(r)
			}
		}
	}()
	fn()
}

// Go runs fn in a new goroutine with panic recovery (see Run).
func Go(name string, fn func(), onPanic func(recovered any)) {
	go Run(name, fn, onPanic)
}

// Recover recovers and logs a panic (with stack). Defer it at the top of a
// goroutine whose body has its own cleanup defers (e.g. a channel close):
//
//	go func() {
//		defer close(outChan)     // still runs after the panic is recovered
//		defer safe.Recover("x")  // recovers so the process survives
//		...
//	}()
func Recover(name string) {
	if r := recover(); r != nil {
		logger.Error("recovered panic in goroutine",
			"name", name, "panic", r, "stack", string(debug.Stack()))
	}
}
