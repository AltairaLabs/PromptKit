package a2aserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// subscriberBuffer is the channel buffer size for broadcast subscribers.
const subscriberBuffer = 64

// ssePayload is a single SSE payload ready to be broadcast.
type ssePayload struct {
	Data []byte // JSON-encoded JSON-RPC response
}

// taskBroadcaster fans out SSE events to multiple subscribers for a single task.
type taskBroadcaster struct {
	mu     sync.Mutex
	subs   []chan ssePayload
	closed bool
}

// subscribe adds a new subscriber and returns its channel.
func (b *taskBroadcaster) subscribe() <-chan ssePayload {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan ssePayload, subscriberBuffer)
	if b.closed {
		close(ch)
		return ch
	}
	b.subs = append(b.subs, ch)
	return ch
}

// unsubscribe removes a subscriber channel.
func (b *taskBroadcaster) unsubscribe(ch <-chan ssePayload) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subs {
		if s == ch {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			return
		}
	}
}

// send broadcasts an event to all subscribers.
func (b *taskBroadcaster) send(evt ssePayload) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, ch := range b.subs {
		select {
		case ch <- evt:
		default:
			// slow subscriber — drop event to avoid blocking
		}
	}
}

// close marks the broadcaster as closed and closes all subscriber channels.
func (b *taskBroadcaster) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, ch := range b.subs {
		close(ch)
	}
	b.subs = nil
}

// getBroadcaster returns or creates a broadcaster for the given task ID.
func (s *Server) getBroadcaster(taskID string) *taskBroadcaster {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	b, ok := s.subs[taskID]
	if !ok {
		b = &taskBroadcaster{}
		s.subs[taskID] = b
	}
	return b
}

// removeBroadcaster removes a broadcaster from the map.
func (s *Server) removeBroadcaster(taskID string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	delete(s.subs, taskID)
}

// closeAllBroadcasters closes all active broadcasters.
func (s *Server) closeAllBroadcasters() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for id, b := range s.subs {
		b.close()
		delete(s.subs, id)
	}
}

// marshalRaw marshals v to json.RawMessage.
func marshalRaw(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// writeSSE writes a single SSE event to the response.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, id, event any) {
	data, _ := json.Marshal(a2a.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  marshalRaw(event),
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// broadcastEvent builds an ssePayload and sends it to the broadcaster.
func broadcastEvent(b *taskBroadcaster, rpcID, event any) {
	data, _ := json.Marshal(a2a.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      rpcID,
		Result:  marshalRaw(event),
	})
	b.send(ssePayload{Data: data})
}

// streamCtx bundles the common parameters for streaming SSE to a client.
type streamCtx struct {
	srv       *Server
	w         http.ResponseWriter
	flusher   http.Flusher
	b         *taskBroadcaster
	rpcID     any
	taskID    string
	contextID string
}

// emit sends an event to both the direct SSE writer and the broadcaster.
func (sc *streamCtx) emit(event any) {
	writeSSE(sc.w, sc.flusher, sc.rpcID, event)
	broadcastEvent(sc.b, sc.rpcID, event)
}

// fail records a task failure in the store and emits the failed status event.
func (sc *streamCtx) fail(errText string) {
	_ = sc.srv.taskStore.SetState(sc.taskID, a2a.TaskStateFailed, &a2a.Message{
		Role:  a2a.RoleAgent,
		Parts: []a2a.Part{{Text: &errText}},
	})
	sc.emit(a2a.TaskStatusUpdateEvent{
		TaskID:    sc.taskID,
		ContextID: sc.contextID,
		Status: a2a.TaskStatus{
			State:   a2a.TaskStateFailed,
			Message: &a2a.Message{Role: a2a.RoleAgent, Parts: []a2a.Part{{Text: &errText}}},
		},
	})
	sc.b.close()
	sc.srv.removeBroadcaster(sc.taskID)
}

// complete records a task completion and emits the completed status event.
func (sc *streamCtx) complete() {
	_ = sc.srv.taskStore.SetState(sc.taskID, a2a.TaskStateCompleted, nil)
	sc.emit(a2a.TaskStatusUpdateEvent{
		TaskID:    sc.taskID,
		ContextID: sc.contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
	})
	sc.b.close()
	sc.srv.removeBroadcaster(sc.taskID)
}

// handleStreamMessage processes a message/stream request.
func (s *Server) handleStreamMessage(
	w http.ResponseWriter, r *http.Request, req *a2a.JSONRPCRequest,
) {
	var params a2a.SendMessageRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	contextID := params.Message.ContextID
	if contextID == "" {
		contextID = generateID()
	}

	conv, err := s.getOrCreateConversation(contextID)
	if err != nil {
		writeRPCError(w, req.ID, -32000,
			fmt.Sprintf("Failed to open conversation: %v", err))
		return
	}

	streamConv, ok := conv.(StreamingConversation)
	if !ok {
		writeRPCError(w, req.ID, -32601,
			"Streaming not supported by this agent")
		return
	}

	// Check if this is a tool-result message for a resumable conversation.
	if toolResults := extractToolResults(params.Message.Parts); len(toolResults) > 0 {
		s.handleStreamToolResultMessage(w, r, req, streamConv, contextID, toolResults)
		return
	}

	pkMsg, err := a2a.MessageToMessage(&params.Message)
	if err != nil {
		writeRPCError(w, req.ID, -32602,
			fmt.Sprintf("Invalid message: %v", err))
		return
	}

	taskID := generateID()
	if _, err := s.taskStore.Create(taskID, contextID); err != nil {
		writeRPCError(w, req.ID, -32000,
			fmt.Sprintf("Failed to create task: %v", err))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRPCError(w, req.ID, -32000, "Streaming not supported")
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sc := &streamCtx{
		srv:       s,
		w:         w,
		flusher:   flusher,
		b:         s.getBroadcaster(taskID),
		rpcID:     req.ID,
		taskID:    taskID,
		contextID: contextID,
	}

	// Set task to working.
	_ = s.taskStore.SetState(taskID, a2a.TaskStateWorking, nil)
	sc.emit(a2a.TaskStatusUpdateEvent{
		TaskID:    taskID,
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	})

	// Use request context so client disconnect cancels the stream.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	s.cancelsMu.Lock()
	s.cancels[taskID] = cancel
	s.cancelsMu.Unlock()

	events := streamConv.Stream(ctx, pkMsg)

	sc.processEvents(ctx, events)
}

// handleStreamToolResultMessage processes a streaming message/send that carries
// client tool results. It submits results, then resumes via ResumeStream.
func (s *Server) handleStreamToolResultMessage(
	w http.ResponseWriter, r *http.Request, req *a2a.JSONRPCRequest,
	conv StreamingConversation, contextID string, results []toolResultEntry,
) {
	resumable, ok := conv.(ResumableConversation)
	if !ok {
		writeRPCError(w, req.ID, -32000, "Conversation does not support client tool results")
		return
	}

	for _, tr := range results {
		if tr.Rejected {
			resumable.RejectClientTool(tr.CallID, tr.Reason)
		} else {
			if err := resumable.SendToolResult(tr.CallID, tr.Result); err != nil {
				writeRPCError(w, req.ID, -32000, fmt.Sprintf("Failed to submit tool result: %v", err))
				return
			}
		}
	}

	taskID := generateID()
	if _, err := s.taskStore.Create(taskID, contextID); err != nil {
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("Failed to create task: %v", err))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRPCError(w, req.ID, -32000, "Streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sc := &streamCtx{
		srv:       s,
		w:         w,
		flusher:   flusher,
		b:         s.getBroadcaster(taskID),
		rpcID:     req.ID,
		taskID:    taskID,
		contextID: contextID,
	}

	_ = s.taskStore.SetState(taskID, a2a.TaskStateWorking, nil)
	sc.emit(a2a.TaskStatusUpdateEvent{
		TaskID:    taskID,
		ContextID: contextID,
		Status:    a2a.TaskStatus{State: a2a.TaskStateWorking},
	})

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	s.cancelsMu.Lock()
	s.cancels[taskID] = cancel
	s.cancelsMu.Unlock()

	events := resumable.ResumeStream(ctx)
	sc.processEvents(ctx, events)
}

// processEvents iterates over the stream channel and emits SSE events.
// It monitors ctx for client disconnects so the loop exits promptly instead
// of blocking on a channel read indefinitely (preventing goroutine leaks).
func (sc *streamCtx) processEvents(ctx context.Context, events <-chan StreamEvent) {
	artifactIdx := 0

	for {
		select {
		case <-ctx.Done():
			// Client disconnected or context canceled. Clean up the
			// broadcaster so subscribers are notified, then exit.
			sc.b.close()
			sc.srv.removeBroadcaster(sc.taskID)
			return

		case evt, ok := <-events:
			if !ok {
				// Channel closed without a Done event — treat as completed.
				sc.complete()
				return
			}

			done, idx := sc.handleEvent(evt, artifactIdx)
			if done {
				return
			}
			artifactIdx = idx
		}
	}
}

// handleEvent processes a single stream event and returns whether the stream
// is finished and the updated artifact index.
func (sc *streamCtx) handleEvent(evt StreamEvent, artifactIdx int) (done bool, nextIdx int) {
	if evt.Error != nil {
		sc.fail(evt.Error.Error())
		return true, artifactIdx
	}

	switch evt.Kind {
	case EventText:
		sc.emitArtifact(artifactIdx, []a2a.Part{{Text: &evt.Text}})
		return false, artifactIdx + 1

	case EventMedia:
		if evt.Media == nil {
			return false, artifactIdx
		}
		part, convErr := a2a.ContentPartToA2APart(types.ContentPart{
			Type:  a2a.InferContentType(evt.Media.MIMEType),
			Media: evt.Media,
		})
		if convErr != nil {
			return false, artifactIdx
		}
		sc.emitArtifact(artifactIdx, []a2a.Part{part})
		return false, artifactIdx + 1

	case EventToolCall:
		// Suppressed — agent opacity. Task stays working.
		return false, artifactIdx

	case EventClientTool:
		sc.emitInputRequired(evt)
		return true, artifactIdx

	case EventDone:
		sc.complete()
		return true, artifactIdx

	default:
		return false, artifactIdx
	}
}

// emitInputRequired emits a task status update with input_required state
// and client tool metadata so the A2A client can fulfill the tool request.
func (sc *streamCtx) emitInputRequired(evt StreamEvent) {
	var msg *a2a.Message
	if evt.ClientTool != nil {
		text := fmt.Sprintf("Client tool required: %s", evt.ClientTool.ToolName)
		msg = &a2a.Message{
			Role: a2a.RoleAgent,
			Parts: []a2a.Part{{
				Text: &text,
				Metadata: map[string]any{
					"tool_call_id":    evt.ClientTool.CallID,
					"tool_name":       evt.ClientTool.ToolName,
					"tool_args":       evt.ClientTool.Args,
					"consent_message": evt.ClientTool.ConsentMsg,
				},
			}},
		}
	}
	_ = sc.srv.taskStore.SetState(sc.taskID, a2a.TaskStateInputRequired, msg)
	sc.emit(a2a.TaskStatusUpdateEvent{
		TaskID:    sc.taskID,
		ContextID: sc.contextID,
		Status: a2a.TaskStatus{
			State:   a2a.TaskStateInputRequired,
			Message: msg,
		},
	})
	sc.b.close()
	sc.srv.removeBroadcaster(sc.taskID)
}

// emitArtifact emits a single artifact update event.
func (sc *streamCtx) emitArtifact(idx int, parts []a2a.Part) {
	sc.emit(a2a.TaskArtifactUpdateEvent{
		TaskID:    sc.taskID,
		ContextID: sc.contextID,
		Artifact: a2a.Artifact{
			ArtifactID: fmt.Sprintf("artifact-%d", idx),
			Parts:      parts,
		},
		Append: true,
	})
}

// handleTaskSubscribe processes a tasks/subscribe request.
func (s *Server) handleTaskSubscribe(w http.ResponseWriter, r *http.Request, req *a2a.JSONRPCRequest) {
	var params a2a.SubscribeTaskRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, "Invalid params")
		return
	}

	// Look up existing broadcaster.
	s.subsMu.Lock()
	broadcaster, hasBroadcaster := s.subs[params.ID]
	s.subsMu.Unlock()

	if !hasBroadcaster {
		// No active broadcaster. Check if task exists and is terminal.
		task, err := s.taskStore.Get(params.ID)
		if err != nil {
			writeRPCError(w, req.ID, -32001, fmt.Sprintf("Task not found: %v", err))
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeRPCError(w, req.ID, -32000, "Streaming not supported")
			return
		}

		// Task exists but no active stream. Send its current status.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		writeSSE(w, flusher, req.ID, a2a.TaskStatusUpdateEvent{
			TaskID:    task.ID,
			ContextID: task.ContextID,
			Status:    task.Status,
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRPCError(w, req.ID, -32000, "Streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := broadcaster.subscribe()
	defer broadcaster.unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				// Broadcaster closed — stream ended.
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", evt.Data)
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}
