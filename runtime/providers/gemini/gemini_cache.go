package gemini

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Explicit context caching (Gemini CachedContent API).
//
// PromptKit otherwise relies on Gemini's IMPLICIT caching, which is best-effort:
// Google decides when a prefix becomes cached, and in long agentic loops the
// first ~10 rounds can run with cachedContentTokenCount=0 — paying full price
// for the whole warmup. Explicit caching (#1404) creates a CachedContent
// resource holding the stable prefix (system + tools) and references it on every
// generateContent call, so the cache hits from the very first reference — the
// Gemini equivalent of Claude's explicit breakpoints.
//
// Hard API constraint (verified live): a request that sets cachedContent must
// NOT also set system_instruction, tools, or tool_config — those move into the
// CachedContent. So each request path, when a handle exists, drops the inline
// prefix and references the cache instead.
//
// The cache is content-addressed by hash(model, system, tools): any request
// with the same stable prefix reuses the same resource — no conversation ID
// needed. A creation/lookup failure NEVER breaks a turn; it degrades to the
// inline (implicit-caching) path.

const (
	// defaultExplicitCacheTTL is how long a created CachedContent lives. Long
	// enough to span a full agent loop; the resource is content-addressed and
	// shared, so one create amortizes across many rounds and conversations.
	defaultExplicitCacheTTL = 10 * time.Minute

	// cacheExpiryMargin makes the manager re-create a handle slightly before its
	// real TTL, so a turn never references an about-to-expire resource.
	cacheExpiryMargin = 60 * time.Second

	// defaultMinCacheChars is the prefix size (in characters) below which
	// explicit caching is skipped. Creating a CachedContent below the model's
	// floor (~1024 tokens for 2.5) costs more than it saves, so only cache a
	// substantial prefix. ~4 chars/token, so ~1024 tokens.
	defaultMinCacheChars = 4096

	// cacheFailureCooldown throttles re-creation after a failed create so a
	// persistently failing prefix doesn't POST cachedContents every turn.
	cacheFailureCooldown = 2 * time.Minute
)

// cacheEntry is a live CachedContent resource handle with its local expiry.
type cacheEntry struct {
	name   string
	expiry time.Time
}

// cacheManager tracks CachedContent resources keyed by stable-prefix hash. It is
// safe for concurrent use by the agent loop's parallel turns.
type cacheManager struct {
	mu        sync.Mutex
	entries   map[string]cacheEntry
	failUntil map[string]time.Time
	ttl       time.Duration
	minChars  int
}

func newCacheManager(ttl time.Duration) *cacheManager {
	if ttl <= 0 {
		ttl = defaultExplicitCacheTTL
	}
	return &cacheManager{
		entries:   map[string]cacheEntry{},
		failUntil: map[string]time.Time{},
		ttl:       ttl,
		minChars:  defaultMinCacheChars,
	}
}

// Close best-effort deletes any CachedContent resources this provider created
// (lifecycle cleanup; TTL would expire them anyway) before delegating to the
// embedded BaseProvider.
func (p *Provider) Close() error {
	if p.cache != nil {
		for _, name := range p.cache.trackedNames() {
			p.deleteCachedContent(context.Background(), name)
		}
	}
	return p.BaseProvider.Close()
}

// applyExplicitCachingConfig enables explicit context caching when the provider
// config opts in via additional_config.explicit_caching: true. An optional
// explicit_cache_ttl_seconds (number) overrides the default TTL. Off by default.
//
//nolint:gocritic // hugeParam: providers.ProviderSpec is passed by value across the factory
func applyExplicitCachingConfig(p *Provider, spec providers.ProviderSpec) {
	if spec.AdditionalConfig == nil {
		return
	}
	enabled, _ := spec.AdditionalConfig["explicit_caching"].(bool)
	if !enabled {
		return
	}
	ttl := defaultExplicitCacheTTL
	if secs, ok := toFloat(spec.AdditionalConfig["explicit_cache_ttl_seconds"]); ok && secs > 0 {
		ttl = time.Duration(secs) * time.Second
	}
	p.enableExplicitCaching(ttl)
}

// toFloat coerces a config value (which may decode as float64, int, or int64)
// to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// resolveCachedContent returns the CachedContent resource name to reference for
// the given stable prefix (system + tools), creating it on first use and reusing
// it within TTL. It returns "" to signal "send the prefix inline" — when
// explicit caching is disabled, the prefix is too small, or any create/lookup
// failed (degrade to implicit). Works on both AI Studio and Vertex. toolsField is
// the request's "tools" value ([]any{decl}) or nil.
func (p *Provider) resolveCachedContent(ctx context.Context, systemText string, toolsField any) string {
	if p.cache == nil {
		return ""
	}
	return p.cache.getOrCreate(ctx, p, systemText, toolsField)
}

// getOrCreate returns a cached resource name for the prefix, or "" to degrade.
func (m *cacheManager) getOrCreate(ctx context.Context, p *Provider, systemText string, toolsField any) string {
	toolsJSON := marshalToolsForCache(toolsField)

	// Min-size guard: skip prefixes below the model's cacheable floor.
	if len(systemText)+len(toolsJSON) < m.minChars {
		return ""
	}

	key := cacheKey(p.model, systemText, toolsJSON)

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if e, ok := m.entries[key]; ok && now.Before(e.expiry.Add(-cacheExpiryMargin)) {
		return e.name
	}
	if until, ok := m.failUntil[key]; ok && now.Before(until) {
		return ""
	}

	name, err := p.createCachedContent(ctx, systemText, toolsField, m.ttl)
	if err != nil {
		m.failUntil[key] = now.Add(cacheFailureCooldown)
		logger.Warn("Gemini explicit caching: create failed, using implicit caching for this prefix",
			"provider", p.ID(), "error", err)
		return ""
	}
	m.entries[key] = cacheEntry{name: name, expiry: now.Add(m.ttl)}
	logger.Debug("Gemini explicit caching: created CachedContent",
		"provider", p.ID(), "name", name, "ttl", m.ttl.String())
	return name
}

// trackedNames returns the resource names currently held, for cleanup.
func (m *cacheManager) trackedNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.entries))
	for _, e := range m.entries {
		names = append(names, e.name)
	}
	return names
}

// cacheKey content-addresses a stable prefix.
func cacheKey(model, systemText, toolsJSON string) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(systemText))
	h.Write([]byte{0})
	h.Write([]byte(toolsJSON))
	return hex.EncodeToString(h.Sum(nil))
}

// marshalToolsForCache renders the tools field deterministically for hashing and
// for the create body. Returns "" when there are no tools.
func marshalToolsForCache(toolsField any) string {
	if toolsField == nil {
		return ""
	}
	b, err := json.Marshal(toolsField)
	if err != nil {
		return ""
	}
	return string(b)
}

// cachedContentCreateBody is the CachedContent create payload. Mirrors the
// generateContent prefix shape so the cached and dropped content are identical.
type cachedContentCreateBody struct {
	Model             string         `json:"model"`
	SystemInstruction *geminiContent `json:"systemInstruction,omitempty"`
	Tools             any            `json:"tools,omitempty"`
	TTL               string         `json:"ttl"`
}

type cachedContentCreateResp struct {
	Name  string `json:"name"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// createCachedContent POSTs a CachedContent resource holding the stable prefix
// and returns its resource name. Works on both AI Studio (name like
// "cachedContents/abc123") and Vertex (full "projects/.../cachedContents/123"
// path) — the URL, model field, and auth vary by platform (see the helpers).
func (p *Provider) createCachedContent(
	ctx context.Context, systemText string, toolsField any, ttl time.Duration,
) (string, error) {
	body := cachedContentCreateBody{
		Model: p.cachedContentModelField(),
		TTL:   fmt.Sprintf("%ds", int(ttl.Seconds())),
	}
	if systemText != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: systemText}}}
	}
	if toolsField != nil {
		body.Tools = toolsField
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal cachedContent: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.cachedContentsCreateURL(), bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("create cachedContent request: %w", err)
	}
	httpReq.Header.Set(contentTypeHeader, applicationJSON)
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return "", fmt.Errorf("apply cachedContent auth: %w", authErr)
	}
	if hdrErr := p.ApplyCustomHeaders(httpReq); hdrErr != nil {
		return "", hdrErr
	}

	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return "", &providers.ProviderTransportError{Cause: err, Provider: p.ID()}
	}
	defer resp.Body.Close()

	respBody, err := providers.ReadResponseBody(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read cachedContent response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cachedContent create returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed cachedContentCreateResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode cachedContent response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("cachedContent create error: %s", parsed.Error.Message)
	}
	if parsed.Name == "" {
		return "", fmt.Errorf("cachedContent create returned no name: %s", string(respBody))
	}
	return parsed.Name, nil
}

// deleteCachedContent best-effort deletes a CachedContent resource (cleanup).
func (p *Provider) deleteCachedContent(ctx context.Context, name string) {
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", p.cachedContentDeleteURL(name), http.NoBody)
	if err != nil {
		return
	}
	if authErr := p.applyAuth(ctx, httpReq); authErr != nil {
		return
	}
	resp, err := p.GetHTTPClient().Do(httpReq)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// CachedContent endpoints differ by platform. AI Studio (direct) uses
// generativelanguage.googleapis.com/v1beta/cachedContents with the API key in
// the query string and a "models/<model>" model field. Vertex uses the regional
// aiplatform host under the project/location, Bearer auth (applied separately),
// and a full publishers/google/models path; created resources are referenced by
// their full returned resource name.

// cachedContentsCreateURL returns the POST URL for creating a CachedContent.
func (p *Provider) cachedContentsCreateURL() string {
	if p.isVertex() {
		// baseURL: .../projects/{proj}/locations/{loc}/publishers/google/models
		return strings.TrimSuffix(p.baseURL, "/publishers/google/models") + "/cachedContents"
	}
	return fmt.Sprintf("%s/cachedContents?key=%s", p.baseURL, p.apiKey)
}

// cachedContentModelField returns the create-body "model" value.
func (p *Provider) cachedContentModelField() string {
	if p.isVertex() {
		if i := strings.Index(p.baseURL, "projects/"); i >= 0 {
			return p.baseURL[i:] + "/" + p.model
		}
	}
	return "models/" + p.model
}

// cachedContentDeleteURL returns the DELETE URL for a CachedContent resource
// name. On Vertex the name is a full resource path appended to the API root.
func (p *Provider) cachedContentDeleteURL(name string) string {
	if p.isVertex() {
		if i := strings.Index(p.baseURL, "/v1/"); i >= 0 {
			return p.baseURL[:i+len("/v1/")] + name
		}
	}
	return fmt.Sprintf("%s/%s?key=%s", p.baseURL, name, p.apiKey)
}
