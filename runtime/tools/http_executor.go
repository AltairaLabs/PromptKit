// HTTP tool executor for live API calls.
//
// This file provides HTTP tool execution capabilities, allowing tools with
// mode "live" to make real HTTP calls to external APIs. For GET requests,
// tool arguments are converted to URL query parameters. For POST/PUT/PATCH,
// arguments are sent as a JSON request body.

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/AltairaLabs/PromptKit/runtime/httputil"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	headerContentType = "Content-Type"
	mimeJSON          = "application/json"
)

// Default configuration values for HTTP tool execution.
const (
	defaultHTTPMethod       = "POST"
	executorNameHTTP        = "http"
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
//
// For GET/HEAD/DELETE requests, tool arguments are encoded as URL query parameters.
// For POST/PUT/PATCH requests, tool arguments are sent as a JSON request body.
//
// Custom RequestMapper and ResponseMapper implementations can be injected to
// override URL templating, argument partitioning, body building, header rendering,
// and response reshaping.
type HTTPExecutor struct {
	client           *http.Client
	aggregateSize    atomic.Int64
	maxAggregateSize int64

	// RequestMapper customizes how tool args map to HTTP requests.
	// If nil, DefaultRequestMapper is used.
	RequestMapper RequestMapper

	// ResponseMapper customizes how HTTP responses map to tool results.
	// If nil, DefaultResponseMapper is used.
	ResponseMapper ResponseMapper
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
	return executorNameHTTP
}

// Execute performs an HTTP request based on the tool descriptor's HTTPConfig.
func (e *HTTPExecutor) Execute(
	ctx context.Context,
	descriptor *ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.HTTPConfig == nil {
		return nil, fmt.Errorf("tool %q has no HTTP configuration", descriptor.Name)
	}

	cfg := descriptor.HTTPConfig

	// Create the request
	req, err := e.buildRequest(ctx, cfg, args)
	if err != nil {
		logger.Error("HTTP tool request build failed",
			"tool", descriptor.Name, "url", cfg.URL, "error", err)
		return nil, err
	}

	// Inject OTel trace context into outbound request headers.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Apply timeout if configured
	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)
	}

	method := req.Method
	logger.Info("HTTP tool call",
		"tool", descriptor.Name, "method", method, "url", req.URL.String())

	// Execute the request
	resp, err := e.client.Do(req)
	if err != nil {
		logger.Error("HTTP tool call failed",
			"tool", descriptor.Name, "method", method, "url", cfg.URL, "error", err)
		return nil, fmt.Errorf("HTTP request to %s %s failed: %w", method, cfg.URL, err)
	}
	defer resp.Body.Close()

	// Process the response
	result, err := e.processResponse(resp, cfg)
	if err != nil {
		logger.Error("HTTP tool response error",
			"tool", descriptor.Name, "method", method, "url", cfg.URL,
			"status", resp.StatusCode, "error", err)
		return nil, err
	}

	// Apply response mapping if configured.
	result, err = e.applyResponseMapping(result, cfg)
	if err != nil {
		logger.Error("Response mapping failed",
			"tool", descriptor.Name, "error", err)
		return nil, err
	}

	logger.Info("HTTP tool call completed",
		"tool", descriptor.Name, "method", method, "url", cfg.URL,
		"status", resp.StatusCode, "response_bytes", len(result))
	return result, nil
}

// isBodyMethod returns true for HTTP methods that typically carry a request body.
func isBodyMethod(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// requestMapper returns the configured RequestMapper or the default.
func (e *HTTPExecutor) requestMapper() RequestMapper {
	if e.RequestMapper != nil {
		return e.RequestMapper
	}
	return &DefaultRequestMapper{}
}

// responseMapper returns the configured ResponseMapper or the default.
func (e *HTTPExecutor) responseMapper() ResponseMapper {
	if e.ResponseMapper != nil {
		return e.ResponseMapper
	}
	return &DefaultResponseMapper{}
}

// buildRequest creates an HTTP request from the config and args.
// When a RequestMapping is configured, it uses the mapper pipeline for URL
// templating, argument partitioning, body building, and header rendering.
// Otherwise, it falls back to the original behavior: GET args become query
// params, POST/PUT/PATCH args become a JSON body.
func (e *HTTPExecutor) buildRequest(
	ctx context.Context,
	cfg *HTTPConfig,
	args json.RawMessage,
) (*http.Request, error) {
	method := cfg.Method
	if method == "" {
		method = defaultHTTPMethod
	}

	hasArgs := len(args) > 0 && string(args) != "null" && string(args) != "{}"

	// If a request mapping is configured, use the mapper pipeline.
	if cfg.Request != nil && hasArgs {
		return e.buildMappedRequest(ctx, cfg, method, args)
	}

	// Legacy path: no mapping config.
	targetURL := cfg.URL
	var body io.Reader

	if hasArgs {
		if isBodyMethod(method) {
			body = bytes.NewReader(args)
		} else {
			var err error
			targetURL, err = appendQueryParams(targetURL, args)
			if err != nil {
				return nil, fmt.Errorf("failed to build query parameters: %w", err)
			}
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if body != nil {
		req.Header.Set(headerContentType, mimeJSON)
	}

	e.applyHeaders(req, cfg)
	return req, nil
}

// appendMappedQueryParams adds partitioned query args to a URL.
func appendMappedQueryParams(targetURL string, queryArgs map[string]any) (string, error) {
	if len(queryArgs) == 0 {
		return targetURL, nil
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %q: %w", targetURL, err)
	}
	q := parsed.Query()
	for k, v := range queryArgs {
		q.Set(k, formatQueryValue(v))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// buildMappedBody produces a JSON body from partitioned body args using the mapper.
func buildMappedBody(mapper RequestMapper, bodyArgs map[string]any, bodyExpr string) io.Reader {
	if len(bodyArgs) == 0 {
		return nil
	}
	bodyJSON, err := mapper.BuildBody(bodyArgs, bodyExpr)
	if err != nil || bodyJSON == nil {
		return nil
	}
	return bytes.NewReader(bodyJSON)
}

// buildMappedRequest uses the RequestMapper pipeline to build a request
// with URL templating, argument partitioning, body shaping, and header rendering.
func (e *HTTPExecutor) buildMappedRequest(
	ctx context.Context,
	cfg *HTTPConfig,
	method string,
	args json.RawMessage,
) (*http.Request, error) {
	mapper := e.requestMapper()

	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse args: %w", err)
	}

	targetURL, err := mapper.RenderURL(cfg.URL, argsMap)
	if err != nil {
		return nil, fmt.Errorf("URL template rendering failed: %w", err)
	}

	queryArgs, _, bodyArgs := mapper.PartitionArgs(argsMap, cfg.Request, cfg.URL)

	// Merge static query params (config-defined values the LLM doesn't control).
	for k, v := range cfg.Request.StaticQuery {
		queryArgs[k] = v
	}

	targetURL, err = appendMappedQueryParams(targetURL, queryArgs)
	if err != nil {
		return nil, err
	}

	// Merge static body fields for POST/PUT/PATCH.
	if isBodyMethod(method) {
		for k, v := range cfg.Request.StaticBody {
			bodyArgs[k] = v
		}
	}

	var body io.Reader
	if isBodyMethod(method) {
		body = buildMappedBody(mapper, bodyArgs, cfg.Request.BodyMapping)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	if body != nil {
		req.Header.Set(headerContentType, mimeJSON)
	}

	if err := e.applyMappedHeaders(req, mapper, cfg.Request.HeaderParams, argsMap); err != nil {
		return nil, err
	}

	// Apply static headers from mapping config.
	for k, v := range cfg.Request.StaticHeaders {
		req.Header.Set(k, v)
	}

	e.applyHeaders(req, cfg)
	return req, nil
}

// applyMappedHeaders renders and sets headers from the request mapping config.
func (e *HTTPExecutor) applyMappedHeaders(
	req *http.Request, mapper RequestMapper, templates map[string]string, args map[string]any,
) error {
	if len(templates) == 0 {
		return nil
	}
	rendered, err := mapper.RenderHeaders(templates, args)
	if err != nil {
		return err
	}
	for k, v := range rendered {
		req.Header.Set(k, v)
	}
	return nil
}

// appendQueryParams parses args as a flat JSON object and appends each
// key-value pair as a URL query parameter. Non-string values are serialized
// to their JSON representation.
func appendQueryParams(rawURL string, args json.RawMessage) (string, error) {
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return "", fmt.Errorf("failed to parse args as object: %w", err)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %q: %w", rawURL, err)
	}

	q := parsed.Query()
	for k, v := range argsMap {
		q.Set(k, formatQueryValue(v))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// formatQueryValue converts a value to a string suitable for a URL query parameter.
func formatQueryValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		// Use compact representation: "42" not "42.000000"
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case nil:
		return ""
	default:
		// For arrays, objects, etc., serialize to JSON
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

// applyHeaders applies static and environment-based headers to the request.
func (e *HTTPExecutor) applyHeaders(req *http.Request, cfg *HTTPConfig) {
	// Apply static headers from config
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	// Apply headers from environment variables.
	// These are configured in tool YAML by the system operator, not by end users
	// or LLM output, so no allowlist is needed.
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
func (e *HTTPExecutor) processResponse(resp *http.Response, cfg *HTTPConfig) (json.RawMessage, error) {
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
		respBody = RedactFields(respBody, cfg.Redact)
	}

	// Validate response is valid JSON
	if !json.Valid(respBody) {
		// Wrap non-JSON response in a result object
		result := map[string]string{"result": string(respBody)}
		return json.Marshal(result)
	}

	return respBody, nil
}

// applyResponseMapping applies the configured JMESPath response mapping.
func (e *HTTPExecutor) applyResponseMapping(result json.RawMessage, cfg *HTTPConfig) (json.RawMessage, error) {
	if cfg.Response == nil || cfg.Response.BodyMapping == "" {
		return result, nil
	}
	mapped, err := e.responseMapper().MapResponse(result, cfg.Response.BodyMapping)
	if err != nil {
		return nil, fmt.Errorf("response mapping failed: %w", err)
	}
	return mapped, nil
}

// ExecuteMultimodal performs an HTTP request and returns multimodal content parts
// when the response is a binary type (image, audio, video). For JSON responses,
// it falls back to the standard Execute path with no content parts.
func (e *HTTPExecutor) ExecuteMultimodal(
	ctx context.Context,
	descriptor *ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	if descriptor.HTTPConfig == nil {
		return nil, nil, fmt.Errorf("tool %q has no HTTP configuration", descriptor.Name)
	}

	cfg := descriptor.HTTPConfig

	// If multimodal is not enabled, delegate to standard Execute.
	if cfg.Multimodal == nil || !cfg.Multimodal.Enabled {
		result, err := e.Execute(ctx, descriptor, args)
		return result, nil, err
	}

	resp, err := e.doMultimodalRequest(ctx, cfg, args)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	// Check if response is binary based on Content-Type.
	contentType := resp.Header.Get(headerContentType)
	if IsBinaryContentType(contentType, cfg.Multimodal.AcceptTypes) {
		return e.handleBinaryResponse(resp, cfg)
	}

	// Non-binary response: fall back to standard processing.
	result, err := e.processResponse(resp, cfg)
	if err != nil {
		return nil, nil, err
	}
	result, err = e.applyResponseMapping(result, cfg)
	return result, nil, err
}

// doMultimodalRequest builds and executes an HTTP request with multimodal Accept headers.
func (e *HTTPExecutor) doMultimodalRequest(
	ctx context.Context, cfg *HTTPConfig, args json.RawMessage,
) (*http.Response, error) {
	req, err := e.buildRequest(ctx, cfg, args)
	if err != nil {
		return nil, err
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)
	}

	if len(cfg.Multimodal.AcceptTypes) > 0 {
		req.Header.Set("Accept", strings.Join(cfg.Multimodal.AcceptTypes, ", "))
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to %s %s failed: %w", req.Method, cfg.URL, err)
	}
	return resp, nil
}

// handleBinaryResponse processes a binary HTTP response into multimodal content parts.
func (e *HTTPExecutor) handleBinaryResponse(
	resp *http.Response, cfg *HTTPConfig,
) (json.RawMessage, []types.ContentPart, error) {
	jsonResult, parts, err := ReadMultimodalResponse(resp, &e.aggregateSize, e.maxAggregateSize)
	if err != nil {
		return nil, nil, err
	}
	jsonResult, err = e.applyResponseMapping(jsonResult, cfg)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult, parts, nil
}

// RedactFields removes or masks sensitive fields from a JSON response.
func RedactFields(data []byte, fields []string) []byte {
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
