package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// newTestServer creates a test HTTP server and registers cleanup via t.Cleanup.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// jsonHandler returns an http.HandlerFunc that writes the given JSON body with 200 OK.
func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

// okHandler returns an http.HandlerFunc that writes the given body with 200 OK.
func okHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

// newTestDescriptor creates a ToolDescriptor with the given URL and method.
func newTestDescriptor(url, method string) *ToolDescriptor {
	return &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: url, Method: method},
	}
}

// mustExecute calls Execute and fails the test on error.
func mustExecute(t *testing.T, exec *HTTPExecutor, desc *ToolDescriptor, args json.RawMessage) json.RawMessage {
	t.Helper()
	result, err := exec.Execute(context.Background(), desc, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return result
}

// mustParseMap unmarshals JSON into map[string]any, failing the test on error.
func mustParseMap(t *testing.T, data json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	return m
}

func TestHTTPExecutor_Name(t *testing.T) {
	executor := NewHTTPExecutor()
	if got := executor.Name(); got != executorNameHTTP {
		t.Errorf("Name() = %q, want %q", got, executorNameHTTP)
	}
}

func TestHTTPExecutor_Execute_Success(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success", "value": 42}`))
	})

	result := mustExecute(t, NewHTTPExecutor(), newTestDescriptor(server.URL, "POST"), json.RawMessage(`{"query": "test"}`))
	parsed := mustParseMap(t, result)
	if parsed["result"] != "success" {
		t.Errorf("result = %v, want %q", parsed["result"], "success")
	}
}

func TestHTTPExecutor_Execute_NoConfig(t *testing.T) {
	_, err := NewHTTPExecutor().Execute(context.Background(), &ToolDescriptor{Name: "test_tool"}, json.RawMessage(`{}`))
	if err == nil {
		t.Error("Execute() expected error for missing HTTPConfig")
	}
}

func TestHTTPExecutor_Execute_HTTPError(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	})

	_, err := NewHTTPExecutor().Execute(context.Background(), newTestDescriptor(server.URL, "POST"), json.RawMessage(`{}`))
	if err == nil {
		t.Error("Execute() expected error for HTTP 400")
	}
}

func TestHTTPExecutor_Execute_GetMethod(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	})
	mustExecute(t, NewHTTPExecutor(), newTestDescriptor(server.URL, "GET"), nil)
}

func TestHTTPExecutor_Execute_GetWithQueryParams(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "London" {
			t.Errorf("query param name = %q, want London", got)
		}
		if got := r.URL.Query().Get("count"); got != "5" {
			t.Errorf("query param count = %q, want 5", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": []}`))
	})
	mustExecute(t, NewHTTPExecutor(), newTestDescriptor(server.URL, "GET"), json.RawMessage(`{"name": "London", "count": 5}`))
}

func TestHTTPExecutor_Execute_GetPreservesExistingQueryParams(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("existing"); got != "value" {
			t.Errorf("existing param = %q, want value", got)
		}
		if got := r.URL.Query().Get("name"); got != "Paris" {
			t.Errorf("name param = %q, want Paris", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	desc := newTestDescriptor(server.URL+"?existing=value", "GET")
	mustExecute(t, NewHTTPExecutor(), desc, json.RawMessage(`{"name": "Paris"}`))
}

func TestHTTPExecutor_Execute_CustomHeaders(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer token123" {
			t.Errorf("Authorization = %q, want Bearer token123", auth)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "POST",
			Headers: map[string]string{"Authorization": "Bearer token123"},
		},
	}
	mustExecute(t, NewHTTPExecutor(), desc, json.RawMessage(`{}`))
}

func TestHTTPExecutor_Execute_HeadersFromEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-key")

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if apiKey := r.Header.Get("X-API-Key"); apiKey != "secret-key" {
			t.Errorf("X-API-Key = %q, want secret-key", apiKey)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "POST",
			HeadersFromEnv: []string{"X-API-Key=TEST_API_KEY"},
		},
	}
	mustExecute(t, NewHTTPExecutor(), desc, json.RawMessage(`{}`))
}

func TestHTTPExecutor_Execute_Redact(t *testing.T) {
	server := newTestServer(t, okHandler(`{"data": "public", "password": "secret123", "token": "abc"}`))

	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "GET",
			Redact: []string{"password", "token"},
		},
	}
	result := mustExecute(t, NewHTTPExecutor(), desc, nil)
	parsed := mustParseMap(t, result)

	if parsed["data"] != "public" {
		t.Errorf("data = %v, want public", parsed["data"])
	}
	if parsed["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED]", parsed["password"])
	}
}

func TestHTTPExecutor_Execute_NonJSONResponse(t *testing.T) {
	server := newTestServer(t, okHandler(`This is plain text`))
	result := mustExecute(t, NewHTTPExecutor(), newTestDescriptor(server.URL, "GET"), nil)

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}
	if parsed["result"] != "This is plain text" {
		t.Errorf("result = %q, want This is plain text", parsed["result"])
	}
}

func TestHTTPExecutor_Execute_Timeout(t *testing.T) {
	server := newTestServer(t, okHandler(`{}`))
	desc := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET", TimeoutMs: 5000},
	}
	mustExecute(t, NewHTTPExecutor(), desc, nil)
}

func TestHTTPExecutor_ResponseMapper_Default(t *testing.T) {
	mapper := NewHTTPExecutor().responseMapper()
	if _, ok := mapper.(*DefaultResponseMapper); !ok {
		t.Errorf("expected *DefaultResponseMapper, got %T", mapper)
	}
}

func TestHTTPExecutor_ResponseMapper_Custom(t *testing.T) {
	executor := NewHTTPExecutor()
	custom := &DefaultResponseMapper{}
	executor.ResponseMapper = custom
	if executor.responseMapper() != custom {
		t.Error("responseMapper() did not return custom mapper")
	}
}

func TestHTTPExecutor_RequestMapper_Custom(t *testing.T) {
	executor := NewHTTPExecutor()
	custom := &DefaultRequestMapper{}
	executor.RequestMapper = custom
	if executor.requestMapper() != custom {
		t.Error("requestMapper() did not return custom mapper")
	}
}

func TestHTTPExecutor_ApplyResponseMapping(t *testing.T) {
	input := json.RawMessage(`{"results": [{"name": "a"}, {"name": "b"}], "meta": {"total": 2}}`)
	cfg := &HTTPConfig{Response: &ResponseMapping{BodyMapping: "results[*].name"}}

	got, err := NewHTTPExecutor().applyResponseMapping(input, cfg)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	if err := json.Unmarshal(got, &names); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("got %v", names)
	}
}

func TestHTTPExecutor_ApplyResponseMapping_NoConfig(t *testing.T) {
	input := json.RawMessage(`{"data": "value"}`)
	got, err := NewHTTPExecutor().applyResponseMapping(input, &HTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Errorf("expected passthrough, got %s", got)
	}
}

func TestHTTPExecutor_Execute_WithResponseMapping(t *testing.T) {
	server := newTestServer(t, jsonHandler(`{"results": [{"name": "London", "pop": 9000000}], "total": 1}`))
	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "GET",
			Response: &ResponseMapping{BodyMapping: "results[0].name"},
		},
	}

	result := mustExecute(t, NewHTTPExecutor(), desc, nil)
	var name string
	if err := json.Unmarshal(result, &name); err != nil {
		t.Fatal(err)
	}
	if name != "London" {
		t.Errorf("got %q, want London", name)
	}
}

func TestHTTPExecutor_ExecuteMultimodal_NotEnabled(t *testing.T) {
	server := newTestServer(t, jsonHandler(`{"data": "value"}`))

	result, parts, err := NewHTTPExecutor().ExecuteMultimodal(
		context.Background(), newTestDescriptor(server.URL, "GET"), nil,
	)
	if err != nil {
		t.Fatalf("ExecuteMultimodal() error = %v", err)
	}
	if parts != nil {
		t.Error("expected nil parts when multimodal not enabled")
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestHTTPExecutor_ExecuteMultimodal_NoConfig(t *testing.T) {
	_, _, err := NewHTTPExecutor().ExecuteMultimodal(
		context.Background(), &ToolDescriptor{Name: "test_tool"}, nil,
	)
	if err == nil {
		t.Error("expected error for missing HTTPConfig")
	}
}

func TestHTTPExecutor_ExecuteMultimodal_BinaryResponse(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG magic bytes
	})

	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "GET",
			Multimodal: &MultimodalConfig{Enabled: true, AcceptTypes: []string{"image/png"}},
		},
	}

	result, parts, err := NewHTTPExecutor().ExecuteMultimodal(context.Background(), desc, nil)
	if err != nil {
		t.Fatalf("ExecuteMultimodal() error = %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected content parts for binary response")
	}
	if parts[0].Type != "image" {
		t.Errorf("part type = %q, want image", parts[0].Type)
	}
	if result == nil {
		t.Error("expected non-nil JSON result alongside parts")
	}
}

func TestHTTPExecutor_ExecuteMultimodal_JSONFallback(t *testing.T) {
	server := newTestServer(t, jsonHandler(`{"status": "ok"}`))
	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "GET",
			Multimodal: &MultimodalConfig{Enabled: true},
		},
	}

	result, parts, err := NewHTTPExecutor().ExecuteMultimodal(context.Background(), desc, nil)
	if err != nil {
		t.Fatalf("ExecuteMultimodal() error = %v", err)
	}
	if parts != nil {
		t.Error("expected nil parts for JSON response")
	}
	parsed := mustParseMap(t, result)
	if parsed["status"] != "ok" {
		t.Errorf("status = %v, want ok", parsed["status"])
	}
}

func TestHTTPExecutor_ApplyMappedHeaders(t *testing.T) {
	executor := NewHTTPExecutor()
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	templates := map[string]string{"Authorization": "Bearer {{.token}}"}
	args := map[string]any{"token": "secret"}

	if err := executor.applyMappedHeaders(req, &DefaultRequestMapper{}, templates, args); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization = %q, want Bearer secret", got)
	}
}

func TestHTTPExecutor_ApplyMappedHeaders_Empty(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	if err := NewHTTPExecutor().applyMappedHeaders(req, &DefaultRequestMapper{}, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPExecutor_AggregateResponseSizeLimit(t *testing.T) {
	payload := strings.Repeat("x", 1024)
	server := newTestServer(t, jsonHandler(`{"data":"`+payload+`"}`))

	executor := NewHTTPExecutorWithMaxAggregate(2048)
	executor.client = server.Client()
	desc := newTestDescriptor(server.URL, "GET")

	// First call should succeed.
	mustExecute(t, executor, desc, nil)

	// Second call may or may not succeed depending on exact overhead.
	_, err := executor.Execute(context.Background(), desc, nil)
	if err != nil && !errors.Is(err, ErrAggregateResponseSizeExceeded) {
		t.Fatalf("second call: unexpected error type: %v", err)
	}

	// Third call should definitely fail with aggregate limit.
	_, err = executor.Execute(context.Background(), desc, nil)
	if err == nil {
		t.Fatal("expected aggregate size error, got nil")
	}
	if !errors.Is(err, ErrAggregateResponseSizeExceeded) {
		t.Errorf("expected ErrAggregateResponseSizeExceeded, got: %v", err)
	}
}

func TestHTTPExecutor_AggregateResponseSize_Tracking(t *testing.T) {
	server := newTestServer(t, okHandler(`{"ok":true}`))
	executor := NewHTTPExecutor()
	executor.client = server.Client()

	mustExecute(t, executor, newTestDescriptor(server.URL, "GET"), nil)
	if got := executor.AggregateResponseSize(); got == 0 {
		t.Error("AggregateResponseSize() should be > 0 after a call")
	}

	executor.ResetAggregateSize()
	if got := executor.AggregateResponseSize(); got != 0 {
		t.Errorf("AggregateResponseSize() after reset = %d, want 0", got)
	}
}

func TestHTTPExecutor_AggregateDisabled(t *testing.T) {
	server := newTestServer(t, okHandler(`{"ok":true}`))
	executor := NewHTTPExecutorWithMaxAggregate(0)
	executor.client = server.Client()
	desc := newTestDescriptor(server.URL, "GET")

	for range 10 {
		mustExecute(t, executor, desc, nil)
	}
}

func TestHTTPExecutor_EmptyArgs(t *testing.T) {
	server := newTestServer(t, okHandler(`{}`))
	executor := NewHTTPExecutor()
	desc := newTestDescriptor(server.URL, "POST")

	mustExecute(t, executor, desc, json.RawMessage(`null`))
	mustExecute(t, executor, desc, json.RawMessage(`{}`))
}

func TestFormatQueryValue(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{float64(42), "42"},
		{float64(3.14), "3.14"},
		{true, "true"},
		{nil, ""},
		{[]any{"a", "b"}, `["a","b"]`},
	}
	for _, tt := range tests {
		if got := formatQueryValue(tt.input); got != tt.want {
			t.Errorf("formatQueryValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRedactFields(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		fields []string
		want   map[string]any
	}{
		{"redact single field", `{"public": "data", "secret": "hidden"}`, []string{"secret"}, map[string]any{"public": "data", "secret": "[REDACTED]"}},
		{"redact multiple fields", `{"public": "data", "password": "123", "token": "abc"}`, []string{"password", "token"}, map[string]any{"public": "data", "password": "[REDACTED]", "token": "[REDACTED]"}},
		{"field not present", `{"public": "data"}`, []string{"secret"}, map[string]any{"public": "data"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParseMap(t, RedactFields([]byte(tt.input), tt.fields))
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("got[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestRedactFields_InvalidJSON(t *testing.T) {
	input := []byte(`not json`)
	if got := RedactFields(input, []string{"field"}); string(got) != string(input) {
		t.Errorf("result = %q, want %q", string(got), string(input))
	}
}

func TestNewHTTPExecutorWithClient(t *testing.T) {
	client := &http.Client{}
	if NewHTTPExecutorWithClient(client).client != client {
		t.Error("NewHTTPExecutorWithClient did not set custom client")
	}
}

func TestNewHTTPExecutorWithMaxAggregate(t *testing.T) {
	if NewHTTPExecutorWithMaxAggregate(100).maxAggregateSize != 100 {
		t.Error("maxAggregateSize not set correctly")
	}
}

func TestHTTPExecutor_Execute_HeadersFromEnv_Legacy(t *testing.T) {
	if err := os.Setenv("TEST_LEGACY_KEY", "legacy-value"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TEST_LEGACY_KEY")

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Legacy"); got != "legacy-value" {
			t.Errorf("X-Legacy = %q, want legacy-value", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	desc := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL: server.URL, Method: "GET",
			HeadersFromEnv: []string{"X-Legacy=TEST_LEGACY_KEY"},
		},
	}
	mustExecute(t, NewHTTPExecutor(), desc, nil)
}
