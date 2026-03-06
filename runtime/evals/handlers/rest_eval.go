package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// maxResponseBodySize is the maximum allowed response body size (10 MB).
const maxResponseBodySize = 10 * 1024 * 1024

// sharedHTTPClientTimeout is the safety net timeout for the shared HTTP client.
const sharedHTTPClientTimeout = 120 * time.Second

// sharedHTTPClient is a package-level HTTP client with shared transport for connection reuse.
// The per-request timeout is set via context.WithTimeout, not on the client itself, so
// that the transport can be safely shared across requests with different timeout requirements.
var sharedHTTPClient = &http.Client{
	Transport: http.DefaultTransport,
	Timeout:   sharedHTTPClientTimeout,
}

// RestEvalHandler evaluates a single assistant turn by POSTing
// conversation context to an external HTTP endpoint and interpreting
// the structured JSON response.
//
// Params:
//   - url (string, required): endpoint URL
//   - method (string, optional): HTTP method, default POST
//   - headers (map[string]string, optional): request headers, supports ${ENV_VAR}
//   - timeout (string, optional): request timeout, default 30s
//   - include_messages (bool, optional): include conversation history, default true
//   - include_tool_calls (bool, optional): include tool call records, default false
//   - criteria (string, optional): evaluation criteria forwarded in request
//   - min_score (float64, optional): minimum score threshold
//   - extra (map[string]any, optional): arbitrary data forwarded in request
type RestEvalHandler struct{}

// Type returns the eval type identifier.
func (h *RestEvalHandler) Type() string { return "rest_eval" }

// Eval sends the current assistant output to the configured REST endpoint.
func (h *RestEvalHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return executeRestEval(ctx, evalCtx, params, h.Type(), evalCtx.CurrentOutput)
}

// RestEvalSessionHandler evaluates an entire conversation by POSTing
// all assistant messages to an external HTTP endpoint.
//
// Params: same as RestEvalHandler.
type RestEvalSessionHandler struct{}

// Type returns the eval type identifier.
func (h *RestEvalSessionHandler) Type() string { return "rest_eval_session" }

// Eval sends all assistant messages to the configured REST endpoint.
func (h *RestEvalSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	content := collectAssistantContent(evalCtx)
	return executeRestEval(ctx, evalCtx, params, h.Type(), content)
}

// executeRestEval is the shared implementation for both REST eval handlers.
func executeRestEval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
	evalType string,
	content string,
) (*evals.EvalResult, error) {
	url, ok := params["url"].(string)
	if !ok || url == "" {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: "rest_eval requires a 'url' param",
		}, nil
	}

	method := "POST"
	if v, ok := params["method"].(string); ok && v != "" {
		method = v
	}

	timeout := parseDuration(params, "timeout", defaultExternalTimeout)
	minScore := extractFloat64Ptr(params, "min_score")

	reqBody := buildExternalRequest(evalCtx, params, content)
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("failed to create request: %v", err),
		}, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Apply custom headers with env var interpolation.
	headers := extractMapStringString(params, "headers")
	for k, v := range headers {
		httpReq.Header.Set(k, expandEnvVars(v))
	}

	// Use per-request timeout via context instead of creating a new client each call
	reqCtx, reqCancel := context.WithTimeout(httpReq.Context(), timeout)
	defer reqCancel()
	httpReq = httpReq.WithContext(reqCtx)

	resp, err := sharedHTTPClient.Do(httpReq)
	if err != nil {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("request failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// Limit response body size to prevent unbounded memory allocation
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("failed to read response: %v", err),
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("endpoint returned status %d: %s", resp.StatusCode, string(respBody)),
		}, nil
	}

	result := parseExternalResponse(respBody, minScore)
	result.Type = evalType
	return result, nil
}
