package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// geminiGenOKBody is a minimal valid generateContent response.
const geminiGenOKBody = `{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},` +
	`"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":10,` +
	`"candidatesTokenCount":2,"cachedContentTokenCount":8,"totalTokenCount":12}}`

// explicitCacheServer routes the two endpoints explicit caching touches: the
// CachedContent create (POST .../cachedContents) and generateContent. It records
// how many creates happened and the last generateContent body, so a test can
// assert whether the request referenced a cache or sent the prefix inline.
type explicitCacheServer struct {
	url          string
	createCalls  int32
	createStatus int
	lastGenBody  []byte
}

func newExplicitCacheServer(t *testing.T, createStatus int) *explicitCacheServer {
	t.Helper()
	s := &explicitCacheServer{createStatus: createStatus}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/cachedContents"):
			atomic.AddInt32(&s.createCalls, 1)
			if s.createStatus != http.StatusOK {
				w.WriteHeader(s.createStatus)
				_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"name":"cachedContents/test123"}`)
		case strings.Contains(r.URL.Path, ":generateContent"),
			strings.Contains(r.URL.Path, ":streamGenerateContent"):
			body, _ := io.ReadAll(r.Body)
			s.lastGenBody = body
			_, _ = io.WriteString(w, geminiGenOKBody)
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

func (s *explicitCacheServer) genBody(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(s.lastGenBody, &m); err != nil {
		t.Fatalf("generateContent body not JSON: %v (%s)", err, s.lastGenBody)
	}
	return m
}

// bigSystem clears the explicit-caching min-size floor (defaultMinCacheChars).
var bigSystem = strings.Repeat("You are a meticulous coding agent. Obey conventions. ", 120)

func newExplicitCacheProvider(t *testing.T, baseURL string, additional map[string]any) providers.Provider {
	t.Helper()
	t.Setenv("GEMINI_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "test-gemini", Type: "gemini", Model: "gemini-2.5-flash",
		BaseURL: baseURL, AdditionalConfig: additional,
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	return p
}

// When explicit caching is enabled and the prefix clears the floor, the request
// references a CachedContent resource and drops the inline systemInstruction.
func TestExplicitCaching_Predict_ReferencesCache(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, map[string]any{"explicit_caching": true})

	_, err := p.Predict(context.Background(), providers.PredictionRequest{
		System:   bigSystem,
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	body := srv.genBody(t)
	if body["cachedContent"] != "cachedContents/test123" {
		t.Errorf("expected cachedContent reference, got %v", body["cachedContent"])
	}
	if _, has := body["systemInstruction"]; has {
		t.Error("systemInstruction must be dropped when cachedContent is set (API rejects both)")
	}
	if atomic.LoadInt32(&srv.createCalls) != 1 {
		t.Errorf("expected exactly 1 cachedContents create, got %d", srv.createCalls)
	}
}

// The tool path must also reference the cache and drop systemInstruction, tools,
// and tool_config (all three are rejected alongside cachedContent).
func TestExplicitCaching_Tools_DropsPrefix(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, map[string]any{"explicit_caching": true})
	tp := p.(*ToolProvider)
	tools, _ := tp.BuildTooling([]*providers.ToolDescriptor{
		{Name: "read_file", Description: "read a file", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})

	_, _, err := tp.PredictWithTools(context.Background(), providers.PredictionRequest{
		System:   bigSystem,
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}, tools, "")
	if err != nil {
		t.Fatalf("PredictWithTools: %v", err)
	}
	body := srv.genBody(t)
	if body["cachedContent"] != "cachedContents/test123" {
		t.Errorf("expected cachedContent reference, got %v", body["cachedContent"])
	}
	for _, k := range []string{"systemInstruction", "tools", "tool_config"} {
		if _, has := body[k]; has {
			t.Errorf("%q must be dropped when cachedContent is set", k)
		}
	}
}

// A CachedContent create failure must degrade to the inline prefix without
// erroring the turn.
func TestExplicitCaching_CreateFailure_DegradesToInline(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusInternalServerError)
	p := newExplicitCacheProvider(t, srv.url, map[string]any{"explicit_caching": true})

	_, err := p.Predict(context.Background(), providers.PredictionRequest{
		System:   bigSystem,
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict must succeed despite cache create failure: %v", err)
	}
	body := srv.genBody(t)
	if _, has := body["cachedContent"]; has {
		t.Error("cachedContent must not be set when create failed")
	}
	if _, has := body["systemInstruction"]; !has {
		t.Error("systemInstruction must be sent inline when caching degrades")
	}
}

// A prefix below the min-size floor must skip the cache entirely (creating a
// tiny CachedContent costs more than it saves).
func TestExplicitCaching_BelowMinSize_SkipsCache(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, map[string]any{"explicit_caching": true})

	_, err := p.Predict(context.Background(), providers.PredictionRequest{
		System:   "short system", // well under defaultMinCacheChars
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if atomic.LoadInt32(&srv.createCalls) != 0 {
		t.Errorf("expected no cachedContents create for a small prefix, got %d", srv.createCalls)
	}
	if _, has := srv.genBody(t)["systemInstruction"]; !has {
		t.Error("small-prefix request must send systemInstruction inline")
	}
}

// Off by default: with no explicit_caching config, the request sends the prefix
// inline and never touches the CachedContent endpoint.
func TestExplicitCaching_DisabledByDefault(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, nil)

	_, err := p.Predict(context.Background(), providers.PredictionRequest{
		System:   bigSystem,
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Predict: %v", err)
	}
	if atomic.LoadInt32(&srv.createCalls) != 0 {
		t.Errorf("explicit caching must be off by default, got %d creates", srv.createCalls)
	}
	if _, has := srv.genBody(t)["systemInstruction"]; !has {
		t.Error("default path must send systemInstruction inline")
	}
}

// A 200 response carrying an error field, or no resource name, must degrade to
// inline rather than referencing a bogus cache.
func TestExplicitCaching_CreateErrorBodies_Degrade(t *testing.T) {
	for _, tc := range []struct {
		name       string
		createBody string
	}{
		{"error field", `{"error":{"message":"quota exceeded"}}`},
		{"no name", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/cachedContents") {
					_, _ = io.WriteString(w, tc.createBody)
					return
				}
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "cachedContent") {
					t.Error("request must not reference a cache when create yielded no usable name")
				}
				_, _ = io.WriteString(w, geminiGenOKBody)
			}))
			t.Cleanup(srv.Close)

			p := newExplicitCacheProvider(t, srv.URL, map[string]any{"explicit_caching": true})
			if _, err := p.Predict(context.Background(), providers.PredictionRequest{
				System: bigSystem, Messages: []types.Message{{Role: "user", Content: "hi"}},
			}); err != nil {
				t.Fatalf("Predict must succeed despite bad cache create: %v", err)
			}
		})
	}
}

// Tracked resources are exposed for cleanup, and deleteCachedContent issues the
// DELETE without erroring.
func TestExplicitCaching_TrackedNamesAndDelete(t *testing.T) {
	var deleted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/cachedContents/"):
			atomic.AddInt32(&deleted, 1)
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/cachedContents"):
			_, _ = io.WriteString(w, `{"name":"cachedContents/test123"}`)
		case strings.Contains(r.URL.Path, ":generateContent"):
			_, _ = io.WriteString(w, geminiGenOKBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	p := newExplicitCacheProvider(t, srv.URL, map[string]any{"explicit_caching": true})
	tp := p.(*ToolProvider)
	if _, err := tp.Predict(context.Background(), providers.PredictionRequest{
		System: bigSystem, Messages: []types.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Predict: %v", err)
	}

	names := tp.cache.trackedNames()
	if len(names) != 1 || names[0] != "cachedContents/test123" {
		t.Fatalf("trackedNames = %v, want [cachedContents/test123]", names)
	}
	tp.deleteCachedContent(context.Background(), names[0])
	if atomic.LoadInt32(&deleted) != 1 {
		t.Errorf("expected 1 DELETE, got %d", deleted)
	}
}

// The TTL config accepts float64/int/int64 and ignores non-numeric values
// (falling back to the default).
func TestExplicitCaching_TTLConfig(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want time.Duration
	}{
		{"float64", float64(30), 30 * time.Second},
		{"int", 45, 45 * time.Second},
		{"int64", int64(60), 60 * time.Second},
		{"bad-type-uses-default", "nope", defaultExplicitCacheTTL},
		{"zero-uses-default", 0, defaultExplicitCacheTTL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{}
			applyExplicitCachingConfig(p, providers.ProviderSpec{
				AdditionalConfig: map[string]any{
					"explicit_caching":           true,
					"explicit_cache_ttl_seconds": tc.val,
				},
			})
			if p.cache == nil {
				t.Fatal("cache should be enabled")
			}
			if p.cache.ttl != tc.want {
				t.Errorf("ttl = %v, want %v", p.cache.ttl, tc.want)
			}
		})
	}
}

// The created CachedContent is reused across rounds: a second request with the
// same prefix references the same resource without creating another.
func TestExplicitCaching_ReusesAcrossRounds(t *testing.T) {
	srv := newExplicitCacheServer(t, http.StatusOK)
	p := newExplicitCacheProvider(t, srv.url, map[string]any{"explicit_caching": true})

	for i := 0; i < 3; i++ {
		if _, err := p.Predict(context.Background(), providers.PredictionRequest{
			System:   bigSystem,
			Messages: []types.Message{{Role: "user", Content: "round"}},
		}); err != nil {
			t.Fatalf("Predict round %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&srv.createCalls); got != 1 {
		t.Errorf("expected 1 create reused across 3 rounds, got %d", got)
	}
}
