// HTTP tool executor for SDK v2.
//
// This file provides HTTP tool execution capabilities, allowing tools defined
// in pack files to make HTTP calls to external APIs.
//
// The core HTTPExecutor lives in runtime/tools and is re-exported here for
// backwards compatibility. The SDK adds the HTTPToolConfig builder pattern
// on top for programmatic use via OnToolHTTP.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// HTTPExecutor is re-exported from runtime/tools for backward compatibility.
type HTTPExecutor = tools.HTTPExecutor

// NewHTTPExecutor creates a new HTTP executor with default settings.
// NewHTTPExecutorWithClient creates one with a custom http.Client.
// NewHTTPExecutorWithMaxAggregate creates one with a custom aggregate size limit.
var (
	NewHTTPExecutor                 = tools.NewHTTPExecutor
	NewHTTPExecutorWithClient       = tools.NewHTTPExecutorWithClient
	NewHTTPExecutorWithMaxAggregate = tools.NewHTTPExecutorWithMaxAggregate
)

// DefaultMaxAggregateSize is the default cumulative response size limit (50 MB).
const DefaultMaxAggregateSize = tools.DefaultMaxAggregateSize

// ErrAggregateResponseSizeExceeded is returned when cumulative response size is exceeded.
var ErrAggregateResponseSizeExceeded = tools.ErrAggregateResponseSizeExceeded

// defaultHTTPMethod is used by HTTPToolConfig when no method is specified.
const defaultHTTPMethod = "POST"

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
	executor := tools.NewHTTPExecutor()

	return func(ctx context.Context, args map[string]any) (any, error) {
		return c.executeHandler(ctx, executor, args)
	}
}

// executeHandler performs the HTTP request with transforms and post-processing.
func (c *HTTPToolConfig) executeHandler(
	ctx context.Context, executor *tools.HTTPExecutor, args map[string]any,
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
