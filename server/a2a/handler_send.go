package a2aserver

import (
	"context"
	"log"
	"net/http"
	"time"

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

	if params.Configuration != nil && params.Configuration.Blocking {
		<-done
	} else {
		select {
		case <-done:
		case <-time.After(sendSettleTime):
		}
	}

	task, err := s.taskStore.Get(taskID)
	if err != nil {
		log.Printf("a2a: failed to retrieve task %s after processing: %v", taskID, err)
		writeRPCError(w, req.ID, -32000, "internal server error")
		return
	}
	writeRPCResult(w, req.ID, task)
}

// runHandlerTurn runs one handler call to completion in the background,
// mirroring runConversation: same cancellation registration so tasks/cancel
// works, same working/failed/completed transitions.
func (s *Server) runHandlerTurn(
	parent context.Context, taskID string, start func(context.Context) <-chan StreamEvent,
) <-chan struct{} {
	ctx, cancel := context.WithCancel(parent)
	s.cancelsMu.Lock()
	s.cancels[taskID] = cancel
	s.cancelsMu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer func() {
			s.cancelsMu.Lock()
			delete(s.cancels, taskID)
			s.cancelsMu.Unlock()
		}()

		if err := s.taskStore.SetState(taskID, a2a.TaskStateWorking, nil); err != nil {
			log.Printf("a2a: task %s: failed to set working state: %v", taskID, err)
		}

		result := drain(ctx, start(ctx))

		if result.err != nil {
			// A canceled context means tasks/cancel already set the state;
			// overwriting it with "failed" would lose that.
			if ctx.Err() == nil {
				errText := result.err.Error()
				if storeErr := s.taskStore.SetState(taskID, a2a.TaskStateFailed, &a2a.Message{
					Role:  a2a.RoleAgent,
					Parts: []a2a.Part{{Text: &errText}},
				}); storeErr != nil {
					log.Printf("a2a: task %s: failed to set failed state: %v", taskID, storeErr)
				}
			}
			return
		}
		if ctx.Err() != nil {
			return
		}

		s.finalizeTask(taskID, result)
	}()
	return done
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
