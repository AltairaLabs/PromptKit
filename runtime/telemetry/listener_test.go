package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// newTestListener returns a listener, in-memory exporter, and TracerProvider for tests.
func newTestListener(t *testing.T) (*OTelEventListener, *tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer(InstrumentationName)
	listener := NewOTelEventListener(tracer)
	return listener, exp, tp
}

// flushAndGetSpans forces span export and returns spans.
// ForceFlush ensures all ended spans are exported; we read them before Shutdown
// because InMemoryExporter.Shutdown resets the buffer.
func flushAndGetSpans(t *testing.T, tp *sdktrace.TracerProvider, exp *tracetest.InMemoryExporter) tracetest.SpanStubs {
	t.Helper()
	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	spans := exp.GetSpans()
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	return spans
}

// findSpan finds a span by name in the stubs or fails.
func findSpan(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, s := range spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("span %q not found in %d spans", name, len(spans))
	return tracetest.SpanStub{}
}

// hasAttr checks if a span has an attribute with the given key and string value.
func hasAttr(span tracetest.SpanStub, key, want string) bool {
	for _, a := range span.Attributes {
		if string(a.Key) == key && a.Value.AsString() == want {
			return true
		}
	}
	return false
}

func TestOTelEventListener_SessionLifecycle(t *testing.T) {
	listener, exp, tp := newTestListener(t)

	listener.StartSession(context.Background(), "sess-1")
	listener.EndSession("sess-1")

	spans := flushAndGetSpans(t, tp, exp)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if s.Name != "promptkit.session" {
		t.Errorf("expected span name 'promptkit.session', got %q", s.Name)
	}
	if !hasAttr(s, "session.id", "sess-1") {
		t.Error("expected session.id attribute")
	}
}

func TestOTelEventListener_PipelineSpan(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventPipelineStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.PipelineStartedData{MiddlewareCount: 2},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted, Timestamp: now.Add(time.Second),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.PipelineCompletedData{
			Duration: time.Second, TotalCost: 0.01,
			InputTokens: 100, OutputTokens: 50,
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	pipelineSpan := findSpan(t, spans, "promptkit.pipeline")
	if pipelineSpan.Status.Code != codes.Ok {
		t.Errorf("expected Ok status, got %v", pipelineSpan.Status.Code)
	}

	// Verify parent relationship.
	sessionSpan := findSpan(t, spans, "promptkit.session")
	if pipelineSpan.Parent.SpanID() != sessionSpan.SpanContext.SpanID() {
		t.Error("pipeline span should be child of session span")
	}
}

func TestOTelEventListener_PipelineFailed(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventPipelineStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.PipelineStartedData{},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventPipelineFailed, Timestamp: now.Add(time.Second),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.PipelineFailedData{
			Duration: time.Second, Error: errors.New("boom"),
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	pipelineSpan := findSpan(t, spans, "promptkit.pipeline")
	if pipelineSpan.Status.Code != codes.Error {
		t.Errorf("expected Error status, got %v", pipelineSpan.Status.Code)
	}
	if pipelineSpan.Status.Description != "boom" {
		t.Errorf("expected error description 'boom', got %q", pipelineSpan.Status.Description)
	}
}

func TestOTelEventListener_ProviderSpan(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallStartedData{
			Provider: "openai", Model: "gpt-4",
			MessageCount: 5, ToolCount: 2,
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallCompleted, Timestamp: now.Add(500 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallCompletedData{
			Provider: "openai", Model: "gpt-4",
			Duration: 500 * time.Millisecond,
			InputTokens: 100, OutputTokens: 50,
			Cost: 0.01, FinishReason: "stop",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	providerSpan := findSpan(t, spans, "promptkit.provider.openai")
	if !hasAttr(providerSpan, "gen_ai.system", "openai") {
		t.Error("expected gen_ai.system attribute")
	}
	if !hasAttr(providerSpan, "gen_ai.request.model", "gpt-4") {
		t.Error("expected gen_ai.request.model attribute")
	}
	if providerSpan.Status.Code != codes.Ok {
		t.Errorf("expected Ok status, got %v", providerSpan.Status.Code)
	}
}

func TestOTelEventListener_ProviderFailed(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallFailed, Timestamp: now.Add(100 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallFailedData{
			Provider: "openai", Model: "gpt-4",
			Duration: 100 * time.Millisecond, Error: errors.New("rate limited"),
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	providerSpan := findSpan(t, spans, "promptkit.provider.openai")
	if providerSpan.Status.Code != codes.Error {
		t.Error("expected Error status")
	}
	if providerSpan.Status.Description != "rate limited" {
		t.Errorf("expected 'rate limited', got %q", providerSpan.Status.Description)
	}
}

func TestOTelEventListener_ToolSpan(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventToolCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ToolCallStartedData{
			ToolName: "search", CallID: "call-123",
			Args: map[string]interface{}{"query": "test"},
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventToolCallCompleted, Timestamp: now.Add(100 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ToolCallCompletedData{
			ToolName: "search", CallID: "call-123",
			Duration: 100 * time.Millisecond, Status: "success",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	toolSpan := findSpan(t, spans, "promptkit.tool.search")
	if !hasAttr(toolSpan, "tool.call_id", "call-123") {
		t.Error("expected tool.call_id attribute")
	}
	if !hasAttr(toolSpan, "tool.status", "success") {
		t.Error("expected tool.status attribute")
	}
}

func TestOTelEventListener_ToolFailed(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventToolCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ToolCallStartedData{ToolName: "search", CallID: "call-1"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventToolCallFailed, Timestamp: now.Add(100 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ToolCallFailedData{
			ToolName: "search", CallID: "call-1",
			Duration: 100 * time.Millisecond, Error: errors.New("tool failed"),
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	toolSpan := findSpan(t, spans, "promptkit.tool.search")
	if toolSpan.Status.Code != codes.Error {
		t.Error("expected Error status")
	}
}

func TestOTelEventListener_MiddlewareSpan(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventMiddlewareStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MiddlewareStartedData{Name: "auth", Index: 0},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventMiddlewareCompleted, Timestamp: now.Add(50 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MiddlewareCompletedData{
			Name: "auth", Index: 0, Duration: 50 * time.Millisecond,
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	mwSpan := findSpan(t, spans, "promptkit.middleware.auth")
	if mwSpan.Status.Code != codes.Ok {
		t.Errorf("expected Ok status, got %v", mwSpan.Status.Code)
	}
}

func TestOTelEventListener_MiddlewareFailed(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventMiddlewareStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MiddlewareStartedData{Name: "auth", Index: 0},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventMiddlewareFailed, Timestamp: now.Add(50 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MiddlewareFailedData{
			Name: "auth", Index: 0,
			Duration: 50 * time.Millisecond, Error: errors.New("auth failed"),
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	mwSpan := findSpan(t, spans, "promptkit.middleware.auth")
	if mwSpan.Status.Code != codes.Error {
		t.Error("expected Error status")
	}
}

func TestOTelEventListener_MessageCreated_OnProvider(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventMessageCreated, Timestamp: now.Add(100 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MessageCreatedData{Role: "user", Content: "Hello!"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallCompleted, Timestamp: now.Add(500 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallCompletedData{
			Provider: "openai", Model: "gpt-4",
			Duration: 500 * time.Millisecond, FinishReason: "stop",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	providerSpan := findSpan(t, spans, "promptkit.provider.openai")
	if len(providerSpan.Events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(providerSpan.Events))
	}
	if providerSpan.Events[0].Name != "gen_ai.user.message" {
		t.Errorf("expected gen_ai.user.message, got %q", providerSpan.Events[0].Name)
	}
}

func TestOTelEventListener_MessageCreated_FallsBackToSession(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	// Message without active provider span falls back to session root.
	listener.OnEvent(&events.Event{
		Type: events.EventMessageCreated, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MessageCreatedData{Role: "user", Content: "Hello without provider"},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	sessionSpan := findSpan(t, spans, "promptkit.session")
	if len(sessionSpan.Events) != 1 {
		t.Fatalf("expected 1 event on session span, got %d", len(sessionSpan.Events))
	}
	if sessionSpan.Events[0].Name != "gen_ai.user.message" {
		t.Errorf("expected gen_ai.user.message, got %q", sessionSpan.Events[0].Name)
	}
}

func TestOTelEventListener_MessageCreated_WithToolCalls(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventMessageCreated, Timestamp: now.Add(100 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.MessageCreatedData{
			Role: "assistant",
			ToolCalls: []events.MessageToolCall{
				{ID: "call-1", Name: "search", Args: `{"query":"test"}`},
			},
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallCompleted, Timestamp: now.Add(500 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallCompletedData{
			Provider: "openai", Model: "gpt-4",
			Duration: 500 * time.Millisecond, FinishReason: "tool_calls",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	providerSpan := findSpan(t, spans, "promptkit.provider.openai")
	if len(providerSpan.Events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(providerSpan.Events))
	}

	// Check tool_calls attribute is present.
	found := false
	for _, a := range providerSpan.Events[0].Attributes {
		if string(a.Key) == "gen_ai.tool_calls" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected gen_ai.tool_calls attribute on message event")
	}
}

func TestOTelEventListener_WorkflowTransition(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventWorkflowTransitioned, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.WorkflowTransitionedData{
			FromState: "greeting", ToState: "issue_triage",
			Event: "issue_reported", PromptTask: "triage the issue",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	wfSpan := findSpan(t, spans, "promptkit.workflow.transition")
	if !hasAttr(wfSpan, "workflow.from_state", "greeting") {
		t.Error("expected workflow.from_state attribute")
	}
	if !hasAttr(wfSpan, "workflow.to_state", "issue_triage") {
		t.Error("expected workflow.to_state attribute")
	}
}

func TestOTelEventListener_WorkflowCompleted(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventWorkflowCompleted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.WorkflowCompletedData{FinalState: "resolved", TransitionCount: 5},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	wfSpan := findSpan(t, spans, "promptkit.workflow.completed")
	if wfSpan.Status.Code != codes.Ok {
		t.Errorf("expected Ok status, got %v", wfSpan.Status.Code)
	}
	if !hasAttr(wfSpan, "workflow.final_state", "resolved") {
		t.Error("expected workflow.final_state attribute")
	}

	// Check transition_count int attribute.
	found := false
	for _, a := range wfSpan.Attributes {
		if string(a.Key) == "workflow.transition_count" && a.Value.AsInt64() == 5 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected workflow.transition_count=5")
	}
}

func TestOTelEventListener_ToolNilArgs(t *testing.T) {
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventToolCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ToolCallStartedData{ToolName: "noop", CallID: "call-nil"},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventToolCallCompleted, Timestamp: now.Add(10 * time.Millisecond),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ToolCallCompletedData{
			ToolName: "noop", CallID: "call-nil",
			Duration: 10 * time.Millisecond, Status: "success",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	toolSpan := findSpan(t, spans, "promptkit.tool.noop")
	for _, a := range toolSpan.Attributes {
		if string(a.Key) == "tool.args" {
			t.Error("expected no tool.args attribute when Args is nil")
		}
	}
}

func TestOTelEventListener_ParentTraceContext(t *testing.T) {
	listener, exp, tp := newTestListener(t)

	// Create a parent span to verify nesting.
	tracer := tp.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "parent-operation")

	listener.StartSession(parentCtx, "sess-1")
	listener.EndSession("sess-1")
	parentSpan.End()

	spans := flushAndGetSpans(t, tp, exp)
	sessionSpan := findSpan(t, spans, "promptkit.session")
	parent := findSpan(t, spans, "parent-operation")

	if sessionSpan.Parent.SpanID() != parent.SpanContext.SpanID() {
		t.Error("session span should be child of parent span")
	}
	if sessionSpan.SpanContext.TraceID() != parent.SpanContext.TraceID() {
		t.Error("session span should share trace ID with parent")
	}
}

func TestOTelEventListener_EndSession_Idempotent(t *testing.T) {
	listener, _, tp := newTestListener(t)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	listener.StartSession(context.Background(), "sess-1")
	listener.EndSession("sess-1")
	// Second call should not panic.
	listener.EndSession("sess-1")
}

func TestOTelEventListener_UnknownEventType(t *testing.T) {
	listener, _, tp := newTestListener(t)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	listener.StartSession(context.Background(), "sess-1")

	// Should not panic on unhandled event types.
	listener.OnEvent(&events.Event{
		Type:      events.EventConversationStarted,
		SessionID: "sess-1", RunID: "run-1",
	})

	listener.EndSession("sess-1")
}

func TestOTelEventListener_SpanAttributes(t *testing.T) {
	// Verify specific attribute values on completed provider span.
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallStartedData{
			Provider: "anthropic", Model: "claude-3",
			MessageCount: 3, ToolCount: 1,
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallCompleted, Timestamp: now.Add(time.Second),
		SessionID: "sess-1", RunID: "run-1",
		Data: &events.ProviderCallCompletedData{
			Provider: "anthropic", Model: "claude-3",
			Duration:     time.Second,
			InputTokens:  200,
			OutputTokens: 100,
			Cost:         0.005,
			FinishReason: "end_turn",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	provSpan := findSpan(t, spans, "promptkit.provider.anthropic")

	// Check numeric attributes.
	attrMap := make(map[string]attribute.Value)
	for _, a := range provSpan.Attributes {
		attrMap[string(a.Key)] = a.Value
	}

	if v, ok := attrMap["gen_ai.usage.input_tokens"]; !ok || v.AsInt64() != 200 {
		t.Errorf("expected gen_ai.usage.input_tokens=200, got %v", attrMap["gen_ai.usage.input_tokens"])
	}
	if v, ok := attrMap["gen_ai.usage.output_tokens"]; !ok || v.AsInt64() != 100 {
		t.Errorf("expected gen_ai.usage.output_tokens=100, got %v", attrMap["gen_ai.usage.output_tokens"])
	}
	if v, ok := attrMap["gen_ai.response.finish_reason"]; !ok || v.AsString() != "end_turn" {
		t.Errorf("expected gen_ai.response.finish_reason=end_turn, got %v", attrMap["gen_ai.response.finish_reason"])
	}
	if v, ok := attrMap["provider.cost"]; !ok || v.AsFloat64() != 0.005 {
		t.Errorf("expected provider.cost=0.005, got %v", attrMap["provider.cost"])
	}
}

func TestOTelEventListener_OutOfOrderDelivery(t *testing.T) {
	// Verify that a "completed" event arriving before "started" still produces a valid span.
	// This happens because EventBus dispatches each Publish() in a separate goroutine.
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	// Send completed BEFORE started (simulates async race).
	listener.OnEvent(&events.Event{
		Type: events.EventPipelineCompleted, Timestamp: now.Add(time.Second),
		SessionID: "sess-1", RunID: "run-1",
		Data: events.PipelineCompletedData{
			Duration: time.Second, TotalCost: 0.01,
			InputTokens: 100, OutputTokens: 50,
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventPipelineStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	pipeSpan := findSpan(t, spans, "promptkit.pipeline")
	if pipeSpan.Status.Code != codes.Ok {
		t.Errorf("expected OK status, got %v", pipeSpan.Status.Code)
	}

	// Verify completion attributes were applied.
	attrMap := make(map[string]attribute.Value)
	for _, a := range pipeSpan.Attributes {
		attrMap[string(a.Key)] = a.Value
	}
	if v, ok := attrMap["pipeline.total_cost"]; !ok || v.AsFloat64() != 0.01 {
		t.Errorf("expected pipeline.total_cost=0.01, got %v", attrMap["pipeline.total_cost"])
	}
}

func TestOTelEventListener_OutOfOrderFailed(t *testing.T) {
	// Verify that a "failed" event arriving before "started" produces a span with error status.
	listener, exp, tp := newTestListener(t)
	now := time.Now()

	listener.StartSession(context.Background(), "sess-1")

	// Send failed BEFORE started.
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallFailed, Timestamp: now.Add(time.Second),
		SessionID: "sess-1", RunID: "run-1",
		Data: events.ProviderCallFailedData{
			Provider: "test", Model: "test-model",
			Error: errors.New("timeout"), Duration: time.Second,
		},
	})
	listener.OnEvent(&events.Event{
		Type: events.EventProviderCallStarted, Timestamp: now,
		SessionID: "sess-1", RunID: "run-1",
		Data: events.ProviderCallStartedData{
			Provider: "test", Model: "test-model",
		},
	})

	listener.EndSession("sess-1")
	spans := flushAndGetSpans(t, tp, exp)

	provSpan := findSpan(t, spans, "promptkit.provider.test")
	if provSpan.Status.Code != codes.Error {
		t.Errorf("expected Error status, got %v", provSpan.Status.Code)
	}
	if provSpan.Status.Description != "timeout" {
		t.Errorf("expected error message 'timeout', got %q", provSpan.Status.Description)
	}
}
