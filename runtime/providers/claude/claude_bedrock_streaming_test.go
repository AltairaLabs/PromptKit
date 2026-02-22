package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
)

// encodeBedrockStreamEvent creates a single binary event-stream frame containing
// a base64-encoded Claude JSON event, matching Bedrock's invoke-with-response-stream format.
func encodeBedrockStreamEvent(t *testing.T, data string) []byte {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	payload := []byte(`{"bytes":"` + encoded + `"}`)

	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: ":event-type", Value: eventstream.StringValue("chunk")},
			{Name: ":content-type", Value: eventstream.StringValue("application/json")},
			{Name: ":message-type", Value: eventstream.StringValue("event")},
		},
		Payload: payload,
	}

	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	if err := encoder.Encode(&buf, msg); err != nil {
		t.Fatalf("failed to encode event: %v", err)
	}
	return buf.Bytes()
}

// buildBedrockStream creates a binary event-stream body from multiple Claude JSON events.
func buildBedrockStream(t *testing.T, events []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, event := range events {
		buf.Write(encodeBedrockStreamEvent(t, event))
	}
	return buf.Bytes()
}

// newBedrockStreamingTestProvider creates a test provider with a mock server
// that returns a binary event-stream response.
func newBedrockStreamingTestProvider(t *testing.T, streamBody []byte) *Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(streamBody)
	}))
	t.Cleanup(server.Close)

	return &Provider{
		BaseProvider: providers.NewBaseProvider("test-bedrock", false, server.Client()),
		model:        "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:      server.URL,
		apiKey:       "test-key",
		platform:     "bedrock",
		defaults:     providers.ProviderDefaults{MaxTokens: 1024},
	}
}

func TestBedrockPredictStream(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" from Bedrock"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	}

	provider := newBedrockStreamingTestProvider(t, buildBedrockStream(t, events))

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	ch, err := provider.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// Should get text delta chunks plus a final message_stop chunk
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks (deltas + final), got %d", len(chunks))
	}

	// Last chunk should have finish reason and accumulated content
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.FinishReason == nil {
		t.Fatal("expected finish reason on last chunk")
	}
	if *lastChunk.FinishReason != "end_turn" {
		t.Errorf("expected finish reason 'end_turn', got %q", *lastChunk.FinishReason)
	}
	if lastChunk.Content != "Hello from Bedrock" {
		t.Errorf("expected accumulated content 'Hello from Bedrock', got %q", lastChunk.Content)
	}
}

func TestBedrockPredictStreamWithTools(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":15}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me search"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"search"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"test\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}`,
		`{"type":"message_stop"}`,
	}

	streamBody := buildBedrockStream(t, events)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(streamBody)
	}))
	t.Cleanup(server.Close)

	baseProvider := &Provider{
		BaseProvider: providers.NewBaseProvider("test-bedrock", false, server.Client()),
		model:        "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:      server.URL,
		apiKey:       "test-key",
		platform:     "bedrock",
		defaults:     providers.ProviderDefaults{MaxTokens: 1024},
	}
	toolProvider := &ToolProvider{Provider: baseProvider}

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "search for test"}},
		MaxTokens: 100,
	}

	ch, err := toolProvider.PredictStreamWithTools(context.Background(), req, nil, "auto")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// Last chunk should have tool calls and finish reason
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.FinishReason == nil || *lastChunk.FinishReason != "tool_use" {
		t.Errorf("expected finish reason 'tool_use', got %v", lastChunk.FinishReason)
	}
	if len(lastChunk.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(lastChunk.ToolCalls))
	}
	if lastChunk.ToolCalls[0].Name != "search" {
		t.Errorf("expected tool name 'search', got %q", lastChunk.ToolCalls[0].Name)
	}
}

func TestBedrockPredictMultimodalStream(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":12}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Image "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"analysis"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		`{"type":"message_stop"}`,
	}

	provider := newBedrockStreamingTestProvider(t, buildBedrockStream(t, events))

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "describe image"}},
		MaxTokens: 100,
	}

	ch, err := provider.PredictMultimodalStream(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []providers.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	lastChunk := chunks[len(chunks)-1]
	if lastChunk.FinishReason == nil || *lastChunk.FinishReason != "end_turn" {
		t.Errorf("expected finish reason 'end_turn', got %v", lastChunk.FinishReason)
	}
	if lastChunk.Content != "Image analysis" {
		t.Errorf("expected content 'Image analysis', got %q", lastChunk.Content)
	}
}

func TestBedrockStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"invalid request"}`))
	}))
	t.Cleanup(server.Close)

	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test-bedrock", false, server.Client()),
		model:        "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:      server.URL,
		apiKey:       "test-key",
		platform:     "bedrock",
		defaults:     providers.ProviderDefaults{MaxTokens: 1024},
	}

	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}

	_, err := provider.PredictStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestBedrockStreamURLs(t *testing.T) {
	provider := &Provider{
		baseURL:  "https://bedrock-runtime.us-east-1.amazonaws.com",
		model:    "anthropic.claude-3-5-haiku-20241022-v1:0",
		platform: "bedrock",
	}

	invokeURL := provider.messagesURL()
	streamURL := provider.messagesStreamURL()

	expectedInvoke := "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke"
	expectedStream := "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-haiku-20241022-v1:0/invoke-with-response-stream"

	if invokeURL != expectedInvoke {
		t.Errorf("expected invoke URL %q, got %q", expectedInvoke, invokeURL)
	}
	if streamURL != expectedStream {
		t.Errorf("expected stream URL %q, got %q", expectedStream, streamURL)
	}

	// Non-Bedrock provider should return the same URL for both
	directProvider := &Provider{
		baseURL: "https://api.anthropic.com/v1",
		model:   "claude-3-5-sonnet-20241022",
	}
	if directProvider.messagesURL() != directProvider.messagesStreamURL() {
		t.Error("expected same URL for non-Bedrock provider")
	}
}

func TestBedrockMarshalStreamingRequest(t *testing.T) {
	provider := &Provider{
		model:    "anthropic.claude-3-5-haiku-20241022-v1:0",
		platform: "bedrock",
	}

	reqMap := map[string]interface{}{
		"model":       provider.model,
		"max_tokens":  1024,
		"messages":    []map[string]string{{"role": "user", "content": "hi"}},
		"stream":      true,
		"temperature": 0.7,
	}

	body, err := provider.marshalBedrockStreamingRequest(reqMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have anthropic_version
	if !bytes.Contains(body, []byte(`"anthropic_version"`)) {
		t.Error("expected anthropic_version in request body")
	}
	// Should NOT have model (Bedrock uses URL path)
	if bytes.Contains(body, []byte(`"model"`)) {
		t.Error("expected model to be removed from request body")
	}
	// Should NOT have stream (Bedrock uses different URL path for streaming)
	if bytes.Contains(body, []byte(`"stream"`)) {
		t.Error("expected stream to be removed from request body")
	}
}

func TestMakeBedrockStreamingRequest_Accept(t *testing.T) {
	var receivedAcceptHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAcceptHeader = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	provider := &Provider{
		BaseProvider: providers.NewBaseProvider("test-bedrock", false, server.Client()),
		model:        "anthropic.claude-3-5-haiku-20241022-v1:0",
		baseURL:      server.URL,
		platform:     "bedrock",
	}

	body, scanner, err := provider.makeBedrockStreamingRequest(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != nil {
		_ = body.(io.Closer).Close()
	}

	if receivedAcceptHeader != "application/vnd.amazon.eventstream" {
		t.Errorf("expected Accept header 'application/vnd.amazon.eventstream', got %q", receivedAcceptHeader)
	}
	if scanner == nil {
		t.Error("expected non-nil scanner")
	}
}
