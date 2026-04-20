package providers

import (
	"context"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
)

const (
	// DefaultGeminiBaseURL is the default base URL for Gemini API (includes version path)
	DefaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Registry manages available providers
type Registry struct {
	providers map[string]Provider
}

// ProviderFactory is a function that creates a provider from a spec
type ProviderFactory func(spec ProviderSpec) (Provider, error)

var providerFactories = make(map[string]ProviderFactory)

// RegisterProviderFactory registers a factory function for a provider type
func RegisterProviderFactory(providerType string, factory ProviderFactory) {
	providerFactories[providerType] = factory
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry using its ID as the key.
func (r *Registry) Register(provider Provider) {
	r.providers[provider.ID()] = provider
}

// Get retrieves a provider by ID, returning the provider and a boolean indicating if it was found.
func (r *Registry) Get(id string) (Provider, bool) {
	provider, exists := r.providers[id]
	return provider, exists
}

// List returns all registered provider IDs
func (r *Registry) List() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// Close closes all registered providers and cleans up their resources.
// Returns the first error encountered, if any.
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
	// Applied after built-in provider headers; collisions cause an error.
	Headers          map[string]string
	Defaults         ProviderDefaults
	IncludeRawOutput bool
	AdditionalConfig map[string]interface{} // Flexible key-value pairs for provider-specific configuration

	// Credential holds the resolved credential for this provider.
	// If nil, providers fall back to environment variable lookup.
	Credential Credential

	// Platform identifies the hosting platform (e.g., "bedrock", "vertex", "azure").
	// Empty string means direct API access to the provider.
	Platform string

	// PlatformConfig holds platform-specific configuration.
	// Only set when Platform is non-empty.
	PlatformConfig *PlatformConfig

	// UnsupportedParams lists model parameters not supported by this provider model.
	// For example, o-series OpenAI models don't support "temperature", "top_p", or "max_tokens".
	UnsupportedParams []string

	// RequestTimeout caps the wall-clock duration of request/response HTTP
	// calls (Predict, embeddings). Zero falls back to
	// httputil.DefaultProviderTimeout. Does not apply to SSE streaming
	// calls, which are unbounded by wall-clock and governed by
	// StreamIdleTimeout + context cancellation. Pre-parsed from
	// config.Provider.RequestTimeout by the arena loader.
	RequestTimeout time.Duration

	// StreamIdleTimeout bounds how long an SSE streaming body may remain
	// silent before it is aborted; timer resets on every byte. Zero falls
	// back to DefaultStreamIdleTimeout. Pre-parsed from
	// config.Provider.StreamIdleTimeout by the arena loader.
	StreamIdleTimeout time.Duration

	// StreamRetry configures bounded retry for streaming requests that
	// fail in the pre-first-chunk window. Zero value (disabled) leaves
	// the provider with no streaming retry. Pre-parsed from
	// config.Provider.StreamRetry by the arena loader.
	StreamRetry StreamRetryPolicy

	// StreamRetryBudget is a pre-constructed token bucket that
	// rate-limits retry attempts across all in-flight requests on this
	// provider. Nil means "unbounded retries" (only MaxAttempts caps
	// them). Pre-parsed from config.Provider.StreamRetry.Budget by the
	// arena loader. Each provider instance gets its own budget so one
	// misbehaving model cannot starve retry capacity for others.
	StreamRetryBudget *RetryBudget

	// StreamMaxConcurrent caps the number of concurrent streaming
	// requests the provider will have in flight. Zero means unlimited
	// (backwards-compatible default). Pre-parsed from
	// config.Provider.StreamMaxConcurrent by the arena loader. Applied
	// via SetStreamSemaphore on providers that implement the
	// streamConcurrencyConfigurable interface.
	StreamMaxConcurrent int

	// HTTPTransport configures the per-provider HTTP connection pool.
	// Zero-valued fields fall back to package-level defaults
	// (DefaultMaxConnsPerHost, etc.). Applied via SetHTTPTransport on
	// providers that implement the httpTransportConfigurable interface,
	// replacing the default pooled transport the factory constructed.
	//
	// See AltairaLabs/PromptKit#873 for motivation: at higher concurrency
	// than the repo was originally sized for, the hardcoded
	// MaxConnsPerHost: 100 becomes the wall before PromptKit's own
	// machinery does. Raising this (paired with operator awareness of
	// the upstream's SETTINGS_MAX_CONCURRENT_STREAMS) is the primary
	// lever on realistic single-process concurrent-stream capacity.
	HTTPTransport HTTPTransportOptions
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

// httpTransportConfigurable is implemented by any provider that embeds
// *BaseProvider. CreateProviderFromSpec uses this to replace the default
// pooled transport with one built from spec.HTTPTransport after the
// provider factory has run. The swap preserves the OpenTelemetry
// instrumentation wrapper so trace propagation is not lost.
type httpTransportConfigurable interface {
	SetHTTPTransport(http.RoundTripper)
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

// CreateProviderFromSpec creates a provider implementation from a spec.
// Returns an error if the provider type is unsupported.
func CreateProviderFromSpec(spec ProviderSpec) (Provider, error) {
	// Use default base URLs if not specified.
	baseURL := spec.BaseURL
	if baseURL == "" {
		switch spec.Type {
		case "openai":
			// Skip the api.openai.com default for hyperscaler-hosted
			// OpenAI — the openai factory builds the platform URL from
			// PlatformConfig (Azure: deployment URL; Bedrock: regional
			// invoke URL). Without these skips the default clobbers
			// spec.BaseURL and the platform branch in
			// NewProviderFromConfig becomes unreachable (#1010).
			if spec.Platform != "azure" && spec.Platform != "bedrock" {
				baseURL = "https://api.openai.com/v1"
			}
		case "gemini":
			// Skip the AI Studio default for Vertex — the gemini
			// factory builds the publisher-models URL from
			// PlatformConfig.Region+Project. Same #1010 root cause as
			// openai+azure: the default would clobber spec.BaseURL
			// and make the Vertex branch unreachable.
			if spec.Platform != "vertex" {
				baseURL = DefaultGeminiBaseURL
			}
		case "claude":
			// Skip the api.anthropic.com default for hyperscaler-hosted
			// Anthropic — the claude factory builds the platform URL from
			// PlatformConfig (Vertex: publishers/anthropic/models; Azure:
			// openai/deployments/{deployment}). Same #1010 root cause.
			if spec.Platform != "vertex" && spec.Platform != "azure" {
				baseURL = "https://api.anthropic.com"
			}
		case "imagen":
			baseURL = DefaultGeminiBaseURL
		case "ollama":
			baseURL = "http://localhost:11434"
		case "vllm":
			baseURL = "http://localhost:8000"
		case "mock":
			// No base URL needed for mock provider
		}
	}

	// Update spec with default baseURL
	spec.BaseURL = baseURL

	// Look up the factory for this provider type
	factory, exists := providerFactories[spec.Type]
	if !exists {
		return nil, &UnsupportedProviderError{ProviderType: spec.Type}
	}

	provider, err := factory(spec)
	if err != nil {
		return nil, err
	}

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
		base := NewPooledTransportWithOptions(spec.HTTPTransport)
		rt := NewInstrumentedTransport(base)
		rt = newConnTrackingTransport(rt, DefaultStreamMetrics())
		htc.SetHTTPTransport(rt)
	}

	// Apply custom HTTP headers for gateway compatibility (OpenRouter,
	// LiteLLM, etc.). Only applied when headers are configured — an
	// empty map is a no-op at request time.
	if hc, ok := provider.(headersConfigurable); ok && len(spec.Headers) > 0 {
		hc.SetCustomHeaders(spec.Headers)
	}

	return provider, nil
}

// UnsupportedProviderError is returned when a provider type is not recognized
type UnsupportedProviderError struct {
	ProviderType string
}

// Error returns the error message for this unsupported provider error.
func (e *UnsupportedProviderError) Error() string {
	return "unsupported provider type: " + e.ProviderType
}

// UnsupportedProviderPlatformError is returned when a provider type is
// recognized but the requested platform is not a real partner endpoint
// for that vendor (e.g. openai+vertex — Vertex does not host OpenAI
// models natively). The error fires at provider construction so the
// failure is config-time rather than first-request, giving operators a
// fast and explicit signal that the combination is unavailable.
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
// Used by per-provider init() to fail fast on (provider, platform) pairs
// the underlying vendor doesn't host. Pairs that are real partner
// endpoints either route through inner with a custom URL builder, or
// fall through unchanged when the spec carries no platform.
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

// HasCredential returns true if the spec has a real (non-empty, non-"none") credential.
// Use this in factory functions to decide between credential-based and env-var-based constructors.
func (s *ProviderSpec) HasCredential() bool {
	return s.Credential != nil && s.Credential.Type() != "none"
}

// CredentialFactory builds a ProviderFactory that routes between a credential-based
// constructor and an env-var-based constructor. This eliminates the duplicated
// init() pattern across provider packages.
//
// Usage in provider init():
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
