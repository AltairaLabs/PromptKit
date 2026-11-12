package providers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
)

const (
	// DefaultGeminiBaseURL is the default base URL for Gemini API (includes version path)
	DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Registry manages available inference providers. It keeps an ID-keyed map for
// Get and List, and a base.Registry keyed by (name, capability) for typed lookups.
//
// Register and Get key on provider.ID(). The typed API (Base, GetByType, GetAll)
// delegates to the underlying base.Registry, which keys on
// (provider.Name(), provider.Type()). Registry is not safe for concurrent
// Register, Get and Close calls.
type Registry struct {
	// providers is the legacy ID-keyed map preserved for back-compat with
	// existing call sites that use Get(id) and List().
	providers map[string]Provider
	// base holds the typed (name, capability)-keyed registry used for the
	// new ancillary provider lookup API.
	base *base.Registry
}

// ProviderFactory is a function that creates a provider from a spec
type ProviderFactory func(spec ProviderSpec) (Provider, error)

// providerFactories is populated from per-provider package init() functions.
// The mutex guards it because RegisteredProviderTypes ranges over the map,
// and tests register factories after init has run.
var (
	providerFactoriesMu sync.RWMutex
	providerFactories   = make(map[string]ProviderFactory)
)

// RegisterProviderFactory registers a factory function for a provider type
func RegisterProviderFactory(providerType string, factory ProviderFactory) {
	providerFactoriesMu.Lock()
	defer providerFactoriesMu.Unlock()
	providerFactories[providerType] = factory
}

// RegisteredProviderTypes returns the completion (chat) provider types with a
// registered factory, sorted. CreateProviderFromSpec resolves against this
// registry for the llm, image and video roles alike, so a type listed here
// constructs for any of them.
func RegisteredProviderTypes() []string {
	providerFactoriesMu.RLock()
	defer providerFactoriesMu.RUnlock()
	types := make([]string, 0, len(providerFactories))
	for t := range providerFactories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// NewRegistry creates a new provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		base:      base.NewRegistry(),
	}
}

// Base returns the underlying base.Registry. Useful for cross-cutting code
// (config loaders, metric collectors) that needs the typed API.
func (r *Registry) Base() *base.Registry {
	return r.base
}

// Register adds a provider to the registry, keyed by provider.ID(), and to the
// typed base.Registry, keyed by provider.Name() and provider.Type().
//
// The last registration wins in both. A provider registered under an ID that
// is already taken replaces the earlier one for Get, List, GetByType and
// GetAll alike, and the earlier provider's typed entry is removed, so the two
// lookups never disagree. Callers rely on this: the SDK re-registers each
// pooled provider as a conversation-scoped view (MediaScoped) and expects
// every lookup to return the view. The replaced provider is not closed.
func (r *Registry) Register(provider Provider) {
	if old, ok := r.providers[provider.ID()]; ok {
		r.base.Unregister(old.Name(), old.Type())
	}
	r.providers[provider.ID()] = provider
	// Replace only fails on a nil provider, which provider.ID() above rules out.
	_ = r.base.Replace(provider)
}

// Get retrieves an inference provider by ID. Returns the provider and a
// boolean indicating if it was found, matching the legacy signature.
func (r *Registry) Get(id string) (Provider, bool) {
	provider, exists := r.providers[id]
	return provider, exists
}

// GetByType retrieves a provider by (name, capability). For non-inference
// types use this; for inference, the legacy single-arg Get(id) still works.
func (r *Registry) GetByType(name string, typ base.ProviderType) (base.Provider, error) {
	return r.base.Get(name, typ)
}

// GetAll returns every provider of the given capability registered in the
// typed base.Registry.
func (r *Registry) GetAll(typ base.ProviderType) []base.Provider {
	return r.base.GetAll(typ)
}

// List returns all registered inference provider IDs (back-compat).
func (r *Registry) List() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// Close closes the registered providers in unspecified order and stops at the
// first error, which it returns. The providers after the failing one stay
// open.
func (r *Registry) Close() error {
	for _, provider := range r.providers {
		if err := provider.Close(); err != nil {
			return err
		}
	}
	return nil
}

// ProviderSpec holds the configuration needed to create a provider instance
type ProviderSpec struct {
	ID      string
	Type    string
	Model   string
	BaseURL string
	// Headers contains custom HTTP headers to include in every request.
	// A header that collides with one the provider sets itself (compared
	// case-insensitively) makes the request fail with an error.
	Headers          map[string]string
	Defaults         ProviderDefaults
	IncludeRawOutput bool
	AdditionalConfig map[string]interface{} // Flexible key-value pairs for provider-specific configuration

	// BaseDir is the directory a relative file path in AdditionalConfig (the
	// mock provider's mock_config) resolves against. Set it to the directory
	// of the config that declared the provider. Empty means the process's
	// working directory.
	BaseDir string

	// Credential holds the resolved credential for this provider.
	// If nil, providers fall back to environment variable lookup.
	Credential Credential

	// Platform identifies the hosting platform (e.g., "bedrock", "vertex", "azure").
	// Empty string means direct API access to the provider.
	Platform string

	// PlatformConfig holds platform-specific configuration.
	// Only set when Platform is non-empty.
	PlatformConfig *PlatformConfig

	// UnsupportedParams lists sampling parameters the model does not take
	// (for example "temperature" or "top_p"). The provider does not send them.
	UnsupportedParams []string

	// Capabilities lists the modalities/features the configured model supports
	// (e.g. "text", "vision", "audio", "video", "tools", "json"). When set, it
	// is the source of truth for multimodal support, replacing per-model
	// hardcoding inside providers. Empty means "not declared", and providers
	// fall back to their built-in defaults.
	Capabilities []string

	// RequestTimeout caps the wall-clock duration of request/response HTTP
	// calls (Predict, embeddings). Zero keeps the provider's default timeout.
	// Does not apply to SSE streaming calls, which are unbounded by wall-clock
	// and governed by StreamIdleTimeout and context cancellation.
	RequestTimeout time.Duration

	// StreamIdleTimeout bounds how long an SSE streaming body may remain
	// silent before it is aborted; the timer resets on every read that returns
	// data. Zero falls back to DefaultStreamIdleTimeout.
	StreamIdleTimeout time.Duration

	// StreamRetry configures bounded retry for streaming requests that
	// fail in the pre-first-chunk window. The zero value (disabled) leaves
	// the provider with no streaming retry.
	StreamRetry StreamRetryPolicy

	// StreamRetryBudget is a pre-constructed token bucket that
	// rate-limits retry attempts across all in-flight requests on this
	// provider. Nil means unbounded retries (only MaxAttempts caps them).
	// It is applied only when StreamRetry.Enabled is true. Each provider
	// instance gets its own budget, so one misbehaving model cannot starve
	// retry capacity for others.
	StreamRetryBudget *RetryBudget

	// StreamMaxConcurrent caps the number of concurrent streaming
	// requests the provider will have in flight. Zero means unlimited. It is
	// applied through SetStreamSemaphore on providers that embed BaseProvider.
	StreamMaxConcurrent int

	// StreamObserver receives the events of every stream the provider opens
	// (see StreamObserver). It is applied through SetStreamObserver on
	// providers that embed BaseProvider. Nil observes nothing.
	StreamObserver StreamObserver

	// StorageService resolves MediaContent.StorageReference values at
	// request-build time (to a model-fetchable URL via GetURL, or bytes via
	// RetrieveMedia). It is applied through SetMediaStorageService on providers
	// implementing MediaStorageConfigurable. Nil disables storage references.
	StorageService storage.MediaStorageService

	// HTTPTransport configures the per-provider HTTP connection pool.
	// Zero-valued fields fall back to package-level defaults
	// (DefaultMaxConnsPerHost, etc.). It is applied through SetHTTPTransport on
	// providers that embed BaseProvider, replacing the default pooled
	// transport the factory constructed. MaxConnsPerHost, together with the
	// upstream's SETTINGS_MAX_CONCURRENT_STREAMS, bounds the concurrent
	// streams one process can hold open to a host.
	HTTPTransport HTTPTransportOptions

	// RateLimit caps how fast this provider instance sends requests (see
	// RateLimitOptions). The zero value is unlimited. It is applied through
	// SetRateLimit on providers that embed BaseProvider.
	RateLimit RateLimitOptions

	// MessageMetadataTemplate renders a message's application metadata
	// (types.Message.Metadata, set with the SDK's WithMessageMetadata) as
	// text the model reads. Its {{key}} placeholders are filled from each
	// message's metadata, and the result is appended to that message's text
	// in the request only; stored history is unchanged. Empty means the model
	// does not see the metadata. Exposed via MessageMetadataTemplater.
	MessageMetadataTemplate string
}

// RateLimitOptions is a token-bucket limit on one provider instance's
// outbound requests: RequestsPerSecond sustained, with up to Burst sent at
// once. Every HTTP request the provider sends (a Predict, a PredictStream,
// and each retry attempt of either) takes one token, waiting for it before
// the request goes out. A RequestsPerSecond of zero or less is unlimited; a
// Burst below 1 counts as 1. The wait respects the request's context.
type RateLimitOptions struct {
	RequestsPerSecond float64
	Burst             int
}

// Credential applies authentication to HTTP requests.
// This is the interface that providers use to authenticate requests.
type Credential interface {
	// Apply adds authentication to the HTTP request.
	Apply(ctx context.Context, req *http.Request) error

	// Type returns the credential type identifier.
	Type() string
}

// PlatformConfig is an alias for credentials.PlatformConfig.
type PlatformConfig = credentials.PlatformConfig

// timeoutConfigurable is implemented by any provider that embeds
// *BaseProvider (or BaseProvider by value and is returned as a pointer).
// CreateProviderFromSpec uses this to apply RequestTimeout and
// StreamIdleTimeout uniformly after the provider factory runs, so each
// factory does not have to thread the durations through by hand.
type timeoutConfigurable interface {
	SetHTTPTimeout(time.Duration)
	SetStreamIdleTimeout(time.Duration)
}

// streamRetryConfigurable is implemented by any provider that embeds
// *BaseProvider. CreateProviderFromSpec uses this to apply the streaming
// retry policy (and its budget) from the spec after the factory runs.
type streamRetryConfigurable interface {
	SetStreamRetryPolicy(StreamRetryPolicy)
	SetStreamRetryBudget(*RetryBudget)
}

// streamConcurrencyConfigurable is implemented by any provider that
// embeds *BaseProvider. CreateProviderFromSpec uses this to install a
// concurrent-stream semaphore from spec.StreamMaxConcurrent. Independent
// of streamRetryConfigurable because back-pressure on concurrency is
// orthogonal to retry behavior — a provider may want one, both, or
// neither.
type streamConcurrencyConfigurable interface {
	SetStreamSemaphore(*StreamSemaphore)
}

// streamObserverConfigurable is implemented by any provider that embeds
// *BaseProvider. CreateProviderFromSpec uses it to install
// spec.StreamObserver.
type streamObserverConfigurable interface {
	SetStreamObserver(StreamObserver)
}

// httpTransportConfigurable is implemented by any provider that embeds
// *BaseProvider. CreateProviderFromSpec uses this to replace the default
// pooled transport with one built from spec.HTTPTransport after the
// provider factory has run. The swap preserves the OpenTelemetry
// instrumentation wrapper so trace propagation is not lost.
type httpTransportConfigurable interface {
	SetHTTPTransport(http.RoundTripper)
}

// rateLimitConfigurable is implemented by any provider that embeds
// *BaseProvider. CreateProviderFromSpec uses it to apply spec.RateLimit.
type rateLimitConfigurable interface {
	SetRateLimit(requestsPerSecond float64, burst int)
}

// headersConfigurable is implemented by any provider that embeds
// *BaseProvider. CreateProviderFromSpec uses this to apply custom HTTP
// headers from spec.Headers after the factory runs. Custom headers are
// applied to every outgoing request and checked for collisions with
// built-in provider headers at request time.
//
// NOSONAR: name intentionally matches the existing *Configurable pattern
// for post-construction wiring interfaces (timeoutConfigurable etc.)
type headersConfigurable interface {
	SetCustomHeaders(map[string]string)
}

// messageMetadataTemplateConfigurable is implemented by any provider embedding
// *base.Implementation (every chat provider, the mock included).
// CreateProviderFromSpec uses it to apply spec.MessageMetadataTemplate.
type messageMetadataTemplateConfigurable interface {
	SetMessageMetadataTemplate(string)
}

// MediaStorageConfigurable is implemented by any provider embedding
// *BaseProvider. CreateProviderFromSpec uses it to inject the media storage
// service so providers can resolve MediaContent.StorageReference values at
// request-build time.
//
// NOSONAR: name intentionally matches the existing *Configurable pattern for
// post-construction wiring interfaces (timeoutConfigurable etc.)
type MediaStorageConfigurable interface {
	SetMediaStorageService(storage.MediaStorageService)
}

// PrivateNetworkMediaConfigurable is implemented by any provider embedding
// *BaseProvider. It lets a host that trusts every media URL it passes allow
// fetching them from non-public addresses; see
// MediaLoaderConfig.AllowPrivateNetworks.
type PrivateNetworkMediaConfigurable interface {
	SetAllowPrivateNetworkMedia(allow bool)
}

// MediaSettings are the media options one conversation applies: the store
// that resolves storage references, and whether URL media may be fetched from
// non-public addresses. A zero field keeps the provider's own setting.
type MediaSettings struct {
	Storage              storage.MediaStorageService
	AllowPrivateNetworks bool
}

// MediaScoped is implemented by a provider that can give one conversation a
// view of itself with that conversation's MediaSettings. The view shares the
// provider's clients and state; only the media settings differ, so a host
// that shares one provider across conversations keeps each one's store and
// network policy its own. Setting them on the shared provider
// (MediaStorageConfigurable) lets the last conversation opened decide for all.
type MediaScoped interface {
	WithMediaSettings(MediaSettings) Provider
}

// openAIBuildsPlatformBaseURL reports whether the openai factory derives the
// base URL itself from PlatformConfig — Azure's deployment URL, Bedrock's
// regional invoke URL, Vertex's Model Garden endpoint.
//
// For those, applying the api.openai.com default here would clobber spec.BaseURL
// and make the platform branch in NewProviderFromConfig unreachable (#1010).
// Kept as a named list because the failure is silent: the provider constructs
// fine and simply talks to the wrong host. A platform added to the factory's
// switch and forgotten here looks implemented and is not (#1768).
func openAIBuildsPlatformBaseURL(platform string) bool {
	switch platform {
	case platformAzure, platformBedrock, platformVertex:
		return true
	default:
		return false
	}
}

// CreateProviderFromSpec creates a provider implementation from a spec.
// It fills in spec.BaseURL with the type's default (OpenAI, Gemini, Claude,
// Ollama and vLLM have one) when it is empty, calls the factory registered for
// spec.Type, then applies the spec's timeouts, stream retry and concurrency
// settings, stream observer, HTTP transport, rate limit, custom headers, media
// storage and message metadata template to the provider. It returns an UnsupportedProviderError
// when no factory is registered for spec.Type.
func CreateProviderFromSpec(spec ProviderSpec) (Provider, error) {
	baseURL := defaultBaseURLForSpec(&spec)

	// Update spec with default baseURL
	spec.BaseURL = baseURL

	// Look up the factory for this provider type
	providerFactoriesMu.RLock()
	factory, exists := providerFactories[spec.Type]
	providerFactoriesMu.RUnlock()
	if !exists {
		return nil, &UnsupportedProviderError{ProviderType: spec.Type}
	}

	provider, err := factory(spec)
	if err != nil {
		return nil, err
	}

	applySpecConfig(provider, &spec)

	return provider, nil
}

// providerTypeOpenAI is the provider type name of the OpenAI provider.
const providerTypeOpenAI = "openai"

// defaultBaseURLForSpec returns spec.BaseURL, or the provider type's default
// base URL when it is empty.
func defaultBaseURLForSpec(spec *ProviderSpec) string {
	// Use default base URLs if not specified.
	baseURL := spec.BaseURL
	if baseURL == "" {
		switch spec.Type {
		case providerTypeOpenAI:
			if !openAIBuildsPlatformBaseURL(spec.Platform) {
				baseURL = "https://api.openai.com/v1"
			}
		case "gemini":
			// Skip the AI Studio default for Vertex — the gemini
			// factory builds the publisher-models URL from
			// PlatformConfig.Region+Project. Same #1010 root cause as
			// openai+azure: the default would clobber spec.BaseURL
			// and make the Vertex branch unreachable.
			if spec.Platform != platformVertex {
				baseURL = DefaultGeminiBaseURL
			}
		case "claude":
			// Skip the api.anthropic.com default for hyperscaler-hosted
			// Anthropic — the claude factory builds the platform URL from
			// PlatformConfig (Vertex: publishers/anthropic/models; Azure:
			// openai/deployments/{deployment}; Bedrock:
			// bedrock-runtime.{region}.amazonaws.com). Same #1010 root cause.
			// See #1029 for the Bedrock case that previously fell through.
			if spec.Platform != platformVertex && spec.Platform != platformAzure &&
				spec.Platform != platformBedrock {
				baseURL = "https://api.anthropic.com"
			}
		case "ollama":
			baseURL = "http://localhost:11434"
		case "vllm":
			baseURL = "http://localhost:8000"
		case "mock":
			// No base URL needed for mock provider
		}
	}
	return baseURL
}

// applySpecConfig applies the spec's optional runtime settings (timeouts,
// stream retry, concurrency, transport, headers, media storage, metadata
// template) to a freshly constructed provider.
func applySpecConfig(provider Provider, spec *ProviderSpec) {
	applyStreamingConfig(provider, spec)
	applyTransportConfig(provider, spec)
}

// applyStreamingConfig applies the timeout, stream-retry, stream concurrency
// and stream observer settings from spec.
func applyStreamingConfig(provider Provider, spec *ProviderSpec) {
	// Apply configured timeouts. Zero values leave the defaults in place
	// (BaseProvider's constructor-time defaults / DefaultStreamIdleTimeout).
	if tc, ok := provider.(timeoutConfigurable); ok {
		if spec.RequestTimeout > 0 {
			tc.SetHTTPTimeout(spec.RequestTimeout)
		}
		if spec.StreamIdleTimeout > 0 {
			tc.SetStreamIdleTimeout(spec.StreamIdleTimeout)
		}
	}

	// Apply the streaming retry policy and its (optional) budget. Both
	// are only applied when StreamRetry.Enabled is true — a budget
	// without retry enabled would be silently ignored at request time,
	// and allocating one anyway wastes tokens that would never be
	// consulted. Gating both together makes the configuration
	// self-consistent.
	if src, ok := provider.(streamRetryConfigurable); ok && spec.StreamRetry.Enabled {
		src.SetStreamRetryPolicy(spec.StreamRetry)
		if spec.StreamRetryBudget != nil {
			src.SetStreamRetryBudget(spec.StreamRetryBudget)
		}
	}

	// Apply the concurrent-stream semaphore. Independent of retry: a
	// provider may want concurrency bounds without retry, or vice
	// versa. A non-positive limit is a no-op (unlimited default).
	if scc, ok := provider.(streamConcurrencyConfigurable); ok && spec.StreamMaxConcurrent > 0 {
		scc.SetStreamSemaphore(NewStreamSemaphore(spec.StreamMaxConcurrent))
	}

	if soc, ok := provider.(streamObserverConfigurable); ok && spec.StreamObserver != nil {
		soc.SetStreamObserver(spec.StreamObserver)
	}
}

// applyTransportConfig applies the HTTP transport, custom headers, media
// storage and message-metadata template settings from spec.
func applyTransportConfig(provider Provider, spec *ProviderSpec) {
	// Replace the HTTP transport with a pooled-and-instrumented one
	// built from spec.HTTPTransport. Done unconditionally (not gated
	// on operator overrides) so the conn-tracking wrapper is always
	// in play — the http_conns_in_use gauge needs to be populated
	// whether the operator has tuned pool config or not. Zero-valued
	// options in spec.HTTPTransport fall back to package defaults, so
	// the replacement is functionally equivalent to the factory's
	// original transport when no overrides are set, with the added
	// conn-tracking layer.
	if htc, ok := provider.(httpTransportConfigurable); ok {
		htc.SetHTTPTransport(NewProviderTransport(spec.HTTPTransport))
	}

	// Rate-limit every outbound request. Applied after the transport swap
	// (SetHTTPTransport keeps an existing limit in front either way). A
	// non-positive rate leaves the provider unlimited.
	if rlc, ok := provider.(rateLimitConfigurable); ok && spec.RateLimit.RequestsPerSecond > 0 {
		rlc.SetRateLimit(spec.RateLimit.RequestsPerSecond, spec.RateLimit.Burst)
	}

	// Apply custom HTTP headers for gateway compatibility (OpenRouter,
	// LiteLLM, etc.). Only applied when headers are configured — an
	// empty map is a no-op at request time.
	if hc, ok := provider.(headersConfigurable); ok && len(spec.Headers) > 0 {
		hc.SetCustomHeaders(spec.Headers)
	}

	// Inject the media storage service so the provider can resolve
	// MediaContent.StorageReference values at request-build time.
	if mc, ok := provider.(MediaStorageConfigurable); ok && spec.StorageService != nil {
		mc.SetMediaStorageService(spec.StorageService)
	}

	if mt, ok := provider.(messageMetadataTemplateConfigurable); ok && spec.MessageMetadataTemplate != "" {
		mt.SetMessageMetadataTemplate(spec.MessageMetadataTemplate)
	}
}

// UnsupportedProviderError is returned when no factory is registered for a
// provider type. When the type names a hosting platform (bedrock, vertex or
// azure), the message says which vendor types to use instead.
type UnsupportedProviderError struct {
	ProviderType string
}

// platformVendors names the vendors reachable on each hosting platform.
//
// A platform is not a provider type: the type is the model's vendor, and the
// platform says who hosts it. Someone deploying to one of these writes its name
// as the type — it is the word in front of them — and "unsupported provider
// type: bedrock" does not tell them what to write instead. The pack deploys,
// the agent reaches ready, and it fails on its first turn, which is the
// expensive place to learn this.
var platformVendors = map[string]string{
	"bedrock": "claude or openai",
	"vertex":  "gemini or claude",
	"azure":   "openai",
}

// Error returns the error message for this unsupported provider error.
func (e *UnsupportedProviderError) Error() string {
	if vendors, ok := platformVendors[e.ProviderType]; ok {
		return fmt.Sprintf(
			"unsupported provider type: %q is a hosting platform, not a provider type — "+
				"set type to the model vendor (%s) and platform to {\"type\": %q}",
			e.ProviderType, vendors, e.ProviderType)
	}
	return "unsupported provider type: " + e.ProviderType
}

// UnsupportedProviderPlatformError is returned when a provider type is
// recognized but the requested platform does not host that vendor's models
// (for example openai on vertex). It is returned at provider construction, so
// the failure surfaces at configuration time rather than on the first request.
type UnsupportedProviderPlatformError struct {
	ProviderType string
	Platform     string
}

// Error returns the error message for this unsupported pair.
func (e *UnsupportedProviderPlatformError) Error() string {
	return "provider type " + e.ProviderType +
		" does not support platform " + e.Platform +
		" — this combination is not offered by the platform vendor"
}

// RejectPlatforms wraps a ProviderFactory so it returns an
// UnsupportedProviderPlatformError when spec.Platform is in rejected.
// Per-provider init() functions use it to fail fast on (provider, platform)
// pairs the underlying vendor does not host.
func RejectPlatforms(rejected map[string]bool, inner ProviderFactory) ProviderFactory {
	return func(spec ProviderSpec) (Provider, error) {
		if rejected[spec.Platform] {
			return nil, &UnsupportedProviderPlatformError{
				ProviderType: spec.Type,
				Platform:     spec.Platform,
			}
		}
		return inner(spec)
	}
}

// HasCredential returns true if the spec has a credential whose type is not
// "none". Factory functions use it to choose between credential-based and
// environment-variable-based constructors.
func (s *ProviderSpec) HasCredential() bool {
	return s.Credential != nil && s.Credential.Type() != "none"
}

// CredentialFactory builds a ProviderFactory that calls withCred when the spec
// has a credential (HasCredential) and withoutCred otherwise.
//
// Example, in a provider's init():
//
//	providers.RegisterProviderFactory("claude", providers.CredentialFactory(
//	    func(spec providers.ProviderSpec) (providers.Provider, error) {
//	        return NewToolProviderWithCredential(spec.ID, spec.Model, ..., spec.Credential, ...), nil
//	    },
//	    func(spec providers.ProviderSpec) (providers.Provider, error) {
//	        return NewToolProvider(spec.ID, spec.Model, ...), nil
//	    },
//	))
func CredentialFactory(withCred, withoutCred ProviderFactory) ProviderFactory {
	return func(spec ProviderSpec) (Provider, error) {
		if spec.HasCredential() {
			return withCred(spec)
		}
		return withoutCred(spec)
	}
}
