// Package adaptersdk provides a lightweight SDK for building deploy adapters.
// It exposes a JSON-RPC 2.0 server over stdio so that adapter authors can
// implement the deploy.Provider interface and call Serve() to run.
package adaptersdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// JSON-RPC method names recognized by the adapter protocol.
const (
	MethodGetProviderInfo = "get_provider_info"
	MethodValidateConfig  = "validate_config"
	MethodPlan            = "plan"
	MethodApply           = "apply"
	MethodDestroy         = "destroy"
	MethodStatus          = "status"
	MethodImport          = "import"
)

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInternalError  = -32603
)

// jsonRPCVersion is the JSON-RPC protocol version string.
const jsonRPCVersion = "2.0"

// request is a JSON-RPC 2.0 request envelope.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// response is a JSON-RPC 2.0 response envelope.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// rpcError represents a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// applyResult wraps the adapter state returned by Apply.
type applyResult struct {
	AdapterState string `json:"adapter_state"`
}

// Serve reads JSON-RPC requests from stdin, dispatches them to the
// given deploy.Provider, and writes responses to stdout. It runs
// until stdin is closed or an unrecoverable I/O error occurs.
func Serve(provider deploy.Provider) error {
	return ServeIO(provider, os.Stdin, os.Stdout)
}

// ServeIO is like Serve but reads from r and writes to w, allowing
// tests to substitute stdin/stdout. If r or w is nil the function
// returns an error.
func ServeIO(
	provider deploy.Provider,
	r io.Reader,
	w io.Writer,
) error {
	if r == nil || w == nil {
		return fmt.Errorf("adaptersdk: reader and writer must not be nil")
	}
	scanner := bufio.NewScanner(r)
	// Allow up to 10 MB per line for large pack payloads.
	const (
		maxScanSize    = 10 * 1024 * 1024
		initialBufSize = 64 * 1024
	)
	scanner.Buffer(make([]byte, 0, initialBufSize), maxScanSize)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := response{
				JSONRPC: jsonRPCVersion,
				Error: &rpcError{
					Code:    CodeParseError,
					Message: "parse error: " + err.Error(),
				},
				ID: nil,
			}
			if encErr := enc.Encode(resp); encErr != nil {
				return fmt.Errorf("adaptersdk: write error: %w", encErr)
			}
			continue
		}

		resp := dispatch(provider, &req)
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("adaptersdk: write error: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("adaptersdk: read error: %w", err)
	}
	return nil
}

// dispatch routes a single JSON-RPC request to the appropriate Provider method.
func dispatch(provider deploy.Provider, req *request) response {
	ctx := context.Background()

	switch req.Method {
	case MethodGetProviderInfo:
		return handleGetProviderInfo(ctx, provider, req)
	case MethodValidateConfig:
		return handleValidateConfig(ctx, provider, req)
	case MethodPlan:
		return handlePlan(ctx, provider, req)
	case MethodApply:
		return handleApply(ctx, provider, req)
	case MethodDestroy:
		return handleDestroy(ctx, provider, req)
	case MethodStatus:
		return handleStatus(ctx, provider, req)
	case MethodImport:
		return handleImport(ctx, provider, req)
	default:
		return response{
			JSONRPC: jsonRPCVersion,
			Error: &rpcError{
				Code:    CodeMethodNotFound,
				Message: "method not found: " + req.Method,
			},
			ID: req.ID,
		}
	}
}

// handleGetProviderInfo handles the get_provider_info method.
func handleGetProviderInfo(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	info, err := provider.GetProviderInfo(ctx)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, info)
}

// handleValidateConfig handles the validate_config method.
func handleValidateConfig(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	var params deploy.ValidateRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeParseError, "invalid params: "+err.Error())
	}
	result, err := provider.ValidateConfig(ctx, &params)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, result)
}

// handlePlan handles the plan method.
func handlePlan(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	var params deploy.PlanRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeParseError, "invalid params: "+err.Error())
	}
	result, err := provider.Plan(ctx, &params)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, result)
}

// handleApply handles the apply method.
func handleApply(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	var params deploy.PlanRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeParseError, "invalid params: "+err.Error())
	}
	// Apply streams events via callback; we collect them and return the
	// final adapter state in the response.
	var events []*deploy.ApplyEvent
	callback := func(event *deploy.ApplyEvent) error {
		events = append(events, event)
		return nil
	}
	state, err := provider.Apply(ctx, &params, callback)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, &applyResult{AdapterState: state})
}

// handleDestroy handles the destroy method.
func handleDestroy(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	var params deploy.DestroyRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeParseError, "invalid params: "+err.Error())
	}
	var events []*deploy.DestroyEvent
	callback := func(event *deploy.DestroyEvent) error {
		events = append(events, event)
		return nil
	}
	err := provider.Destroy(ctx, &params, callback)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, map[string]string{"status": "destroyed"})
}

// handleStatus handles the status method.
func handleStatus(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	var params deploy.StatusRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeParseError, "invalid params: "+err.Error())
	}
	result, err := provider.Status(ctx, &params)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, result)
}

// handleImport handles the import method.
func handleImport(
	ctx context.Context,
	provider deploy.Provider,
	req *request,
) response {
	var params deploy.ImportRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, CodeParseError, "invalid params: "+err.Error())
	}
	result, err := provider.Import(ctx, &params)
	if err != nil {
		return errResponse(req.ID, CodeInternalError, err.Error())
	}
	return okResponse(req.ID, result)
}

// okResponse builds a successful JSON-RPC response.
func okResponse(id json.RawMessage, result any) response {
	return response{
		JSONRPC: jsonRPCVersion,
		Result:  result,
		ID:      id,
	}
}

// errResponse builds an error JSON-RPC response.
func errResponse(id json.RawMessage, code int, message string) response {
	return response{
		JSONRPC: jsonRPCVersion,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}
