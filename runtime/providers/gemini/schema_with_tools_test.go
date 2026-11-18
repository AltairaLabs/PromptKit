package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A response schema alongside tools (#2771). Neither Gemini API can apply a
// schema to a round that may call tools: generateContent rejects it (2.5) or
// never stops calling (3.x), and the Interactions API, measured live on
// gemini-3.7-flash, also calls the tool on every round while a schema is set.
// So the provider runs the tool round without the schema and, when that round
// answers instead of calling a tool, asks for the answer again with the schema
// and no tools. These tests drive that through the public entry points against
// a fake server and assert what each request carried and what the caller got.

// sentRequest is what one call to the fake server carried.
type sentRequest struct {
	path      string
	hasTools  bool
	hasSchema bool
	body      map[string]any
}

// schemaToolsServer answers each call with the next scripted body and records
// what each call sent.
type schemaToolsServer struct {
	mu      sync.Mutex
	replies []string
	sent    []sentRequest
	srv     *httptest.Server
}

func newSchemaToolsServer(t *testing.T, replies ...string) *schemaToolsServer {
	t.Helper()
	s := &schemaToolsServer{replies: replies}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gen, _ := body[keyGenerationConfig].(map[string]any)
		_, genSchema := gen["responseSchema"]
		_, genMime := gen["responseMimeType"]
		_, intSchema := body["response_format"]
		_, hasTools := body["tools"]

		s.mu.Lock()
		i := len(s.sent)
		s.sent = append(s.sent, sentRequest{
			path: r.URL.Path, hasTools: hasTools, hasSchema: genSchema || genMime || intSchema, body: body,
		})
		s.mu.Unlock()

		if i >= len(s.replies) {
			http.Error(w, "unexpected extra call", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s.replies[i]))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *schemaToolsServer) requests() []sentRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentRequest(nil), s.sent...)
}

func schemaToolsProvider(t *testing.T, url string) (*ToolProvider, providers.ProviderTools) {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	tp := NewToolProvider("g-schema-tools", "gemini-3.7-flash", url,
		providers.ProviderDefaults{MaxTokens: 512}, false)
	tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
		Name: "get_temperature", Description: "temperature",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
	}})
	require.NoError(t, err)
	return tp, tools
}

func schemaToolsRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages: []types.Message{{Role: roleUser, Content: "temperature in Bristol?"}},
		ResponseFormat: &providers.ResponseFormat{
			Type: providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(
				`{"type":"object","properties":{"celsius":{"type":"number"}},"required":["celsius"]}`),
		},
	}
}

// gcText is a generateContent response whose only part is text.
func gcText(text string, in, out int) string {
	b, _ := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"content":      map[string]any{"role": "model", "parts": []any{map[string]any{"text": text}}},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]int{"promptTokenCount": in, "candidatesTokenCount": out, "totalTokenCount": in + out},
	})
	return string(b)
}

// gcCall is a generateContent response that calls get_temperature.
func gcCall(in, out int) string {
	b, _ := json.Marshal(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{map[string]any{
				"functionCall": map[string]any{"name": "get_temperature", "args": map[string]any{"city": "Bristol"}},
			}}},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]int{"promptTokenCount": in, "candidatesTokenCount": out, "totalTokenCount": in + out},
	})
	return string(b)
}

func TestPredictWithTools_SchemaWithTools_AnswerIsAskedAgainUnderSchema(t *testing.T) {
	srv := newSchemaToolsServer(t,
		gcText("It is 21 degrees.", 100, 20),
		gcText(`{"celsius":21}`, 120, 7))
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	resp, calls, err := tp.PredictWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)

	assert.Empty(t, calls)
	assert.Equal(t, `{"celsius":21}`, resp.Content, "the answer is the one asked for under the schema")

	sent := srv.requests()
	require.Len(t, sent, 2)
	for i := range sent {
		assert.Containsf(t, sent[i].path, ":generateContent", "call %d goes to generateContent", i)
	}
	assert.True(t, sent[0].hasTools, "the tool round offers the tools")
	assert.False(t, sent[0].hasSchema, "the tool round carries no schema")
	assert.False(t, sent[1].hasTools, "the answer round offers no tools")
	assert.True(t, sent[1].hasSchema, "the answer round carries the schema")

	require.NotNil(t, resp.CostInfo)
	assert.Equal(t, 220, resp.CostInfo.InputTokens, "both calls' input is billed")
	assert.Equal(t, 27, resp.CostInfo.OutputTokens, "both calls' output is billed")
}

func TestPredictWithTools_SchemaWithTools_ToolCallReturnsWithoutSecondCall(t *testing.T) {
	srv := newSchemaToolsServer(t, gcCall(100, 10))
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	_, calls, err := tp.PredictWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)

	require.Len(t, calls, 1)
	assert.Equal(t, "get_temperature", calls[0].Name)
	sent := srv.requests()
	require.Len(t, sent, 1, "a round that calls a tool is not asked again")
	assert.False(t, sent[0].hasSchema)
	assert.True(t, sent[0].hasTools)
}

// Without tools there is nothing to compete with the schema: one call, with it.
func TestPredictWithTools_SchemaWithoutTools_OneCallWithSchema(t *testing.T) {
	srv := newSchemaToolsServer(t, gcText(`{"celsius":21}`, 12, 3))
	tp, _ := schemaToolsProvider(t, srv.srv.URL)

	resp, _, err := tp.PredictWithTools(context.Background(), schemaToolsRequest(), nil, "")
	require.NoError(t, err)

	assert.Equal(t, `{"celsius":21}`, resp.Content)
	sent := srv.requests()
	require.Len(t, sent, 1)
	assert.True(t, sent[0].hasSchema)
	assert.False(t, sent[0].hasTools)
}

// An explicit api_mode still decides the API, and the same two-step rule
// applies there: the Interactions API loops on schema plus tools as well.
func TestPredictWithTools_SchemaWithTools_PinnedInteractions(t *testing.T) {
	srv := newSchemaToolsServer(t,
		`{"id":"i1","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"21 degrees"}]}]}`,
		`{"id":"i2","status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"celsius\":21}"}]}]}`)
	tp, tools := schemaToolsProvider(t, srv.srv.URL)
	tp.apiMode = APIModeInteractions

	resp, calls, err := tp.PredictWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)

	assert.Empty(t, calls)
	assert.Equal(t, `{"celsius":21}`, resp.Content)
	sent := srv.requests()
	require.Len(t, sent, 2)
	assert.Contains(t, sent[0].path, "/interactions")
	assert.Contains(t, sent[1].path, "/interactions")
	assert.True(t, sent[0].hasTools)
	assert.False(t, sent[0].hasSchema)
	assert.False(t, sent[1].hasTools)
	assert.True(t, sent[1].hasSchema)
}

func TestPredictWithTools_SchemaWithTools_SecondCallErrorSurfaces(t *testing.T) {
	srv := newSchemaToolsServer(t, gcText("It is 21 degrees.", 10, 2)) // no second reply: 500
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	_, _, err := tp.PredictWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected extra call")
}

// collectStream drains a stream, returning every chunk.
func collectStream(t *testing.T, ch <-chan providers.StreamChunk) []providers.StreamChunk {
	t.Helper()
	var out []providers.StreamChunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}

func TestPredictStreamWithTools_SchemaWithTools_OnlyTheSchemaAnswerIsStreamed(t *testing.T) {
	srv := newSchemaToolsServer(t,
		"["+gcText("It is 21 degrees.", 100, 20)+"]",
		"["+gcText(`{"celsius":21}`, 120, 7)+"]")
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	ch, err := tp.PredictStreamWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)
	chunks := collectStream(t, ch)

	require.NotEmpty(t, chunks)
	for i := range chunks {
		require.NoError(t, chunks[i].Error)
		assert.NotContainsf(t, chunks[i].Content, "degrees",
			"chunk %d streamed the answer given without the schema", i)
	}
	last := chunks[len(chunks)-1]
	assert.Equal(t, `{"celsius":21}`, last.Content)
	require.NotNil(t, last.FinishReason)
	require.NotNil(t, last.CostInfo)
	assert.Equal(t, 220, last.CostInfo.InputTokens, "both calls' input is billed")
	assert.Equal(t, 27, last.CostInfo.OutputTokens, "both calls' output is billed")
	final, ok := last.FinalResult.(*providers.PredictionResponse)
	require.True(t, ok)
	require.NotNil(t, final.CostInfo)
	assert.Equal(t, 220, final.CostInfo.InputTokens, "the final result carries the same cost")

	sent := srv.requests()
	require.Len(t, sent, 2)
	assert.Contains(t, sent[0].path, ":streamGenerateContent")
	assert.Contains(t, sent[1].path, ":streamGenerateContent")
	assert.True(t, sent[0].hasTools)
	assert.False(t, sent[0].hasSchema)
	assert.False(t, sent[1].hasTools)
	assert.True(t, sent[1].hasSchema)
}

func TestPredictStreamWithTools_SchemaWithTools_ToolCallRoundIsDelivered(t *testing.T) {
	srv := newSchemaToolsServer(t, "["+gcCall(100, 10)+"]")
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	ch, err := tp.PredictStreamWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)
	chunks := collectStream(t, ch)

	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.NoError(t, last.Error)
	require.Len(t, last.ToolCalls, 1)
	assert.Equal(t, "get_temperature", last.ToolCalls[0].Name)
	require.NotNil(t, last.CostInfo)
	assert.Equal(t, 100, last.CostInfo.InputTokens)
	require.Len(t, srv.requests(), 1, "a round that calls a tool is not asked again")
}

func TestPredictStreamWithTools_SchemaWithTools_SecondCallErrorIsAChunk(t *testing.T) {
	srv := newSchemaToolsServer(t, "["+gcText("It is 21 degrees.", 10, 2)+"]")
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	ch, err := tp.PredictStreamWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)
	chunks := collectStream(t, ch)

	require.Len(t, chunks, 1)
	require.Error(t, chunks[0].Error)
	assert.Contains(t, chunks[0].Error.Error(), "unexpected extra call")
	require.NotNil(t, chunks[0].FinishReason)
	assert.Equal(t, "error", *chunks[0].FinishReason)
}

// A tool round that fails is delivered as it is, not asked again.
func TestPredictStreamWithTools_SchemaWithTools_FailedToolRoundIsDelivered(t *testing.T) {
	blocked := `[{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"SAFETY"}]}]`
	srv := newSchemaToolsServer(t, blocked, "["+gcText(`{"celsius":21}`, 12, 3)+"]")
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	ch, err := tp.PredictStreamWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)
	chunks := collectStream(t, ch)

	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.Error(t, last.Error)
	assert.Contains(t, last.Error.Error(), "SAFETY")
	assert.Len(t, srv.requests(), 1, "a failed round is not asked again")
}

// A tool round that reports no usage bills only the answer round.
func TestPredictStreamWithTools_SchemaWithTools_DraftWithoutUsage(t *testing.T) {
	noUsage := `[{"candidates":[{"content":{"role":"model","parts":[{"text":"21 degrees"}]},"finishReason":"STOP"}]}]`
	srv := newSchemaToolsServer(t, noUsage, "["+gcText(`{"celsius":21}`, 12, 3)+"]")
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	ch, err := tp.PredictStreamWithTools(context.Background(), schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)
	chunks := collectStream(t, ch)

	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	assert.Equal(t, `{"celsius":21}`, last.Content)
	require.NotNil(t, last.CostInfo)
	assert.Equal(t, 12, last.CostInfo.InputTokens)
	assert.Equal(t, 3, last.CostInfo.OutputTokens)
}

func TestPredictStreamWithTools_SchemaWithTools_CanceledContextStops(t *testing.T) {
	srv := newSchemaToolsServer(t,
		"["+gcText("It is 21 degrees.", 10, 2)+"]",
		"["+gcText(`{"celsius":21}`, 12, 3)+"]")
	tp, tools := schemaToolsProvider(t, srv.srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := tp.PredictStreamWithTools(ctx, schemaToolsRequest(), tools, "auto")
	require.NoError(t, err)
	cancel()
	for c := range ch {
		assert.NotContains(t, c.Content, "degrees")
	}
}
