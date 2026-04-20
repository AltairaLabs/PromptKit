package claude

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// buildVertexSSEStream returns the body of a Vertex Anthropic-partner SSE
// stream: each event is a single JSON object preceded by `data: `, terminated
// by a blank line (matching Anthropic's SSE format that Vertex re-emits).
func buildVertexSSEStream(events []string) string {
	var sb strings.Builder
	for _, ev := range events {
		sb.WriteString("data: ")
		sb.WriteString(ev)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// vertexStreamCapture holds the request and body the test server saw.
// Returned by newVertexStreamingTestProvider so tests can assert on the
// wire format after the stream has been consumed.
type vertexStreamCapture struct {
	req  *http.Request
	body string
}

// newVertexStreamingTestProvider stands up an httptest server that answers
// :streamRawPredict with a canned SSE body, and returns a Provider wired to
// it together with a capture struct populated when the request arrives.
func newVertexStreamingTestProvider(
	t *testing.T, sseBody string,
) (*Provider, *vertexStreamCapture) {
	t.Helper()

	cap := &vertexStreamCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.req = r.Clone(context.Background())
		body, _ := io.ReadAll(r.Body)
		cap.body = string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
	t.Cleanup(server.Close)

	p := &Provider{
		BaseProvider: providers.NewBaseProvider("test-vertex", false, server.Client()),
		model:        "claude-haiku-4-5@20251001",
		baseURL:      server.URL, // tests do not exercise URL derivation here
		platform:     vertexPlatform,
		credential:   &mockBearerCredential{},
		defaults:     providers.ProviderDefaults{MaxTokens: 256},
	}
	return p, cap
}

func TestVertexPredictStream_SendsRawPredictAndPartnerBody(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	}

	p, cap := newVertexStreamingTestProvider(t, buildVertexSSEStream(events))

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	}
	ch, err := p.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictStream returned error: %v", err)
	}

	var (
		text          string
		stopChunkSeen bool
	)
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		if chunk.Delta != "" {
			text += chunk.Delta
		}
		if chunk.FinishReason != nil {
			stopChunkSeen = true
		}
	}

	if text != "Hello" {
		t.Errorf("accumulated text = %q, want %q", text, "Hello")
	}
	if !stopChunkSeen {
		t.Error("expected a final chunk with finish reason")
	}

	if cap.req == nil {
		t.Fatal("test server never received a request")
	}

	// Verify the wire format the provider sent.
	if got := cap.req.URL.Path; !strings.HasSuffix(got, "/claude-haiku-4-5@20251001:streamRawPredict") {
		t.Errorf("URL path = %q, want suffix /<model>:streamRawPredict", got)
	}
	if got := cap.req.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
	if h := cap.req.Header.Get(anthropicVersionKey); h != "" {
		t.Errorf("Vertex must not set %s header (version is in body), got %q", anthropicVersionKey, h)
	}
	if h := cap.req.Header.Get(apiKeyHeader); h != "" {
		t.Errorf("Vertex must not set %s header, got %q", apiKeyHeader, h)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(cap.body), &body); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if body[bedrockVersionBodyKey] != vertexVersionValue {
		t.Errorf("anthropic_version = %v, want %q", body[bedrockVersionBodyKey], vertexVersionValue)
	}
	if _, hasModel := body["model"]; hasModel {
		t.Error("Vertex body must not include `model` field")
	}
	if _, hasStream := body["stream"]; hasStream {
		t.Error("Vertex body must not include `stream` field (URL action signals streaming)")
	}
}

func TestVertexPredictStream_AppliesCredential(t *testing.T) {
	cred := &mockBearerCredential{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	t.Cleanup(server.Close)

	p := &Provider{
		BaseProvider: providers.NewBaseProvider("test-vertex", false, server.Client()),
		model:        "claude-haiku-4-5@20251001",
		baseURL:      server.URL,
		platform:     vertexPlatform,
		credential:   cred,
		defaults:     providers.ProviderDefaults{MaxTokens: 64},
	}

	ch, err := p.PredictStream(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	for range ch {
	}

	if !cred.applied {
		t.Error("Vertex stream path must call credential.Apply (Bearer)")
	}
}
