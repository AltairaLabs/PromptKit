package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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

	// Pattern checks run against the human-readable output text.
	outputText := extractResultText(result.Result)

	if sp, _ := params["success_pattern"].(string); sp != "" {
		rx, compileErr := regexp.Compile(sp)
		if compileErr != nil {
			msg := fmt.Sprintf("tool_exec: success_pattern %q is not a valid regex: %v", sp, compileErr)
			return failResult(h.Type(), msg), nil
		}
		if !rx.MatchString(outputText) {
			msg := fmt.Sprintf("tool_exec: %q succeeded but output did not match success_pattern %q", toolName, sp)
			return failResult(h.Type(), msg), nil
		}
	}

	if fp, _ := params["failure_pattern"].(string); fp != "" {
		rx, compileErr := regexp.Compile(fp)
		if compileErr != nil {
			msg := fmt.Sprintf("tool_exec: failure_pattern %q is not a valid regex: %v", fp, compileErr)
			return failResult(h.Type(), msg), nil
		}
		if rx.MatchString(outputText) {
			return failResult(h.Type(), fmt.Sprintf("tool_exec: %q output matched failure_pattern %q", toolName, fp)), nil
		}
	}

	value := map[string]any{
		"tool":       toolName,
		"latency_ms": result.LatencyMs,
	}
	// Surface the tool's parsed response on Details so this handler
	// can serve as a non-gating measurement: tools that emit JSON
	// metrics (e.g. a diff-stats script in a sandbox) flow into the
	// report's per-result details for jq aggregation. We populate
	// Details (not Value) because the AssertionEvalHandler wrapper
	// overwrites Value with a boolean pass/fail when this eval is
	// used as a conversation assertion, while Details survives.
	// JSON parse failures are non-fatal — the handler still reports
	// success on the binary score.
	details := map[string]any{
		"tool":       toolName,
		"latency_ms": result.LatencyMs,
	}
	if payload := parseToolPayload(result.Result); payload != nil {
		value["result"] = payload
		details["result"] = payload
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("tool_exec: %q succeeded", toolName),
		Value:       value,
		Details:     details,
	}, nil
}

// parseToolPayload best-effort decodes the tool's response into a Go
// value for inclusion on EvalResult.Value/Details. When the decoded
// value is itself a JSON string (common for shell-script wrappers
// where stdout is the captured payload), it tries one more decode so
// the metrics surface as structured fields rather than an escaped
// string blob. Empty/null bodies and unparseable bytes return nil.
func parseToolPayload(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	// Double-parse: if the outer decode produced a string and the
	// string itself parses as JSON, prefer the structured form.
	// Trims trailing whitespace because shell scripts often append
	// a newline to JSON output.
	if s, ok := decoded.(string); ok {
		trimmed := strings.TrimSpace(s)
		if len(trimmed) >= 2 && (trimmed[0] == '{' || trimmed[0] == '[') {
			var inner any
			if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
				return inner
			}
		}
	}
	return decoded
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

// extractResultText returns a plain-text string from a tool result payload
// suitable for regex matching. The Bash MCP tool wraps its stdout in a
// JSON string (e.g. `"exit: 1\nsome output\n"`), so we unwrap one level of
// JSON string encoding when present. For any other JSON shape (objects,
// arrays, numbers) we fall back to the raw JSON bytes.
func extractResultText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// failResult is a small helper for the early-exit fail paths.
func failResult(typeName, explanation string) *evals.EvalResult {
	return &evals.EvalResult{
		Type:        typeName,
		Score:       boolScore(false),
		Explanation: explanation,
	}
}
