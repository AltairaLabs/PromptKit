package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// These golden tests pin the exact JSON request body produced by every Claude
// request-building path. #1379 consolidates the three independent builders
// (Predict's struct, PredictStream's map, buildToolRequest's map) plus the
// Bedrock/Vertex marshalers into one. The acceptance criterion is "no behavior
// change" — the produced bodies must be byte-identical before and after. Run
// with PROMPTKIT_UPDATE_GOLDEN=1 to (re)generate the fixtures on the
// pre-refactor code; the refactor must leave them untouched.

// goldenCaptureServer records the raw request body bytes (not a decoded map, so
// key order and formatting are preserved exactly) and replies with a minimal
// JSON message or SSE stream so the call completes without error.
func goldenCaptureServer(t *testing.T, dst *[]byte, sse bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*dst = body
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
		_ = json.NewEncoder(w).Encode(map[string]any{
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

// assertGolden compares got against testdata/golden/<name>.json. The body is
// canonicalized (object keys sorted) before comparison, so the assertion pins
// the *semantic* request body, not the incidental key order of whichever Go
// type produced it. That is the real "no behavior change" contract: #1379
// migrates the map-based stream/tool builders (sorted keys) onto the struct
// builder (field-order keys), and the wire body must stay semantically
// identical even though raw key order shifts. PROMPTKIT_UPDATE_GOLDEN=1
// rewrites the fixture.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	var canonical any
	if err := json.Unmarshal(got, &canonical); err != nil {
		t.Fatalf("captured body is not valid JSON: %v\nbody: %s", err, got)
	}
	// Marshal of map[string]interface{} sorts keys, giving a canonical form.
	sorted, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("re-marshal canonical body: %v", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, sorted, "", "  "); err != nil {
		t.Fatalf("indent canonical body: %v", err)
	}
	pretty.WriteByte('\n')
	path := filepath.Join("testdata", "golden", name+".json")
	if os.Getenv("PROMPTKIT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with PROMPTKIT_UPDATE_GOLDEN=1 to create)", path, err)
	}
	if !bytes.Equal(want, pretty.Bytes()) {
		t.Errorf("request body for %s changed.\n--- want ---\n%s\n--- got ---\n%s", name, want, pretty.Bytes())
	}
}

// goldenSystem is long enough (>4096 chars) to trigger the system cache_control
// breakpoint on every path.
var goldenSystem = strings.Repeat("You are a meticulous assistant. ", 140)

// goldenLongUserContent is long enough (>8192 chars) to trigger the
// last-message cache_control breakpoint on the non-tool paths.
var goldenLongUserContent = strings.Repeat("Please help me with this task. ", 280)

func goldenJSONSchemaResponseFormat() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type:       providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}`),
	}
}

func goldenToolMessages() []types.Message {
	return []types.Message{
		{Role: "user", Content: goldenLongUserContent},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "t1", Name: "Bash", Args: json.RawMessage(`{"command":"ls"}`)}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "t1", Name: "Bash", Parts: []types.ContentPart{types.NewTextPart("file output")}}},
		{Role: "assistant", Content: "Continuing the work."},
	}
}

// When temperature resolves to 0 on a model that supports it, the unified
// builder OMITS temperature on every path (struct omitempty). This converges
// the previously divergent behavior — the map-based stream/tool builders used
// to send temperature:0 — onto Predict's omit-when-zero behavior (#1379, the
// "omit when zero" decision). Pinning it here guards against a regression that
// would silently re-introduce temperature:0 on the tool/stream paths.
func TestUnifiedBuilder_OmitsZeroTemperature(t *testing.T) {
	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "t", Type: "claude", Model: "claude-3-opus", BaseURL: "https://example.invalid",
		Defaults: providers.ProviderDefaults{MaxTokens: 100}, // Temperature defaults to 0
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := provider.(*ToolProvider)
	if !tp.paramSupported("temperature") {
		t.Fatal("precondition: claude-3-opus should support temperature")
	}
	req := providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100, // Temperature unset -> resolves to 0
	}
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "Bash", Description: "run", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	body, err := json.Marshal(tp.buildToolRequest(req, tools, ""))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(body, []byte(`"temperature"`)) {
		t.Errorf("expected temperature omitted when it resolves to 0; got: %s", body)
	}
}

func TestGolden_Predict_Direct(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var body []byte
	server := goldenCaptureServer(t, &body, false)
	provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	req := providers.PredictionRequest{
		System:         goldenSystem,
		Messages:       []types.Message{{Role: "user", Content: goldenLongUserContent}},
		Temperature:    0.1,
		MaxTokens:      100,
		ResponseFormat: goldenJSONSchemaResponseFormat(),
	}
	if _, err := provider.Predict(context.Background(), req); err != nil {
		t.Fatalf("Predict: %v", err)
	}
	assertGolden(t, "predict_direct", body)
}

func TestGolden_PredictStream_Direct(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var body []byte
	server := goldenCaptureServer(t, &body, true)
	provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	req := providers.PredictionRequest{
		System:         goldenSystem,
		Messages:       []types.Message{{Role: "user", Content: goldenLongUserContent}},
		Temperature:    0.1,
		MaxTokens:      100,
		ResponseFormat: goldenJSONSchemaResponseFormat(),
	}
	ch, err := provider.PredictStream(context.Background(), req)
	if err != nil {
		t.Fatalf("PredictStream: %v", err)
	}
	for range ch {
	}
	assertGolden(t, "predict_stream_direct", body)
}

func TestGolden_PredictWithTools_Direct(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var body []byte
	server := goldenCaptureServer(t, &body, false)
	provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := provider.(*ToolProvider)
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "Bash", Description: "run a shell command", InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)},
	})
	req := providers.PredictionRequest{
		System:    goldenSystem,
		Messages:  goldenToolMessages(),
		MaxTokens: 100,
	}
	if _, _, err := tp.PredictWithTools(context.Background(), req, tools, ""); err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	assertGolden(t, "predict_tools_direct", body)
}

// The partner-platform marshalers (Bedrock/Vertex) are layered transforms on
// top of the base body. These function-level goldens pin their output —
// including the existing drift that marshalBedrockRequest drops output_config
// and tools while marshalBedrockStreamingRequest preserves them. #1379 must
// preserve this exactly; fixing the drift is a separate, explicit change.

func goldenBedrockProvider(t *testing.T) *Provider {
	t.Helper()
	return NewProviderWithCredential(
		"id", "anthropic.claude-opus-4-8",
		"https://bedrock-runtime.us-west-2.amazonaws.com",
		providers.ProviderDefaults{}, false, nil,
		bedrockPlatform,
		&providers.PlatformConfig{Type: "bedrock", Region: "us-west-2"},
	)
}

func TestGolden_MarshalBedrockRequest(t *testing.T) {
	p := goldenBedrockProvider(t)
	req := &claudeRequest{
		Model:     "anthropic.claude-opus-4-8",
		MaxTokens: 100,
		Messages: []claudeMessage{{
			Role:    "user",
			Content: []claudeContentBlock{{Type: "text", Text: "hello"}},
		}},
		System:       []claudeContentBlock{{Type: "text", Text: "you are helpful"}},
		Temperature:  0.1,
		OutputConfig: outputConfigFor(goldenJSONSchemaResponseFormat()),
	}
	body, err := p.marshalBedrockRequest(req)
	if err != nil {
		t.Fatalf("marshalBedrockRequest: %v", err)
	}
	assertGolden(t, "marshal_bedrock_request", body)
}

func TestGolden_MarshalBedrockStreamingRequest(t *testing.T) {
	p := goldenBedrockProvider(t)
	cr := claudeRequest{
		Model:     "anthropic.claude-opus-4-8",
		MaxTokens: 100,
		Messages: []claudeMessage{{
			Role:    "user",
			Content: []claudeContentBlock{{Type: "text", Text: "hello"}},
		}},
		System:       []claudeContentBlock{{Type: "text", Text: "you are helpful"}},
		Temperature:  0.1,
		Stream:       true,
		OutputConfig: outputConfigFor(goldenJSONSchemaResponseFormat()),
	}
	body, err := p.marshalBedrockStreamingRequest(&cr)
	if err != nil {
		t.Fatalf("marshalBedrockStreamingRequest: %v", err)
	}
	assertGolden(t, "marshal_bedrock_streaming_request", body)
}

func TestGolden_PredictStreamWithTools_Direct(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	var body []byte
	server := goldenCaptureServer(t, &body, true)
	provider, err := providers.CreateProviderFromSpec(newClaudeSpec(server.URL, nil))
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	tp := provider.(*ToolProvider)
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "Bash", Description: "run a shell command", InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)},
	})
	req := providers.PredictionRequest{
		System:    goldenSystem,
		Messages:  goldenToolMessages(),
		MaxTokens: 100,
	}
	ch, err := tp.PredictStreamWithTools(context.Background(), req, tools, "")
	if err != nil {
		t.Fatalf("PredictStreamWithTools: %v", err)
	}
	for range ch {
	}
	assertGolden(t, "predict_stream_tools_direct", body)
}
