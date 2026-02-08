package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// StreamChunkType identifies the kind of streaming chunk.
type StreamChunkType int

// StreamChunk type constants.
const (
	StreamChunkText     StreamChunkType = iota // Incremental text
	StreamChunkMedia                           // Media content (image, audio, etc.)
	StreamChunkToolCall                        // Tool call (suppressed — agent opacity)
	StreamChunkDone                            // Final signal
)

// subscriberBuffer is the channel buffer size for broadcast subscribers.
const subscriberBuffer = 64

// StreamChunk is a single unit of streaming output from a conversation.
type StreamChunk struct {
	Type  StreamChunkType
	Text  string
	Media *types.MediaContent
	Error error
}

// StreamingConversation extends Conversation with streaming support.
type StreamingConversation interface {
	Conversation
	Stream(ctx context.Context, msg *types.Message) (<-chan StreamChunk, error)
}

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
	data, _ := json.Marshal(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  marshalRaw(event),
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// broadcastEvent builds an ssePayload and sends it to the broadcaster.
func broadcastEvent(b *taskBroadcaster, rpcID, event any) {
	data, _ := json.Marshal(JSONRPCResponse{
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
	_ = sc.srv.taskStore.SetState(sc.taskID, TaskStateFailed, &Message{
		Role:  RoleAgent,
		Parts: []Part{{Text: &errText}},
	})
	sc.emit(TaskStatusUpdateEvent{
		TaskID:    sc.taskID,
		ContextID: sc.contextID,
		Status: TaskStatus{
			State:   TaskStateFailed,
			Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &errText}}},
		},
	})
	sc.b.close()
	sc.srv.removeBroadcaster(sc.taskID)
}

// complete records a task completion and emits the completed status event.
func (sc *streamCtx) complete() {
	_ = sc.srv.taskStore.SetState(sc.taskID, TaskStateCompleted, nil)
	sc.emit(TaskStatusUpdateEvent{
		TaskID:    sc.taskID,
		ContextID: sc.contextID,
		Status:    TaskStatus{State: TaskStateCompleted},
	})
	sc.b.close()
	sc.srv.removeBroadcaster(sc.taskID)
}

// handleStreamMessage processes a message/stream request.
func (s *Server) handleStreamMessage(
	w http.ResponseWriter, r *http.Request, req *JSONRPCRequest,
) {
	var params SendMessageRequest
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

	pkMsg, err := MessageToMessage(&params.Message)
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
	_ = s.taskStore.SetState(taskID, TaskStateWorking, nil)
	sc.emit(TaskStatusUpdateEvent{
		TaskID:    taskID,
		ContextID: contextID,
		Status:    TaskStatus{State: TaskStateWorking},
	})

	// Use request context so client disconnect cancels the stream.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	s.cancelsMu.Lock()
	s.cancels[taskID] = cancel
	s.cancelsMu.Unlock()

	chunks, streamErr := streamConv.Stream(ctx, pkMsg)
	if streamErr != nil {
		sc.fail(streamErr.Error())
		return
	}

	sc.processChunks(chunks)
}

// processChunks iterates over the stream channel and emits SSE events.
func (sc *streamCtx) processChunks(chunks <-chan StreamChunk) {
	artifactIdx := 0

	for chunk := range chunks {
		if chunk.Error != nil {
			sc.fail(chunk.Error.Error())
			return
		}

		switch chunk.Type {
		case StreamChunkText:
			evt := TaskArtifactUpdateEvent{
				TaskID:    sc.taskID,
				ContextID: sc.contextID,
				Artifact: Artifact{
					ArtifactID: fmt.Sprintf("artifact-%d", artifactIdx),
					Parts:      []Part{{Text: &chunk.Text}},
				},
				Append: true,
			}
			artifactIdx++
			sc.emit(evt)

		case StreamChunkMedia:
			if chunk.Media == nil {
				continue
			}
			part, convErr := ContentPartToA2APart(types.ContentPart{
				Type:  InferContentType(chunk.Media.MIMEType),
				Media: chunk.Media,
			})
			if convErr != nil {
				continue
			}
			evt := TaskArtifactUpdateEvent{
				TaskID:    sc.taskID,
				ContextID: sc.contextID,
				Artifact: Artifact{
					ArtifactID: fmt.Sprintf("artifact-%d", artifactIdx),
					Parts:      []Part{part},
				},
				Append: true,
			}
			artifactIdx++
			sc.emit(evt)

		case StreamChunkToolCall:
			// Suppressed — agent opacity. Task stays working.

		case StreamChunkDone:
			sc.complete()
			return
		}
	}

	// Channel closed without a Done chunk — treat as completed.
	sc.complete()
}

// handleTaskSubscribe processes a tasks/subscribe request.
func (s *Server) handleTaskSubscribe(w http.ResponseWriter, r *http.Request, req *JSONRPCRequest) {
	var params SubscribeTaskRequest
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

		writeSSE(w, flusher, req.ID, TaskStatusUpdateEvent{
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
