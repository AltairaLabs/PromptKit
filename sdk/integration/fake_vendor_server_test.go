package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeVendorServer is an httptest server that answers the chat endpoints of
// the vendor wire formats PromptKit's HTTP providers speak (OpenAI-compatible
// chat completions, Anthropic messages, Gemini generateContent). It records
// every request it receives, so a test asserts on what actually reached the
// wire through the real provider and the real http.Client.
type fakeVendorServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []recordedRequest

	// redirect, when set, answers a request with a redirect instead of a reply.
	// It returns the Location and status, or "" to answer normally.
	redirect func(r *http.Request) (string, int)
}

// recordedRequest is one request as the server saw it.
type recordedRequest struct {
	At     time.Time
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

func newFakeVendorServer(t *testing.T) *fakeVendorServer {
	t.Helper()
	f := &fakeVendorServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeVendorServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{
		At: time.Now(), Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body,
	})
	redirect := f.redirect
	f.mu.Unlock()

	if redirect != nil {
		if loc, status := redirect(r); loc != "" {
			w.Header().Set("Location", loc)
			w.WriteHeader(status)
			return
		}
	}

	streaming := strings.Contains(string(body), `"stream":true`)
	switch {
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		writeOpenAIReply(w, streaming)
	case strings.HasSuffix(r.URL.Path, "/responses"):
		writeOpenAIResponsesReply(w, streaming)
	case strings.HasSuffix(r.URL.Path, "/embeddings"):
		writeOpenAIEmbeddings(w, body)
	case strings.HasSuffix(r.URL.Path, "/messages"):
		writeClaudeReply(w, streaming)
	case strings.Contains(r.URL.Path, ":embedContent"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"embedding":{"values":[0.1,0.2,0.3]}}`)
	case strings.Contains(r.URL.Path, ":batchEmbedContents"):
		writeGeminiBatchEmbeddings(w, body)
	case strings.Contains(r.URL.Path, ":generateContent"):
		writeGeminiReply(w, false)
	case strings.Contains(r.URL.Path, ":streamGenerateContent"):
		writeGeminiReply(w, true)
	default:
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}
}

func writeOpenAIReply(w http.ResponseWriter, streaming bool) {
	if !streaming {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"gpt-4o",`+
			`"choices":[{"index":0,"message":{"role":"assistant","content":"fake reply"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o",`+
		`"choices":[{"index":0,"delta":{"role":"assistant","content":"fake reply"},"finish_reason":null}]}`+"\n\n")
	_, _ = io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o",`+
		`"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+
		`"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`+"\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// writeOpenAIEmbeddings answers an OpenAI embeddings request with one vector
// per input.
func writeOpenAIEmbeddings(w http.ResponseWriter, body []byte) {
	var req struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(body, &req)
	n := 1
	var inputs []string
	if json.Unmarshal(req.Input, &inputs) == nil {
		n = len(inputs)
	}
	data := make([]string, n)
	for i := range data {
		data[i] = fmt.Sprintf(`{"object":"embedding","index":%d,"embedding":[0.1,0.2,0.3]}`, i)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"object":"list","model":"text-embedding-3-small","data":[`+
		strings.Join(data, ",")+`],"usage":{"prompt_tokens":1,"total_tokens":1}}`)
}

// writeGeminiBatchEmbeddings answers batchEmbedContents with one vector per
// request in the batch.
func writeGeminiBatchEmbeddings(w http.ResponseWriter, body []byte) {
	var req struct {
		Requests []json.RawMessage `json:"requests"`
	}
	_ = json.Unmarshal(body, &req)
	vecs := make([]string, len(req.Requests))
	for i := range vecs {
		vecs[i] = `{"values":[0.1,0.2,0.3]}`
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"embeddings":[`+strings.Join(vecs, ",")+`]}`)
}

const openAIResponseJSON = `{"id":"r1","object":"response","status":"completed","model":"gpt-4o",` +
	`"output":[{"type":"message","id":"m1","role":"assistant","status":"completed",` +
	`"content":[{"type":"output_text","text":"fake reply","annotations":[]}]}],` +
	`"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`

func writeOpenAIResponsesReply(w http.ResponseWriter, streaming bool) {
	if !streaming {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, openAIResponseJSON)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "event: response.output_text.delta\n"+
		`data: {"type":"response.output_text.delta","delta":"fake reply"}`+"\n\n")
	_, _ = io.WriteString(w, "event: response.completed\n"+
		`data: {"type":"response.completed","response":`+openAIResponseJSON+"}\n\n")
}

func writeClaudeReply(w http.ResponseWriter, streaming bool) {
	if !streaming {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","model":"claude-sonnet-4-5",`+
			`"content":[{"type":"text","text":"fake reply"}],"stop_reason":"end_turn",`+
			`"usage":{"input_tokens":3,"output_tokens":2}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	events := []string{
		`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"m1","type":"message",` +
			`"role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,` +
			`"usage":{"input_tokens":3,"output_tokens":0}}}`,
		`event: content_block_start` + "\n" +
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta` + "\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"fake reply"}}`,
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
		`event: message_delta` + "\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
	}
	for _, e := range events {
		_, _ = io.WriteString(w, e+"\n\n")
	}
}

const geminiReplyJSON = `{"candidates":[{"content":{"role":"model","parts":[{"text":"fake reply"}]},` +
	`"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`

func writeGeminiReply(w http.ResponseWriter, streaming bool) {
	w.Header().Set("Content-Type", "application/json")
	if streaming {
		_, _ = io.WriteString(w, "["+geminiReplyJSON+"]")
		return
	}
	_, _ = io.WriteString(w, geminiReplyJSON)
}

// got returns a snapshot of the recorded requests.
func (f *fakeVendorServer) got() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func (f *fakeVendorServer) setRedirect(fn func(r *http.Request) (string, int)) {
	f.mu.Lock()
	f.redirect = fn
	f.mu.Unlock()
}

// vendorCase describes one HTTP provider type: how to point it at the fake
// server, and which header carries its credential.
type vendorCase struct {
	name       string
	typ        string
	model      string
	baseSuffix string // appended to the server URL to form base_url
	authHeader string
	authValue  func(key string) string
	extra      map[string]any
}

// vendorCases are the HTTP completion providers whose credential, redirect and
// rate-limit handling a caller reaches through a provider config.
var vendorCases = []vendorCase{
	{
		name: "openai", typ: "openai", model: "gpt-4o", baseSuffix: "/v1",
		authHeader: "Authorization", authValue: func(k string) string { return "Bearer " + k },
		extra: map[string]any{"api_mode": "completions"},
	},
	{
		name: "openai_responses", typ: "openai", model: "gpt-4o", baseSuffix: "/v1",
		authHeader: "Authorization", authValue: func(k string) string { return "Bearer " + k },
		extra: map[string]any{"api_mode": "responses"},
	},
	{
		name: "claude", typ: "claude", model: "claude-sonnet-4-5", baseSuffix: "/v1",
		authHeader: "X-Api-Key", authValue: func(k string) string { return k },
	},
	{
		name: "gemini", typ: "gemini", model: "gemini-2.0-flash", baseSuffix: "/v1beta",
		authHeader: "X-Goog-Api-Key", authValue: func(k string) string { return k },
	},
	{
		name: "vllm", typ: "vllm", model: "meta-llama/Llama-3.1-8B-Instruct", baseSuffix: "",
		authHeader: "Authorization", authValue: func(k string) string { return "Bearer " + k },
	},
	{
		name: "ollama", typ: "ollama", model: "llama3.2", baseSuffix: "",
		authHeader: "Authorization", authValue: func(k string) string { return "Bearer " + k },
	},
}

// providerYAMLSpec renders the spec block of a provider config for vc pointed
// at baseURL. extraSpec is appended verbatim (already indented two spaces).
func providerYAMLSpec(vc vendorCase, baseURL, apiKey, extraSpec string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  id: %s-under-test\n", vc.name)
	fmt.Fprintf(&b, "  type: %s\n", vc.typ)
	fmt.Fprintf(&b, "  model: %s\n", vc.model)
	fmt.Fprintf(&b, "  base_url: %s%s\n", baseURL, vc.baseSuffix)
	if apiKey != "" {
		fmt.Fprintf(&b, "  credential:\n    api_key: %s\n", apiKey)
	}
	if len(vc.extra) > 0 {
		b.WriteString("  additional_config:\n")
		for k, v := range vc.extra {
			fmt.Fprintf(&b, "    %s: %v\n", k, v)
		}
	}
	b.WriteString(extraSpec)
	return b.String()
}

// writeProviderFile writes a *.provider.yaml for vc and returns its path.
func writeProviderFile(t *testing.T, vc vendorCase, baseURL, apiKey, extraSpec string) string {
	t.Helper()
	doc := "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Provider\nmetadata:\n  name: under-test\nspec:\n" +
		providerYAMLSpec(vc, baseURL, apiKey, extraSpec)
	path := filepath.Join(t.TempDir(), vc.name+".provider.yaml")
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))
	return path
}

// writeRuntimeConfigWithProvider writes a RuntimeConfig declaring vc as its
// only provider and returns its path.
func writeRuntimeConfigWithProvider(t *testing.T, vc vendorCase, baseURL, apiKey, extraSpec string) string {
	t.Helper()
	spec := providerYAMLSpec(vc, baseURL, apiKey, extraSpec)
	// Re-indent the provider spec as one entry of spec.providers.
	lines := strings.Split(strings.TrimRight(spec, "\n"), "\n")
	var b strings.Builder
	b.WriteString("apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: RuntimeConfig\nmetadata:\n  name: rc\nspec:\n")
	b.WriteString("  providers:\n")
	for i, l := range lines {
		l = strings.TrimPrefix(l, "  ")
		if i == 0 {
			b.WriteString("    - " + l + "\n")
			continue
		}
		b.WriteString("      " + l + "\n")
	}
	path := filepath.Join(t.TempDir(), "runtime.yaml")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	return path
}

// requestBodyJSON decodes a recorded request body.
func requestBodyJSON(t *testing.T, r recordedRequest) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(r.Body, &m), "request body: %s", r.Body)
	return m
}
