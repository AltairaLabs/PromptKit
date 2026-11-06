package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/time/rate"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/safe"
	"github.com/AltairaLabs/PromptKit/runtime/v2/storage"
)

// Connection pooling defaults for HTTP transports shared across providers.
const (
	DefaultMaxIdleConns        = 1000
	DefaultMaxIdleConnsPerHost = 100
	// DefaultMaxConnsPerHost is the default cap on total TCP connections
	// per upstream host. Zero means unlimited, matching Go's stdlib
	// http.Transport default. Real LLM providers enforce rate limits
	// server-side; a client-side connection cap adds a hidden throughput
	// ceiling that's hard to debug.
	DefaultMaxConnsPerHost     = 0
	DefaultIdleConnTimeout     = 90 * time.Second
	DefaultTLSHandshakeTimeout = 10 * time.Second
	DefaultDialTimeout         = 30 * time.Second
	DefaultDialKeepAlive       = 30 * time.Second
)

// MaxErrorResponseSize is the maximum size for error response bodies (1 MB).
// Error responses should be small; this prevents reading huge bodies on failures.
const MaxErrorResponseSize int64 = 1 << 20

// Payload size limits.
const (
	// DefaultMaxPayloadSize is the default maximum request payload size (100 MB).
	// This is generous but prevents accidentally sending multi-GB payloads.
	DefaultMaxPayloadSize int64 = 100 * 1024 * 1024

	// payloadWarningThreshold is the size above which a warning is logged
	// even if the payload is under the max limit, as large payloads are likely slow.
	payloadWarningThreshold int64 = 10 * 1024 * 1024

	// bytesPerMB is used for converting bytes to megabytes in log messages.
	bytesPerMB = 1024 * 1024
)

// HTTPTransportOptions configures the connection pool for a pooled
// HTTP transport. A zero MaxConnsPerHost means unlimited (matching Go's
// http.Transport); a zero MaxIdleConnsPerHost or IdleConnTimeout falls back to
// its package default (DefaultMaxIdleConnsPerHost, DefaultIdleConnTimeout).
//
// These are the single-process pool controls that bound how many concurrent
// streams a provider can multiplex to one upstream host. Together with the
// upstream's advertised SETTINGS_MAX_CONCURRENT_STREAMS (RFC 7540 section
// 6.5.2), MaxConnsPerHost sets the steady-state ceiling for concurrent streams
// per process.
type HTTPTransportOptions struct {
	// MaxConnsPerHost caps the total TCP connections the transport may
	// open to any single host (in-use + idle). Zero means unlimited
	// (matching Go's http.Transport default). Negative values fall
	// back to DefaultMaxConnsPerHost.
	MaxConnsPerHost int
	// MaxIdleConnsPerHost caps the number of idle keep-alive
	// connections the transport will retain per host for reuse. Zero
	// uses DefaultMaxIdleConnsPerHost. Usually tracked to
	// MaxConnsPerHost to keep the whole pool reusable.
	MaxIdleConnsPerHost int
	// IdleConnTimeout is how long an idle keep-alive connection lingers
	// before being closed. Zero uses DefaultIdleConnTimeout. Longer
	// timeouts favor reuse; shorter ones release memory and FD slots
	// faster.
	IdleConnTimeout time.Duration
}

// NewPooledTransport creates an *http.Transport configured with default
// connection pooling settings suitable for high-throughput provider
// communication. Equivalent to NewPooledTransportWithOptions(HTTPTransportOptions{}).
func NewPooledTransport() *http.Transport {
	return NewPooledTransportWithOptions(HTTPTransportOptions{})
}

// NewPooledTransportWithOptions creates an *http.Transport with the given
// pool configuration. Zero-valued fields in opts fall back to the
// package-level defaults. The resulting transport is otherwise identical
// to NewPooledTransport (same TLS minimum version, same dial timeouts,
// same HTTP/2 upgrade policy).
func NewPooledTransportWithOptions(opts HTTPTransportOptions) *http.Transport {
	maxConns := opts.MaxConnsPerHost
	if maxConns < 0 {
		maxConns = DefaultMaxConnsPerHost
	}
	maxIdle := opts.MaxIdleConnsPerHost
	if maxIdle <= 0 {
		maxIdle = DefaultMaxIdleConnsPerHost
	}
	idleTimeout := opts.IdleConnTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleConnTimeout
	}
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   DefaultDialTimeout,
			KeepAlive: DefaultDialKeepAlive,
		}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:        DefaultMaxIdleConns,
		MaxIdleConnsPerHost: maxIdle,
		MaxConnsPerHost:     maxConns,
		IdleConnTimeout:     idleTimeout,
		TLSHandshakeTimeout: DefaultTLSHandshakeTimeout,
		ForceAttemptHTTP2:   true,
	}
}

// NewInstrumentedTransport wraps an http.RoundTripper with OpenTelemetry
// instrumentation. This propagates trace context (W3C traceparent header)
// on outgoing requests and creates client-side HTTP spans. When no
// TracerProvider is configured, the wrapper is a near-zero-cost passthrough.
func NewInstrumentedTransport(next http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(next,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "provider " + r.Method
		}),
	)
}

// NewProviderTransport builds the transport every completion provider runs
// on: a pooled transport configured from opts, wrapped with OpenTelemetry
// instrumentation and connection tracking (the http_conns_in_use gauge).
// Zero-valued options fall back to package defaults.
//
// Capability providers (tts, stt, embedding, inference, rerank) get this
// transport only when their provider file sets http_transport.
func NewProviderTransport(opts HTTPTransportOptions) http.RoundTripper {
	rt := NewInstrumentedTransport(NewPooledTransportWithOptions(opts))
	return newConnTrackingTransport(rt, DefaultStreamMetrics())
}

// BaseProvider provides common functionality shared across all provider implementations.
// It should be embedded in concrete provider structs to avoid code duplication.
//
// It carries two distinct HTTP clients: `client` for request/response calls
// (Predict, embeddings, etc.) which honors the configured request timeout, and
// `streamingClient` for long-lived SSE streams (PredictStream*) which has
// Timeout=0 so the client does not impose a wall-clock cap on streams.
// Liveness for streams is bounded separately by the IdleTimeoutReader
// wrapping the response body (see StreamIdleTimeout) and by context
// cancellation from the caller's deadline.
type BaseProvider struct {
	*base.Implementation
	id                    string
	includeRawOutput      bool
	client                *http.Client // request/response calls
	streamingClient       *http.Client // SSE streams; Timeout: 0
	streamIdleTimeout     time.Duration
	streamRetryPolicy     StreamRetryPolicy
	streamRetryBudget     *RetryBudget
	streamSemaphore       *StreamSemaphore
	streamObserver        StreamObserver
	rateLimiter           *rate.Limiter
	credential            Credential
	checkRedirect         func(*http.Request, []*http.Request) error // the caller's, applied after ours
	retryPolicy           pipeline.RetryPolicy
	maxRequestPayloadSize int64
	customHeaders         map[string]string
	mediaStorage          storage.MediaStorageService
	allowPrivateMediaURLs bool
	// rejected holds the sampling parameters the API rejected for this
	// model (RetryRejectedParams); shared by copies of the provider.
	rejected *rejectedParams
}

// NewBaseProvider creates a new BaseProvider with common fields. A companion
// streaming client is auto-derived from the given client's transport with
// Timeout=0 so SSE call sites can use GetStreamingHTTPClient() without any
// extra wiring.
//
// The provider works on a copy of client, so installing its redirect policy
// (see applyClientPolicies) never changes a client the caller shares, such as
// http.DefaultClient. The copy keeps client's CheckRedirect and applies it
// after the provider's own.
func NewBaseProvider(id string, includeRawOutput bool, client *http.Client) BaseProvider {
	var checkRedirect func(*http.Request, []*http.Request) error
	if client != nil {
		own := *client
		client = &own
		checkRedirect = client.CheckRedirect
	}
	b := BaseProvider{
		Implementation:        base.NewImplementation(id, base.ProviderTypeInference, nil),
		id:                    id,
		includeRawOutput:      includeRawOutput,
		client:                client,
		streamingClient:       newStreamingClient(client),
		checkRedirect:         checkRedirect,
		retryPolicy:           DefaultRetryPolicy(),
		maxRequestPayloadSize: DefaultMaxPayloadSize,
		rejected:              &rejectedParams{set: map[string]bool{}},
	}
	b.applyClientPolicies()
	return b
}

// newStreamingClient builds a companion http.Client for SSE streams that
// shares the given client's transport but imposes no wall-clock Timeout.
// Returns nil when client is nil so tests that construct a zero-value
// BaseProvider still behave.
func newStreamingClient(client *http.Client) *http.Client {
	if client == nil {
		return nil
	}
	return &http.Client{
		Timeout:   0,
		Transport: client.Transport,
	}
}

// SetRetryPolicy configures the retry policy for this provider.
func (b *BaseProvider) SetRetryPolicy(policy pipeline.RetryPolicy) {
	b.retryPolicy = policy
}

// GetRetryPolicy returns the current retry policy.
func (b *BaseProvider) GetRetryPolicy() pipeline.RetryPolicy {
	return b.retryPolicy
}

// NewBaseProviderWithAPIKey creates a BaseProvider and retrieves API key from environment
// It tries the primary key first, then falls back to the secondary key if primary is empty.
func NewBaseProviderWithAPIKey(
	id string, includeRawOutput bool, primaryKey, fallbackKey string,
) (provider BaseProvider, apiKey string) {
	apiKey = os.Getenv(primaryKey)
	if apiKey == "" {
		apiKey = os.Getenv(fallbackKey)
	}

	client := &http.Client{
		Timeout:   httputil.DefaultProviderTimeout,
		Transport: NewInstrumentedTransport(NewPooledTransport()),
	}
	return NewBaseProvider(id, includeRawOutput, client), apiKey
}

// ExtractAPIKey extracts an API key string from a Credential, if it is an APIKeyCredential.
// Returns an empty string if the credential is nil, not an api_key type, or does not
// implement the APIKey() method.
func ExtractAPIKey(cred Credential) string {
	if cred == nil || cred.Type() != "api_key" {
		return ""
	}
	if akc, ok := cred.(interface{ APIKey() string }); ok {
		return akc.APIKey()
	}
	return ""
}

// NewBaseProviderWithCredential creates a BaseProvider with an explicit credential.
// It creates an HTTP client with the given timeout, builds the BaseProvider, and
// extracts the API key from the credential (if it is an api_key credential).
// This eliminates the duplicated credential-setup boilerplate across providers.
//
// The BaseProvider authenticates its requests with cred (see
// AuthorizeRequest). A provider that sends a different credential, such as the
// extracted key in its own header, replaces it with SetCredential.
func NewBaseProviderWithCredential(
	id string, includeRawOutput bool, timeout time.Duration, cred Credential,
) (bp BaseProvider, apiKey string) {
	client := &http.Client{
		Timeout:   timeout,
		Transport: NewInstrumentedTransport(NewPooledTransport()),
	}
	bp = NewBaseProvider(id, includeRawOutput, client)
	bp.SetCredential(cred)
	apiKey = ExtractAPIKey(cred)
	return bp, apiKey
}

// ID returns the provider ID
func (b *BaseProvider) ID() string {
	return b.id
}

// ShouldIncludeRawOutput returns whether to include raw API responses in output
func (b *BaseProvider) ShouldIncludeRawOutput() bool {
	return b.includeRawOutput
}

// Close closes the HTTP client's idle connections
func (b *BaseProvider) Close() error {
	if b.client != nil {
		b.client.CloseIdleConnections()
	}
	return nil
}

// SupportsStreaming returns true by default (can be overridden by providers that don't support streaming)
func (b *BaseProvider) SupportsStreaming() bool {
	return true
}

// GetHTTPClient returns the underlying HTTP client for request/response
// calls. This client has a finite Timeout (the request_timeout) and MUST
// NOT be used for SSE streaming. Use GetStreamingHTTPClient for that.
func (b *BaseProvider) GetHTTPClient() *http.Client {
	return b.client
}

// GetStreamingHTTPClient returns a dedicated HTTP client for SSE streaming
// calls. It shares the non-streaming client's transport but has Timeout=0
// so long-lived streams are not killed by a wall-clock cap. When no
// dedicated streaming client is configured it falls back to the regular
// client so callers never receive nil.
func (b *BaseProvider) GetStreamingHTTPClient() *http.Client {
	if b.streamingClient != nil {
		return b.streamingClient
	}
	return b.client
}

// SetHTTPTimeout replaces the request/response HTTP client with a new one
// that uses the given timeout while preserving the existing transport
// configuration. Does not affect the streaming client, which remains at
// Timeout=0.
func (b *BaseProvider) SetHTTPTimeout(timeout time.Duration) {
	transport := base.ClientTransport(b.client)
	b.client = &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	// Keep the streaming client's transport in sync with the new client so
	// both share connection pooling.
	if b.streamingClient != nil {
		b.streamingClient.Transport = transport
	} else if transport != nil {
		b.streamingClient = &http.Client{Timeout: 0, Transport: transport}
	}
	b.applyClientPolicies()
}

// SetHTTPTransport replaces the RoundTripper on both the regular and
// streaming HTTP clients so the provider routes every outbound request
// through the supplied transport. Both clients share the transport so
// connection pooling is effective across request/response and streaming
// traffic to the same upstream.
//
// CreateProviderFromSpec calls it to apply per-provider connection pool
// config (see ProviderSpec.HTTPTransport). Provider factories create their
// client with the default pooled transport and the spec wiring replaces it
// after construction.
//
// A nil transport resets both clients to Go's http.DefaultTransport via
// the http.Client zero-value behavior. Passing nil is not the typical
// use case; callers should build a transport via
// NewPooledTransportWithOptions and wrap it with NewInstrumentedTransport
// so the OpenTelemetry span wiring is preserved.
func (b *BaseProvider) SetHTTPTransport(rt http.RoundTripper) {
	if b.client != nil {
		b.client.Timeout = base.ClientTimeout(b.client)
		b.client.Transport = rt
	}
	if b.streamingClient != nil {
		b.streamingClient.Transport = rt
	} else if rt != nil {
		// Tests and edge cases may construct a BaseProvider without a
		// streaming client. Materialize one here so streaming callers
		// also pick up the new transport.
		b.streamingClient = &http.Client{Timeout: 0, Transport: rt}
	}
	// Keep a configured rate limit in front of the new transport.
	b.applyClientPolicies()
}

// SetCustomHeaders stores custom HTTP headers that will be applied to
// every outgoing request via ApplyCustomHeaders. Intended for
// OpenAI-compatible gateway headers (e.g. OpenRouter's HTTP-Referer,
// X-Title). Called by CreateProviderFromSpec after factory construction.
func (b *BaseProvider) SetCustomHeaders(headers map[string]string) {
	b.customHeaders = headers
	b.applyClientPolicies()
}

// SetMediaStorageService injects the media storage service that resolves
// MediaContent.StorageReference values at request-build time. Nil (the default)
// disables storage-reference resolution. See MediaStorageConfigurable in registry.go.
func (b *BaseProvider) SetMediaStorageService(store storage.MediaStorageService) {
	mediaSettingsMu.Lock()
	defer mediaSettingsMu.Unlock()
	b.mediaStorage = store
}

// mediaSettingsMu guards every provider's media settings. A host can share
// one provider across conversations opened concurrently (a PackTemplate
// serving requests), and each Open applies these settings to the pool, so the
// writes race with each other and with MediaLoader's reads. One package lock,
// rather than a field, keeps BaseProvider copyable by value; the settings
// change only at Open.
var mediaSettingsMu sync.RWMutex

// SetAllowPrivateNetworkMedia lets this provider fetch URL media from
// non-public addresses. See MediaLoaderConfig.AllowPrivateNetworks for why it
// is off by default.
func (b *BaseProvider) SetAllowPrivateNetworkMedia(allow bool) {
	mediaSettingsMu.Lock()
	defer mediaSettingsMu.Unlock()
	b.allowPrivateMediaURLs = allow
}

// LoggingContext returns ctx carrying this provider's ID and model for the
// logs written during a call.
func (b *BaseProvider) LoggingContext(ctx context.Context, model string) context.Context {
	return logger.WithLoggingContext(ctx, &logger.LoggingFields{Provider: b.id, Model: model})
}

// WithMedia returns a copy of b using m's media settings. A zero field keeps
// b's own: a nil Storage keeps the store the provider was built with, and
// AllowPrivateNetworks only ever widens. Providers build their MediaScoped
// views from it.
func (b *BaseProvider) WithMedia(m MediaSettings) BaseProvider {
	mediaSettingsMu.RLock()
	c := *b
	mediaSettingsMu.RUnlock()
	if m.Storage != nil {
		c.mediaStorage = m.Storage
	}
	if m.AllowPrivateNetworks {
		c.allowPrivateMediaURLs = true
	}
	return c
}

// MediaLoader returns a new MediaLoader configured with this provider's
// injected storage service (if any). Providers use it to resolve media parts
// (ResolveURL for URL-first providers, GetBase64Data for byte-based ones).
// Each loader counts its own media against MaxMediaItems and
// MaxAggregateMediaSize, so the built-in providers call it once per message
// and share the loader across that message's parts: a conversation resends
// every earlier message, and a loader per request would fail every turn once
// the history reached a limit.
func (b *BaseProvider) MediaLoader() *MediaLoader {
	mediaSettingsMu.RLock()
	defer mediaSettingsMu.RUnlock()
	return NewMediaLoader(MediaLoaderConfig{
		StorageService:       b.mediaStorage,
		AllowPrivateNetworks: b.allowPrivateMediaURLs,
	})
}

// ApplyCustomHeaders applies stored custom headers to the HTTP request.
// Must be called AFTER the provider sets its own built-in headers
// (Authorization, Content-Type, etc.) and calls AuthorizeRequest. Returns an
// error if any custom header collides with a header already set on the
// request, or with a header the credential sets once the request is sent
// (case-insensitive per HTTP spec).
func (b *BaseProvider) ApplyCustomHeaders(req *http.Request) error {
	// A credential applied at build time is already on req; one deferred to
	// the transport is checked by name.
	var deferred http.Header
	if b.credentialDeferred() {
		deferred = b.credentialHeaders()
	}
	for key, value := range b.customHeaders {
		if req.Header.Get(key) != "" || deferred.Get(key) != "" {
			return customHeaderCollision(key)
		}
		req.Header.Set(key, value)
	}
	return nil
}

func customHeaderCollision(key string) error {
	return fmt.Errorf("custom header %q collides with built-in header set by provider", key)
}

// HeaderKeyCredential returns a credential that sends key in header, after
// prefix (for example "Bearer "). An empty key returns nil, which sends no
// credential. Providers that authenticate with a static key use it with
// SetCredential.
func HeaderKeyCredential(key, header, prefix string) Credential {
	if key == "" {
		return nil
	}
	return credentials.NewAPIKeyCredential(key, credentials.WithHeaderName(header), credentials.WithPrefix(prefix))
}

// SetCredential sets the credential the provider authenticates its requests
// with, replacing the one NewBaseProviderWithCredential stored. Nil sends no
// credential. See AuthorizeRequest.
func (b *BaseProvider) SetCredential(cred Credential) {
	b.credential = cred
	b.applyClientPolicies()
}

// Credential returns the credential the provider authenticates its requests
// with, or nil when it sends none.
func (b *BaseProvider) Credential() Credential {
	return b.credential
}

// AuthorizeRequest applies the provider's credential to req while the request
// is being built; a provider calls it once for each request it builds, before
// ApplyCustomHeaders. With no rate limit it applies the credential at once,
// as it always has. Under a rate limit it does nothing: the limiter's
// transport applies the credential once, after the request's wait ends, so a
// SigV4 signature or a token is never older than the wait and is computed
// once per attempt. With no credential it does nothing.
func (b *BaseProvider) AuthorizeRequest(ctx context.Context, req *http.Request) error {
	if b.credential == nil || b.credentialDeferred() {
		return nil
	}
	return b.credential.Apply(ctx, req)
}

// credentialDeferred reports whether the credential is applied by the rate
// limiter's transport rather than while the request is built. It reads the
// client's transport, not b.rateLimiter: a WithMedia view shares the clients
// of the provider it was copied from, so the transport is what will actually
// send the request.
func (b *BaseProvider) credentialDeferred() bool {
	if b.credential == nil {
		return false
	}
	c := b.client
	if c == nil {
		c = b.streamingClient
	}
	if c == nil {
		return false
	}
	rlt, ok := c.Transport.(*base.RateLimitTransport)
	return ok && rlt.Authorize != nil
}

// credentialHeaders returns, as header keys, the headers the credential sets,
// when it can name them (credentials.HeaderNamer). Values are placeholders.
func (b *BaseProvider) credentialHeaders() http.Header {
	namer, ok := b.credential.(credentials.HeaderNamer)
	if !ok {
		return nil
	}
	h := http.Header{}
	for _, name := range namer.HeaderNames() {
		h.Set(name, "credential")
	}
	return h
}

// transportAuthorizer returns what the rate limiter's transport runs after
// each wait: the credential's Apply. A credential that cannot name its headers
// is checked there instead of at build time: if applying it changes a custom
// header, the request fails without being sent.
func transportAuthorizer(cred Credential, custom map[string]string) func(context.Context, *http.Request) error {
	if cred == nil {
		return nil
	}
	if _, named := cred.(credentials.HeaderNamer); named || len(custom) == 0 {
		return cred.Apply
	}
	return func(ctx context.Context, req *http.Request) error {
		before := make(map[string][]string, len(custom))
		for key := range custom {
			before[key] = req.Header.Values(key)
		}
		if err := cred.Apply(ctx, req); err != nil {
			return err
		}
		for key, values := range before {
			if !slices.Equal(values, req.Header.Values(key)) {
				return customHeaderCollision(key)
			}
		}
		return nil
	}
}

// StreamIdleTimeout returns the configured SSE body idle timeout or the
// package default (DefaultStreamIdleTimeout) when none is set.
func (b *BaseProvider) StreamIdleTimeout() time.Duration {
	if b.streamIdleTimeout > 0 {
		return b.streamIdleTimeout
	}
	return DefaultStreamIdleTimeout
}

// SetStreamIdleTimeout configures the SSE body idle timeout. A zero or
// negative value resets to DefaultStreamIdleTimeout.
func (b *BaseProvider) SetStreamIdleTimeout(d time.Duration) {
	if d <= 0 {
		b.streamIdleTimeout = 0
		return
	}
	b.streamIdleTimeout = d
}

// StreamRetryPolicy returns the configured streaming-retry policy. The zero
// value (retry disabled) is the default.
func (b *BaseProvider) StreamRetryPolicy() StreamRetryPolicy {
	return b.streamRetryPolicy
}

// SetStreamRetryPolicy configures bounded retry behavior for the
// pre-first-chunk streaming window. See StreamRetryPolicy for details.
func (b *BaseProvider) SetStreamRetryPolicy(policy StreamRetryPolicy) {
	b.streamRetryPolicy = policy
}

// StreamRetryBudget returns the per-provider retry budget. A nil return
// means retries are unbounded (only MaxAttempts caps them).
func (b *BaseProvider) StreamRetryBudget() *RetryBudget {
	return b.streamRetryBudget
}

// SetStreamRetryBudget installs a token bucket that rate-limits retry
// attempts across all in-flight requests on this provider. Passing nil
// restores unbounded-retry behavior.
func (b *BaseProvider) SetStreamRetryBudget(budget *RetryBudget) {
	b.streamRetryBudget = budget
}

// StreamSemaphore returns the concurrent-stream semaphore. A nil return
// means unlimited concurrency (backwards-compatible default).
func (b *BaseProvider) StreamSemaphore() *StreamSemaphore {
	return b.streamSemaphore
}

// SetStreamSemaphore installs a semaphore that caps concurrent streaming
// requests. Passing nil restores unlimited concurrency.
func (b *BaseProvider) SetStreamSemaphore(sem *StreamSemaphore) {
	b.streamSemaphore = sem
}

// SetStreamObserver installs an observer that receives the events of every
// stream this provider opens through RunStreamingRequest. Nil (the default)
// observes nothing. See StreamObserver.
func (b *BaseProvider) SetStreamObserver(o StreamObserver) {
	b.streamObserver = o
}

// AcquireStreamSlot blocks on the configured concurrent-stream semaphore
// until a slot is available or the context is done. Returns nil on
// successful acquire (caller MUST call b.ReleaseStreamSlot exactly once)
// or the context error on cancellation/deadline. Classifies the
// rejection reason and records it on the
// promptkit_stream_concurrency_rejections_total counter so operators
// can see saturation.
//
// With no semaphore configured it returns nil without blocking or emitting
// metrics, so callers can invoke it unconditionally.
func (b *BaseProvider) AcquireStreamSlot(ctx context.Context) error {
	if b.streamSemaphore == nil {
		return nil
	}
	if err := b.streamSemaphore.Acquire(ctx); err != nil {
		reason := "context_canceled"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "deadline_exceeded"
		}
		DefaultStreamMetrics().ConcurrencyRejected(b.id, reason)
		return err
	}
	return nil
}

// ReleaseStreamSlot returns a slot to the concurrent-stream semaphore.
// Nil-safe; must be paired with a successful AcquireStreamSlot.
func (b *BaseProvider) ReleaseStreamSlot() {
	b.streamSemaphore.Release()
}

// StreamConsumer is called on the success path of RunStreamingRequest
// inside a dedicated goroutine. It receives the (possibly retry-replayed)
// response body and the output channel; it must fully drain the body
// and close outChan when done. Typical implementations wrap body in an
// IdleTimeoutReader + SSEScanner (or equivalent) and run the provider's
// existing stream parser.
type StreamConsumer func(ctx context.Context, body io.ReadCloser, outChan chan<- StreamChunk)

// RunStreamingRequest is the entry point streaming-capable providers delegate
// through. It combines the concurrent-stream semaphore, the retry budget and
// pre-first-chunk retry with in-flight gauge bookkeeping, so a provider's
// streaming function supplies only the request and a consumer that parses the
// provider-specific stream framing.
//
// It:
//
//  1. Acquires a concurrent-stream slot (blocks on ctx; a nil semaphore
//     is a no-op).
//  2. Increments the streams_in_flight and provider_calls_in_flight gauges.
//  3. Opens the stream with OpenStreamWithRetryRequest and req.
//  4. Starts a goroutine that runs the consumer on the response body and
//     releases the semaphore slot when the consumer returns.
//  5. Starts a relay goroutine that forwards the consumer's chunks to the
//     returned channel, decrements the gauges and closes the channel when the
//     stream ends.
//
// When req.Policy enables StreamRetryWindowAlways and the stream fails after
// content was forwarded, the relay sends a chunk with Reset set, opens a new
// stream and forwards that stream's chunks instead, if the retry budget has a
// token and the context is not canceled.
//
// The relay also gives the final chunk of a stream that ends without an error
// a *PredictionResponse in FinalResult (unless the provider set one), and
// reports every chunk to the provider's StreamObserver, if one is installed.
//
// On an error before the goroutines start, it releases the slot and gauges.
//
// Callers must set req.ProviderName to b.ID().
func (b *BaseProvider) RunStreamingRequest(
	ctx context.Context,
	req *StreamRetryRequest,
	consumer StreamConsumer,
) (<-chan StreamChunk, error) {
	start := time.Now()
	if acqErr := b.AcquireStreamSlot(ctx); acqErr != nil {
		return nil, fmt.Errorf("failed to acquire stream slot: %w", acqErr)
	}
	slotReleased := false
	defer func() {
		if !slotReleased {
			b.ReleaseStreamSlot()
		}
	}()

	metrics := DefaultStreamMetrics()
	providerID := b.id
	metrics.StreamsInFlightInc(providerID)
	metrics.ProviderCallsInFlightInc(providerID)
	released := false
	defer func() {
		if !released {
			metrics.StreamsInFlightDec(providerID)
			metrics.ProviderCallsInFlightDec(providerID)
		}
	}()

	result, err := OpenStreamWithRetryRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Two-channel pipeline so we can instrument every stream at a single
	// point rather than threading bookkeeping through every provider's
	// stream consumer:
	//
	//   body → consumer → innerChan → relay → outChan → caller
	//
	// The relay goroutine counts content chunks and observes the
	// stream_error_chunks_forwarded histogram on the terminal error
	// chunk. A distribution concentrated at 0 means pre-first-chunk retry
	// covers the failures; mass at 1 or more means mid-stream failures
	// leak past the safe-retry window.
	innerChan := make(chan StreamChunk, DefaultStreamBufferSize)
	outChan := make(chan StreamChunk, DefaultStreamBufferSize)
	tap := &streamTap{out: outChan, observer: b.streamObserver, start: start}

	// From this point on, ownership of the acquired slot and gauge
	// increments transfers to the goroutines' defers below. Flip the
	// flags so the outer defers are no-ops on the success path.
	released = true
	slotReleased = true

	// Consumer goroutine: drives the body through the provider-specific
	// parser into innerChan, then closes innerChan. Releases the
	// concurrent-stream semaphore the moment the upstream body is
	// fully drained so a slow caller does not hold an upstream slot.
	go func() {
		defer b.ReleaseStreamSlot()
		// Recover any panic from the provider-specific parser (malformed stream,
		// bad chunk) so it can't crash the process. The consumer's own deferred
		// close of innerChan still runs during unwinding, so the relay sees EOF.
		safe.Run("provider-stream-consumer", func() {
			consumer(ctx, result.Body, innerChan)
		}, nil)
	}()

	// Relay goroutine: observes instrumentation while forwarding every
	// chunk from innerChan to outChan. The in-flight gauges are
	// decremented here (not in the consumer) so they reflect the
	// caller's view of stream lifetime: "stream is in flight until
	// outChan is closed". The terminal close of outChan is the last
	// step, signaling the caller that the stream is fully delivered.
	//
	// When retry_window is "always" and a mid-stream error occurs
	// after content has been forwarded, the relay emits a Reset chunk,
	// opens a new stream via the retry driver, and continues relaying
	// from the fresh consumer — the caller sees Reset followed by the
	// new response as if the first attempt never happened.
	go func() {
		defer func() {
			close(outChan)
			metrics.StreamsInFlightDec(providerID)
			metrics.ProviderCallsInFlightDec(providerID)
		}()
		// Recover any panic so the relay can't crash the process; the deferred
		// close(outChan) still runs, so the caller sees EOF instead of a hang.
		safe.Run("provider-stream-relay", func() {
			b.relayStream(ctx, req, consumer, innerChan, tap, metrics, providerID)
			tap.finish()
		}, nil)
	}()

	return outChan, nil
}

// relayStream is the inner loop of the relay goroutine. It forwards
// chunks from innerChan to outChan, counting content chunks and
// recording the error-chunks histogram. When retry_window is "always"
// and a mid-stream error arrives after content was forwarded, it emits
// a Reset chunk, opens a fresh stream, and loops — giving the caller
// a clean second attempt. The budget gates the retry to prevent storms.
func (b *BaseProvider) relayStream(
	ctx context.Context,
	req *StreamRetryRequest,
	consumer StreamConsumer,
	innerChan <-chan StreamChunk,
	tap *streamTap,
	metrics *StreamMetrics,
	providerID string,
) {
	canMidStreamRetry := req.Policy.Enabled &&
		req.Policy.Window == StreamRetryWindowAlways

	contentChunks := forwardChunks(ctx, innerChan, tap, metrics, providerID)
	if contentChunks < 0 {
		return // stream completed cleanly (no error)
	}

	// Mid-stream retry: if the policy allows it, content was
	// forwarded (so pre-first-chunk retry could not catch this), and the budget
	// has a token, emit Reset and retry from scratch.
	// Never for a canceled stream: the retry cannot succeed, and its Reset
	// chunk would wipe the partial output the caller has already seen.
	if canMidStreamRetry && contentChunks > 0 && ctx.Err() == nil && req.Budget.TryAcquire() {
		midStreamRetry(ctx, req, consumer, tap, metrics, providerID)
	}
}

// forwardChunks relays chunks from src to dst, counting content
// chunks and recording error-histogram observations. Returns -1 when
// the source closes cleanly (no error), or the non-negative content
// count when a terminal error chunk has been forwarded.
//
// A canceled stream always ends on ctx.Err(). Consumers close the response
// body when ctx is done, so the parser usually stops on a read error from the
// closed body, or on a clean EOF with no error at all. The first would report
// a network fault; the second would make a truncated reply look complete.
func forwardChunks(
	ctx context.Context, src <-chan StreamChunk, dst *streamTap,
	metrics *StreamMetrics, providerID string,
) int {
	var contentChunks int
	finished := false
	for chunk := range src {
		if chunkForwardedContent(&chunk) {
			contentChunks++
		}
		if chunk.Error != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && !errors.Is(chunk.Error, ctxErr) {
				logger.Debug("stream read failed after cancellation; reporting the cancellation",
					"provider", providerID, "read_error", chunk.Error)
				chunk.Error = ctxErr
			}
			metrics.ObserveStreamErrorChunksForwarded(providerID, contentChunks)
			dst.send(chunk)
			return contentChunks
		}
		if chunk.FinishReason != nil {
			finished = true
		}
		dst.send(chunk)
	}
	if ctxErr := ctx.Err(); ctxErr != nil && !finished {
		metrics.ObserveStreamErrorChunksForwarded(providerID, contentChunks)
		dst.send(StreamChunk{Error: ctxErr})
		return contentChunks
	}
	return -1 // clean close
}

// midStreamRetry handles the retry-after-reset path: emits a Reset
// chunk, opens a fresh stream through the retry driver, and relays
// the new consumer's output into outChan. If the retry itself fails
// to open, a terminal error chunk is emitted instead.
func midStreamRetry(
	ctx context.Context,
	req *StreamRetryRequest,
	consumer StreamConsumer,
	tap *streamTap,
	metrics *StreamMetrics,
	providerID string,
) {
	metrics.RetryAttempt(providerID, "mid_stream_reset")
	logger.Warn("mid-stream failure, emitting reset and retrying",
		"provider", providerID,
	)
	tap.send(StreamChunk{Reset: true})

	retryResult, retryErr := OpenStreamWithRetryRequest(ctx, req)
	if retryErr != nil {
		tap.send(StreamChunk{
			Error:        fmt.Errorf("mid-stream retry failed: %w", retryErr),
			FinishReason: StringPtr("error"),
		})
		return
	}

	retryChan := make(chan StreamChunk, DefaultStreamBufferSize)
	go func() {
		// Recover any panic from the retry parser; consumer's deferred close of
		// retryChan still runs so forwardChunks sees EOF rather than a crash.
		safe.Run("provider-stream-retry-consumer", func() {
			consumer(ctx, retryResult.Body, retryChan)
		}, nil)
	}()

	forwardChunks(ctx, retryChan, tap, metrics, providerID)
}

// chunkForwardedContent reports whether a StreamChunk delivered any
// user-visible payload downstream. Only payload-bearing chunks count
// toward the "chunks forwarded before error" histogram; pure
// finish-reason chunks and error terminals do not. Takes a pointer
// rather than a value because StreamChunk is large (multiple string
// and slice fields) and this function is called once per chunk in
// the relay hot path.
func chunkForwardedContent(chunk *StreamChunk) bool {
	if chunk.Delta != "" {
		return true
	}
	// Reasoning is delivered to the caller as it streams, so a failure after
	// it is a mid-stream failure like any other.
	if chunk.Reasoning != "" || len(chunk.OpaqueReasoning) > 0 {
		return true
	}
	if chunk.MediaData != nil {
		return true
	}
	if len(chunk.ToolCalls) > 0 {
		return true
	}
	return false
}

// HTTPTimeout returns the current HTTP client timeout, or 0 if no client is set.
func (b *BaseProvider) HTTPTimeout() time.Duration {
	return base.ClientTimeout(b.client)
}

// SetRateLimit configures per-provider rate limiting. requestsPerSecond controls
// the sustained rate, and burst controls how many requests can be made
// at once before throttling kicks in; a burst below 1 counts as 1. A zero or
// negative requestsPerSecond disables rate limiting (the default).
//
// The limit applies to every HTTP request the provider sends through its
// request/response and streaming clients (Predict, PredictStream, and each
// retry attempt) by wrapping their transport in a base.RateLimitTransport.
// A request waits for its turn before it is sent; the wait ends when the
// request's context is canceled, or at once when its turn falls past the
// context deadline (base.ErrRateLimitPastDeadline). The wait does not count
// against the request timeout, which starts when the request is sent. The
// provider's credential is applied once per attempt, after its wait ends, and
// to a redirect only when it stays on the same scheme and host (see
// AuthorizeRequest). CreateProviderFromSpec calls it with spec.RateLimit.
func (b *BaseProvider) SetRateLimit(requestsPerSecond float64, burst int) {
	b.rateLimiter = base.NewRequestLimiter(requestsPerSecond, burst)
	b.applyClientPolicies()
}

// applyClientPolicies installs b.rateLimiter (or removes the limit when nil) on
// both clients, with b.credential applied after each wait. The request
// client's timeout moves behind the wait (see base.ApplyRateLimit), so
// request_timeout bounds the upstream call, not the time spent queued for a
// token.
//
// It also gives both clients the credential redirect policy
// (base.CredentialRedirectPolicy): a redirect to another host loses every
// header the credential names and every custom header, with or without a rate
// limit, so a key or signature never leaves its origin.
func (b *BaseProvider) applyClientPolicies() {
	authorize := transportAuthorizer(b.credential, b.customHeaders)
	checkRedirect := base.CredentialRedirectPolicy(
		offOriginHeaders(b.credential, b.customHeaders, b.rateLimiter != nil), b.checkRedirect)
	for _, c := range []*http.Client{b.client, b.streamingClient} {
		base.ApplyRateLimit(c, b.rateLimiter)
		if c == nil {
			continue
		}
		c.CheckRedirect = checkRedirect
		if rlt, ok := c.Transport.(*base.RateLimitTransport); ok {
			rlt.Authorize = authorize
		}
	}
}

// appliedHeaderNames finds the headers a credential that cannot name them
// sets, by applying it to a blank request that is never sent. It runs only for
// a redirect off the origin, the one place a credential applied while the
// request was built would otherwise travel on. An Apply error gives no names.
func appliedHeaderNames(cred Credential) []string {
	scratch, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "https://scratch.invalid/", http.NoBody)
	if err != nil {
		return nil
	}
	if cred.Apply(scratch.Context(), scratch) != nil {
		return nil
	}
	names := make([]string, 0, len(scratch.Header))
	for name := range scratch.Header {
		names = append(names, name)
	}
	return names
}

// offOriginHeaders returns what a redirect to another host must not carry,
// beyond base.WellKnownCredentialHeaders: the headers cred names, and the
// custom headers, which may carry a gateway's own key. It captures cred and
// custom as they are now; applyClientPolicies reinstalls it when either
// changes.
//
// limited says the credential is applied by the rate limiter's transport,
// which never applies it off the origin, so a credential that cannot name its
// headers need not be probed for them.
func offOriginHeaders(cred Credential, custom map[string]string, limited bool) func() []string {
	return func() []string {
		var names []string
		if namer, ok := cred.(credentials.HeaderNamer); ok {
			names = append(names, namer.HeaderNames()...)
		} else if cred != nil && !limited {
			names = append(names, appliedHeaderNames(cred)...)
		}
		for name := range custom {
			names = append(names, name)
		}
		return names
	}
}

// SetMaxPayloadSize configures the maximum allowed request payload size in bytes.
// A zero or negative value disables payload size checking.
func (b *BaseProvider) SetMaxPayloadSize(size int64) {
	b.maxRequestPayloadSize = size
}

// MaxPayloadSize returns the current maximum request payload size in bytes.
func (b *BaseProvider) MaxPayloadSize() int64 {
	return b.maxRequestPayloadSize
}

// RateLimiter returns the current rate limiter, or nil if rate limiting is
// not configured. This is useful for inspecting or sharing limiters.
func (b *BaseProvider) RateLimiter() *rate.Limiter {
	return b.rateLimiter
}

// WaitForRateLimit blocks until the rate limiter allows a request to
// proceed, or until the context is canceled. If no rate limiter is
// configured, it returns immediately. Requests sent through the provider's
// HTTP clients already wait inside their transport, so call it only for
// outbound work that does not use those clients; each call takes a token.
func (b *BaseProvider) WaitForRateLimit(ctx context.Context) error {
	if b.rateLimiter == nil {
		return nil
	}
	return b.rateLimiter.Wait(ctx)
}

// ReadResponseBody reads and returns the response body, limiting the size to
// DefaultMaxPayloadSize to prevent unbounded memory consumption.
func ReadResponseBody(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, DefaultMaxPayloadSize))
}

// ReadErrorBody reads and returns an error response body, limiting the size to
// MaxErrorResponseSize. Error responses should be small; this is a safety net.
func ReadErrorBody(body io.Reader) []byte {
	b, _ := io.ReadAll(io.LimitReader(body, MaxErrorResponseSize))
	return b
}

// DoAndReadResponse executes an HTTP request using the provider's client, reads
// the response body (with size limiting), and logs the response. On read error
// it sets predictResp.Latency. Returns the body bytes and HTTP status code.
func (b *BaseProvider) DoAndReadResponse(
	req *http.Request, predictResp *PredictionResponse, start time.Time, providerName string,
) (body []byte, statusCode int, err error) {
	resp, err := b.client.Do(req) //nolint:gosec // G704: req targets the provider's configured endpoint
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, 0, &ProviderTransportError{Cause: err, Provider: b.id}
	}
	defer func() { _ = resp.Body.Close() }()

	body, err = ReadResponseBody(resp.Body)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	logger.APIResponse(providerName, resp.StatusCode, string(body), nil)
	return body, resp.StatusCode, nil
}

// CheckHTTPError checks if HTTP response is an error and returns formatted error with body
func CheckHTTPError(resp *http.Response, url string) error {
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body := ReadErrorBody(resp.Body)
		return &ProviderHTTPError{
			StatusCode: resp.StatusCode,
			URL:        url,
			Body:       string(body),
		}
	}
	return nil
}

// UnmarshalJSON unmarshals JSON with error recovery that sets latency and raw response
func UnmarshalJSON(respBody []byte, v any, predictResp *PredictionResponse, start time.Time) error {
	if err := json.Unmarshal(respBody, v); err != nil {
		predictResp.Latency = time.Since(start)
		predictResp.Raw = respBody
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return nil
}

// SetErrorResponse sets latency and raw body on error responses
func SetErrorResponse(predictResp *PredictionResponse, respBody []byte, start time.Time) {
	predictResp.Latency = time.Since(start)
	predictResp.Raw = respBody
}

// checkPayloadSize validates the request body against the configured maximum
// payload size and logs a warning for payloads above the warning threshold.
func (b *BaseProvider) checkPayloadSize(body []byte, providerName string) error {
	payloadSize := int64(len(body))
	if b.maxRequestPayloadSize > 0 && payloadSize > b.maxRequestPayloadSize {
		return fmt.Errorf(
			"%w: payload size %d bytes exceeds maximum %d bytes",
			ErrPayloadTooLarge, payloadSize, b.maxRequestPayloadSize,
		)
	}
	if payloadSize > payloadWarningThreshold {
		logger.Warn("large request payload",
			"provider", providerName,
			"payload_bytes", payloadSize,
			"payload_mb", fmt.Sprintf("%.1f", float64(payloadSize)/float64(bytesPerMB)),
		)
	}
	return nil
}

// RequestHeaders is a map of HTTP header key-value pairs
type RequestHeaders map[string]string

// MakeJSONRequest performs a JSON HTTP POST request with common error handling.
// This reduces duplication across provider implementations.
// providerName is used for logging purposes.
func (b *BaseProvider) MakeJSONRequest(
	ctx context.Context,
	url string,
	request any,
	headers RequestHeaders,
	providerName string,
) ([]byte, error) {
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	return b.MakeRawRequest(ctx, url, reqBytes, headers, providerName)
}

// mergeCustomHeaders merges the provider-level custom headers into the
// caller's headers. http.Header gives us case-insensitive lookup so we don't
// have to re-implement the collision check that ApplyCustomHeaders already does.
func (b *BaseProvider) mergeCustomHeaders(headers RequestHeaders) (RequestHeaders, error) {
	if len(b.customHeaders) == 0 {
		return headers, nil
	}
	merged := http.Header{}
	for k, v := range headers {
		merged.Set(k, v)
	}
	// The credential is applied to each attempt after these headers.
	credHeaders := b.credentialHeaders()
	for k, v := range b.customHeaders {
		if merged.Get(k) != "" || credHeaders.Get(k) != "" {
			return nil, customHeaderCollision(k)
		}
		merged.Set(k, v)
	}
	flat := make(RequestHeaders, len(merged))
	for k, vs := range merged {
		if len(vs) > 0 {
			flat[k] = vs[0]
		}
	}
	return flat, nil
}

// maskHeadersForLog returns a copy of headers with credential headers masked.
func maskHeadersForLog(headers RequestHeaders) map[string]string {
	logHeaders := make(map[string]string)
	for k, v := range headers {
		if k == "Authorization" || k == "x-api-key" {
			logHeaders[k] = "***"
		} else {
			logHeaders[k] = v
		}
	}
	return logHeaders
}

// MakeRawRequest performs an HTTP POST request with pre-marshaled body.
// It automatically retries on transient errors (429, 502, 503, 504 and
// network errors) according to the provider's RetryPolicy. A final status
// other than 200 returns an error that includes the status code and body.
//
// Custom headers set with SetCustomHeaders are merged into the outgoing
// request headers up front, with a case-insensitive collision check against
// the headers the caller passed and the headers the provider's credential
// sets. A collision fails before the retry loop runs, so it does not consume
// retries. Each attempt is authorized with the provider's credential (see
// AuthorizeRequest).
func (b *BaseProvider) MakeRawRequest(
	ctx context.Context,
	url string,
	body []byte,
	headers RequestHeaders,
	providerName string,
) ([]byte, error) {
	effectiveHeaders, mergeErr := b.mergeCustomHeaders(headers)
	if mergeErr != nil {
		return nil, mergeErr
	}

	// Log the request once (mask sensitive headers for logging).
	logger.APIRequest(providerName, "POST", url, maskHeadersForLog(effectiveHeaders), json.RawMessage(body))

	// Validate payload size before sending the request.
	if err := b.checkPayloadSize(body, providerName); err != nil {
		return nil, err
	}

	// The rate limit, when set, is applied by b.client's transport to each
	// attempt below (see SetRateLimit).
	doFn := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(
			ctx, "POST", url, bytes.NewReader(body),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		for key, value := range effectiveHeaders {
			req.Header.Set(key, value)
		}
		if authErr := b.AuthorizeRequest(ctx, req); authErr != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", authErr)
		}
		return b.client.Do(req)
	}

	resp, err := DoWithRetry(ctx, b.retryPolicy, providerName, doFn)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := ReadResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	logger.APIResponse(
		providerName, resp.StatusCode, string(respBytes), nil,
	)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"API error (status %d): %s",
			resp.StatusCode, string(respBytes),
		)
	}

	return respBytes, nil
}
