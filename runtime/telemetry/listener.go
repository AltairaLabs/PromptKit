package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// staleEntryTimeout is the maximum age of an inflight or pendingEnd entry
// before it is considered stale and cleaned up to prevent unbounded map growth.
const staleEntryTimeout = 5 * time.Minute

// cleanupInterval is how often the listener checks for stale entries.
const cleanupInterval = 1 * time.Minute

// spanEntry tracks an in-flight span and its context.
type spanEntry struct {
	span      trace.Span
	ctx       context.Context //nolint:containedctx // needed to parent child spans
	createdAt time.Time
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
	errMsg    string // empty means success
	attrs     []attribute.KeyValue
	createdAt time.Time
}

// OTelEventListener converts runtime events into OTel spans in real time.
// It implements the events.Listener function signature via its OnEvent method.
// It is safe for concurrent use and tolerates out-of-order event delivery.
// Call Close when the listener is no longer needed to stop the cleanup goroutine.
type OTelEventListener struct {
	tracer trace.Tracer

	mu          sync.Mutex
	sessions    map[string]*sessionState // sessionID → root span + ctx
	inflight    map[string]*spanEntry    // "pipeline:<runID>" → span + ctx
	pendingEnds map[string]*pendingEnd   // buffered completions for out-of-order delivery

	stopCleanup chan struct{}
}

// NewOTelEventListener creates a listener that creates OTel spans from runtime events.
// A background goroutine periodically cleans up stale entries to prevent unbounded
// map growth. Call Close when the listener is no longer needed.
func NewOTelEventListener(tracer trace.Tracer) *OTelEventListener {
	l := &OTelEventListener{
		tracer:      tracer,
		sessions:    make(map[string]*sessionState),
		inflight:    make(map[string]*spanEntry),
		pendingEnds: make(map[string]*pendingEnd),
		stopCleanup: make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Close stops the background cleanup goroutine.
func (l *OTelEventListener) Close() {
	select {
	case <-l.stopCleanup:
		// Already closed.
	default:
		close(l.stopCleanup)
	}
}

// cleanupLoop periodically removes stale inflight and pendingEnd entries.
func (l *OTelEventListener) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCleanup:
			return
		case <-ticker.C:
			l.evictStale()
		}
	}
}

// evictStale removes inflight and pendingEnd entries older than staleEntryTimeout.
func (l *OTelEventListener) evictStale() {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, entry := range l.inflight {
		if now.Sub(entry.createdAt) > staleEntryTimeout {
			entry.span.SetStatus(codes.Error, "stale span evicted")
			entry.span.End()
			delete(l.inflight, key)
		}
	}
	for key, pe := range l.pendingEnds {
		if now.Sub(pe.createdAt) > staleEntryTimeout {
			delete(l.pendingEnds, key)
		}
	}
}

// AgentInfo holds optional agent identity metadata for session spans.
type AgentInfo struct {
	Name string // Agent/pack name (maps to gen_ai.agent.name)
	ID   string // Agent/pack ID (maps to gen_ai.agent.id)
}

// StartSession creates a root span for the given session, optionally parented
// under the span context in parentCtx.
// It is idempotent: if a session already exists for the given ID, the previous
// session span is ended before creating a new one. This allows callers to call
// StartSession on every Send/Stream with a fresh parent context.
// The optional agent parameter provides agent identity attributes for the span.
func (l *OTelEventListener) StartSession(parentCtx context.Context, sessionID string, agent ...AgentInfo) {
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "invoke_agent"),
		attribute.String("gen_ai.system", "promptkit"),
		attribute.String("gen_ai.conversation.id", sessionID),
	}
	if len(agent) > 0 {
		if agent[0].Name != "" {
			attrs = append(attrs, attribute.String("gen_ai.agent.name", agent[0].Name))
		}
		if agent[0].ID != "" {
			attrs = append(attrs, attribute.String("gen_ai.agent.id", agent[0].ID))
		}
	}
	ctx, span := l.tracer.Start(parentCtx, "promptkit invoke_agent",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
	l.mu.Lock()
	prev, hadPrev := l.sessions[sessionID]
	l.sessions[sessionID] = &sessionState{span: span, ctx: ctx}
	l.mu.Unlock()

	// End previous session span outside the lock to avoid holding it during span.End().
	if hadPrev {
		prev.span.End()
	}
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
	case events.EventValidationStarted:
		l.startValidation(evt)
	case events.EventValidationPassed:
		l.completeValidation(evt)
	case events.EventValidationFailed:
		l.failValidation(evt)
	case events.EventEvalCompleted:
		l.handleEvalCompleted(evt)
	case events.EventEvalFailed:
		l.handleEvalFailed(evt)
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

// parentCtxForRun returns the context of the inflight pipeline span for the
// given runID, falling back to the session root span context if no pipeline
// span is active. This ensures provider, tool, and middleware spans are nested
// under their pipeline span in trace viewers.
func (l *OTelEventListener) parentCtxForRun(sessionID, runID string) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, ok := l.inflight["pipeline:"+runID]; ok {
		return entry.ctx
	}
	if ss, ok := l.sessions[sessionID]; ok {
		return ss.ctx
	}
	return context.Background()
}

// startSpan starts a span parented under the given parent context and stores it in inflight.
// If a completion was already buffered (out-of-order delivery), the span is
// immediately ended.
func (l *OTelEventListener) startSpan(
	parentCtx context.Context, key, name string, kind trace.SpanKind, attrs ...attribute.KeyValue,
) {
	ctx, span := l.tracer.Start(parentCtx, name,
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrs...),
	)
	l.mu.Lock()
	pe, havePending := l.pendingEnds[key]
	if havePending {
		delete(l.pendingEnds, key)
	} else {
		l.inflight[key] = &spanEntry{span: span, ctx: ctx, createdAt: time.Now()}
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
		l.pendingEnds[key] = &pendingEnd{attrs: attrs, createdAt: time.Now()}
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
func (l *OTelEventListener) failSpan(key, errMsg string) {
	l.mu.Lock()
	entry, ok := l.inflight[key]
	if ok {
		delete(l.inflight, key)
	} else {
		l.pendingEnds[key] = &pendingEnd{errMsg: errMsg, createdAt: time.Now()}
	}
	l.mu.Unlock()
	if !ok {
		return
	}
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
	l.startSpan(l.sessionCtx(evt.SessionID), "pipeline:"+evt.RunID, "promptkit.pipeline",
		trace.SpanKindInternal,
		attribute.String("promptkit.run.id", evt.RunID),
	)
}

func (l *OTelEventListener) completePipeline(evt *events.Event) {
	data, ok := asPtr[events.PipelineCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("pipeline:"+evt.RunID,
		attribute.Float64("promptkit.pipeline.cost", data.TotalCost),
		attribute.Int("gen_ai.usage.input_tokens", data.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", data.OutputTokens),
	)
}

func (l *OTelEventListener) failPipeline(evt *events.Event) {
	data, ok := asPtr[events.PipelineFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("pipeline:"+evt.RunID, data.Error.Error())
}

// --- Provider ---

func (l *OTelEventListener) startProvider(evt *events.Event) {
	data, ok := asPtr[events.ProviderCallStartedData](evt.Data)
	if !ok {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.system", data.Provider),
		attribute.String("gen_ai.request.model", data.Model),
		attribute.Int("promptkit.message.count", data.MessageCount),
		attribute.Int("promptkit.tool.count", data.ToolCount),
	}
	attrs = append(attrs, labelsToAttributes(data.Labels)...)
	l.startSpan(l.parentCtxForRun(evt.SessionID, evt.RunID), "provider:"+evt.RunID,
		data.Provider+" chat",
		trace.SpanKindClient,
		attrs...,
	)
}

func (l *OTelEventListener) completeProvider(evt *events.Event) {
	data, ok := asPtr[events.ProviderCallCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("provider:"+evt.RunID,
		attribute.Int("gen_ai.usage.input_tokens", data.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", data.OutputTokens),
		attribute.String("gen_ai.response.finish_reason", data.FinishReason),
		attribute.Float64("promptkit.provider.cost", data.Cost),
	)
}

func (l *OTelEventListener) failProvider(evt *events.Event) {
	data, ok := asPtr[events.ProviderCallFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("provider:"+evt.RunID, data.Error.Error())
}

// --- Tool ---

func (l *OTelEventListener) startTool(evt *events.Event) {
	data, ok := asPtr[events.ToolCallStartedData](evt.Data)
	if !ok {
		return
	}
	toolType := "function"
	if strings.HasPrefix(data.ToolName, "mcp__") {
		toolType = "extension"
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.name", data.ToolName),
		attribute.String("gen_ai.tool.call.id", data.CallID),
		attribute.String("gen_ai.tool.type", toolType),
	}
	if data.Args != nil {
		if argsJSON, err := json.Marshal(data.Args); err == nil {
			attrs = append(attrs, attribute.String("gen_ai.tool.call.arguments", string(argsJSON)))
		}
	}
	attrs = append(attrs, labelsToAttributes(data.Labels)...)
	l.startSpan(l.parentCtxForRun(evt.SessionID, evt.RunID), "tool:"+data.CallID, "execute_tool",
		trace.SpanKindInternal,
		attrs...,
	)
}

func (l *OTelEventListener) completeTool(evt *events.Event) {
	data, ok := asPtr[events.ToolCallCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("tool:" + data.CallID)
}

func (l *OTelEventListener) failTool(evt *events.Event) {
	data, ok := asPtr[events.ToolCallFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("tool:"+data.CallID, data.Error.Error())
}

// --- Middleware ---

func (l *OTelEventListener) startMiddleware(evt *events.Event) {
	data, ok := asPtr[events.MiddlewareStartedData](evt.Data)
	if !ok {
		return
	}
	l.startSpan(l.parentCtxForRun(evt.SessionID, evt.RunID), "middleware:"+data.Name, "promptkit.middleware."+data.Name,
		trace.SpanKindInternal,
		attribute.String("promptkit.middleware.name", data.Name),
		attribute.Int("promptkit.middleware.index", data.Index),
	)
}

func (l *OTelEventListener) completeMiddleware(evt *events.Event) {
	data, ok := asPtr[events.MiddlewareCompletedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("middleware:" + data.Name)
}

func (l *OTelEventListener) failMiddleware(evt *events.Event) {
	data, ok := asPtr[events.MiddlewareFailedData](evt.Data)
	if !ok {
		return
	}
	l.failSpan("middleware:"+data.Name, data.Error.Error())
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
			attribute.String("promptkit.workflow.from_state", data.FromState),
			attribute.String("promptkit.workflow.to_state", data.ToState),
			attribute.String("promptkit.workflow.event", data.Event),
			attribute.String("promptkit.workflow.prompt_task", data.PromptTask),
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
			attribute.String("promptkit.workflow.final_state", data.FinalState),
			attribute.Int("promptkit.workflow.transition_count", data.TransitionCount),
		),
	)
	span.SetStatus(codes.Ok, "")
	span.End()
}

// --- Validation (guardrails) ---

func (l *OTelEventListener) startValidation(evt *events.Event) {
	data, ok := asPtr[events.ValidationStartedData](evt.Data)
	if !ok {
		return
	}
	l.startSpan(l.parentCtxForRun(evt.SessionID, evt.RunID), "validation:"+data.ValidatorName,
		"promptkit.eval."+data.ValidatorName,
		trace.SpanKindInternal,
		attribute.String("gen_ai.evaluation.name", data.ValidatorName),
		attribute.String("promptkit.eval.type", data.ValidatorType),
		attribute.Bool("promptkit.guardrail", true),
	)
}

func (l *OTelEventListener) completeValidation(evt *events.Event) {
	data, ok := asPtr[events.ValidationPassedData](evt.Data)
	if !ok {
		return
	}
	l.endSpan("validation:"+data.ValidatorName,
		attribute.Float64("gen_ai.evaluation.score", 1.0),
	)
}

func (l *OTelEventListener) failValidation(evt *events.Event) {
	data, ok := asPtr[events.ValidationFailedData](evt.Data)
	if !ok {
		return
	}
	explanation := ""
	if data.Error != nil {
		explanation = data.Error.Error()
	}
	if len(data.Violations) > 0 && explanation == "" {
		explanation = strings.Join(data.Violations, "; ")
	}
	l.endSpan("validation:"+data.ValidatorName,
		attribute.Float64("gen_ai.evaluation.score", 0.0),
		attribute.String("gen_ai.evaluation.explanation", explanation),
	)
}

// --- Evals ---

func (l *OTelEventListener) handleEvalCompleted(evt *events.Event) {
	data, ok := asPtr[events.EvalCompletedData](evt.Data)
	if !ok {
		return
	}
	l.emitEvalSpan(evt, data)
}

func (l *OTelEventListener) handleEvalFailed(evt *events.Event) {
	data, ok := asPtr[events.EvalFailedData](evt.Data)
	if !ok {
		return
	}
	l.emitEvalSpan(evt, data)
}

func (l *OTelEventListener) emitEvalSpan(evt *events.Event, data *events.EvalEventData) {
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.evaluation.name", data.EvalID),
		attribute.String("promptkit.eval.type", data.EvalType),
		attribute.Bool("promptkit.guardrail", false),
	}
	if data.Score != nil {
		attrs = append(attrs, attribute.Float64("gen_ai.evaluation.score", *data.Score))
	}
	if data.Explanation != "" {
		attrs = append(attrs, attribute.String("gen_ai.evaluation.explanation", data.Explanation))
	}

	parentCtx := l.parentCtxForRun(evt.SessionID, evt.RunID)
	_, span := l.tracer.Start(parentCtx, "promptkit.eval."+data.EvalID,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)

	if data.Passed {
		span.SetStatus(codes.Ok, "")
	} else {
		errMsg := data.Error
		if errMsg == "" && data.Explanation != "" {
			errMsg = data.Explanation
		}
		if errMsg == "" {
			errMsg = "eval failed"
		}
		span.SetStatus(codes.Error, errMsg)
	}
	span.End()
}

// labelsToAttributes converts a map of string labels to OTel attribute.KeyValues
// with a "promptkit.label." prefix. Returns nil for empty/nil maps.
func labelsToAttributes(labels map[string]string) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		attrs = append(attrs, attribute.String("promptkit.label."+k, v))
	}
	return attrs
}
