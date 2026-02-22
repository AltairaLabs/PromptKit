package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

func TestEventConverter_ConvertSession(t *testing.T) {
	converter := NewEventConverter(nil)

	t.Run("converts empty events", func(t *testing.T) {
		spans, err := converter.ConvertSession("session-1", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spans != nil {
			t.Error("expected nil spans for empty events")
		}
	})

	t.Run("creates root span for session", func(t *testing.T) {
		startTime := time.Now()
		endTime := startTime.Add(time.Second)

		sessionEvents := []events.Event{
			{
				Type:      events.EventPipelineStarted,
				Timestamp: startTime,
				SessionID: "session-1",
				RunID:     "run-1",
				Data:      &events.PipelineStartedData{MiddlewareCount: 3},
			},
			{
				Type:      events.EventPipelineCompleted,
				Timestamp: endTime,
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.PipelineCompletedData{
					Duration:     time.Second,
					TotalCost:    0.01,
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
		}

		spans, err := converter.ConvertSession("session-1", sessionEvents)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(spans) < 1 {
			t.Fatal("expected at least 1 span (root)")
		}

		root := spans[0]
		if root.Name != "session" {
			t.Errorf("expected root span name 'session', got %q", root.Name)
		}
		if root.Attributes["session.id"] != "session-1" {
			t.Error("expected session.id attribute")
		}
	})

	t.Run("converts provider call events", func(t *testing.T) {
		startTime := time.Now()

		sessionEvents := []events.Event{
			{
				Type:      events.EventProviderCallStarted,
				Timestamp: startTime,
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.ProviderCallStartedData{
					Provider:     "openai",
					Model:        "gpt-4",
					MessageCount: 5,
					ToolCount:    2,
				},
			},
			{
				Type:      events.EventProviderCallCompleted,
				Timestamp: startTime.Add(500 * time.Millisecond),
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.ProviderCallCompletedData{
					Provider:     "openai",
					Model:        "gpt-4",
					Duration:     500 * time.Millisecond,
					InputTokens:  100,
					OutputTokens: 50,
					Cost:         0.01,
					FinishReason: "stop",
				},
			},
		}

		spans, err := converter.ConvertSession("session-1", sessionEvents)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should have root span + provider span
		if len(spans) < 2 {
			t.Fatalf("expected at least 2 spans, got %d", len(spans))
		}

		// Find provider span
		var providerSpan *Span
		for _, s := range spans {
			if s.Name == "provider.openai" {
				providerSpan = s
				break
			}
		}

		if providerSpan == nil {
			t.Fatal("expected provider span")
		}

		if providerSpan.Kind != SpanKindClient {
			t.Errorf("expected SpanKindClient, got %d", providerSpan.Kind)
		}
		if providerSpan.Attributes["gen_ai.usage.input_tokens"] != 100 {
			t.Error("expected input_tokens attribute")
		}
	})

	t.Run("converts tool call events", func(t *testing.T) {
		startTime := time.Now()

		sessionEvents := []events.Event{
			{
				Type:      events.EventToolCallStarted,
				Timestamp: startTime,
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.ToolCallStartedData{
					ToolName: "search",
					CallID:   "call-123",
					Args:     map[string]interface{}{"query": "test"},
				},
			},
			{
				Type:      events.EventToolCallCompleted,
				Timestamp: startTime.Add(100 * time.Millisecond),
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.ToolCallCompletedData{
					ToolName: "search",
					CallID:   "call-123",
					Duration: 100 * time.Millisecond,
					Status:   "success",
				},
			},
		}

		spans, err := converter.ConvertSession("session-1", sessionEvents)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Find tool span
		var toolSpan *Span
		for _, s := range spans {
			if s.Name == "tool.search" {
				toolSpan = s
				break
			}
		}

		if toolSpan == nil {
			t.Fatal("expected tool span")
		}

		if toolSpan.Attributes["tool.call_id"] != "call-123" {
			t.Error("expected tool.call_id attribute")
		}
	})

	t.Run("handles failed events", func(t *testing.T) {
		startTime := time.Now()

		sessionEvents := []events.Event{
			{
				Type:      events.EventProviderCallStarted,
				Timestamp: startTime,
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.ProviderCallStartedData{
					Provider: "openai",
					Model:    "gpt-4",
				},
			},
			{
				Type:      events.EventProviderCallFailed,
				Timestamp: startTime.Add(100 * time.Millisecond),
				SessionID: "session-1",
				RunID:     "run-1",
				Data: &events.ProviderCallFailedData{
					Provider: "openai",
					Model:    "gpt-4",
					Duration: 100 * time.Millisecond,
					Error:    errors.New("rate limited"),
				},
			},
		}

		spans, err := converter.ConvertSession("session-1", sessionEvents)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Find provider span
		var providerSpan *Span
		for _, s := range spans {
			if s.Name == "provider.openai" {
				providerSpan = s
				break
			}
		}

		if providerSpan == nil {
			t.Fatal("expected provider span")
		}

		if providerSpan.Status == nil || providerSpan.Status.Code != StatusCodeError {
			t.Error("expected error status")
		}
		if providerSpan.Status.Message != "rate limited" {
			t.Errorf("expected error message 'rate limited', got %q", providerSpan.Status.Message)
		}
	})
}

func TestGenerateTraceID(t *testing.T) {
	traceID := generateTraceID("session-1")

	if len(traceID) != 32 {
		t.Errorf("expected trace ID length 32, got %d", len(traceID))
	}

	// Should be consistent
	traceID2 := generateTraceID("session-1")
	if traceID != traceID2 {
		t.Error("expected consistent trace IDs")
	}

	// Different input should give different ID
	traceID3 := generateTraceID("session-2")
	if traceID == traceID3 {
		t.Error("expected different trace IDs for different inputs")
	}
}

func TestGenerateSpanID(t *testing.T) {
	spanID := generateSpanID("span-1")

	if len(spanID) != 16 {
		t.Errorf("expected span ID length 16, got %d", len(spanID))
	}
}

// mockHTTPClient implements HTTPClient for testing.
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestOTLPExporter_Export(t *testing.T) {
	t.Run("exports spans successfully", func(t *testing.T) {
		var receivedPayload otlpPayload
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				if err := json.Unmarshal(body, &receivedPayload); err != nil {
					t.Errorf("failed to unmarshal request: %v", err)
				}
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(nil)),
				}, nil
			},
		}

		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithHTTPClient(client))

		spans := []*Span{
			{
				TraceID:   "abc123",
				SpanID:    "def456",
				Name:      "test-span",
				Kind:      SpanKindInternal,
				StartTime: time.Now(),
				EndTime:   time.Now().Add(time.Second),
				Attributes: map[string]interface{}{
					"key": "value",
				},
			},
		}

		err := exporter.Export(context.Background(), spans)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(receivedPayload.ResourceSpans) != 1 {
			t.Error("expected 1 resource span")
		}
		if len(receivedPayload.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
			t.Error("expected 1 span")
		}
	})

	t.Run("handles HTTP errors", func(t *testing.T) {
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 500,
					Body:       io.NopCloser(bytes.NewReader([]byte("internal error"))),
				}, nil
			},
		}

		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithHTTPClient(client))

		err := exporter.Export(context.Background(), []*Span{{Name: "test"}})
		if err == nil {
			t.Error("expected error for 500 response")
		}
	})

	t.Run("handles network errors", func(t *testing.T) {
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			},
		}

		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithHTTPClient(client))

		err := exporter.Export(context.Background(), []*Span{{Name: "test"}})
		if err == nil {
			t.Error("expected error for network failure")
		}
	})

	t.Run("includes custom headers", func(t *testing.T) {
		var receivedHeaders http.Header
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				receivedHeaders = req.Header
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(nil)),
				}, nil
			},
		}

		exporter := NewOTLPExporter(
			"http://localhost:4318/v1/traces",
			WithHTTPClient(client),
			WithHeaders(map[string]string{
				"Authorization": "Bearer token123",
			}),
		)

		err := exporter.Export(context.Background(), []*Span{{Name: "test"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedHeaders.Get("Authorization") != "Bearer token123" {
			t.Error("expected Authorization header")
		}
	})

	t.Run("skips empty spans", func(t *testing.T) {
		called := false
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				called = true
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(nil)),
				}, nil
			},
		}

		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithHTTPClient(client))

		err := exporter.Export(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if called {
			t.Error("should not call HTTP client for empty spans")
		}
	})
}

func TestConvertAttribute(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value interface{}
		check func(t *testing.T, attr otlpAttribute)
	}{
		{
			name:  "string value",
			key:   "key",
			value: "value",
			check: func(t *testing.T, attr otlpAttribute) {
				if attr.Value.StringValue == nil || *attr.Value.StringValue != "value" {
					t.Error("expected string value")
				}
			},
		},
		{
			name:  "int value",
			key:   "count",
			value: 42,
			check: func(t *testing.T, attr otlpAttribute) {
				if attr.Value.IntValue == nil || *attr.Value.IntValue != 42 {
					t.Error("expected int value 42")
				}
			},
		},
		{
			name:  "float value",
			key:   "score",
			value: 0.95,
			check: func(t *testing.T, attr otlpAttribute) {
				if attr.Value.DoubleValue == nil || *attr.Value.DoubleValue != 0.95 {
					t.Error("expected float value 0.95")
				}
			},
		},
		{
			name:  "bool value",
			key:   "enabled",
			value: true,
			check: func(t *testing.T, attr otlpAttribute) {
				if attr.Value.BoolValue == nil || !*attr.Value.BoolValue {
					t.Error("expected bool value true")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attr := convertAttribute(tc.key, tc.value)
			if attr.Key != tc.key {
				t.Errorf("expected key %q, got %q", tc.key, attr.Key)
			}
			tc.check(t, attr)
		})
	}
}

func TestDefaultResource(t *testing.T) {
	resource := DefaultResource()

	if resource.Attributes["service.name"] != "promptkit" {
		t.Error("expected service.name to be 'promptkit'")
	}
}

func TestEventConverter_MiddlewareEvents(t *testing.T) {
	converter := NewEventConverter(nil)
	startTime := time.Now()

	sessionEvents := []events.Event{
		{
			Type:      events.EventMiddlewareStarted,
			Timestamp: startTime,
			SessionID: "session-1",
			RunID:     "run-1",
			Data: &events.MiddlewareStartedData{
				Name:  "auth",
				Index: 0,
			},
		},
		{
			Type:      events.EventMiddlewareCompleted,
			Timestamp: startTime.Add(50 * time.Millisecond),
			SessionID: "session-1",
			RunID:     "run-1",
			Data: &events.MiddlewareCompletedData{
				Name:     "auth",
				Index:    0,
				Duration: 50 * time.Millisecond,
			},
		},
	}

	spans, err := converter.ConvertSession("session-1", sessionEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find middleware span
	var middlewareSpan *Span
	for _, s := range spans {
		if s.Name == "middleware.auth" {
			middlewareSpan = s
			break
		}
	}

	if middlewareSpan == nil {
		t.Fatal("expected middleware span")
	}

	if middlewareSpan.Kind != SpanKindInternal {
		t.Errorf("expected SpanKindInternal, got %d", middlewareSpan.Kind)
	}
}

func TestEventConverter_MiddlewareFailed(t *testing.T) {
	converter := NewEventConverter(nil)
	startTime := time.Now()

	sessionEvents := []events.Event{
		{
			Type:      events.EventMiddlewareStarted,
			Timestamp: startTime,
			SessionID: "session-1",
			RunID:     "run-1",
			Data: &events.MiddlewareStartedData{
				Name:  "auth",
				Index: 0,
			},
		},
		{
			Type:      events.EventMiddlewareFailed,
			Timestamp: startTime.Add(50 * time.Millisecond),
			SessionID: "session-1",
			RunID:     "run-1",
			Data: &events.MiddlewareFailedData{
				Name:     "auth",
				Index:    0,
				Duration: 50 * time.Millisecond,
				Error:    errors.New("auth failed"),
			},
		},
	}

	spans, err := converter.ConvertSession("session-1", sessionEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find middleware span
	var middlewareSpan *Span
	for _, s := range spans {
		if s.Name == "middleware.auth" {
			middlewareSpan = s
			break
		}
	}

	if middlewareSpan == nil {
		t.Fatal("expected middleware span")
	}

	if middlewareSpan.Status == nil || middlewareSpan.Status.Code != StatusCodeError {
		t.Error("expected error status")
	}
}

func TestEventConverter_ToolFailed(t *testing.T) {
	converter := NewEventConverter(nil)
	startTime := time.Now()

	sessionEvents := []events.Event{
		{
			Type:      events.EventToolCallStarted,
			Timestamp: startTime,
			SessionID: "session-1",
			RunID:     "run-1",
			Data: &events.ToolCallStartedData{
				ToolName: "search",
				CallID:   "call-1",
			},
		},
		{
			Type:      events.EventToolCallFailed,
			Timestamp: startTime.Add(100 * time.Millisecond),
			SessionID: "session-1",
			RunID:     "run-1",
			Data: &events.ToolCallFailedData{
				ToolName: "search",
				CallID:   "call-1",
				Duration: 100 * time.Millisecond,
				Error:    errors.New("tool failed"),
			},
		},
	}

	spans, err := converter.ConvertSession("session-1", sessionEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find tool span
	var toolSpan *Span
	for _, s := range spans {
		if s.Name == "tool.search" {
			toolSpan = s
			break
		}
	}

	if toolSpan == nil {
		t.Fatal("expected tool span")
	}

	if toolSpan.Status == nil || toolSpan.Status.Code != StatusCodeError {
		t.Error("expected error status")
	}
}

func TestOTLPExporter_Shutdown(t *testing.T) {
	t.Run("flushes pending spans", func(t *testing.T) {
		exportCount := 0
		client := &mockHTTPClient{
			doFunc: func(req *http.Request) (*http.Response, error) {
				exportCount++
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(nil)),
				}, nil
			},
		}

		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithHTTPClient(client))
		exporter.pending = []*Span{{Name: "pending-span"}}

		err := exporter.Shutdown(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if exportCount != 1 {
			t.Errorf("expected 1 export call, got %d", exportCount)
		}
	})

	t.Run("no-op with no pending spans", func(t *testing.T) {
		exporter := NewOTLPExporter("http://localhost:4318/v1/traces")

		err := exporter.Shutdown(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestOTLPExporter_Options(t *testing.T) {
	t.Run("WithResource sets custom resource", func(t *testing.T) {
		resource := &Resource{
			Attributes: map[string]interface{}{
				"custom.attr": "value",
			},
		}

		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithResource(resource))
		if exporter.resource.Attributes["custom.attr"] != "value" {
			t.Error("expected custom resource attribute")
		}
	})

	t.Run("WithBatchSize sets batch size", func(t *testing.T) {
		exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithBatchSize(50))
		if exporter.batchSize != 50 {
			t.Errorf("expected batch size 50, got %d", exporter.batchSize)
		}
	})
}

func TestOTLPExporter_SpanWithEvents(t *testing.T) {
	var receivedPayload otlpPayload
	client := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(body, &receivedPayload)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		},
	}

	exporter := NewOTLPExporter("http://localhost:4318/v1/traces", WithHTTPClient(client))

	spans := []*Span{
		{
			TraceID:   "abc123",
			SpanID:    "def456",
			Name:      "test-span",
			Kind:      SpanKindInternal,
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Second),
			Events: []*SpanEvent{
				{
					Name: "event1",
					Time: time.Now(),
					Attributes: map[string]interface{}{
						"key": "value",
					},
				},
			},
		},
	}

	err := exporter.Export(context.Background(), spans)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(receivedPayload.ResourceSpans[0].ScopeSpans[0].Spans[0].Events) != 1 {
		t.Error("expected 1 span event")
	}
}

func TestConvertAttribute_Int64(t *testing.T) {
	attr := convertAttribute("count", int64(100))
	if attr.Value.IntValue == nil || *attr.Value.IntValue != 100 {
		t.Error("expected int64 value 100")
	}
}

func TestConvertAttribute_Unknown(t *testing.T) {
	attr := convertAttribute("unknown", struct{ Field string }{Field: "test"})
	if attr.Value.StringValue == nil {
		t.Error("expected string representation of unknown type")
	}
}

func TestNewEventConverter_WithResource(t *testing.T) {
	resource := &Resource{
		Attributes: map[string]interface{}{
			"custom": "value",
		},
	}

	converter := NewEventConverter(resource)
	if converter.Resource.Attributes["custom"] != "value" {
		t.Error("expected custom resource")
	}
}

// providerCallPair returns a provider started/completed event pair for use in tests.
func providerCallPair(t0 time.Time, finishReason string) (events.Event, events.Event) {
	return events.Event{
			Type: events.EventProviderCallStarted, Timestamp: t0,
			SessionID: "session-1", RunID: "run-1",
			Data: &events.ProviderCallStartedData{Provider: "openai", Model: "gpt-4"},
		}, events.Event{
			Type: events.EventProviderCallCompleted, Timestamp: t0.Add(500 * time.Millisecond),
			SessionID: "session-1", RunID: "run-1",
			Data: &events.ProviderCallCompletedData{
				Provider: "openai", Model: "gpt-4",
				Duration: 500 * time.Millisecond, FinishReason: finishReason,
			},
		}
}

// pipelineEventPair returns a pipeline started/completed event pair for use in tests.
func pipelineEventPair(t0 time.Time) (events.Event, events.Event) {
	return events.Event{
			Type: events.EventPipelineStarted, Timestamp: t0,
			SessionID: "session-1", RunID: "run-1",
			Data: &events.PipelineStartedData{MiddlewareCount: 0},
		}, events.Event{
			Type: events.EventPipelineCompleted, Timestamp: t0.Add(time.Second),
			SessionID: "session-1", RunID: "run-1",
			Data: &events.PipelineCompletedData{Duration: time.Second},
		}
}

// requireSpan finds a span by name in the slice or fails the test.
func requireSpan(t *testing.T, spans []*Span, name string) *Span {
	t.Helper()
	for _, s := range spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("expected span %q not found", name)
	return nil
}

// convertSession is a test helper that calls ConvertSession and fails on error.
func convertSession(t *testing.T, sessionEvents []events.Event) []*Span {
	t.Helper()
	spans, err := NewEventConverter(nil).ConvertSession("session-1", sessionEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return spans
}

func TestEventConverter_MessageCreated(t *testing.T) {
	startTime := time.Now()
	provStart, provEnd := providerCallPair(startTime, "stop")

	sessionEvents := []events.Event{
		provStart,
		{
			Type: events.EventMessageCreated, Timestamp: startTime.Add(100 * time.Millisecond),
			SessionID: "session-1", RunID: "run-1",
			Data: &events.MessageCreatedData{Role: "user", Content: "Hello, world!"},
		},
		{
			Type: events.EventMessageCreated, Timestamp: startTime.Add(200 * time.Millisecond),
			SessionID: "session-1", RunID: "run-1",
			Data: &events.MessageCreatedData{Role: "assistant", Content: "Hi there!"},
		},
		provEnd,
	}

	spans := convertSession(t, sessionEvents)
	providerSpan := requireSpan(t, spans, "provider.openai")

	if len(providerSpan.Events) != 2 {
		t.Fatalf("expected 2 span events on provider span, got %d", len(providerSpan.Events))
	}
	if providerSpan.Events[0].Name != "gen_ai.user.message" {
		t.Errorf("expected gen_ai.user.message, got %q", providerSpan.Events[0].Name)
	}
	if providerSpan.Events[0].Attributes["gen_ai.message.content"] != "Hello, world!" {
		t.Error("expected message content attribute on user message")
	}
	if providerSpan.Events[1].Name != "gen_ai.assistant.message" {
		t.Errorf("expected gen_ai.assistant.message, got %q", providerSpan.Events[1].Name)
	}
	if providerSpan.Events[1].Attributes["gen_ai.message.content"] != "Hi there!" {
		t.Error("expected message content attribute on assistant message")
	}
}

func TestEventConverter_MessageCreatedWithToolCalls(t *testing.T) {
	startTime := time.Now()
	provStart, provEnd := providerCallPair(startTime, "tool_calls")

	sessionEvents := []events.Event{
		provStart,
		{
			Type: events.EventMessageCreated, Timestamp: startTime.Add(100 * time.Millisecond),
			SessionID: "session-1", RunID: "run-1",
			Data: &events.MessageCreatedData{
				Role: "assistant", Content: "",
				ToolCalls: []events.MessageToolCall{
					{ID: "call-1", Name: "search", Args: `{"query":"test"}`},
				},
			},
		},
		provEnd,
	}

	spans := convertSession(t, sessionEvents)
	providerSpan := requireSpan(t, spans, "provider.openai")

	if len(providerSpan.Events) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(providerSpan.Events))
	}

	evt := providerSpan.Events[0]
	if evt.Name != "gen_ai.assistant.message" {
		t.Errorf("expected gen_ai.assistant.message, got %q", evt.Name)
	}
	toolCallsAttr, ok := evt.Attributes["gen_ai.tool_calls"].(string)
	if !ok || toolCallsAttr == "" {
		t.Fatal("expected non-empty gen_ai.tool_calls attribute")
	}
}

func TestEventConverter_MessageCreatedNoProvider(t *testing.T) {
	startTime := time.Now()

	sessionEvents := []events.Event{
		{
			Type: events.EventMessageCreated, Timestamp: startTime,
			SessionID: "session-1", RunID: "run-1",
			Data: &events.MessageCreatedData{Role: "user", Content: "Hello without provider"},
		},
	}

	spans := convertSession(t, sessionEvents)
	root := spans[0]
	if root.Name != "session" {
		t.Fatal("expected root span")
	}
	if len(root.Events) != 1 {
		t.Fatalf("expected 1 event on root span, got %d", len(root.Events))
	}
	if root.Events[0].Name != "gen_ai.user.message" {
		t.Errorf("expected gen_ai.user.message, got %q", root.Events[0].Name)
	}
}

// toolCallPair returns a tool started/completed event pair for use in tests.
func toolCallPair(t0 time.Time, name, callID string, args map[string]interface{}) (events.Event, events.Event) {
	return events.Event{
			Type: events.EventToolCallStarted, Timestamp: t0,
			SessionID: "session-1", RunID: "run-1",
			Data: &events.ToolCallStartedData{ToolName: name, CallID: callID, Args: args},
		}, events.Event{
			Type: events.EventToolCallCompleted, Timestamp: t0.Add(100 * time.Millisecond),
			SessionID: "session-1", RunID: "run-1",
			Data: &events.ToolCallCompletedData{
				ToolName: name, CallID: callID,
				Duration: 100 * time.Millisecond, Status: "success",
			},
		}
}

func TestEventConverter_ToolArgsEnriched(t *testing.T) {
	startTime := time.Now()
	start, end := toolCallPair(startTime, "search", "call-args-1",
		map[string]interface{}{"query": "test", "limit": float64(10)})

	spans := convertSession(t, []events.Event{start, end})
	toolSpan := requireSpan(t, spans, "tool.search")

	argsAttr, ok := toolSpan.Attributes["tool.args"].(string)
	if !ok {
		t.Fatal("expected tool.args attribute as string")
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsAttr), &args); err != nil {
		t.Fatalf("expected valid JSON in tool.args, got error: %v", err)
	}
	if args["query"] != "test" {
		t.Errorf("expected query=test, got %v", args["query"])
	}
}

func TestEventConverter_ToolArgsNil(t *testing.T) {
	startTime := time.Now()
	start, end := toolCallPair(startTime, "noop", "call-nil-args", nil)

	spans := convertSession(t, []events.Event{start, end})
	toolSpan := requireSpan(t, spans, "tool.noop")

	if _, exists := toolSpan.Attributes["tool.args"]; exists {
		t.Error("expected no tool.args attribute when Args is nil")
	}
}

func TestEventConverter_WorkflowTransitioned(t *testing.T) {
	startTime := time.Now()
	sessionEvents := []events.Event{
		{
			Type: events.EventWorkflowTransitioned, Timestamp: startTime,
			SessionID: "session-1", RunID: "run-1",
			Data: &events.WorkflowTransitionedData{
				FromState: "greeting", ToState: "issue_triage",
				Event: "issue_reported", PromptTask: "triage the issue",
			},
		},
	}

	spans := convertSession(t, sessionEvents)
	wfSpan := requireSpan(t, spans, "workflow.transition")

	if wfSpan.Kind != SpanKindInternal {
		t.Errorf("expected SpanKindInternal, got %d", wfSpan.Kind)
	}
	if wfSpan.Attributes["workflow.from_state"] != "greeting" {
		t.Errorf("expected from_state=greeting, got %v", wfSpan.Attributes["workflow.from_state"])
	}
	if wfSpan.Attributes["workflow.to_state"] != "issue_triage" {
		t.Errorf("expected to_state=issue_triage, got %v", wfSpan.Attributes["workflow.to_state"])
	}
	if wfSpan.Attributes["workflow.event"] != "issue_reported" {
		t.Errorf("expected event=issue_reported, got %v", wfSpan.Attributes["workflow.event"])
	}
	if wfSpan.Attributes["workflow.prompt_task"] != "triage the issue" {
		t.Errorf("expected prompt_task, got %v", wfSpan.Attributes["workflow.prompt_task"])
	}
	if !wfSpan.StartTime.Equal(wfSpan.EndTime) {
		t.Error("expected instant span (start == end)")
	}
}

func TestEventConverter_WorkflowCompleted(t *testing.T) {
	startTime := time.Now()
	sessionEvents := []events.Event{
		{
			Type: events.EventWorkflowCompleted, Timestamp: startTime,
			SessionID: "session-1", RunID: "run-1",
			Data: &events.WorkflowCompletedData{FinalState: "resolved", TransitionCount: 5},
		},
	}

	spans := convertSession(t, sessionEvents)
	wfSpan := requireSpan(t, spans, "workflow.completed")

	if wfSpan.Attributes["workflow.final_state"] != "resolved" {
		t.Errorf("expected final_state=resolved, got %v", wfSpan.Attributes["workflow.final_state"])
	}
	if wfSpan.Attributes["workflow.transition_count"] != 5 {
		t.Errorf("expected transition_count=5, got %v", wfSpan.Attributes["workflow.transition_count"])
	}
	if wfSpan.Status == nil || wfSpan.Status.Code != StatusCodeOk {
		t.Error("expected Ok status")
	}
}

func TestEventConverter_ConvertSessionWithParent(t *testing.T) {
	converter := NewEventConverter(nil)
	startTime := time.Now()
	pipStart, pipEnd := pipelineEventPair(startTime)
	sessionEvents := []events.Event{pipStart, pipEnd}

	t.Run("valid traceparent", func(t *testing.T) {
		traceCtx := &TraceContext{
			Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		}
		spans, err := converter.ConvertSessionWithParent("session-1", sessionEvents, traceCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		root := spans[0]
		if root.TraceID != "0af7651916cd43dd8448eb211c80319c" {
			t.Errorf("expected parent trace ID, got %q", root.TraceID)
		}
		if root.ParentSpanID != "b7ad6b7169203331" {
			t.Errorf("expected parent span ID, got %q", root.ParentSpanID)
		}
	})

	t.Run("nil trace context", func(t *testing.T) {
		spans, err := converter.ConvertSessionWithParent("session-1", sessionEvents, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spans[0].ParentSpanID != "" {
			t.Errorf("expected no parent span ID, got %q", spans[0].ParentSpanID)
		}
	})

	t.Run("invalid traceparent", func(t *testing.T) {
		traceCtx := &TraceContext{Traceparent: "invalid-traceparent"}
		spans, err := converter.ConvertSessionWithParent("session-1", sessionEvents, traceCtx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spans[0].ParentSpanID != "" {
			t.Errorf("expected no parent span ID for invalid traceparent, got %q", spans[0].ParentSpanID)
		}
	})
}

func TestResourceWithPackID(t *testing.T) {
	resource := ResourceWithPackID("my-pack-v1")

	if resource.Attributes["pack.id"] != "my-pack-v1" {
		t.Errorf("expected pack.id=my-pack-v1, got %v", resource.Attributes["pack.id"])
	}
	if resource.Attributes["service.name"] != "promptkit" {
		t.Error("expected default service.name to be preserved")
	}
}
