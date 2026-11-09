package conformance_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The request side has its matrix (wire_feature_matrix_test.go); this is the
// response side. Every provider is handed the same two turns in its own wire
// format, unary and streamed, and must parse them to the same normalized
// result: the text, the tool call (name and arguments), the canonical
// finish_reason, and the token counts in CostInfo's units (input is uncached,
// cache reads are separate).
//
// The bodies follow each vendor's documented response format field for field
// (OpenAI chat.completion / chat.completion.chunk, Anthropic Messages and its
// SSE events, Gemini generateContent and the JSON-array streamGenerateContent
// stream), so a
// parser that reads a field the API does not send, or misses one it does,
// fails here rather than in production.
//
// The two turns:
//
//	tool turn  the model calls get_weather({"city":"Paris"})
//	text turn  the model answers "It is sunny in Paris."
//
// with usage of 18 uncached input tokens, 64 cache-read tokens (where the
// vendor reports caching) and 17 output tokens.

const (
	parseWantText = "It is sunny in Paris."
	parseWantTool = "get_weather"
	parseWantArgs = `{"city":"Paris"}`
)

// wireTurn is one turn as one vendor sends it.
type wireTurn struct {
	unary  string // the JSON response body
	stream string // the SSE response body
}

// parseCase is one provider and its two turns.
type parseCase struct {
	name   string
	build  func(t *testing.T, url string) providers.Provider
	tool   wireTurn
	text   wireTurn
	cached int    // cache-read tokens the vendor reports (0 if it reports none)
	toolID string // the tool-call id the vendor sends ("" = vendor sends none)
}

func sseData(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: ")
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

func claudeSSE(events ...[2]string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("event: " + e[0] + "\ndata: " + e[1] + "\n\n")
	}
	return b.String()
}

// openAICompatTurns are the chat.completions bodies; usage is the vendor's
// usage object for the final response or chunk.
func openAICompatTurns(model, usage string) (tool, text wireTurn) {
	tool = wireTurn{
		unary: `{"id":"chatcmpl-1","object":"chat.completion","created":1731000000,"model":"` + model + `",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_abc123",` +
			`"type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}],"refusal":null},` +
			`"logprobs":null,"finish_reason":"tool_calls"}],"usage":` + usage + `}`,
		stream: sseData(
			`{"id":"chatcmpl-2","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,`+
				`"id":"call_abc123","type":"function","function":{"name":"get_weather","arguments":""}}]},`+
				`"logprobs":null,"finish_reason":null}]}`,
			`{"id":"chatcmpl-2","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},`+
				`"logprobs":null,"finish_reason":null}]}`,
			`{"id":"chatcmpl-2","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},`+
				`"logprobs":null,"finish_reason":null}]}`,
			`{"id":"chatcmpl-2","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{},"logprobs":null,"finish_reason":"tool_calls"}]}`,
			`{"id":"chatcmpl-2","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[],"usage":`+usage+`}`,
			`[DONE]`,
		),
	}
	text = wireTurn{
		unary: `{"id":"chatcmpl-3","object":"chat.completion","created":1731000000,"model":"` + model + `",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"It is sunny in Paris.","refusal":null},` +
			`"logprobs":null,"finish_reason":"stop"}],"usage":` + usage + `}`,
		stream: sseData(
			`{"id":"chatcmpl-4","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{"role":"assistant","content":""},"logprobs":null,"finish_reason":null}]}`,
			`{"id":"chatcmpl-4","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{"content":"It is sunny"},"logprobs":null,"finish_reason":null}]}`,
			`{"id":"chatcmpl-4","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{"content":" in Paris."},"logprobs":null,"finish_reason":null}]}`,
			`{"id":"chatcmpl-4","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[{"index":0,"delta":{},"logprobs":null,"finish_reason":"stop"}]}`,
			`{"id":"chatcmpl-4","object":"chat.completion.chunk","created":1731000000,"model":"`+model+`",`+
				`"choices":[],"usage":`+usage+`}`,
			`[DONE]`,
		),
	}
	return tool, text
}

func parseCases() []parseCase {
	// OpenAI counts cached tokens inside prompt_tokens: 82 = 18 uncached + 64.
	openAITool, openAIText := openAICompatTurns("gpt-4o-mini-2024-07-18",
		`{"prompt_tokens":82,"completion_tokens":17,"total_tokens":99,`+
			`"prompt_tokens_details":{"cached_tokens":64,"audio_tokens":0},`+
			`"completion_tokens_details":{"reasoning_tokens":0,"audio_tokens":0,`+
			`"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}`)
	// Ollama and vLLM report no cache breakdown.
	compatUsage := `{"prompt_tokens":18,"completion_tokens":17,"total_tokens":35}`
	ollamaTool, ollamaText := openAICompatTurns("llama3.2", compatUsage)
	vllmTool, vllmText := openAICompatTurns("Qwen/Qwen2.5-7B-Instruct", compatUsage)

	// Anthropic: input_tokens excludes cache reads, which arrive separately.
	const claudeUsage = `{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":64,"output_tokens":17}`
	claudeStart := [2]string{"message_start", `{"type":"message_start","message":{"id":"msg_01","type":"message",` +
		`"role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"stop_sequence":null,` +
		`"usage":{"input_tokens":18,"cache_creation_input_tokens":0,"cache_read_input_tokens":64,"output_tokens":1}}}`}
	claudeEnd := func(stop string) [][2]string {
		return [][2]string{
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"` + stop + `","stop_sequence":null},` +
				`"usage":{"output_tokens":17}}`},
			{"message_stop", `{"type":"message_stop"}`},
		}
	}
	claudeTool := wireTurn{
		unary: `{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4-6",` +
			`"content":[{"type":"tool_use","id":"toolu_01A","name":"get_weather","input":{"city":"Paris"}}],` +
			`"stop_reason":"tool_use","stop_sequence":null,"usage":` + claudeUsage + `}`,
		stream: claudeSSE(append([][2]string{claudeStart,
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use",` +
				`"id":"toolu_01A","name":"get_weather","input":{}}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta",` +
				`"partial_json":"{\"city\":"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta",` +
				`"partial_json":"\"Paris\"}"}}`},
		}, claudeEnd("tool_use")...)...),
	}
	claudeText := wireTurn{
		unary: `{"id":"msg_02","type":"message","role":"assistant","model":"claude-sonnet-4-6",` +
			`"content":[{"type":"text","text":"It is sunny in Paris."}],` +
			`"stop_reason":"end_turn","stop_sequence":null,"usage":` + claudeUsage + `}`,
		stream: claudeSSE(append([][2]string{claudeStart,
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"It is sunny"}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" in Paris."}}`},
		}, claudeEnd("end_turn")...)...),
	}

	// Gemini counts cachedContentTokenCount inside promptTokenCount, and ends a
	// function-call turn with finishReason STOP.
	const geminiUsage = `{"promptTokenCount":82,"candidatesTokenCount":17,"totalTokenCount":99,` +
		`"cachedContentTokenCount":64,"promptTokensDetails":[{"modality":"TEXT","tokenCount":82}]}`
	geminiCall := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"city":"Paris"}}}],` +
		`"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":` + geminiUsage +
		`,"modelVersion":"gemini-2.5-flash","responseId":"r1"}`
	// The provider calls streamGenerateContent without alt=sse, so the stream
	// is the JSON-array form: "[" + the response objects comma-separated + "]".
	geminiTool := wireTurn{unary: geminiCall, stream: "[" + geminiCall + "\n]"}
	geminiText := wireTurn{
		unary: `{"candidates":[{"content":{"parts":[{"text":"It is sunny in Paris."}],"role":"model"},` +
			`"finishReason":"STOP","index":0}],"usageMetadata":` + geminiUsage +
			`,"modelVersion":"gemini-2.5-flash","responseId":"r2"}`,
		stream: "[" +
			`{"candidates":[{"content":{"parts":[{"text":"It is sunny"}],"role":"model"},"index":0}],` +
			`"usageMetadata":{"promptTokenCount":82,"totalTokenCount":82,"cachedContentTokenCount":64},` +
			`"modelVersion":"gemini-2.5-flash","responseId":"r3"}` + ",\r\n" +
			`{"candidates":[{"content":{"parts":[{"text":" in Paris."}],"role":"model"},"finishReason":"STOP",` +
			`"index":0}],"usageMetadata":` + geminiUsage + `,"modelVersion":"gemini-2.5-flash","responseId":"r3"}` +
			"\n]",
	}

	defaults := providers.ProviderDefaults{MaxTokens: 256}
	return []parseCase{
		{name: "openai", tool: openAITool, text: openAIText, cached: 64, toolID: "call_abc123",
			build: func(t *testing.T, url string) providers.Provider {
				t.Setenv("OPENAI_API_KEY", "test-key")
				return openai.NewToolProvider("openai-parse", "gpt-4o-mini", url, defaults, false, nil, nil)
			}},
		{name: "claude", tool: claudeTool, text: claudeText, cached: 64, toolID: "toolu_01A",
			build: func(t *testing.T, url string) providers.Provider {
				t.Setenv("ANTHROPIC_API_KEY", "test-key")
				return claude.NewToolProvider("claude-parse", "claude-sonnet-4-6", url, defaults, false)
			}},
		{name: "gemini", tool: geminiTool, text: geminiText, cached: 64,
			build: func(t *testing.T, url string) providers.Provider {
				t.Setenv("GEMINI_API_KEY", "test-key")
				return gemini.NewToolProvider("gemini-parse", "gemini-2.5-flash", url, defaults, false)
			}},
		{name: "ollama", tool: ollamaTool, text: ollamaText, toolID: "call_abc123",
			build: func(t *testing.T, url string) providers.Provider {
				return ollama.NewToolProvider("ollama-parse", "llama3.2", url, defaults, false, nil)
			}},
		{name: "vllm", tool: vllmTool, text: vllmText, toolID: "call_abc123",
			build: func(t *testing.T, url string) providers.Provider {
				return vllm.NewProvider("vllm-parse", "Qwen/Qwen2.5-7B-Instruct", url, defaults, false, nil)
			}},
	}
}

// turnServer answers each request with the turn's stream body when the
// request asks to stream (a "stream":true body or Gemini's streaming method)
// and with its JSON body otherwise.
func turnServer(t *testing.T, turn wireTurn) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Stream || strings.Contains(r.URL.Path, "streamGenerateContent") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, turn.stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, turn.unary)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// parsed is what a caller reads off a finished turn, on either path.
type parsed struct {
	content   string
	toolCalls []types.MessageToolCall
	finish    string
	cost      *types.CostInfo
}

func runUnary(t *testing.T, p providers.Provider, withTools bool) parsed {
	t.Helper()
	req := parseRequest()
	if !withTools {
		resp, err := p.Predict(context.Background(), req)
		require.NoError(t, err)
		return parsed{resp.Content, resp.ToolCalls, resp.FinishReason, resp.CostInfo}
	}
	ts := p.(providers.ToolSupport)
	resp, calls, err := ts.PredictWithTools(context.Background(), req, parseTools(t, ts), "auto")
	require.NoError(t, err)
	return parsed{resp.Content, calls, resp.FinishReason, resp.CostInfo}
}

func runStream(t *testing.T, p providers.Provider, withTools bool) parsed {
	t.Helper()
	req := parseRequest()
	var ch <-chan providers.StreamChunk
	var err error
	if withTools {
		ts := p.(providers.ToolSupport)
		ch, err = ts.PredictStreamWithTools(context.Background(), req, parseTools(t, ts), "auto")
	} else {
		ch, err = p.PredictStream(context.Background(), req)
	}
	require.NoError(t, err)
	var out parsed
	for c := range ch {
		require.NoError(t, c.Error)
		out.content = c.Content
		if len(c.ToolCalls) > 0 {
			out.toolCalls = c.ToolCalls
		}
		if c.FinishReason != nil {
			out.finish = *c.FinishReason
		}
		if c.CostInfo != nil {
			out.cost = c.CostInfo
		}
	}
	return out
}

func parseRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages:  []types.Message{{Role: "user", Content: "What is the weather in Paris?"}},
		MaxTokens: 256,
	}
}

func parseTools(t *testing.T, ts providers.ToolSupport) providers.ProviderTools {
	t.Helper()
	tools, err := ts.BuildTooling([]*providers.ToolDescriptor{{
		Name:        parseWantTool,
		Description: "Get the current weather for a city",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}})
	require.NoError(t, err)
	return tools
}

func assertUsage(t *testing.T, pc parseCase, cost *types.CostInfo) {
	t.Helper()
	require.NotNil(t, cost, "the response's usage must produce CostInfo")
	assert.Equal(t, []int{18, pc.cached, 17}, []int{cost.InputTokens, cost.CachedTokens, cost.OutputTokens},
		"CostInfo [uncached input, cache read, output]")
}

func TestProviders_ParseTheSameTurnTheSameWay(t *testing.T) {
	type pathCase struct {
		name      string
		run       func(*testing.T, providers.Provider, bool) parsed
		withTools bool
	}
	paths := []pathCase{
		{"predict", runUnary, false},
		{"predict_stream", runStream, false},
		{"predict_with_tools", runUnary, true},
		{"predict_stream_with_tools", runStream, true},
	}

	for _, pc := range parseCases() {
		t.Run(pc.name, func(t *testing.T) {
			for _, path := range paths {
				t.Run(path.name+"/text", func(t *testing.T) {
					p := pc.build(t, turnServer(t, pc.text))
					defer func() { _ = p.Close() }()
					got := path.run(t, p, path.withTools)

					assert.Equal(t, parseWantText, got.content)
					assert.Empty(t, got.toolCalls)
					assert.Equal(t, types.FinishReasonStop, got.finish)
					assertUsage(t, pc, got.cost)
				})
				if !path.withTools {
					continue
				}
				t.Run(path.name+"/tool_call", func(t *testing.T) {
					p := pc.build(t, turnServer(t, pc.tool))
					defer func() { _ = p.Close() }()
					got := path.run(t, p, true)

					require.Len(t, got.toolCalls, 1)
					call := got.toolCalls[0]
					assert.Equal(t, parseWantTool, call.Name)
					assert.JSONEq(t, parseWantArgs, string(call.Args))
					if pc.toolID != "" {
						assert.Equal(t, pc.toolID, call.ID, "the vendor's call id must survive for the result round")
					} else {
						assert.NotEmpty(t, call.ID, "a call id is needed to pair the tool result")
					}
					assert.Equal(t, types.FinishReasonToolUse, got.finish)
					assertUsage(t, pc, got.cost)
				})
			}
		})
	}
}
