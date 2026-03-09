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

func TestHTTPExecutor_Name(t *testing.T) {
	executor := NewHTTPExecutor()
	if got := executor.Name(); got != "http" {
		t.Errorf("Name() = %q, want %q", got, "http")
	}
}

func TestHTTPExecutor_Execute_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Method = %q, want %q", r.Method, "POST")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success", "value": 42}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "POST",
		},
	}

	args := json.RawMessage(`{"query": "test"}`)
	result, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if parsed["result"] != "success" {
		t.Errorf("result = %v, want %q", parsed["result"], "success")
	}
}

func TestHTTPExecutor_Execute_NoConfig(t *testing.T) {
	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{Name: "test_tool"}

	_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	if err == nil {
		t.Error("Execute() expected error for missing HTTPConfig")
	}
}

func TestHTTPExecutor_Execute_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "POST"},
	}

	_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	if err == nil {
		t.Error("Execute() expected error for HTTP 400")
	}
}

func TestHTTPExecutor_Execute_GetMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Method = %q, want %q", r.Method, "GET")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET"},
	}

	_, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_GetWithQueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Method = %q, want %q", r.Method, "GET")
		}
		// Verify query parameters were set
		if got := r.URL.Query().Get("name"); got != "London" {
			t.Errorf("query param name = %q, want %q", got, "London")
		}
		if got := r.URL.Query().Get("count"); got != "5" {
			t.Errorf("query param count = %q, want %q", got, "5")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET"},
	}

	args := json.RawMessage(`{"name": "London", "count": 5}`)
	result, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}
}

func TestHTTPExecutor_Execute_GetPreservesExistingQueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("existing"); got != "value" {
			t.Errorf("existing param = %q, want %q", got, "value")
		}
		if got := r.URL.Query().Get("name"); got != "Paris" {
			t.Errorf("name param = %q, want %q", got, "Paris")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL + "?existing=value", Method: "GET"},
	}

	args := json.RawMessage(`{"name": "Paris"}`)
	_, err := executor.Execute(context.Background(), descriptor, args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer token123" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer token123")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "POST",
			Headers: map[string]string{
				"Authorization": "Bearer token123",
			},
		},
	}

	_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_HeadersFromEnv(t *testing.T) {
	t.Setenv("TEST_API_KEY", "secret-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey := r.Header.Get("X-API-Key"); apiKey != "secret-key" {
			t.Errorf("X-API-Key = %q, want %q", apiKey, "secret-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:            server.URL,
			Method:         "POST",
			HeadersFromEnv: []string{"X-API-Key=TEST_API_KEY"},
		},
	}

	_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_Redact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": "public", "password": "secret123", "token": "abc"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "GET",
			Redact: []string{"password", "token"},
		},
	}

	result, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if parsed["data"] != "public" {
		t.Errorf("data = %v, want %q", parsed["data"], "public")
	}
	if parsed["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want %q", parsed["password"], "[REDACTED]")
	}
}

func TestHTTPExecutor_Execute_NonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`This is plain text`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET"},
	}

	result, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if parsed["result"] != "This is plain text" {
		t.Errorf("result = %q, want %q", parsed["result"], "This is plain text")
	}
}

func TestHTTPExecutor_Execute_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:       server.URL,
			Method:    "GET",
			TimeoutMs: 5000,
		},
	}

	_, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_ResponseMapper_Default(t *testing.T) {
	executor := NewHTTPExecutor()
	mapper := executor.responseMapper()
	if mapper == nil {
		t.Fatal("responseMapper() returned nil")
	}
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
	executor := NewHTTPExecutor()
	input := json.RawMessage(`{"results": [{"name": "a"}, {"name": "b"}], "meta": {"total": 2}}`)
	cfg := &HTTPConfig{
		Response: &ResponseMapping{
			BodyMapping: "results[*].name",
		},
	}

	got, err := executor.applyResponseMapping(input, cfg)
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
	executor := NewHTTPExecutor()
	input := json.RawMessage(`{"data": "value"}`)

	got, err := executor.applyResponseMapping(input, &HTTPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Errorf("expected passthrough, got %s", got)
	}
}

func TestHTTPExecutor_Execute_WithResponseMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": [{"name": "London", "pop": 9000000}], "total": 1}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "GET",
			Response: &ResponseMapping{
				BodyMapping: "results[0].name",
			},
		},
	}

	result, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var name string
	if err := json.Unmarshal(result, &name); err != nil {
		t.Fatal(err)
	}
	if name != "London" {
		t.Errorf("got %q, want %q", name, "London")
	}
}

func TestHTTPExecutor_ExecuteMultimodal_NotEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": "value"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "GET",
		},
	}

	result, parts, err := executor.ExecuteMultimodal(context.Background(), descriptor, nil)
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
	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{Name: "test_tool"}

	_, _, err := executor.ExecuteMultimodal(context.Background(), descriptor, nil)
	if err == nil {
		t.Error("expected error for missing HTTPConfig")
	}
}

func TestHTTPExecutor_ExecuteMultimodal_BinaryResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG magic bytes
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "GET",
			Multimodal: &MultimodalConfig{
				Enabled:     true,
				AcceptTypes: []string{"image/png"},
			},
		},
	}

	result, parts, err := executor.ExecuteMultimodal(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("ExecuteMultimodal() error = %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("expected content parts for binary response")
	}
	if parts[0].Type != "image" {
		t.Errorf("part type = %q, want %q", parts[0].Type, "image")
	}
	if result == nil {
		t.Error("expected non-nil JSON result alongside parts")
	}
}

func TestHTTPExecutor_ExecuteMultimodal_JSONFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:    server.URL,
			Method: "GET",
			Multimodal: &MultimodalConfig{
				Enabled: true,
			},
		},
	}

	result, parts, err := executor.ExecuteMultimodal(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("ExecuteMultimodal() error = %v", err)
	}
	if parts != nil {
		t.Error("expected nil parts for JSON response")
	}

	var parsed map[string]any
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["status"] != "ok" {
		t.Errorf("status = %v, want %q", parsed["status"], "ok")
	}
}

func TestHTTPExecutor_ApplyMappedHeaders(t *testing.T) {
	executor := NewHTTPExecutor()
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	mapper := &DefaultRequestMapper{}
	templates := map[string]string{
		"Authorization": "Bearer {{.token}}",
	}
	args := map[string]any{"token": "secret"}

	err := executor.applyMappedHeaders(req, mapper, templates, args)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer secret")
	}
}

func TestHTTPExecutor_ApplyMappedHeaders_Empty(t *testing.T) {
	executor := NewHTTPExecutor()
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	mapper := &DefaultRequestMapper{}

	err := executor.applyMappedHeaders(req, mapper, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPExecutor_AggregateResponseSizeLimit(t *testing.T) {
	payload := strings.Repeat("x", 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"` + payload + `"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutorWithMaxAggregate(2048)
	executor.client = server.Client()

	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET"},
	}

	// First call should succeed.
	_, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	// Second call may or may not succeed depending on exact overhead.
	_, err = executor.Execute(context.Background(), descriptor, nil)
	if err != nil && !errors.Is(err, ErrAggregateResponseSizeExceeded) {
		t.Fatalf("second call: unexpected error type: %v", err)
	}

	// Third call should definitely fail with aggregate limit.
	_, err = executor.Execute(context.Background(), descriptor, nil)
	if err == nil {
		t.Fatal("expected aggregate size error, got nil")
	}
	if !errors.Is(err, ErrAggregateResponseSizeExceeded) {
		t.Errorf("expected ErrAggregateResponseSizeExceeded, got: %v", err)
	}
}

func TestHTTPExecutor_AggregateResponseSize_Tracking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	executor.client = server.Client()

	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET"},
	}

	_, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := executor.AggregateResponseSize(); got == 0 {
		t.Error("AggregateResponseSize() should be > 0 after a call")
	}

	executor.ResetAggregateSize()
	if got := executor.AggregateResponseSize(); got != 0 {
		t.Errorf("AggregateResponseSize() after reset = %d, want 0", got)
	}
}

func TestHTTPExecutor_AggregateDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutorWithMaxAggregate(0)
	executor.client = server.Client()

	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "GET"},
	}

	for range 10 {
		_, err := executor.Execute(context.Background(), descriptor, nil)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}
}

func TestHTTPExecutor_EmptyArgs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name:       "test_tool",
		HTTPConfig: &HTTPConfig{URL: server.URL, Method: "POST"},
	}

	_, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("Execute() with null error = %v", err)
	}

	_, err = executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() with empty object error = %v", err)
	}
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
		got := formatQueryValue(tt.input)
		if got != tt.want {
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
		{
			name:   "redact single field",
			input:  `{"public": "data", "secret": "hidden"}`,
			fields: []string{"secret"},
			want:   map[string]any{"public": "data", "secret": "[REDACTED]"},
		},
		{
			name:   "redact multiple fields",
			input:  `{"public": "data", "password": "123", "token": "abc"}`,
			fields: []string{"password", "token"},
			want:   map[string]any{"public": "data", "password": "[REDACTED]", "token": "[REDACTED]"},
		},
		{
			name:   "field not present",
			input:  `{"public": "data"}`,
			fields: []string{"secret"},
			want:   map[string]any{"public": "data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactFields([]byte(tt.input), tt.fields)
			var got map[string]any
			if err := json.Unmarshal(result, &got); err != nil {
				t.Fatalf("Failed to parse result: %v", err)
			}
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
	result := RedactFields(input, []string{"field"})
	if string(result) != string(input) {
		t.Errorf("result = %q, want %q", string(result), string(input))
	}
}

func TestNewHTTPExecutorWithClient(t *testing.T) {
	customClient := &http.Client{}
	executor := NewHTTPExecutorWithClient(customClient)
	if executor.client != customClient {
		t.Error("NewHTTPExecutorWithClient did not set custom client")
	}
}

func TestNewHTTPExecutorWithMaxAggregate(t *testing.T) {
	executor := NewHTTPExecutorWithMaxAggregate(100)
	if executor.maxAggregateSize != 100 {
		t.Errorf("maxAggregateSize = %d, want 100", executor.maxAggregateSize)
	}
}

func TestHTTPExecutor_Execute_HeadersFromEnv_Legacy(t *testing.T) {
	// Ensure the old os.Setenv approach still works
	if err := os.Setenv("TEST_LEGACY_KEY", "legacy-value"); err != nil {
		t.Fatal(err)
	}
	defer os.Unsetenv("TEST_LEGACY_KEY")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Legacy"); got != "legacy-value" {
			t.Errorf("X-Legacy = %q, want %q", got, "legacy-value")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &HTTPConfig{
			URL:            server.URL,
			Method:         "GET",
			HeadersFromEnv: []string{"X-Legacy=TEST_LEGACY_KEY"},
		},
	}

	_, err := executor.Execute(context.Background(), descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
