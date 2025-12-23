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
