package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// defaultToolExecTimeout bounds the per-call timeout when params don't
// specify one. Generous because the typical use case is "run the test
// suite inside a sandbox" — much longer than a normal tool call.
const defaultToolExecTimeout = 120 * time.Second

// metadataKeyToolRegistry is the EvalContext.Metadata key under which
// the consuming host (arena, SDK, etc.) injects a *tools.Registry.
// Without it, ToolExecHandler returns a clear configuration error
// rather than panicking on a nil registry.
const metadataKeyToolRegistry = "tool_registry"

// ToolExecHandler invokes a tool by name through the runtime tool
// registry and asserts the call succeeded. The pass condition is:
//
//   - tools.Registry.Execute returns no error, AND
//   - the resulting ToolResult has an empty Error field.
//
// This makes it a generic "is this tool happy" gate that works with
// any registered tool: MCP-backed (e.g. a sandbox's run_tests),
// HTTP/local executors, custom client tools — whatever the host has
// wired up. The handler doesn't know or care about the transport.
//
// Params:
//   - tool string (required) — registry name of the tool to invoke
//   - args map[string]any (optional) — arguments passed verbatim to Execute
//   - timeout_seconds int (optional) — bounds the call; default 120
//
// Score is 1.0 on success, 0.0 on any failure (error returned, result
// has Error field set, missing tool, missing registry, timeout).
type ToolExecHandler struct{}

// Type returns the eval type identifier.
func (h *ToolExecHandler) Type() string { return "tool_exec" }

// Eval invokes the named tool and returns pass/fail based on whether
// the tool reported success.
func (h *ToolExecHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	toolName, _ := params["tool"].(string)
	if toolName == "" {
		return failResult(h.Type(), "tool_exec: 'tool' parameter is required"), nil
	}

	registry, err := h.resolveRegistry(evalCtx)
	if err != nil {
		return failResult(h.Type(), err.Error()), nil
	}

	args, err := h.encodeArgs(params)
	if err != nil {
		return failResult(h.Type(), fmt.Sprintf("tool_exec: encode args for %q: %v", toolName, err)), nil
	}

	timeout := h.timeout(params)
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, execErr := registry.Execute(execCtx, toolName, args)
	if execErr != nil {
		return failResult(h.Type(), fmt.Sprintf("tool_exec: %q failed: %v", toolName, execErr)), nil
	}
	if result == nil {
		return failResult(h.Type(), fmt.Sprintf("tool_exec: %q returned no result", toolName)), nil
	}
	if result.Error != "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("tool_exec: %q reported error: %s", toolName, result.Error),
			Value:       map[string]any{"tool": toolName, "error": result.Error},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("tool_exec: %q succeeded", toolName),
		Value: map[string]any{
			"tool":       toolName,
			"latency_ms": result.LatencyMs,
		},
	}, nil
}

// resolveRegistry pulls the tools.Registry out of EvalContext.Metadata.
// Returns a clear error rather than nil so callers learn how to fix
// the wiring (host needs to inject the registry before evaluating).
func (h *ToolExecHandler) resolveRegistry(evalCtx *evals.EvalContext) (*tools.Registry, error) {
	if evalCtx == nil || evalCtx.Metadata == nil {
		return nil, fmt.Errorf("tool_exec: no metadata on eval context; host must inject tool_registry")
	}
	raw, ok := evalCtx.Metadata[metadataKeyToolRegistry]
	if !ok {
		return nil, fmt.Errorf(
			"tool_exec: %q not present in metadata; host must inject the tool registry",
			metadataKeyToolRegistry,
		)
	}
	registry, ok := raw.(*tools.Registry)
	if !ok || registry == nil {
		return nil, fmt.Errorf("tool_exec: %q is not a *tools.Registry (got %T)", metadataKeyToolRegistry, raw)
	}
	return registry, nil
}

// encodeArgs marshals the params.args map (if any) to JSON for Execute.
// Empty / missing args returns an empty json.RawMessage which most
// executors accept as "no args".
func (h *ToolExecHandler) encodeArgs(params map[string]any) (json.RawMessage, error) {
	raw, ok := params["args"]
	if !ok || raw == nil {
		return json.RawMessage(`{}`), nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// timeout reads timeout_seconds from params, clamped to a sane default.
func (h *ToolExecHandler) timeout(params map[string]any) time.Duration {
	secs := extractInt(params, "timeout_seconds", 0)
	if secs <= 0 {
		return defaultToolExecTimeout
	}
	return time.Duration(secs) * time.Second
}

// failResult is a small helper for the early-exit fail paths.
func failResult(typeName, explanation string) *evals.EvalResult {
	return &evals.EvalResult{
		Type:        typeName,
		Score:       boolScore(false),
		Explanation: explanation,
	}
}
