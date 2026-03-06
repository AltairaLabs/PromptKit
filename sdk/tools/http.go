// HTTP tool executor for SDK v2.
//
// This file provides HTTP tool execution capabilities, allowing tools defined
// in pack files to make HTTP calls to external APIs.

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Default configuration values
const (
	defaultHTTPMethod       = "POST"
	maxResponseSize         = 10 * 1024 * 1024 // 10MB per response
	DefaultMaxAggregateSize = 50 * 1024 * 1024 // 50MB cumulative across all responses
	envHeaderParts          = 2                // key=value split parts
)

// ErrAggregateResponseSizeExceeded is returned when the cumulative response
// size across all HTTP tool calls exceeds the configured maximum.
var ErrAggregateResponseSizeExceeded = fmt.Errorf("aggregate HTTP response size limit exceeded")

// HTTPExecutor executes tools that make HTTP calls based on pack configuration.
// It reads the HTTPConfig from the tool descriptor and makes the appropriate HTTP request.
// It tracks cumulative response sizes and rejects calls once the aggregate limit is reached.
type HTTPExecutor struct {
	client           *http.Client
	aggregateSize    atomic.Int64
	maxAggregateSize int64
}

// NewHTTPExecutor creates a new HTTP executor with the default HTTP client
// and default aggregate response size limit.
func NewHTTPExecutor() *HTTPExecutor {
	return &HTTPExecutor{
		client:           httputil.NewHTTPClient(httputil.DefaultToolTimeout),
		maxAggregateSize: DefaultMaxAggregateSize,
	}
}

// NewHTTPExecutorWithClient creates a new HTTP executor with a custom HTTP client.
// This is useful for testing or when custom transport configuration is needed.
func NewHTTPExecutorWithClient(client *http.Client) *HTTPExecutor {
	return &HTTPExecutor{
		client:           client,
		maxAggregateSize: DefaultMaxAggregateSize,
	}
}

// NewHTTPExecutorWithMaxAggregate creates a new HTTP executor with a custom
// aggregate response size limit. Use 0 or a negative value to disable.
func NewHTTPExecutorWithMaxAggregate(maxAggregate int64) *HTTPExecutor {
	return &HTTPExecutor{
		client:           httputil.NewHTTPClient(httputil.DefaultToolTimeout),
		maxAggregateSize: maxAggregate,
	}
}

// AggregateResponseSize returns the cumulative response size consumed so far.
func (e *HTTPExecutor) AggregateResponseSize() int64 {
	return e.aggregateSize.Load()
}

// ResetAggregateSize resets the cumulative response size counter to zero.
func (e *HTTPExecutor) ResetAggregateSize() {
	e.aggregateSize.Store(0)
}

// Name returns the executor name used for registration.
func (e *HTTPExecutor) Name() string {
	return "http"
}

// Execute performs an HTTP request based on the tool descriptor's HTTPConfig.
// The args are serialized to JSON and sent as the request body.
func (e *HTTPExecutor) Execute(
	ctx context.Context,
	descriptor *tools.ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, error) {
	return e.ExecuteWithContext(ctx, descriptor, args)
}

// ExecuteWithContext performs an HTTP request with context support for cancellation.
func (e *HTTPExecutor) ExecuteWithContext(
	ctx context.Context,
	descriptor *tools.ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.HTTPConfig == nil {
		return nil, fmt.Errorf("tool %q has no HTTP configuration", descriptor.Name)
	}

	cfg := descriptor.HTTPConfig

	// Create the request
	req, err := e.buildRequest(ctx, cfg, args)
	if err != nil {
		return nil, err
	}

	// Apply timeout if configured
	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)
	}

	// Execute the request
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Process the response
	return e.processResponse(resp, cfg)
}

// buildRequest creates an HTTP request from the config and args.
func (e *HTTPExecutor) buildRequest(
	ctx context.Context,
	cfg *tools.HTTPConfig,
	args json.RawMessage,
) (*http.Request, error) {
	// Determine HTTP method
	method := cfg.Method
	if method == "" {
		method = defaultHTTPMethod
	}

	// Create request body from args
	var body io.Reader
	if len(args) > 0 && string(args) != "null" && string(args) != "{}" {
		body = bytes.NewReader(args)
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set default content type for requests with body
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Apply headers
	e.applyHeaders(req, cfg)

	return req, nil
}

// applyHeaders applies static and environment-based headers to the request.
func (e *HTTPExecutor) applyHeaders(req *http.Request, cfg *tools.HTTPConfig) {
	// Apply static headers from config
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	// Apply headers from environment variables.
	// Security consideration: headers_from_env allows pack authors to inject arbitrary
	// environment variable values into HTTP requests. A malicious pack could exfiltrate
	// sensitive environment variables (e.g., AWS_SECRET_ACCESS_KEY) by directing requests
	// to an attacker-controlled endpoint.
	// TODO: implement an allowlist of permitted environment variable names or patterns
	// to limit which variables can be read via headers_from_env.
	for _, envHeader := range cfg.HeadersFromEnv {
		// Format: "Header-Name=ENV_VAR_NAME"
		parts := strings.SplitN(envHeader, "=", envHeaderParts)
		if len(parts) == envHeaderParts {
			headerName := parts[0]
			envVar := parts[1]
			if value := os.Getenv(envVar); value != "" {
				req.Header.Set(headerName, value)
			}
		}
	}
}

// processResponse reads and processes the HTTP response.
func (e *HTTPExecutor) processResponse(resp *http.Response, cfg *tools.HTTPConfig) (json.RawMessage, error) {
	// Check aggregate limit before reading (fast fail).
	if e.maxAggregateSize > 0 && e.aggregateSize.Load() >= e.maxAggregateSize {
		return nil, fmt.Errorf("%w: %d bytes consumed, limit is %d",
			ErrAggregateResponseSizeExceeded, e.aggregateSize.Load(), e.maxAggregateSize)
	}

	// Read response body with per-response size limit.
	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Track cumulative size and enforce aggregate limit.
	newTotal := e.aggregateSize.Add(int64(len(respBody)))
	if e.maxAggregateSize > 0 && newTotal > e.maxAggregateSize {
		return nil, fmt.Errorf("%w: %d bytes consumed, limit is %d",
			ErrAggregateResponseSizeExceeded, newTotal, e.maxAggregateSize)
	}

	// Check for HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP request returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Redact sensitive fields if configured
	if len(cfg.Redact) > 0 {
		respBody = redactFields(respBody, cfg.Redact)
	}

	// Validate response is valid JSON
	if !json.Valid(respBody) {
		// Wrap non-JSON response in a result object
		result := map[string]string{"result": string(respBody)}
		return json.Marshal(result)
	}

	return respBody, nil
}

// redactFields removes or masks sensitive fields from a JSON response.
func redactFields(data []byte, fields []string) []byte {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return data // Return original if not a JSON object
	}

	for _, field := range fields {
		if _, exists := obj[field]; exists {
			obj[field] = "[REDACTED]"
		}
	}

	result, err := json.Marshal(obj)
	if err != nil {
		return data
	}
	return result
}

// HTTPToolConfig provides a builder pattern for configuring HTTP tool handlers.
// This is used with OnToolHTTP to register HTTP-based tools programmatically.
type HTTPToolConfig struct {
	url         string
	method      string
	headers     map[string]string
	headersEnv  []string
	timeoutMs   int
	redact      []string
	transform   func(args map[string]any) (map[string]any, error)
	preRequest  func(req *http.Request) error
	postProcess func(resp []byte) ([]byte, error)
}

// HTTPToolOption configures an HTTPToolConfig.
type HTTPToolOption func(*HTTPToolConfig)

// NewHTTPToolConfig creates a new HTTP tool configuration.
func NewHTTPToolConfig(url string, opts ...HTTPToolOption) *HTTPToolConfig {
	cfg := &HTTPToolConfig{
		url:     url,
		method:  defaultHTTPMethod,
		headers: make(map[string]string),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// WithMethod sets the HTTP method.
func WithMethod(method string) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.method = method
	}
}

// WithHeader adds a static header.
func WithHeader(key, value string) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.headers[key] = value
	}
}

// WithHeaderFromEnv adds a header that reads its value from an environment variable.
// Format: "Header-Name=ENV_VAR_NAME"
func WithHeaderFromEnv(headerEnv string) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.headersEnv = append(c.headersEnv, headerEnv)
	}
}

// WithTimeout sets the request timeout in milliseconds.
func WithTimeout(ms int) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.timeoutMs = ms
	}
}

// WithRedact specifies fields to redact from the response.
func WithRedact(fields ...string) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.redact = append(c.redact, fields...)
	}
}

// WithTransform adds a function to transform arguments before the request.
func WithTransform(transform func(args map[string]any) (map[string]any, error)) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.transform = transform
	}
}

// WithPreRequest adds a function to modify the request before sending.
func WithPreRequest(fn func(req *http.Request) error) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.preRequest = fn
	}
}

// WithPostProcess adds a function to process the response after receiving.
func WithPostProcess(fn func(resp []byte) ([]byte, error)) HTTPToolOption {
	return func(c *HTTPToolConfig) {
		c.postProcess = fn
	}
}

// ToDescriptorConfig converts the HTTPToolConfig to a runtime HTTPConfig
// that can be used with a tool descriptor.
func (c *HTTPToolConfig) ToDescriptorConfig() *tools.HTTPConfig {
	return &tools.HTTPConfig{
		URL:            c.url,
		Method:         c.method,
		Headers:        c.headers,
		HeadersFromEnv: c.headersEnv,
		TimeoutMs:      c.timeoutMs,
		Redact:         c.redact,
	}
}

// Handler returns a tool handler function that makes the HTTP request.
// This is used with OnTool to register an HTTP-based tool.
func (c *HTTPToolConfig) Handler() func(args map[string]any) (any, error) {
	ctxHandler := c.HandlerCtx()
	return func(args map[string]any) (any, error) {
		return ctxHandler(context.Background(), args)
	}
}

// HandlerCtx returns a context-aware tool handler function that makes the HTTP request.
// The context is propagated to the underlying HTTP executor for tracing and cancellation.
func (c *HTTPToolConfig) HandlerCtx() func(ctx context.Context, args map[string]any) (any, error) {
	// TODO: Consider caching the HTTPExecutor at the HTTPToolConfig level or using
	// a package-level pool. Currently each HandlerCtx() call creates a new executor
	// with its own http.Client, which is fine for low-cardinality tool sets but
	// wasteful if called repeatedly for the same tool config.
	executor := NewHTTPExecutor()

	return func(ctx context.Context, args map[string]any) (any, error) {
		return c.executeHandler(ctx, executor, args)
	}
}

// executeHandler performs the HTTP request with transforms and post-processing.
func (c *HTTPToolConfig) executeHandler(
	ctx context.Context, executor *HTTPExecutor, args map[string]any,
) (any, error) {
	// Apply transform if configured
	if c.transform != nil {
		var err error
		args, err = c.transform(args)
		if err != nil {
			return nil, fmt.Errorf("argument transform failed: %w", err)
		}
	}

	// Create a synthetic descriptor with the config
	descriptor := &tools.ToolDescriptor{
		Name:       "http_tool",
		HTTPConfig: c.ToDescriptorConfig(),
	}

	// Serialize args to JSON
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize arguments: %w", err)
	}

	// Execute the HTTP request with pipeline context for tracing and cancellation
	result, err := executor.Execute(ctx, descriptor, argsJSON)
	if err != nil {
		return nil, err
	}

	// Apply post-processing if configured
	if c.postProcess != nil {
		result, err = c.postProcess(result)
		if err != nil {
			return nil, fmt.Errorf("post-processing failed: %w", err)
		}
	}

	// Parse the result back to a Go value
	var parsed any
	if json.Unmarshal(result, &parsed) != nil {
		return string(result), nil
	}
	return parsed, nil
}
