// Package telemetry provides OpenTelemetry export for session recordings.
// This enables exporting session events as distributed traces to observability platforms.
package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// Exporter exports session events to an observability backend.
type Exporter interface {
	// Export sends events to the backend.
	Export(ctx context.Context, spans []*Span) error

	// Shutdown performs cleanup and flushes any pending data.
	Shutdown(ctx context.Context) error
}

// Span represents a trace span in OpenTelemetry format.
type Span struct {
	// TraceID is the unique identifier for the trace (16 bytes, hex-encoded).
	TraceID string `json:"traceId"`
	// SpanID is the unique identifier for this span (8 bytes, hex-encoded).
	SpanID string `json:"spanId"`
	// ParentSpanID is the ID of the parent span (empty for root spans).
	ParentSpanID string `json:"parentSpanId,omitempty"`
	// Name is the operation name.
	Name string `json:"name"`
	// Kind is the span kind (client, server, producer, consumer, internal).
	Kind SpanKind `json:"kind"`
	// StartTime is when the span started.
	StartTime time.Time `json:"startTimeUnixNano"`
	// EndTime is when the span ended.
	EndTime time.Time `json:"endTimeUnixNano"`
	// Attributes are key-value pairs associated with the span.
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	// Status is the span status.
	Status *SpanStatus `json:"status,omitempty"`
	// Events are timestamped events within the span.
	Events []*SpanEvent `json:"events,omitempty"`
}

// SpanKind represents the type of span.
type SpanKind int

// Span kinds.
const (
	SpanKindUnspecified SpanKind = 0
	SpanKindInternal    SpanKind = 1
	SpanKindServer      SpanKind = 2
	SpanKindClient      SpanKind = 3
	SpanKindProducer    SpanKind = 4
	SpanKindConsumer    SpanKind = 5
)

// SpanStatus represents the status of a span.
type SpanStatus struct {
	// Code is the status code (0=Unset, 1=Ok, 2=Error).
	Code StatusCode `json:"code"`
	// Message is the status message.
	Message string `json:"message,omitempty"`
}

// StatusCode represents the status of a span.
type StatusCode int

// Status codes.
const (
	StatusCodeUnset StatusCode = 0
	StatusCodeOk    StatusCode = 1
	StatusCodeError StatusCode = 2
)

// SpanEvent represents an event within a span.
type SpanEvent struct {
	// Name is the event name.
	Name string `json:"name"`
	// Time is when the event occurred.
	Time time.Time `json:"timeUnixNano"`
	// Attributes are key-value pairs associated with the event.
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// Resource represents the entity producing telemetry.
type Resource struct {
	// Attributes are key-value pairs describing the resource.
	Attributes map[string]interface{} `json:"attributes"`
}

// DefaultResource returns a default resource for PromptKit.
func DefaultResource() *Resource {
	return &Resource{
		Attributes: map[string]interface{}{
			"service.name":    "promptkit",
			"service.version": "1.0.0",
			"telemetry.sdk":   "promptkit-telemetry",
		},
	}
}

// ResourceWithPackID returns a default resource with the pack.id attribute set.
func ResourceWithPackID(packID string) *Resource {
	r := DefaultResource()
	r.Attributes["pack.id"] = packID
	return r
}

// EventConverter converts runtime events to OTLP spans.
type EventConverter struct {
	// Resource is the resource to attach to spans.
	Resource *Resource
}

// NewEventConverter creates a new event converter.
func NewEventConverter(resource *Resource) *EventConverter {
	if resource == nil {
		resource = DefaultResource()
	}
	return &EventConverter{Resource: resource}
}

// ConvertSession converts a session's events to spans.
// The session becomes the root span, with pipeline/middleware/provider calls as child spans.
func (c *EventConverter) ConvertSession(
	sessionID string, sessionEvents []events.Event,
) ([]*Span, error) {
	if len(sessionEvents) == 0 {
		return nil, nil
	}
	traceID := generateTraceID(sessionID)
	return c.buildTrace(sessionID, sessionEvents, traceID, "")
}

// convertEvent converts a single event to a span or updates an existing span.
func (c *EventConverter) convertEvent(
	traceID, parentSpanID string, evt *events.Event, spanStack map[string]*Span,
) *Span {
	//nolint:exhaustive // Only handling span-producing events, others are ignored via default
	switch evt.Type {
	case events.EventPipelineStarted:
		return c.createPipelineSpan(traceID, parentSpanID, evt, spanStack)
	case events.EventPipelineCompleted, events.EventPipelineFailed:
		return c.completePipelineSpan(evt, spanStack)
	case events.EventProviderCallStarted:
		return c.createProviderSpan(traceID, parentSpanID, evt, spanStack)
	case events.EventProviderCallCompleted, events.EventProviderCallFailed:
		return c.completeProviderSpan(evt, spanStack)
	case events.EventToolCallStarted:
		return c.createToolSpan(traceID, parentSpanID, evt, spanStack)
	case events.EventToolCallCompleted, events.EventToolCallFailed:
		return c.completeToolSpan(evt, spanStack)
	case events.EventMiddlewareStarted:
		return c.createMiddlewareSpan(traceID, parentSpanID, evt, spanStack)
	case events.EventMiddlewareCompleted, events.EventMiddlewareFailed:
		return c.completeMiddlewareSpan(evt, spanStack)
	case events.EventMessageCreated:
		c.handleMessageCreated(evt, spanStack)
		return nil
	case events.EventWorkflowTransitioned:
		return c.createWorkflowTransitionSpan(traceID, parentSpanID, evt)
	case events.EventWorkflowCompleted:
		return c.createWorkflowCompletedSpan(traceID, parentSpanID, evt)
	default:
		return nil
	}
}

func (c *EventConverter) createPipelineSpan(
	traceID, parentSpanID string, evt *events.Event, spanStack map[string]*Span,
) *Span {
	spanID := generateSpanID(evt.RunID + ":pipeline")
	span := &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         "pipeline",
		Kind:         SpanKindInternal,
		StartTime:    evt.Timestamp,
		EndTime:      evt.Timestamp, // Updated on completion
		Attributes: map[string]interface{}{
			"run.id": evt.RunID,
		},
	}
	spanStack["pipeline:"+evt.RunID] = span
	return nil // Don't return until completed
}

func (c *EventConverter) completePipelineSpan(evt *events.Event, spanStack map[string]*Span) *Span {
	key := "pipeline:" + evt.RunID
	span, ok := spanStack[key]
	if !ok {
		return nil
	}
	delete(spanStack, key)

	span.EndTime = evt.Timestamp

	switch data := evt.Data.(type) {
	case *events.PipelineCompletedData:
		span.Attributes["pipeline.duration_ms"] = data.Duration.Milliseconds()
		span.Attributes["pipeline.total_cost"] = data.TotalCost
		span.Attributes["pipeline.input_tokens"] = data.InputTokens
		span.Attributes["pipeline.output_tokens"] = data.OutputTokens
		span.Status = &SpanStatus{Code: StatusCodeOk}
	case *events.PipelineFailedData:
		span.Attributes["pipeline.duration_ms"] = data.Duration.Milliseconds()
		span.Status = &SpanStatus{
			Code:    StatusCodeError,
			Message: data.Error.Error(),
		}
	}

	return span
}

func (c *EventConverter) createProviderSpan(
	traceID, parentSpanID string, evt *events.Event, spanStack map[string]*Span,
) *Span {
	data, ok := evt.Data.(*events.ProviderCallStartedData)
	if !ok {
		return nil
	}

	spanID := generateSpanID(evt.RunID + ":provider:" + data.Provider)
	span := &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         "provider." + data.Provider,
		Kind:         SpanKindClient,
		StartTime:    evt.Timestamp,
		EndTime:      evt.Timestamp,
		Attributes: map[string]interface{}{
			"provider.name":    data.Provider,
			"provider.model":   data.Model,
			"message.count":    data.MessageCount,
			"tool.count":       data.ToolCount,
			"gen_ai.system":    data.Provider,
			"gen_ai.operation": "chat",
		},
	}
	spanStack["provider:"+evt.RunID] = span
	return nil
}

func (c *EventConverter) completeProviderSpan(evt *events.Event, spanStack map[string]*Span) *Span {
	key := "provider:" + evt.RunID
	span, ok := spanStack[key]
	if !ok {
		return nil
	}
	delete(spanStack, key)

	span.EndTime = evt.Timestamp

	switch data := evt.Data.(type) {
	case *events.ProviderCallCompletedData:
		span.Attributes["provider.duration_ms"] = data.Duration.Milliseconds()
		span.Attributes["gen_ai.usage.input_tokens"] = data.InputTokens
		span.Attributes["gen_ai.usage.output_tokens"] = data.OutputTokens
		span.Attributes["gen_ai.response.finish_reason"] = data.FinishReason
		span.Attributes["provider.cost"] = data.Cost
		span.Status = &SpanStatus{Code: StatusCodeOk}
	case *events.ProviderCallFailedData:
		span.Attributes["provider.duration_ms"] = data.Duration.Milliseconds()
		span.Status = &SpanStatus{
			Code:    StatusCodeError,
			Message: data.Error.Error(),
		}
	}

	return span
}

func (c *EventConverter) createToolSpan(
	traceID, parentSpanID string, evt *events.Event, spanStack map[string]*Span,
) *Span {
	data, ok := evt.Data.(*events.ToolCallStartedData)
	if !ok {
		return nil
	}

	spanID := generateSpanID(evt.RunID + ":tool:" + data.CallID)
	span := &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         "tool." + data.ToolName,
		Kind:         SpanKindInternal,
		StartTime:    evt.Timestamp,
		EndTime:      evt.Timestamp,
		Attributes: map[string]interface{}{
			"tool.name":    data.ToolName,
			"tool.call_id": data.CallID,
		},
	}
	if data.Args != nil {
		argsJSON, err := json.Marshal(data.Args)
		if err == nil {
			span.Attributes["tool.args"] = string(argsJSON)
		}
	}
	spanStack["tool:"+data.CallID] = span
	return nil
}

func (c *EventConverter) completeToolSpan(evt *events.Event, spanStack map[string]*Span) *Span {
	var callID string
	switch data := evt.Data.(type) {
	case *events.ToolCallCompletedData:
		callID = data.CallID
	case *events.ToolCallFailedData:
		callID = data.CallID
	default:
		return nil
	}

	key := "tool:" + callID
	span, ok := spanStack[key]
	if !ok {
		return nil
	}
	delete(spanStack, key)

	span.EndTime = evt.Timestamp

	switch data := evt.Data.(type) {
	case *events.ToolCallCompletedData:
		span.Attributes["tool.duration_ms"] = data.Duration.Milliseconds()
		span.Attributes["tool.status"] = data.Status
		span.Status = &SpanStatus{Code: StatusCodeOk}
	case *events.ToolCallFailedData:
		span.Attributes["tool.duration_ms"] = data.Duration.Milliseconds()
		span.Status = &SpanStatus{
			Code:    StatusCodeError,
			Message: data.Error.Error(),
		}
	}

	return span
}

func (c *EventConverter) createMiddlewareSpan(
	traceID, parentSpanID string, evt *events.Event, spanStack map[string]*Span,
) *Span {
	data, ok := evt.Data.(*events.MiddlewareStartedData)
	if !ok {
		return nil
	}

	spanID := generateSpanID(evt.RunID + ":middleware:" + data.Name)
	span := &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         "middleware." + data.Name,
		Kind:         SpanKindInternal,
		StartTime:    evt.Timestamp,
		EndTime:      evt.Timestamp,
		Attributes: map[string]interface{}{
			"middleware.name":  data.Name,
			"middleware.index": data.Index,
		},
	}
	spanStack["middleware:"+data.Name] = span
	return nil
}

func (c *EventConverter) completeMiddlewareSpan(evt *events.Event, spanStack map[string]*Span) *Span {
	var name string
	switch data := evt.Data.(type) {
	case *events.MiddlewareCompletedData:
		name = data.Name
	case *events.MiddlewareFailedData:
		name = data.Name
	default:
		return nil
	}

	key := "middleware:" + name
	span, ok := spanStack[key]
	if !ok {
		return nil
	}
	delete(spanStack, key)

	span.EndTime = evt.Timestamp

	switch data := evt.Data.(type) {
	case *events.MiddlewareCompletedData:
		span.Attributes["middleware.duration_ms"] = data.Duration.Milliseconds()
		span.Status = &SpanStatus{Code: StatusCodeOk}
	case *events.MiddlewareFailedData:
		span.Attributes["middleware.duration_ms"] = data.Duration.Milliseconds()
		span.Status = &SpanStatus{
			Code:    StatusCodeError,
			Message: data.Error.Error(),
		}
	}

	return span
}

func (c *EventConverter) handleMessageCreated(evt *events.Event, spanStack map[string]*Span) {
	data, ok := evt.Data.(*events.MessageCreatedData)
	if !ok {
		return
	}

	// Find the active provider span for this run, fall back to any provider span
	targetSpan := spanStack["provider:"+evt.RunID]

	spanEvent := &SpanEvent{
		Name: "gen_ai." + data.Role + ".message",
		Time: evt.Timestamp,
		Attributes: map[string]interface{}{
			"gen_ai.message.content": data.Content,
		},
	}

	if len(data.ToolCalls) > 0 {
		toolCallsJSON, err := json.Marshal(data.ToolCalls)
		if err == nil {
			spanEvent.Attributes["gen_ai.tool_calls"] = string(toolCallsJSON)
		}
	}

	if data.ToolResult != nil {
		toolResultJSON, err := json.Marshal(data.ToolResult)
		if err == nil {
			spanEvent.Attributes["gen_ai.tool_result"] = string(toolResultJSON)
		}
	}

	if targetSpan != nil {
		targetSpan.Events = append(targetSpan.Events, spanEvent)
	}
	// If no provider span exists, the event is stored on the root span fallback.
	// The root span is tracked separately in ConvertSession, so we store pending
	// events on a special key.
	if targetSpan == nil {
		if root, ok := spanStack["root"]; ok {
			root.Events = append(root.Events, spanEvent)
		}
	}
}

func (c *EventConverter) createWorkflowTransitionSpan(
	traceID, parentSpanID string, evt *events.Event,
) *Span {
	data, ok := evt.Data.(*events.WorkflowTransitionedData)
	if !ok {
		return nil
	}

	spanID := generateSpanID(evt.RunID + ":workflow:transition:" + data.FromState + ":" + data.ToState)
	return &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         "workflow.transition",
		Kind:         SpanKindInternal,
		StartTime:    evt.Timestamp,
		EndTime:      evt.Timestamp,
		Attributes: map[string]interface{}{
			"workflow.from_state":  data.FromState,
			"workflow.to_state":    data.ToState,
			"workflow.event":       data.Event,
			"workflow.prompt_task": data.PromptTask,
		},
	}
}

func (c *EventConverter) createWorkflowCompletedSpan(
	traceID, parentSpanID string, evt *events.Event,
) *Span {
	data, ok := evt.Data.(*events.WorkflowCompletedData)
	if !ok {
		return nil
	}

	spanID := generateSpanID(evt.RunID + ":workflow:completed:" + data.FinalState)
	return &Span{
		TraceID:      traceID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         "workflow.completed",
		Kind:         SpanKindInternal,
		StartTime:    evt.Timestamp,
		EndTime:      evt.Timestamp,
		Attributes: map[string]interface{}{
			"workflow.final_state":      data.FinalState,
			"workflow.transition_count": data.TransitionCount,
		},
		Status: &SpanStatus{Code: StatusCodeOk},
	}
}

// ConvertSessionWithParent converts a session's events to spans, using the provided
// trace context as the parent trace instead of generating a fresh one from session ID.
// If traceCtx is nil or has an empty Traceparent, it falls back to ConvertSession behavior.
func (c *EventConverter) ConvertSessionWithParent(
	sessionID string, sessionEvents []events.Event, traceCtx *TraceContext,
) ([]*Span, error) {
	if traceCtx == nil || traceCtx.Traceparent == "" {
		return c.ConvertSession(sessionID, sessionEvents)
	}

	parentTraceID, parentSpanID, ok := parseTraceparent(traceCtx.Traceparent)
	if !ok {
		return c.ConvertSession(sessionID, sessionEvents)
	}

	if len(sessionEvents) == 0 {
		return nil, nil
	}

	return c.buildTrace(sessionID, sessionEvents, parentTraceID, parentSpanID)
}

// buildTrace creates the root session span and converts all events into child spans.
// parentSpanID is set on the root span when propagating an inbound trace context.
func (c *EventConverter) buildTrace(
	sessionID string, sessionEvents []events.Event, traceID, parentSpanID string,
) ([]*Span, error) {
	rootSpanID := generateSpanID(sessionID + ":root")

	var startTime, endTime time.Time
	for _, evt := range sessionEvents {
		if startTime.IsZero() || evt.Timestamp.Before(startTime) {
			startTime = evt.Timestamp
		}
		if endTime.IsZero() || evt.Timestamp.After(endTime) {
			endTime = evt.Timestamp
		}
	}

	rootSpan := &Span{
		TraceID:      traceID,
		SpanID:       rootSpanID,
		ParentSpanID: parentSpanID,
		Name:         "session",
		Kind:         SpanKindServer,
		StartTime:    startTime,
		EndTime:      endTime,
		Attributes: map[string]interface{}{
			"session.id": sessionID,
		},
		Status: &SpanStatus{Code: StatusCodeOk},
	}

	spans := []*Span{rootSpan}
	spanStack := make(map[string]*Span)
	spanStack["root"] = rootSpan

	for i := range sessionEvents {
		span := c.convertEvent(traceID, rootSpanID, &sessionEvents[i], spanStack)
		if span != nil {
			spans = append(spans, span)
		}
	}

	return spans, nil
}

// parseTraceparent extracts trace ID and span ID from a W3C traceparent header.
// Format: version-trace_id-parent_id-trace_flags (e.g., 00-<32 hex>-<16 hex>-<2 hex>).
func parseTraceparent(tp string) (traceID, spanID string, ok bool) {
	if !traceparentRe.MatchString(tp) {
		return "", "", false
	}
	// 00-<32 hex traceID>-<16 hex spanID>-<2 hex flags>
	traceID = tp[3:35]
	spanID = tp[36:52]
	return traceID, spanID, true
}

// generateTraceID generates a 16-byte trace ID from a string.
func generateTraceID(s string) string {
	// Use first 16 bytes of SHA256 hash
	hash := sha256Sum(s)
	return hex.EncodeToString(hash[:16])
}

// generateSpanID generates an 8-byte span ID from a string.
func generateSpanID(s string) string {
	// Use first 8 bytes of SHA256 hash
	hash := sha256Sum(s)
	return hex.EncodeToString(hash[:8])
}

// sha256Sum computes SHA256 hash of a string.
func sha256Sum(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}
