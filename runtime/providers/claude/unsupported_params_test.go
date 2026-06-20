package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// newParamCaptureServer returns a test server that decodes each request body
// into *captured. When sse is true it replies with a minimal Claude SSE stream
// so PredictStream completes without error; otherwise a single JSON message.
func newParamCaptureServer(t *testing.T, captured *map[string]interface{}, sse bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(captured)
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: message_start\n" +
				`data: {"type":"message_start","message":{"usage":{"input_tokens":1}}}` + "\n\n"))
			_, _ = w.Write([]byte("event: content_block_delta\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` + "\n\n"))
			_, _ = w.Write([]byte("event: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "msg_test",
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]string{{"type": "text", "text": "hi"}},
			"model":   "claude-opus-4-8",
			"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func newClaudeSpec(baseURL string, unsupported []string) providers.ProviderSpec {
	return providers.ProviderSpec{
		ID:      "test-opus48",
		Type:    "claude",
		Model:   "claude-opus-4-8",
		BaseURL: baseURL,
		Defaults: providers.ProviderDefaults{
			Temperature: 0.1,
			MaxTokens:   100,
			TopP:        1.0,
		},
		UnsupportedParams: unsupported,
	}
}

// TestUnsupportedParams_TemperatureOmitted verifies that when a Claude provider
// is configured with "temperature" in UnsupportedParams, the parameter is
// dropped from the request across all three direct-API request builders:
// non-streaming Predict, streaming PredictStream, and the tool request builder.
// Claude 4.7+ models reject temperature, so sending it returns a 400.
func TestUnsupportedParams_TemperatureOmitted(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	t.Run("non-streaming Predict", func(t *testing.T) {
		var captured map[string]interface{}
		server := newParamCaptureServer(t, &captured, false)

		provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, []string{"temperature", "top_p"}))
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}

		req := providers.PredictionRequest{
			Messages:    []types.Message{{Role: "user", Content: "hi"}},
			Temperature: 0.1,
			MaxTokens:   100,
		}
		if _, err := provider.Predict(context.Background(), req); err != nil {
			t.Fatalf("Predict: %v", err)
		}
		if _, ok := captured["temperature"]; ok {
			t.Errorf("temperature should be omitted, got request: %v", captured)
		}
	})

	t.Run("streaming PredictStream", func(t *testing.T) {
		var captured map[string]interface{}
		server := newParamCaptureServer(t, &captured, true)

		provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, []string{"temperature", "top_p"}))
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}

		req := providers.PredictionRequest{
			Messages:    []types.Message{{Role: "user", Content: "hi"}},
			Temperature: 0.1,
			MaxTokens:   100,
		}
		ch, err := provider.PredictStream(context.Background(), req)
		if err != nil {
			t.Fatalf("PredictStream: %v", err)
		}
		for range ch { //nolint:revive // drain the stream so the request completes
		}
		if _, ok := captured["temperature"]; ok {
			t.Errorf("temperature should be omitted, got request: %v", captured)
		}
	})

	t.Run("tool request builder", func(t *testing.T) {
		provider, err := providers.CreateProviderFromSpec(newClaudeSpec("https://example.invalid", []string{"temperature", "top_p"}))
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}
		tp, ok := provider.(*ToolProvider)
		if !ok {
			t.Fatalf("expected *ToolProvider, got %T", provider)
		}

		request := tp.buildToolRequest(providers.PredictionRequest{
			Messages:    []types.Message{{Role: "user", Content: "hi"}},
			Temperature: 0.1,
			MaxTokens:   100,
		}, nil, "")
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if bytes.Contains(body, []byte(`"temperature"`)) {
			t.Errorf("temperature should be omitted from tool request, got: %s", body)
		}
	})
}

// TestDeclaredCapabilities_OverrideDefaults verifies that a declared capability
// list is authoritative for Claude's multimodal support: declaring audio turns
// it on (default is off), and omitting vision turns images off (default is on).
// With no declaration, Claude's built-in defaults apply.
func TestDeclaredCapabilities_OverrideDefaults(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	build := func(caps []string) providers.MultimodalCapabilityProvider {
		spec := providers.ProviderSpec{
			ID: "test-caps", Type: "claude", Model: "claude-opus-4-8",
			BaseURL: "https://example.invalid", Capabilities: caps,
		}
		provider, err := providers.CreateProviderFromSpec(spec)
		if err != nil {
			t.Fatalf("CreateProviderFromSpec: %v", err)
		}
		return provider.(providers.MultimodalCapabilityProvider)
	}

	declared := build([]string{"text", "audio"}).GetMultimodalCapabilities()
	if !declared.SupportsAudio {
		t.Error("declared audio should enable audio support")
	}
	if declared.SupportsImages {
		t.Error("omitting vision should disable image support")
	}

	defaults := build(nil).GetMultimodalCapabilities()
	if defaults.SupportsAudio {
		t.Error("default Claude should not support audio")
	}
	if !defaults.SupportsImages {
		t.Error("default Claude should support images")
	}
}

// TestUnsupportedParams_TemperatureSentByDefault is the regression guard: with
// no UnsupportedParams configured, temperature is still sent as before.
func TestUnsupportedParams_TemperatureSentByDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	var captured map[string]interface{}
	server := newParamCaptureServer(t, &captured, false)

	provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	req := providers.PredictionRequest{
		Messages:    []types.Message{{Role: "user", Content: "hi"}},
		Temperature: 0.1,
		MaxTokens:   100,
	}
	if _, err := provider.Predict(context.Background(), req); err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if _, ok := captured["temperature"]; !ok {
		t.Errorf("temperature should be sent by default, got request: %v", captured)
	}
}
