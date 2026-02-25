package sdk

import (
	"context"
	"log"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// evalMiddleware holds dispatch state for eval execution within a conversation.
type evalMiddleware struct {
	dispatcher   evals.EvalDispatcher
	defs         []evals.EvalDef
	resultWriter evals.ResultWriter
	conv         *Conversation
	turnIndex    int
}

// newEvalMiddleware creates eval middleware for a conversation.
// Returns nil if no dispatcher is configured or no eval defs are resolved.
func newEvalMiddleware(conv *Conversation) *evalMiddleware {
	if conv.config == nil || conv.config.evalDispatcher == nil {
		return nil
	}

	// Resolve eval defs from pack + prompt
	var packEvals, promptEvals []evals.EvalDef
	if conv.pack != nil {
		packEvals = conv.pack.Evals
	}
	if conv.prompt != nil {
		promptEvals = conv.prompt.Evals
	}

	defs := evals.ResolveEvals(packEvals, promptEvals)
	if len(defs) == 0 {
		return nil
	}

	// Build composite result writer from configured writers
	var resultWriter evals.ResultWriter
	if len(conv.config.evalResultWriters) == 1 {
		resultWriter = conv.config.evalResultWriters[0]
	} else if len(conv.config.evalResultWriters) > 1 {
		resultWriter = evals.NewCompositeResultWriter(conv.config.evalResultWriters...)
	}

	return &evalMiddleware{
		dispatcher:   conv.config.evalDispatcher,
		defs:         defs,
		resultWriter: resultWriter,
		conv:         conv,
	}
}

// dispatchTurnEvals dispatches turn-level evals asynchronously.
// Nil-safe: no-op if middleware is nil.
func (em *evalMiddleware) dispatchTurnEvals(ctx context.Context) {
	if em == nil {
		return
	}

	em.turnIndex++
	evalCtx := em.buildEvalContext()

	// Dispatch async — don't block Send()
	go func() {
		results, err := em.dispatcher.DispatchTurnEvals(ctx, em.defs, evalCtx)
		if err != nil {
			log.Printf("evals: turn dispatch error: %v", err)
		}
		if em.resultWriter != nil && len(results) > 0 {
			if writeErr := em.resultWriter.WriteResults(ctx, results); writeErr != nil {
				log.Printf("evals: result write error: %v", writeErr)
			}
		}
	}()
}

// dispatchSessionEvals dispatches session-complete evals synchronously.
// Nil-safe: no-op if middleware is nil.
// Runs synchronously during Close() to ensure completion.
func (em *evalMiddleware) dispatchSessionEvals(ctx context.Context) {
	if em == nil {
		return
	}

	evalCtx := em.buildEvalContext()

	results, err := em.dispatcher.DispatchSessionEvals(ctx, em.defs, evalCtx)
	if err != nil {
		log.Printf("evals: session dispatch error: %v", err)
	}
	if em.resultWriter != nil && len(results) > 0 {
		if writeErr := em.resultWriter.WriteResults(ctx, results); writeErr != nil {
			log.Printf("evals: session result write error: %v", writeErr)
		}
	}
}

// buildEvalContext creates an EvalContext from the conversation state.
func (em *evalMiddleware) buildEvalContext() *evals.EvalContext {
	ctx := &evals.EvalContext{
		TurnIndex: em.turnIndex,
		PromptID:  em.conv.promptName,
	}

	// Safely get session info — sessions may not be initialized in tests
	// or when middleware is used standalone.
	if em.conv.unarySession != nil || em.conv.duplexSession != nil {
		ctx.Messages = em.conv.Messages(context.Background())
		ctx.SessionID = em.conv.ID()

		for i := len(ctx.Messages) - 1; i >= 0; i-- {
			if ctx.Messages[i].Role == "assistant" {
				ctx.CurrentOutput = ctx.Messages[i].GetContent()
				break
			}
		}
	}

	return ctx
}
