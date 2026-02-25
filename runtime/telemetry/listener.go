package telemetry

import (
	"context"
	"encoding/json"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// spanEntry tracks an in-flight span and its context.
type spanEntry struct {
	span trace.Span
	ctx  context.Context //nolint:containedctx // needed to parent child spans
}

// sessionState tracks the root span for a session.
type sessionState struct {
	span trace.Span
	ctx  context.Context //nolint:containedctx // needed to parent child spans
}

// pendingEnd buffers a span completion that arrived before the corresponding start.
// The EventBus dispatches each Publish() in a separate goroutine, so completion
// events can race ahead of start events.
type pendingEnd struct {
	errMsg string // empty means success
	attrs  []attribute.KeyValue
}

// OTelEventListener converts runtime events into OTel spans in real time.
// It implements the events.Listener function signature via its OnEvent method.
// It is safe for concurrent use and tolerates out-of-order event delivery.
type OTelEventListener struct {
	tracer trace.Tracer

	mu          sync.Mutex
	sessions    map[string]*sessionState // sessionID → root span + ctx
	inflight    map[string]*spanEntry    // "pipeline:<runID>" → span + ctx
	pendingEnds map[string]*pendingEnd   // buffered completions for out-of-order delivery
}

// NewOTelEventListener creates a listener that creates OTel spans from runtime events.
func NewOTelEventListener(tracer trace.Tracer) *OTelEventListener {
	return &OTelEventListener{
		tracer:      tracer,
		sessions:    make(map[string]*sessionState),
		inflight:    make(map[string]*spanEntry),
		pendingEnds: make(map[string]*pendingEnd),
	}
}

// StartSession creates a root span for the given session, optionally parented
// under the span context in parentCtx.
func (l *OTelEventListener) StartSession(parentCtx context.Context, sessionID string) {
	ctx, span := l.tracer.Start(parentCtx, "promptkit.session",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("session.id", sessionID)),
	)
	l.mu.Lock()
	l.sessions[sessionID] = &sessionState{span: span, ctx: ctx}
	l.mu.Unlock()
}

// EndSession ends the root span for the given session.
func (l *OTelEventListener) EndSession(sessionID string) {
	l.mu.Lock()
	ss, ok := l.sessions[sessionID]
	if ok {
		delete(l.sessions, sessionID)
	}
	l.mu.Unlock()
	if ok {
		ss.span.End()
	}
}

// OnEvent handles a single runtime event and creates/completes OTel spans accordingly.
// It is safe for concurrent use and can be passed to EventBus.SubscribeAll.
func (l *OTelEventListener) OnEvent(evt *events.Event) {
	//nolint:exhaustive // Only handling span-producing events
	switch evt.Type {
	case events.EventPipelineStarted:
		l.startPipeline(evt)
	case events.EventPipelineCompleted:
		l.completePipeline(evt)
	case events.EventPipelineFailed:
		l.failPipeline(evt)
	case events.EventProviderCallStarted:
		l.startProvider(evt)
	case events.EventProviderCallCompleted:
		l.completeProvider(evt)
	case events.EventProviderCallFailed:
		l.failProvider(evt)
	case events.EventToolCallStarted:
		l.startTool(evt)
	case events.EventToolCallCompleted:
		l.completeTool(evt)
	case events.EventToolCallFailed:
		l.failTool(evt)
	case events.EventMiddlewareStarted:
		l.startMiddleware(evt)
	case events.EventMiddlewareCompleted:
		l.completeMiddleware(evt)
	case events.EventMiddlewareFailed:
		l.failMiddleware(evt)
	case events.EventMessageCreated:
		l.handleMessage(evt)
	case events.EventWorkflowTransitioned:
		l.handleWorkflowTransition(evt)
	case events.EventWorkflowCompleted:
		l.handleWorkflowCompleted(evt)
	}
}

// sessionCtx returns the context for the session (to parent child spans).
// Falls back to context.Background() if the session is unknown.
func (l *OTelEventListener) sessionCtx(sessionID string) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ss, ok := l.sessions[sessionID]; ok {
		return ss.ctx
	}
	return context.Background()
}

// startSpan starts a span parented under the session root and stores it in inflight.
// If a completion was already buffered (out-of-order delivery), the span is
// immediately ended.
func (l *OTelEventListener) startSpan(
	sessionID, key, name string, kind trace.SpanKind, attrs ...attribute.KeyValue,
) {
	parentCtx := l.sessionCtx(sessionID)
	ctx, span := l.tracer.Start(parentCtx, name,
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrs...),
	)
	l.mu.Lock()
	pe, havePending := l.pendingEnds[key]
	if havePending {
		delete(l.pendingEnds, key)
	} else {
		l.inflight[key] = &spanEntry{span: span, ctx: ctx}
	}
	l.mu.Unlock()

	if havePending {
		span.SetAttributes(pe.attrs...)
		if pe.errMsg != "" {
			span.SetStatus(codes.Error, pe.errMsg)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}
}

// endSpan ends an inflight span and removes it from the map.
// If the span hasn't started yet (out-of-order delivery), the completion is
// buffered and will be applied when startSpan creates the span.
func (l *OTelEventListener) endSpan(key string, attrs ...attribute.KeyValue) {
	l.mu.Lock()
	entry, ok := l.inflight[key]
	if ok {
		delete(l.inflight, key)
	} else {
		l.pendingEnds[key] = &pendingEnd{attrs: attrs}
	}
	l.mu.Unlock()
	if !ok {
		return
	}
	entry.span.SetAttributes(attrs...)
	entry.span.SetStatus(codes.Ok, "")
	entry.span.End()
}

// failSpan ends an inflight span with an error status.
// If the span hasn't started yet (out-of-order delivery), the failure is
// buffered and will be applied when startSpan creates the span.
func (l *OTelEventListener) failSpan(key, errMsg string, attrs ...attribute.KeyValue) {
	l.mu.Lock()
	entry, ok := l.inflight[key]
	if ok {
		delete(l.inflight, key)
	} else {
		l.pendingEnds[key] = &pendingEnd{errMsg: errMsg, attrs: attrs}
	}
	l.mu.Unlock()
	if !ok {
		return
	}
	entry.span.SetAttributes(attrs...)
	entry.span.SetStatus(codes.Error, errMsg)
	entry.span.End()
}

// asPtr extracts event data as a pointer, handling both value and pointer types.
// The emitter may pass either T or *T depending on the event.
func asPtr[T any](data any) (*T, bool) {
	if p, ok := data.(*T); ok {
		return p, true
	}
	if v, ok := data.(T); ok {
		return &v, true
	}
	return nil, false
}

// --- Pipeline ---

func (l *OTelEventListener) startPipeline(evt *events.Event) {
	l.startSpan(evt.SessionID, "pipeline:"+evt.RunID, "promptkit.pipeline",
		trace.SpanKindInternal,
		attribute.String("run.id", evt.RunID),
	)
}

func (l *OTelEventListener) completePipeline(evt *events.Event) {
	data, ok := asPtr[events.PipelineCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("pipeline:"+evt.RunID,
		attribute.Int64("pipeline.duration_ms", data.Duration.Milliseconds()),
		attribute.Float64("pipeline.total_cost", data.TotalCost),
		attribute.Int("gen_ai.usage.input_tokens", data.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", data.OutputTokens),
	)
}

func (l *OTelEventListener) failPipeline(evt *events.Event) {
	data, ok := asPtr[events.PipelineFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("pipeline:"+evt.RunID, data.Error.Error(),
		attribute.Int64("pipeline.duration_ms", data.Duration.Milliseconds()),
	)
}

// --- Provider ---

func (l *OTelEventListener) startProvider(evt *events.Event) {
	data, ok := asPtr[events.ProviderCallStartedData](evt.Data)
	if !ok {
		return
	}
	l.startSpan(evt.SessionID, "provider:"+evt.RunID, "promptkit.provider."+data.Provider,
		trace.SpanKindClient,
		attribute.String("gen_ai.system", data.Provider),
		attribute.String("gen_ai.request.model", data.Model),
		attribute.Int("message.count", data.MessageCount),
		attribute.Int("tool.count", data.ToolCount),
	)
}

func (l *OTelEventListener) completeProvider(evt *events.Event) {
	data, ok := asPtr[events.ProviderCallCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("provider:"+evt.RunID,
		attribute.Int64("provider.duration_ms", data.Duration.Milliseconds()),
		attribute.Int("gen_ai.usage.input_tokens", data.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", data.OutputTokens),
		attribute.String("gen_ai.response.finish_reason", data.FinishReason),
		attribute.Float64("provider.cost", data.Cost),
	)
}

func (l *OTelEventListener) failProvider(evt *events.Event) {
	data, ok := asPtr[events.ProviderCallFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("provider:"+evt.RunID, data.Error.Error(),
		attribute.Int64("provider.duration_ms", data.Duration.Milliseconds()),
	)
}

// --- Tool ---

func (l *OTelEventListener) startTool(evt *events.Event) {
	data, ok := asPtr[events.ToolCallStartedData](evt.Data)
	if !ok {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("tool.name", data.ToolName),
		attribute.String("tool.call_id", data.CallID),
	}
	if data.Args != nil {
		if argsJSON, err := json.Marshal(data.Args); err == nil {
			attrs = append(attrs, attribute.String("tool.args", string(argsJSON)))
		}
	}
	l.startSpan(evt.SessionID, "tool:"+data.CallID, "promptkit.tool."+data.ToolName,
		trace.SpanKindInternal,
		attrs...,
	)
}

func (l *OTelEventListener) completeTool(evt *events.Event) {
	data, ok := asPtr[events.ToolCallCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("tool:"+data.CallID,
		attribute.Int64("tool.duration_ms", data.Duration.Milliseconds()),
		attribute.String("tool.status", data.Status),
	)
}

func (l *OTelEventListener) failTool(evt *events.Event) {
	data, ok := asPtr[events.ToolCallFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("tool:"+data.CallID, data.Error.Error(),
		attribute.Int64("tool.duration_ms", data.Duration.Milliseconds()),
	)
}

// --- Middleware ---

func (l *OTelEventListener) startMiddleware(evt *events.Event) {
	data, ok := asPtr[events.MiddlewareStartedData](evt.Data)
	if !ok {
		return
	}
	l.startSpan(evt.SessionID, "middleware:"+data.Name, "promptkit.middleware."+data.Name,
		trace.SpanKindInternal,
		attribute.String("middleware.name", data.Name),
		attribute.Int("middleware.index", data.Index),
	)
}

func (l *OTelEventListener) completeMiddleware(evt *events.Event) {
	data, ok := asPtr[events.MiddlewareCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("middleware:"+data.Name,
		attribute.Int64("middleware.duration_ms", data.Duration.Milliseconds()),
	)
}

func (l *OTelEventListener) failMiddleware(evt *events.Event) {
	data, ok := asPtr[events.MiddlewareFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("middleware:"+data.Name, data.Error.Error(),
		attribute.Int64("middleware.duration_ms", data.Duration.Milliseconds()),
	)
}

// --- Message ---

func (l *OTelEventListener) handleMessage(evt *events.Event) {
	data, ok := asPtr[events.MessageCreatedData](evt.Data)
	if !ok {
		return
	}

	evtAttrs := []attribute.KeyValue{
		attribute.String("gen_ai.message.content", data.Content),
	}
	if len(data.ToolCalls) > 0 {
		if b, err := json.Marshal(data.ToolCalls); err == nil {
			evtAttrs = append(evtAttrs, attribute.String("gen_ai.tool_calls", string(b)))
		}
	}
	if data.ToolResult != nil {
		if b, err := json.Marshal(data.ToolResult); err == nil {
			evtAttrs = append(evtAttrs, attribute.String("gen_ai.tool_result", string(b)))
		}
	}

	eventName := "gen_ai." + data.Role + ".message"

	// Attach event to active provider span if present, otherwise session root.
	l.mu.Lock()
	entry, ok := l.inflight["provider:"+evt.RunID]
	if ok {
		entry.span.AddEvent(eventName, trace.WithAttributes(evtAttrs...))
	} else if ss, ok := l.sessions[evt.SessionID]; ok {
		ss.span.AddEvent(eventName, trace.WithAttributes(evtAttrs...))
	}
	l.mu.Unlock()
}

// --- Workflow ---

func (l *OTelEventListener) handleWorkflowTransition(evt *events.Event) {
	data, ok := asPtr[events.WorkflowTransitionedData](evt.Data)
	if !ok {
		return
	}
	parentCtx := l.sessionCtx(evt.SessionID)
	_, span := l.tracer.Start(parentCtx, "promptkit.workflow.transition",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("workflow.from_state", data.FromState),
			attribute.String("workflow.to_state", data.ToState),
			attribute.String("workflow.event", data.Event),
			attribute.String("workflow.prompt_task", data.PromptTask),
		),
	)
	span.End()
}

func (l *OTelEventListener) handleWorkflowCompleted(evt *events.Event) {
	data, ok := asPtr[events.WorkflowCompletedData](evt.Data)
	if !ok {
		return
	}
	parentCtx := l.sessionCtx(evt.SessionID)
	_, span := l.tracer.Start(parentCtx, "promptkit.workflow.completed",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("workflow.final_state", data.FinalState),
			attribute.Int("workflow.transition_count", data.TransitionCount),
		),
	)
	span.SetStatus(codes.Ok, "")
	span.End()
}
