package a2aserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// streamWriter writes events to one SSE caller, in that caller's protocol
// version and under its JSON-RPC id. Once the caller has gone (detach), writes
// are dropped: the turn behind the stream runs on without it.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	id      any
	v       a2a.ProtocolVersion

	mu       sync.Mutex
	started  bool
	detached bool
}

// detach stops all further writes. After it returns, the response writer is
// never touched again, so the HTTP handler may return.
func (sw *streamWriter) detach() {
	sw.mu.Lock()
	sw.detached = true
	sw.mu.Unlock()
}

// newStreamWriter prepares an SSE response for call, answering with an error
// itself and returning nil when the connection cannot stream.
func newStreamWriter(call *rpcCall) *streamWriter {
	flusher, ok := call.w.(http.Flusher)
	if !ok {
		call.fail(a2a.ErrCodeUnsupportedOperation, "Streaming is not supported on this connection")
		return nil
	}
	return &streamWriter{w: call.w, flusher: flusher, id: call.req.ID, v: call.v}
}

// write sends one event: a *Task, *Message, *TaskStatusUpdateEvent or
// *TaskArtifactUpdateEvent.
func (sw *streamWriter) write(event any) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.detached {
		return
	}
	if !sw.started {
		h := sw.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		sw.started = true
	}
	payload, err := sw.v.WireStreamEvent(event)
	if err != nil {
		log.Printf("a2a: stream: %v", err)
		return
	}
	result, err := json.Marshal(payload)
	if err != nil {
		log.Printf("a2a: stream: failed to marshal event: %v", err)
		return
	}
	data, err := json.Marshal(a2a.JSONRPCResponse{JSONRPC: jsonRPCVersion, ID: sw.id, Result: result})
	if err != nil {
		log.Printf("a2a: stream: failed to marshal response: %v", err)
		return
	}
	_, _ = fmt.Fprintf(sw.w, "data: %s\n\n", data)
	sw.flusher.Flush()
}

// streamTurn runs one streamed turn: it turns the conversation's events into
// A2A events for the caller and the task's subscribers, and records the
// result on the task.
type streamTurn struct {
	srv       *Server
	out       *streamWriter
	taskID    string
	contextID string

	// text is the text run being streamed, if any. Contiguous text chunks are
	// one artifact: the first chunk opens it, later ones extend it with
	// append, and the last carries lastChunk. A chunk is held back until the
	// next arrives, because only then is it known not to be the last.
	text *textRun
	// nextArtifact numbers artifacts within the task.
	nextArtifact int
	// artifacts accumulates what was streamed, so the finished task carries
	// its result and not just the events that already went out (#2032).
	artifacts []a2a.Artifact
	// pending records an EventPending: the turn ends waiting on something the
	// server cannot see, which is input-required, not completed.
	pending bool
}

// textRun is one text artifact being streamed.
type textRun struct {
	id      string
	started bool
	held    *string
	all     strings.Builder
}

// emit sends an event to the caller and to the task's subscribers.
func (st *streamTurn) emit(evt TaskEvent) {
	st.out.write(evt.payload())
	st.srv.publish(st.taskID, evt)
}

func (st *streamTurn) newArtifactID() string {
	st.nextArtifact++
	return fmt.Sprintf("artifact-%d", st.nextArtifact)
}

// addText streams a text chunk as part of the current text artifact.
func (st *streamTurn) addText(text string) {
	if text == "" {
		return
	}
	if st.text == nil {
		st.text = &textRun{id: st.newArtifactID()}
	}
	if st.text.held != nil {
		st.emitTextChunk(*st.text.held, false)
	}
	st.text.held = &text
	st.text.all.WriteString(text)
}

// emitTextChunk sends one chunk of the current text artifact.
func (st *streamTurn) emitTextChunk(text string, last bool) {
	run := st.text
	st.emit(TaskEvent{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
		TaskID:    st.taskID,
		ContextID: st.contextID,
		Artifact:  a2a.Artifact{ArtifactID: run.id, Parts: []a2a.Part{{Text: &text}}},
		Append:    run.started,
		LastChunk: last,
	}})
	run.started = true
}

// flushText closes the current text artifact: the held chunk goes out as the
// last one, and the whole run is kept for the task.
func (st *streamTurn) flushText() {
	run := st.text
	if run == nil {
		return
	}
	if run.held != nil {
		st.emitTextChunk(*run.held, true)
	}
	all := run.all.String()
	st.artifacts = append(st.artifacts, a2a.Artifact{ArtifactID: run.id, Parts: []a2a.Part{{Text: &all}}})
	st.text = nil
}

// addPart streams a non-text part as an artifact of its own.
func (st *streamTurn) addPart(part a2a.Part) {
	st.flushText()
	artifact := a2a.Artifact{ArtifactID: st.newArtifactID(), Parts: []a2a.Part{part}}
	st.artifacts = append(st.artifacts, artifact)
	st.emit(TaskEvent{ArtifactUpdate: &a2a.TaskArtifactUpdateEvent{
		TaskID: st.taskID, ContextID: st.contextID, Artifact: artifact, LastChunk: true,
	}})
}

// finish records the turn's outcome and sends the status that ends the
// stream.
//
// Only a completed turn stores its artifacts, as the task's result: a failed
// or interrupted one must not leave a partial answer behind as its output.
// They are stored before the state changes, so a consumer that reacts to the
// final state and immediately reads the task cannot race the write. If the
// state cannot be set — CancelTask got there first — the caller is told the
// state the task actually has, and nothing is published, because whoever set
// that state already did.
func (st *streamTurn) finish(state a2a.TaskState, msg *a2a.Message) {
	st.flushText()
	if state == a2a.TaskStateCompleted && len(st.artifacts) > 0 {
		if err := st.srv.taskStore.AddArtifacts(st.taskID, st.artifacts); err != nil {
			log.Printf("a2a: task %s: failed to add streamed artifacts: %v", st.taskID, err)
		}
	}
	if msg != nil {
		msg.MessageID, msg.ContextID, msg.TaskID = generateID(), st.contextID, st.taskID
		msg.Role = a2a.RoleAgent
	}
	if err := st.srv.taskStore.SetState(st.taskID, state, msg); err != nil {
		log.Printf("a2a: task %s: failed to set %s state: %v", st.taskID, state, err)
		st.reportActualState()
		return
	}
	st.emit(TaskEvent{StatusUpdate: &a2a.TaskStatusUpdateEvent{
		TaskID: st.taskID, ContextID: st.contextID, Status: a2a.TaskStatus{State: state, Message: msg},
	}})
}

// reportActualState tells the caller the task's stored status.
func (st *streamTurn) reportActualState() {
	task, err := st.srv.taskStore.Get(st.taskID)
	if err != nil {
		return
	}
	st.out.write(&a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status})
}

// process consumes the conversation's events until the turn ends. It watches
// ctx so a CancelTask (or Shutdown) ends the loop promptly instead of blocking
// on a channel read (and leaking the goroutine). The caller disconnecting does
// not end it: the task's lifecycle is independent of any one stream's (A2A
// 1.0 §3.5.2).
func (st *streamTurn) process(ctx context.Context, events <-chan StreamEvent) {
	for {
		select {
		case <-ctx.Done():
			// CancelTask stopped the turn: tell the caller, if still
			// there, how its task ended.
			st.flushText()
			st.reportActualState()
			return

		case evt, ok := <-events:
			if !ok {
				st.finishDone()
				return
			}
			if st.handle(evt) {
				return
			}
		}
	}
}

// finishDone ends a turn whose producer finished normally.
func (st *streamTurn) finishDone() {
	if st.pending {
		st.finish(a2a.TaskStateInputRequired, nil)
		return
	}
	st.finish(a2a.TaskStateCompleted, nil)
}

// handle processes one stream event and reports whether the turn is over.
func (st *streamTurn) handle(evt StreamEvent) (done bool) {
	if evt.Error != nil {
		errText := evt.Error.Error()
		st.finish(a2a.TaskStateFailed, &a2a.Message{Parts: []a2a.Part{{Text: &errText}}})
		return true
	}

	switch evt.Kind {
	case EventText:
		st.addText(evt.Text)

	case EventMedia:
		if evt.Media == nil {
			return false
		}
		part, err := a2a.ContentPartToA2APart(types.ContentPart{
			Type:  a2a.InferContentType(evt.Media.MIMEType),
			Media: evt.Media,
		})
		if err != nil {
			return false
		}
		st.addPart(part)

	case EventToolCall:
		// Suppressed — agent opacity. Task stays working.

	case EventClientTool:
		var msg *a2a.Message
		if evt.ClientTool != nil {
			msg = &a2a.Message{Parts: []a2a.Part{clientToolPart(*evt.ClientTool)}}
		}
		st.finish(a2a.TaskStateInputRequired, msg)
		return true

	case EventPending:
		// As on the SendMessage path: the turn will end waiting, so it must
		// not be reported completed.
		st.pending = true
		st.addText(evt.Text)

	case EventDone:
		st.finishDone()
		return true
	}
	return false
}

// handleStreamMessage processes SendStreamingMessage (0.3: message/stream).
//
// The stream opens with the Task (A2A 1.0 §3.1.2), then carries artifact and
// status updates, and ends with the status that finishes or interrupts the
// task.
func (s *Server) handleStreamMessage(call *rpcCall) {
	var params a2a.SendMessageRequest
	if !call.decodeParams(&params) {
		return
	}

	target, ok := s.resolveTarget(call, &params.Message)
	if !ok {
		return
	}
	contextID := target.contextID

	out := newStreamWriter(call)
	if out == nil {
		return
	}

	// Which half of the server owns conversations decides where the events come
	// from. Resolved before the task is claimed, so a refusal costs nothing —
	// except for tool results, which are submitted only once it is.
	startTurn, ok := s.resolveStreamTurn(call, &target, params)
	if !ok {
		return
	}

	if !s.beginTask(call, &target) {
		return
	}
	taskID := target.taskID
	task, err := s.taskStore.Get(taskID)
	if err != nil {
		call.internalError(fmt.Sprintf("failed to read task %s", taskID), err)
		return
	}
	out.write(task)

	// The turn is detached from the request: closing a stream must not affect
	// its task (A2A 1.0 §3.5.2). It keeps the request's values, as SendMessage
	// does, and CancelTask still reaches it.
	ctx, cancel := context.WithCancel(context.WithoutCancel(call.r.Context()))
	s.registerCancel(taskID, cancel)
	events := startTurn(ctx, taskID)

	st := &streamTurn{srv: s, out: out, taskID: taskID, contextID: contextID}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer s.unregisterCancel(taskID)
		st.process(ctx, events)
	}()

	select {
	case <-done:
	case <-call.r.Context().Done():
		// The caller left. Its stream ends here; the turn runs on, and its
		// result still reaches the task and its subscribers.
		out.detach()
	}
}

// handleTaskSubscribe processes SubscribeToTask (0.3: tasks/resubscribe).
//
// The stream opens with the task as it stands (A2A 1.0 §3.1.6), then carries
// the task's updates until it finishes or is interrupted. A task that has
// already finished cannot be subscribed to.
func (s *Server) handleTaskSubscribe(call *rpcCall) {
	var params a2a.SubscribeTaskRequest
	if !call.decodeParams(&params) {
		return
	}

	task := s.getTaskFor(call, params.ID)
	if task == nil {
		return
	}
	if task.Status.State.IsTerminal() {
		call.fail(a2a.ErrCodeUnsupportedOperation,
			fmt.Sprintf("Task is %s; a finished task cannot be subscribed to", task.Status.State.V03Name()))
		return
	}

	out := newStreamWriter(call)
	if out == nil {
		return
	}

	// Subscribe before reading the task again, so no update can fall between
	// the snapshot and the first event; a duplicate is harmless, a gap is not.
	ctx := call.r.Context()
	events, err := s.events.Subscribe(ctx, params.ID)
	if err != nil {
		if errors.Is(err, ErrTooManySubscribers) {
			call.fail(a2a.ErrCodeInternal, "Too many subscribers for this task")
			return
		}
		call.internalError(fmt.Sprintf("subscribe to task %s", params.ID), err)
		return
	}
	if task, err = s.taskStore.Get(params.ID); err != nil {
		call.fail(a2a.ErrCodeTaskNotFound, "Task not found")
		return
	}
	out.write(task)
	if state := task.Status.State; state.IsTerminal() || state.IsInterrupted() {
		return
	}
	relayEvents(ctx, out, events)
}

// relayEvents writes a subscription's events to out until a final event, the
// end of the subscription, or the caller going away.
func relayEvents(ctx context.Context, out *streamWriter, events <-chan TaskEvent) {
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				return
			}
			out.write(evt.payload())
			if evt.IsFinal() {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// resolveStreamTurn produces the function that starts this request's event
// stream, for either server mode. ok is false when the request has already
// been answered with an error.
//
// Tool results are submitted here, after the target's task is claimed, so
// the task is already claimed when it returns with them.
func (s *Server) resolveStreamTurn(
	call *rpcCall, target *turnTarget, params a2a.SendMessageRequest,
) (start func(ctx context.Context, taskID string) <-chan StreamEvent, ok bool) {
	toolResults := extractToolResults(params.Message.Parts)
	contextID := target.contextID

	if s.handler != nil {
		return s.statelessStreamTurn(call, contextID, params, toolResults)
	}

	conv := s.openConversation(call, contextID)
	if conv == nil {
		return nil, false
	}

	streamConv, isStreaming := conv.(StreamingConversation)
	if !isStreaming {
		call.fail(a2a.ErrCodeUnsupportedOperation, "Streaming is not supported by this agent")
		return nil, false
	}

	if len(toolResults) > 0 {
		resumable := s.claimAndSubmit(call, target, streamConv, toolResults)
		if resumable == nil {
			return nil, false
		}
		return func(ctx context.Context, _ string) <-chan StreamEvent {
			return resumable.ResumeStream(ctx)
		}, true
	}

	pkMsg, err := a2a.MessageToMessage(&params.Message)
	if err != nil {
		call.fail(a2a.ErrCodeInvalidParams, fmt.Sprintf("Invalid message: %v", err))
		return nil, false
	}

	return func(ctx context.Context, _ string) <-chan StreamEvent {
		return streamConv.Stream(ctx, pkMsg)
	}, true
}

// statelessStreamTurn is resolveStreamTurn's handler-mode half: no conversation
// to open, and tool results only when the handler asked for client tools.
func (s *Server) statelessStreamTurn(
	call *rpcCall, contextID string, params a2a.SendMessageRequest, toolResults []toolResultEntry,
) (start func(ctx context.Context, taskID string) <-chan StreamEvent, ok bool) {
	trh, resumable := s.handler.(ToolResultHandler)
	if len(toolResults) > 0 && !resumable {
		call.fail(a2a.ErrCodeUnsupportedOperation, "Client tool results are not supported by this agent")
		return nil, false
	}

	return func(ctx context.Context, taskID string) <-chan StreamEvent {
		if len(toolResults) > 0 {
			return trh.HandleToolResult(ctx, ToolResultRequest{
				ContextID: contextID,
				TaskID:    taskID,
				Results:   toToolResults(toolResults),
			})
		}
		return s.handler.Handle(ctx, MessageRequest{
			ContextID: contextID,
			TaskID:    taskID,
			Message:   params.Message,
		})
	}, true
}
