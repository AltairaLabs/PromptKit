package sdk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/v2/config"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

func cred(tok string) *pkgconfig.CredentialConfig {
	return &pkgconfig.CredentialConfig{APIKey: tok}
}

func TestApplyProviderConfig_LLMBecomesAgent(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "m", Type: "mock", Role: pkgconfig.RoleLLM}); err != nil {
		t.Fatalf("applyProviderConfig: %v", err)
	}
	if c.getAgentProvider() == nil {
		t.Fatal("expected agent provider to be set")
	}
}

func TestApplyProviderConfig_SecondLLMPooledNotAgent(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "first", Type: "mock", Role: pkgconfig.RoleLLM}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "second", Type: "mock", Role: pkgconfig.RoleLLM}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := c.agentProviderID; got != "first" {
		t.Fatalf("agent should remain first-declared, got %q", got)
	}
	if _, ok := c.providers.Get("second"); !ok {
		t.Fatal("second provider should still be registered in the pool")
	}
}

func TestApplyProviderConfig_TTS(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{Type: "openai", Role: pkgconfig.RoleTTS, Credential: cred("tok")}); err != nil {
		t.Fatalf("tts: %v", err)
	}
	if c.ttsService == nil {
		t.Fatal("expected ttsService set")
	}
}

func TestApplyProviderConfig_Inference(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{ID: "hf", Type: "huggingface", Role: pkgconfig.RoleInference, Credential: cred("tok")}); err != nil {
		t.Fatalf("inference: %v", err)
	}
	if _, err := c.inferenceRegistry.Get("hf"); err != nil {
		t.Fatalf("hf inference provider should resolve: %v", err)
	}
}

func TestApplyProviderConfig_STT(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{Type: "openai", Role: pkgconfig.RoleSTT, Credential: cred("tok")}); err != nil {
		t.Fatalf("stt: %v", err)
	}
	if c.sttService == nil {
		t.Fatal("expected sttService set")
	}
}

func TestApplyProviderConfig_ImagePooledAsAgent(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{
		ID: "img", Type: "imagen", Model: "imagen-4.0-generate-001",
		Role: pkgconfig.RoleImage, Credential: cred("tok"),
	}); err != nil {
		t.Fatalf("image: %v", err)
	}
	if c.getAgentProvider() == nil {
		t.Fatal("expected image provider to be set as the agent provider")
	}
}

func TestProviderSpecFromConfig_CarriesPlatform(t *testing.T) {
	pc := &pkgconfig.PlatformConfig{Type: "azure", Endpoint: "https://my-resource.openai.azure.com"}
	spec := providerSpecFromConfig(&pkgconfig.Provider{
		ID:       "emb",
		Type:     "openai",
		Role:     pkgconfig.RoleEmbedding,
		Platform: pc,
	})
	if spec.Platform == nil {
		t.Fatal("expected Platform to be carried through providerSpecFromConfig, got nil")
	}
	if spec.Platform.Type != "azure" {
		t.Fatalf("expected Platform.Type %q, got %q", "azure", spec.Platform.Type)
	}
	if spec.Platform.Endpoint != "https://my-resource.openai.azure.com" {
		t.Fatalf("expected Platform.Endpoint carried through, got %q", spec.Platform.Endpoint)
	}
}

func TestApplyProviderConfig_UnknownRoleRejected(t *testing.T) {
	c := &config{}
	if err := c.applyProviderConfig(&pkgconfig.Provider{Type: "x", Role: "bogus"}); err == nil {
		t.Fatal("expected unknown-role error")
	}
}

// --- Task 2: WithProviderFile + WithProvidersDir ---

func writeProviderYAML(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

const ttsProviderYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: openai-tts
spec:
  id: openai-tts
  type: openai
  role: tts
  credential:
    api_key: tok
`

func TestWithProviderFile_LoadsTTS(t *testing.T) {
	// LoadProvider schema-validates; pin to the in-repo schema so the test is
	// hermetic (no network fetch of the published schema).
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()
	path := writeProviderYAML(t, dir, "tts.provider.yaml", ttsProviderYAML)

	c := &config{}
	if err := WithProviderFile(path)(c); err != nil {
		t.Fatalf("WithProviderFile: %v", err)
	}
	if c.ttsService == nil {
		t.Fatal("expected ttsService set from file")
	}
}

func TestWithProviderFile_MissingFile(t *testing.T) {
	c := &config{}
	if err := WithProviderFile("/nonexistent/x.provider.yaml")(c); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWithProvidersDir_LoadsAllFirstWinsAgent(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()
	// Two llm providers + one tts; glob is sorted, so "a-llm" sorts before "b-llm".
	writeProviderYAML(t, dir, "a-llm.provider.yaml", `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: a
spec:
  id: a
  type: mock
  role: llm
`)
	writeProviderYAML(t, dir, "b-llm.provider.yaml", `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: b
spec:
  id: b
  type: mock
  role: llm
`)
	writeProviderYAML(t, dir, "tts.provider.yaml", ttsProviderYAML)

	c := &config{}
	if err := WithProvidersDir(dir)(c); err != nil {
		t.Fatalf("WithProvidersDir: %v", err)
	}
	if c.agentProviderID != "a" {
		t.Fatalf("first-declared (sorted) llm should be agent, got %q", c.agentProviderID)
	}
	if _, ok := c.providers.Get("b"); !ok {
		t.Fatal("second llm should still be pooled")
	}
	if c.ttsService == nil {
		t.Fatal("tts from dir should be set")
	}
}

func TestWithProvidersDir_Empty(t *testing.T) {
	c := &config{}
	if err := WithProvidersDir(t.TempDir())(c); err != nil {
		t.Fatalf("empty dir should be a no-op, got %v", err)
	}
}

// --- Request tuning on capability roles (#2128) ---

// tunedProviderYAML is a provider file for role/type at baseURL carrying the
// tuning fields a capability provider honors, plus any extra spec lines.
func tunedProviderYAML(role, typ, baseURL, extra string) string {
	return `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: tuned
spec:
  id: tuned
  type: ` + typ + `
  role: ` + role + `
  base_url: ` + baseURL + `
  credential:
    api_key: tok
  headers:
    X-Gateway: v
  request_timeout: 100ms
` + extra
}

// gatewayServer answers every request with body after delay, recording the
// X-Gateway header it saw. A zero delay answers at once.
type gatewayServer struct {
	*httptest.Server
	mu  sync.Mutex
	got string
}

func newGatewayServer(t *testing.T, body string, delay time.Duration) *gatewayServer {
	t.Helper()
	g := &gatewayServer{}
	done := make(chan struct{})
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.got = r.Header.Get("X-Gateway")
		g.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-done:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(func() {
		close(done)
		g.Close()
	})
	return g
}

func (g *gatewayServer) header() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.got
}

const tunedEmbeddingBody = `{"object":"list","data":[{"object":"embedding","index":0,` +
	`"embedding":[0.1,0.2,0.3]}],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`

const tunedRerankBody = `{"object":"list","data":[{"index":0,"relevance_score":0.9}],` +
	`"model":"rerank-2.5","usage":{"total_tokens":1}}`

// embeddingDims pins the dimension so the 3-element test vector is accepted.
const embeddingDims = "  additional_config:\n    dimensions: 3\n"

func loadTunedProvider(t *testing.T, body string) *config {
	t.Helper()
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	path := writeProviderYAML(t, t.TempDir(), "tuned.provider.yaml", body)
	c := &config{}
	require.NoError(t, WithProviderFile(path)(c))
	return c
}

func TestWithProviderFile_EmbeddingHonorsHeadersAndTimeout(t *testing.T) {
	fast := newGatewayServer(t, tunedEmbeddingBody, 0)
	c := loadTunedProvider(t, tunedProviderYAML("embedding", "openai", fast.URL, embeddingDims))
	_, err := c.embeddingProviders["tuned"].Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, "v", fast.header())

	slow := newGatewayServer(t, tunedEmbeddingBody, 2*time.Second)
	c = loadTunedProvider(t, tunedProviderYAML("embedding", "openai", slow.URL, embeddingDims))
	start := time.Now()
	_, err = c.embeddingProviders["tuned"].Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"a"}})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 300*time.Millisecond, "request_timeout 100ms must apply")
}

func TestWithProviderFile_TTSHonorsHeadersAndTimeout(t *testing.T) {
	fast := newGatewayServer(t, "audio", 0)
	c := loadTunedProvider(t, tunedProviderYAML("tts", "openai", fast.URL, ""))
	rc, err := c.ttsService.Synthesize(context.Background(), "hi", tts.SynthesisConfig{})
	require.NoError(t, err)
	_ = rc.Close()
	assert.Equal(t, "v", fast.header())

	slow := newGatewayServer(t, "audio", 2*time.Second)
	c = loadTunedProvider(t, tunedProviderYAML("tts", "openai", slow.URL, ""))
	start := time.Now()
	rc, err = c.ttsService.Synthesize(context.Background(), "hi", tts.SynthesisConfig{})
	if rc != nil {
		_ = rc.Close()
	}
	require.Error(t, err)
	assert.Less(t, time.Since(start), 300*time.Millisecond, "request_timeout 100ms must apply")
}

func TestWithProviderFile_RerankHonorsHeadersAndTimeout(t *testing.T) {
	req := providers.RerankRequest{Query: "q", Documents: []string{"d"}}
	fast := newGatewayServer(t, tunedRerankBody, 0)
	c := loadTunedProvider(t, tunedProviderYAML("rerank", "voyageai", fast.URL, ""))
	_, err := c.rerankProviders["tuned"].Rerank(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "v", fast.header())

	slow := newGatewayServer(t, tunedRerankBody, 2*time.Second)
	c = loadTunedProvider(t, tunedProviderYAML("rerank", "voyageai", slow.URL, ""))
	start := time.Now()
	_, err = c.rerankProviders["tuned"].Rerank(context.Background(), req)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 300*time.Millisecond, "request_timeout 100ms must apply")
}

func TestWithProviderFile_STTRejectsStreamRetry(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	body := tunedProviderYAML("stt", "openai", "http://127.0.0.1:1", "  stream_retry:\n    enabled: true\n")
	path := writeProviderYAML(t, t.TempDir(), "stt.provider.yaml", body)
	err := WithProviderFile(path)(&config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream_retry")
	assert.Contains(t, err.Error(), `role "stt"`)
}

// TestProviderSpecFromConfig_RoundTripsTuningFields guards the seam between
// the provider-file loader and the option family: every tuning field read
// from a file must survive providerSpecFromConfig → toPkgProvider, or the
// option it lands in cannot honor it.
func TestProviderSpecFromConfig_RoundTripsTuningFields(t *testing.T) {
	caching := false
	in := &pkgconfig.Provider{
		ID:                  "rt",
		Type:                "openai",
		Model:               "m",
		BaseURL:             "http://x",
		Credential:          cred("k"),
		AdditionalConfig:    map[string]any{"a": 1},
		Platform:            &pkgconfig.PlatformConfig{Type: "azure", Endpoint: "https://e"},
		Headers:             map[string]string{"X-A": "1"},
		RequestTimeout:      "3s",
		StreamIdleTimeout:   "45s",
		StreamRetry:         &pkgconfig.StreamRetryConfig{Enabled: true, MaxAttempts: 2},
		StreamMaxConcurrent: 5,
		HTTPTransport:       &pkgconfig.HTTPTransportConfig{MaxConnsPerHost: 9},
		Defaults:            pkgconfig.ProviderDefaults{PromptCaching: &caching},
	}
	out := providerSpecFromConfig(in).toPkgProvider()
	assert.Equal(t, in, out)
}

func TestWithLLMProvider_HonorsStreamIdleTimeout(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	c := &config{}
	require.NoError(t, WithLLMProvider(ProviderSpec{
		ID: "agent", Type: "openai", Model: "gpt-4o-mini", StreamIdleTimeout: "45s",
	})(c))
	tuned, ok := c.getAgentProvider().(interface{ StreamIdleTimeout() time.Duration })
	require.True(t, ok, "provider %T does not expose its tuning", c.getAgentProvider())
	assert.Equal(t, 45*time.Second, tuned.StreamIdleTimeout())
}
