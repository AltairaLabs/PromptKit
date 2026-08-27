package conformance_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/providers/vllm"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// A provider exposes four request paths — {tools, no tools} x {streaming,
// non-streaming} — and each builds its own wire request. A field honored on
// one path and forgotten on another is invisible: the option is accepted, the
// call succeeds, and the constraint simply never reaches the API.
//
// That exact bug has now been found four times in this repo, always the same
// way round: the non-tool path correct, the tool path silently dropping the
// field.
//
//	openai reasoning_content   non-streaming and tool paths dropped it
//	openai Responses reasoning streaming dropped the summary
//	gemini thinking config     streaming dropped it
//	claude ResponseFormat      both tool paths dropped it (#1848)
//
// Constructor tests cannot catch this, because each builder looks correct in
// isolation. Only the assembled request shows it. This matrix therefore drives
// every provider down all four paths against a capturing server and asserts the
// feature reaches the wire on each — turning a class of silent omission into a
// failing test.

// captureServer records the body of every request it receives.
type captureServer struct {
	mu     sync.Mutex
	bodies []string
	srv    *httptest.Server
}

func newCaptureServer(t *testing.T, response string) *captureServer {
	t.Helper()
	cs := &captureServer{}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cs.mu.Lock()
		cs.bodies = append(cs.bodies, string(body))
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

// lastBody returns the most recent captured request body, or "" if none.
// Providers may issue more than one request (retries); the last is the one the
// assertions care about.
func (c *captureServer) lastBody() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		return ""
	}
	return c.bodies[len(c.bodies)-1]
}

// wireProviderCase describes one provider and what evidence of a honored
// ResponseFormat looks like in its wire format.
type wireProviderCase struct {
	name string
	// build returns a provider bound to the capture server.
	build func(t *testing.T, url string) providers.Provider
	// marker is the substring that must appear in the request body when the
	// caller sets a JSON-schema ResponseFormat.
	marker string
	// response is a minimal body the provider can parse. The assertions only
	// read the captured REQUEST, so a parse failure downstream is harmless —
	// but a well-formed reply keeps the logs quiet.
	response string
	// wantOnPath records, per path, whether the schema must reach the wire.
	// This is per-path rather than per-provider because at least one vendor
	// constraint is genuinely path-specific: Gemini's API rejects function
	// calling combined with a JSON response mime type, so sending it on the
	// tool paths would be a hard 400 rather than an improvement.
	wantOnPath map[string]bool
	// gap explains every false entry, so an expectation is a statement about
	// the world rather than a silent skip.
	gap string
}

// allPaths returns the same expectation for every path.
func allPaths(want bool) map[string]bool {
	return map[string]bool{
		"predict":                   want,
		"predict_stream":            want,
		"predict_with_tools":        want,
		"predict_stream_with_tools": want,
	}
}

func wireProviderCases() []wireProviderCase {
	const claudeReply = `{"id":"m","type":"message","role":"assistant","model":"c",` +
		`"content":[{"type":"text","text":"{}"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":1,"output_tokens":1}}`
	const openAIReply = `{"id":"c","object":"chat.completion","model":"m","choices":` +
		`[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	const geminiReply = `{"candidates":[{"content":{"role":"model","parts":[{"text":"{}"}]},` +
		`"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,` +
		`"candidatesTokenCount":1,"totalTokenCount":2}}`

	return []wireProviderCase{
		{
			name:       "claude",
			marker:     "output_config",
			response:   claudeReply,
			wantOnPath: allPaths(true),
			build: func(t *testing.T, url string) providers.Provider {
				t.Helper()
				t.Setenv("ANTHROPIC_API_KEY", "test-key")
				return claude.NewToolProvider("claude-wire", "claude-sonnet-4-6", url,
					providers.ProviderDefaults{MaxTokens: 256}, false)
			},
		},
		{
			name:       "openai",
			marker:     "response_format",
			response:   openAIReply,
			wantOnPath: allPaths(true),
			build: func(t *testing.T, url string) providers.Provider {
				t.Helper()
				t.Setenv("OPENAI_API_KEY", "test-key")
				return openai.NewToolProvider("openai-wire", "gpt-4o-mini", url,
					providers.ProviderDefaults{MaxTokens: 256}, false, nil, nil)
			},
		},
		{
			name:     "gemini_2.5",
			marker:   "responseSchema",
			response: geminiReply,
			// VENDOR CONSTRAINT, verified live on BOTH generations: 2.5 returns
			// HTTP 400 for tools + JSON response mime type, and 3.x accepts it
			// then never stops calling tools (5/5 rounds with the schema; the
			// identical loop terminated on round 2 without it). So neither
			// generation gets the schema on a tool-carrying round. Rounds
			// without tools carry it normally.
			wantOnPath: map[string]bool{
				"predict":                   true,
				"predict_stream":            true,
				"predict_with_tools":        false,
				"predict_stream_with_tools": false,
			},
			gap: "Gemini 2.5 rejects function calling combined with responseMimeType " +
				"application/json (HTTP 400). The provider logs a warning when it drops " +
				"the schema on a tool-using round rather than discarding it silently.",
			build: func(t *testing.T, url string) providers.Provider {
				t.Helper()
				t.Setenv("GEMINI_API_KEY", "test-key")
				return gemini.NewToolProvider("gemini-wire", "gemini-2.5-flash", url,
					providers.ProviderDefaults{MaxTokens: 256}, false)
			},
		},
		{
			// Gemini 3 ACCEPTS a schema alongside tools — and then never stops
			// calling them. Verified live: 5/5 rounds called the same tool and
			// produced no answer, while the identical loop without the schema
			// terminated on round 2. Accepting is not the same as working, so
			// this generation is treated exactly like 2.5.
			name:     "gemini_3",
			marker:   "responseSchema",
			response: geminiReply,
			wantOnPath: map[string]bool{
				"predict":                   true,
				"predict_stream":            true,
				"predict_with_tools":        false,
				"predict_stream_with_tools": false,
			},
			gap: "Gemini 3 accepts tools + schema but then loops: the model keeps calling " +
				"the tool and never emits a final text part. Dropping the schema on " +
				"tool-carrying rounds is what lets the turn finish at all.",
			build: func(t *testing.T, url string) providers.Provider {
				t.Helper()
				t.Setenv("GEMINI_API_KEY", "test-key")
				return gemini.NewToolProvider("gemini3-wire", "gemini-3.7-flash", url,
					providers.ProviderDefaults{MaxTokens: 256}, false)
			},
		},
		{
			name:       "ollama",
			marker:     "response_format",
			response:   openAIReply,
			wantOnPath: allPaths(false),
			gap: "ollama ignores ResponseFormat entirely — no references anywhere in the " +
				"provider. Its OpenAI-compatible endpoint accepts response_format, so this " +
				"is unimplemented rather than unsupported.",
			build: func(t *testing.T, url string) providers.Provider {
				t.Helper()
				return ollama.NewToolProvider("ollama-wire", "llama3", url,
					providers.ProviderDefaults{MaxTokens: 256}, false, nil)
			},
		},
		{
			name:       "vllm",
			marker:     "response_format",
			response:   openAIReply,
			wantOnPath: allPaths(false),
			gap: "vllm ignores ResponseFormat entirely — no references anywhere in the " +
				"provider. Its OpenAI-compatible server supports guided decoding via " +
				"response_format, so this is unimplemented rather than unsupported.",
			build: func(t *testing.T, url string) providers.Provider {
				t.Helper()
				return vllm.NewProvider("vllm-wire", "test-model", url,
					providers.ProviderDefaults{MaxTokens: 256}, false, nil)
			},
		},
	}
}

// schemaResponseFormat is the caller-set feature under test.
func schemaResponseFormat() *providers.ResponseFormat {
	return &providers.ResponseFormat{
		Type: providers.ResponseFormatJSONSchema,
		JSONSchema: json.RawMessage(
			`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`),
		SchemaName: "answer_schema",
		Strict:     true,
	}
}

// exercisePath drives one of the four request paths and returns the captured
// request body. Provider errors are deliberately ignored: the capture server
// returns a minimal reply, and what matters is the request that was SENT.
func exercisePath(t *testing.T, p providers.Provider, path string) string {
	t.Helper()

	req := providers.PredictionRequest{
		Messages:       []types.Message{{Role: "user", Content: "hello"}},
		MaxTokens:      256,
		ResponseFormat: schemaResponseFormat(),
	}

	ts, hasTools := p.(providers.ToolSupport)
	var tools providers.ProviderTools
	if hasTools {
		built, err := ts.BuildTooling([]*providers.ToolDescriptor{{
			Name:        "probe",
			Description: "a probe tool",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		}})
		require.NoError(t, err)
		tools = built
	}

	drain := func(ch <-chan providers.StreamChunk, err error) {
		if err != nil {
			return
		}
		for range ch { //nolint:revive // draining
		}
	}

	switch path {
	case "predict":
		_, _ = p.Predict(context.Background(), req)
	case "predict_stream":
		drain(p.PredictStream(context.Background(), req))
	case "predict_with_tools":
		if !hasTools {
			t.Skip("provider does not implement ToolSupport")
		}
		_, _, _ = ts.PredictWithTools(context.Background(), req, tools, "auto")
	case "predict_stream_with_tools":
		if !hasTools {
			t.Skip("provider does not implement ToolSupport")
		}
		drain(ts.PredictStreamWithTools(context.Background(), req, tools, "auto"))
	default:
		t.Fatalf("unknown path %q", path)
	}

	return ""
}

// TestProviders_ResponseFormatReachesWireOnAllPaths is the class check.
//
// For every provider that implements ResponseFormat, a caller-set JSON schema
// must appear in the request on ALL FOUR paths. A provider that honors it on
// three and drops it on the fourth fails here, which is the failure mode that
// has repeatedly shipped.
func TestProviders_ResponseFormatReachesWireOnAllPaths(t *testing.T) {
	paths := []string{"predict", "predict_stream", "predict_with_tools", "predict_stream_with_tools"}

	for _, pc := range wireProviderCases() {
		t.Run(pc.name, func(t *testing.T) {
			for _, path := range paths {
				t.Run(path, func(t *testing.T) {
					cs := newCaptureServer(t, pc.response)
					p := pc.build(t, cs.srv.URL)
					defer func() { _ = p.Close() }()

					exercisePath(t, p, path)

					body := cs.lastBody()
					require.NotEmptyf(t, body,
						"%s/%s sent no request at all", pc.name, path)

					if pc.wantOnPath[path] {
						assert.Containsf(t, body, pc.marker,
							"%s DROPPED the caller's ResponseFormat on the %s path. "+
								"Other paths honor it, so this is the silent-omission bug: "+
								"the option is accepted and never reaches the API.\nrequest=%s",
							pc.name, path, body)
						return
					}

					// Not expected on this path: assert the KNOWN state, so
					// implementing it (or a vendor lifting its constraint)
					// turns this red and prompts updating the expectation
					// rather than going unnoticed.
					assert.NotContainsf(t, body, pc.marker,
						"%s now sends ResponseFormat on the %s path. If that is intended, "+
							"flip wantOnPath for it — but verify against the live API first, "+
							"since at least one vendor rejects the combination outright.\n"+
							"expectation was: %s", pc.name, path, pc.gap)
				})
			}
		})
	}
}
