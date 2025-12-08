package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestHTTPExecutor_Name(t *testing.T) {
	executor := NewHTTPExecutor()
	if got := executor.Name(); got != "http" {
		t.Errorf("Name() = %q, want %q", got, "http")
	}
}

func TestHTTPExecutor_Execute_Success(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Method = %q, want %q", r.Method, "POST")
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}

		// Return a JSON response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success", "value": 42}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "POST",
		},
	}

	args := json.RawMessage(`{"query": "test"}`)
	result, err := executor.Execute(descriptor, args)
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
	if parsed["value"] != float64(42) {
		t.Errorf("value = %v, want %v", parsed["value"], 42)
	}
}

func TestHTTPExecutor_Execute_NoConfig(t *testing.T) {
	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		// No HTTPConfig
	}

	_, err := executor.Execute(descriptor, json.RawMessage(`{}`))
	if err == nil {
		t.Error("Execute() expected error for missing HTTPConfig")
	}
}

func TestHTTPExecutor_Execute_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "POST",
		},
	}

	_, err := executor.Execute(descriptor, json.RawMessage(`{}`))
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
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "GET",
		},
	}

	_, err := executor.Execute(descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer token123" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer token123")
		}
		if custom := r.Header.Get("X-Custom-Header"); custom != "custom-value" {
			t.Errorf("X-Custom-Header = %q, want %q", custom, "custom-value")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "POST",
			Headers: map[string]string{
				"Authorization":   "Bearer token123",
				"X-Custom-Header": "custom-value",
			},
		},
	}

	_, err := executor.Execute(descriptor, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_HeadersFromEnv(t *testing.T) {
	// Set environment variable
	os.Setenv("TEST_API_KEY", "secret-key")
	defer os.Unsetenv("TEST_API_KEY")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiKey := r.Header.Get("X-API-Key"); apiKey != "secret-key" {
			t.Errorf("X-API-Key = %q, want %q", apiKey, "secret-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:            server.URL,
			Method:         "POST",
			HeadersFromEnv: []string{"X-API-Key=TEST_API_KEY"},
		},
	}

	_, err := executor.Execute(descriptor, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestHTTPExecutor_Execute_Redact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": "public", "password": "secret123", "token": "abc"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "GET",
			Redact: []string{"password", "token"},
		},
	}

	result, err := executor.Execute(descriptor, nil)
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
	if parsed["token"] != "[REDACTED]" {
		t.Errorf("token = %v, want %q", parsed["token"], "[REDACTED]")
	}
}

func TestHTTPExecutor_Execute_NonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`This is plain text`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "GET",
		},
	}

	result, err := executor.Execute(descriptor, nil)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response - but we use a short timeout in config
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:       server.URL,
			Method:    "GET",
			TimeoutMs: 5000, // 5 seconds
		},
	}

	// This should succeed since server responds quickly
	_, err := executor.Execute(descriptor, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestNewHTTPExecutorWithClient(t *testing.T) {
	customClient := &http.Client{}
	executor := NewHTTPExecutorWithClient(customClient)
	if executor.client != customClient {
		t.Error("NewHTTPExecutorWithClient did not set custom client")
	}
}

// HTTPToolConfig tests

func TestHTTPToolConfig_Options(t *testing.T) {
	cfg := NewHTTPToolConfig("https://api.example.com/endpoint",
		WithMethod("PUT"),
		WithHeader("Authorization", "Bearer token"),
		WithHeader("X-Custom", "value"),
		WithHeaderFromEnv("X-API-Key=API_KEY_ENV"),
		WithTimeout(5000),
		WithRedact("secret", "password"),
	)

	if cfg.url != "https://api.example.com/endpoint" {
		t.Errorf("url = %q, want %q", cfg.url, "https://api.example.com/endpoint")
	}
	if cfg.method != "PUT" {
		t.Errorf("method = %q, want %q", cfg.method, "PUT")
	}
	if cfg.headers["Authorization"] != "Bearer token" {
		t.Errorf("headers[Authorization] = %q, want %q", cfg.headers["Authorization"], "Bearer token")
	}
	if cfg.timeoutMs != 5000 {
		t.Errorf("timeoutMs = %d, want %d", cfg.timeoutMs, 5000)
	}
	if len(cfg.redact) != 2 {
		t.Errorf("len(redact) = %d, want %d", len(cfg.redact), 2)
	}
}

func TestHTTPToolConfig_ToDescriptorConfig(t *testing.T) {
	cfg := NewHTTPToolConfig("https://api.example.com",
		WithMethod("POST"),
		WithHeader("Auth", "Bearer x"),
		WithTimeout(1000),
	)

	desc := cfg.ToDescriptorConfig()
	if desc.URL != "https://api.example.com" {
		t.Errorf("URL = %q, want %q", desc.URL, "https://api.example.com")
	}
	if desc.Method != "POST" {
		t.Errorf("Method = %q, want %q", desc.Method, "POST")
	}
	if desc.TimeoutMs != 1000 {
		t.Errorf("TimeoutMs = %d, want %d", desc.TimeoutMs, 1000)
	}
}

func TestHTTPToolConfig_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"processed": true}`))
	}))
	defer server.Close()

	cfg := NewHTTPToolConfig(server.URL,
		WithMethod("POST"),
	)

	handler := cfg.Handler()
	result, err := handler(map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if resultMap["processed"] != true {
		t.Errorf("processed = %v, want true", resultMap["processed"])
	}
}

func TestHTTPToolConfig_Handler_WithTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		if body["transformed"] != true {
			t.Errorf("transformed = %v, want true", body["transformed"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := NewHTTPToolConfig(server.URL,
		WithTransform(func(args map[string]any) (map[string]any, error) {
			args["transformed"] = true
			return args, nil
		}),
	)

	handler := cfg.Handler()
	_, err := handler(map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
}

func TestHTTPToolConfig_Handler_WithPostProcess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"original": true}`))
	}))
	defer server.Close()

	cfg := NewHTTPToolConfig(server.URL,
		WithPostProcess(func(resp []byte) ([]byte, error) {
			return []byte(`{"postProcessed": true}`), nil
		}),
	)

	handler := cfg.Handler()
	result, err := handler(map[string]any{})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if resultMap["postProcessed"] != true {
		t.Errorf("postProcessed = %v, want true", resultMap["postProcessed"])
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
			result := redactFields([]byte(tt.input), tt.fields)
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
	result := redactFields(input, []string{"field"})
	if string(result) != string(input) {
		t.Errorf("result = %q, want %q", string(result), string(input))
	}
}

func TestWithPreRequest(t *testing.T) {
	cfg := &HTTPToolConfig{}
	opt := WithPreRequest(func(req *http.Request) error {
		req.Header.Set("X-Test", "value")
		return nil
	})
	opt(cfg)
	if cfg.preRequest == nil {
		t.Error("preRequest should be set")
	}
}

func TestDefaultMethod(t *testing.T) {
	cfg := NewHTTPToolConfig("https://example.com")
	if cfg.method != "POST" {
		t.Errorf("default method = %q, want %q", cfg.method, "POST")
	}
}

func TestEmptyArgs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor()
	descriptor := &tools.ToolDescriptor{
		Name: "test_tool",
		HTTPConfig: &tools.HTTPConfig{
			URL:    server.URL,
			Method: "POST",
		},
	}

	// Test with null args
	_, err := executor.Execute(descriptor, json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("Execute() with null error = %v", err)
	}

	// Test with empty object
	_, err = executor.Execute(descriptor, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() with empty object error = %v", err)
	}
}
