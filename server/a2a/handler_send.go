package a2aserver

import (
	"context"
	"log"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// The non-streaming half of stateless mode.
//
// message/send has to answer with a finished task, and the task machinery asks
// a SendResult what happened. A handler yields only events, so the server
// drains them into one (see streamResult). Everything else — task creation, the
// settle wait, the blocking configuration, the final read — is the same as the
// conversation-backed path, deliberately: the protocol behavior a client sees
// must not depend on which mode the embedder chose.

// handleSendViaHandler serves message/send from a MessageHandler.
func (s *Server) handleSendViaHandler(
	w http.ResponseWriter, r *http.Request, req *a2a.JSONRPCRequest,
	contextID string, params a2a.SendMessageRequest,
) {
	toolResults := extractToolResults(params.Message.Parts)

	// A tool result is a continuation, and only a handler that asked for client
	// tools can continue. Refusing is honest: the server holds nothing to
	// resume, so pretending would produce a turn with no history behind it.
	trh, resumable := s.handler.(ToolResultHandler)
	if len(toolResults) > 0 && !resumable {
		writeRPCError(w, req.ID, -32601,
			"Client tool results are not supported by this agent")
		return
	}

	taskID := generateID()
	if _, err := s.taskStore.Create(taskID, contextID); err != nil {
		log.Printf("a2a: failed to create task for context %s: %v", contextID, err)
		writeRPCError(w, req.ID, -32000, "internal server error")
		return
	}

	// Detached from the request's cancellation because this goroutine outlives
	// the HTTP handler on the non-blocking path, but keeping its values so the
	// handler still sees caller identity and the inbound trace (#2018).
	bgCtx := context.WithoutCancel(r.Context())

	var done <-chan struct{}
	if len(toolResults) > 0 {
		done = s.runHandlerTurn(bgCtx, taskID, func(ctx context.Context) <-chan StreamEvent {
			return trh.HandleToolResult(ctx, ToolResultRequest{
				ContextID: contextID,
				TaskID:    taskID,
				Results:   toToolResults(toolResults),
			})
		})
	} else {
		done = s.runHandlerTurn(bgCtx, taskID, func(ctx context.Context) <-chan StreamEvent {
			return s.handler.Handle(ctx, MessageRequest{
				ContextID: contextID,
				TaskID:    taskID,
				Message:   params.Message,
			})
		})
	}

	s.awaitTurn(w, req, taskID, done, params.Configuration)
}

// runHandlerTurn runs one handler call to completion, drained into the
// SendResult the task machinery needs.
func (s *Server) runHandlerTurn(
	parent context.Context, taskID string, start func(context.Context) <-chan StreamEvent,
) <-chan struct{} {
	return s.runTurn(parent, taskID, func(ctx context.Context) (SendResult, error) {
		result := drain(ctx, start(ctx))
		if result.err != nil {
			return nil, result.err
		}
		return result, nil
	})
}

// toToolResults converts the wire form into what a handler receives.
func toToolResults(entries []toolResultEntry) []ToolResult {
	// The two types are deliberately identical in shape: toolResultEntry is the
	// wire form and ToolResult is what a handler receives, and keeping them
	// separate means the exported one can change without the parser moving.
	out := make([]ToolResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, ToolResult(e))
	}
	return out
}
