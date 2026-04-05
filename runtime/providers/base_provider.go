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
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/time/rate"

	"github.com/AltairaLabs/PromptKit/runtime/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
)

// Connection pooling defaults for HTTP transports shared across providers.
const (
	DefaultMaxIdleConns        = 1000
	DefaultMaxIdleConnsPerHost = 100
	DefaultMaxConnsPerHost     = 100
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

// NewPooledTransport creates an *http.Transport configured with connection
// pooling settings suitable for high-throughput provider communication.
func NewPooledTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   DefaultDialTimeout,
			KeepAlive: DefaultDialKeepAlive,
		}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:        DefaultMaxIdleConns,
		MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
		MaxConnsPerHost:     DefaultMaxConnsPerHost,
		IdleConnTimeout:     DefaultIdleConnTimeout,
		TLSHandshakeTimeout: DefaultTLSHandshakeTimeout,
		ForceAttemptHTTP2:   true,
	}
}

// NewInstrumentedTransport wraps an http.RoundTripper with OpenTelemetry
// instrumentation. This propagates trace context (W3C traceparent header)
// on outgoing requests and creates client-side HTTP spans. When no
// TracerProvider is configured, the wrapper is a near-zero-cost passthrough.
func NewInstrumentedTransport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "provider " + r.Method
		}),
	)
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
	id                    string
	includeRawOutput      bool
	client                *http.Client // request/response calls
	streamingClient       *http.Client // SSE streams; Timeout: 0
	streamIdleTimeout     time.Duration
	streamRetryPolicy     StreamRetryPolicy
	streamRetryBudget     *RetryBudget
	streamSemaphore       *StreamSemaphore
	rateLimiter           *rate.Limiter
	retryPolicy           pipeline.RetryPolicy
	maxRequestPayloadSize int64
}

// NewBaseProvider creates a new BaseProvider with common fields. A companion
// streaming client is auto-derived from the given client's transport with
// Timeout=0 so SSE call sites can use GetStreamingHTTPClient() without any
// extra wiring.
func NewBaseProvider(id string, includeRawOutput bool, client *http.Client) BaseProvider {
	return BaseProvider{
		id:                    id,
		includeRawOutput:      includeRawOutput,
		client:                client,
		streamingClient:       newStreamingClient(client),
		retryPolicy:           DefaultRetryPolicy(),
		maxRequestPayloadSize: DefaultMaxPayloadSize,
	}
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
func NewBaseProviderWithAPIKey(id string, includeRawOutput bool, primaryKey, fallbackKey string) (provider BaseProvider, apiKey string) {
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
func NewBaseProviderWithCredential(
	id string, includeRawOutput bool, timeout time.Duration, cred Credential,
) (base BaseProvider, apiKey string) {
	client := &http.Client{
		Timeout:   timeout,
		Transport: NewInstrumentedTransport(NewPooledTransport()),
	}
	base = NewBaseProvider(id, includeRawOutput, client)
	apiKey = ExtractAPIKey(cred)
	return base, apiKey
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
// NOT be used for SSE streaming — use GetStreamingHTTPClient for that.
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
// Timeout=0 by design.
func (b *BaseProvider) SetHTTPTimeout(timeout time.Duration) {
	var transport http.RoundTripper
	if b.client != nil {
		transport = b.client.Transport
	}
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
// value (retry disabled) is the default — callers must opt in via config.
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
// requests. Passing nil (or a zero-limit semaphore) restores unlimited
// concurrency.
func (b *BaseProvider) SetStreamSemaphore(sem *StreamSemaphore) {
	b.streamSemaphore = sem
}

// AcquireStreamSlot blocks on the configured concurrent-stream semaphore
// until a slot is available or the context is done. Returns nil on
// successful acquire (caller MUST call b.ReleaseStreamSlot exactly once)
// or the context error on cancellation/deadline. Classifies the
// rejection reason and records it on the
// promptkit_stream_concurrency_rejections_total counter so operators
// can see saturation.
//
// Nil semaphore is a no-op — returns nil without blocking or emitting
// metrics, so callers can invoke this unconditionally.
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

// RunStreamingRequest is the single entry point every streaming-capable
// provider should delegate through. It composes the three layers of
// back-pressure (semaphore, budget, retry) with in-flight gauge
// bookkeeping and the "release on all exit paths" defer pattern into
// one helper, so individual provider streaming functions don't have to
// re-implement the same ~60 lines of acquire/release/metric scaffolding.
//
// The caller constructs the retry request (policy/budget/host/request
// factory/etc.) and provides a consumer that knows how to parse the
// provider-specific stream framing. On success, this function:
//
//  1. Acquires a concurrent-stream slot (blocks on ctx; nil semaphore
//     is a no-op).
//  2. Increments streams_in_flight and provider_calls_in_flight gauges.
//  3. Delegates to OpenStreamWithRetryRequest with the given req.
//  4. Spawns a goroutine that invokes the consumer on the result body
//     and, on consumer return, decrements the gauges and releases the
//     semaphore slot.
//
// On any error path before the goroutine is spawned, all acquired
// resources are released correctly via the deferred cleanup flags.
//
// Callers must set req.ProviderName to b.ID() — this is not done
// automatically to avoid hiding the coupling.
func (b *BaseProvider) RunStreamingRequest(
	ctx context.Context,
	req *StreamRetryRequest,
	consumer StreamConsumer,
) (<-chan StreamChunk, error) {
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

	outChan := make(chan StreamChunk, DefaultStreamBufferSize)
	// From this point on, ownership of the acquired slot and gauge
	// increments transfers to the stream goroutine's defer below. We
	// flip the flags so the outer defers are no-ops on the success path.
	released = true
	slotReleased = true
	go func() {
		defer func() {
			metrics.StreamsInFlightDec(providerID)
			metrics.ProviderCallsInFlightDec(providerID)
			b.ReleaseStreamSlot()
		}()
		consumer(ctx, result.Body, outChan)
	}()

	return outChan, nil
}

// HTTPTimeout returns the current HTTP client timeout, or 0 if no client is set.
func (b *BaseProvider) HTTPTimeout() time.Duration {
	if b.client == nil {
		return 0
	}
	return b.client.Timeout
}

// SetRateLimit configures per-provider rate limiting. requestsPerSecond controls
// the sustained rate, and burst controls how many requests can be made
// simultaneously before throttling kicks in. A zero or negative
// requestsPerSecond disables rate limiting (the default).
func (b *BaseProvider) SetRateLimit(requestsPerSecond float64, burst int) {
	if requestsPerSecond <= 0 {
		b.rateLimiter = nil
		return
	}
	b.rateLimiter = rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
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

// WaitForRateLimit blocks until the rate limiter allows the request to
// proceed, or until the context is canceled. If no rate limiter is
// configured, it returns immediately. Providers should call this before
// making HTTP requests to respect rate limits.
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
	resp, err := b.client.Do(req)
	if err != nil {
		predictResp.Latency = time.Since(start)
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
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
		return fmt.Errorf("API request to %s failed with status %d: %s", url, resp.StatusCode, string(body))
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

// MakeRawRequest performs an HTTP POST request with pre-marshaled body.
// It automatically retries on transient errors (429, 502, 503, 504 and
// network errors) according to the provider's RetryPolicy.
func (b *BaseProvider) MakeRawRequest(
	ctx context.Context,
	url string,
	body []byte,
	headers RequestHeaders,
	providerName string,
) ([]byte, error) {
	// Log the request once (mask sensitive headers for logging).
	logHeaders := make(map[string]string)
	for k, v := range headers {
		if k == "Authorization" || k == "x-api-key" {
			logHeaders[k] = "***"
		} else {
			logHeaders[k] = v
		}
	}
	logger.APIRequest(providerName, "POST", url, logHeaders, json.RawMessage(body))

	// Validate payload size before sending the request.
	if err := b.checkPayloadSize(body, providerName); err != nil {
		return nil, err
	}

	// Wait for rate limiter before making the HTTP call
	if waitErr := b.WaitForRateLimit(ctx); waitErr != nil {
		return nil, fmt.Errorf("rate limit wait: %w", waitErr)
	}

	doFn := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(
			ctx, "POST", url, bytes.NewReader(body),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
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
