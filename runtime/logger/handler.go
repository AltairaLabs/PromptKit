package logger

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
)

// ContextHandler is a slog.Handler that automatically extracts logging fields
// from context and adds them to log records. It wraps an inner handler and
// delegates all actual logging to it after enriching records with context data.
type ContextHandler struct {
	inner        slog.Handler
	commonFields []slog.Attr
}

// ModuleHandler extends ContextHandler with per-module log level filtering.
// It determines the module name from the call stack and applies the appropriate
// log level from the module configuration.
type ModuleHandler struct {
	ContextHandler
	moduleConfig *ModuleConfig
}

// NewContextHandler creates a new ContextHandler wrapping the given handler.
// The commonFields are added to every log record (useful for environment, service name, etc.).
func NewContextHandler(inner slog.Handler, commonFields ...slog.Attr) *ContextHandler {
	return &ContextHandler{
		inner:        inner,
		commonFields: commonFields,
	}
}

// Enabled reports whether the handler handles records at the given level.
// It delegates to the inner handler.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle processes the log record by extracting context fields and adding them
// to the record before delegating to the inner handler.
//
//nolint:gocritic // slog.Record is passed by value per slog.Handler interface contract
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	// Add common fields and context fields directly to the passed record.
	// slog passes records by value, so this is safe and avoids allocating
	// a new Record with slog.NewRecord (which allocates an internal attr slice).
	// Note: attrs added here appear after the caller's attrs in iteration order.
	for _, attr := range h.commonFields {
		r.AddAttrs(attr)
	}
	h.addContextFields(ctx, &r)

	return h.inner.Handle(ctx, r)
}

// addContextFields extracts all known context keys and adds them as attributes.
func (h *ContextHandler) addContextFields(ctx context.Context, r *slog.Record) {
	for _, key := range allContextKeys {
		if v := ctx.Value(key); v != nil {
			if s, ok := v.(string); ok && s != "" {
				r.AddAttrs(slog.String(string(key), s))
			}
		}
	}
}

// WithAttrs returns a new handler with the given attributes added.
// The attributes are added to the inner handler.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{
		inner:        h.inner.WithAttrs(attrs),
		commonFields: h.commonFields,
	}
}

// WithGroup returns a new handler with the given group name.
// The group is added to the inner handler.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{
		inner:        h.inner.WithGroup(name),
		commonFields: h.commonFields,
	}
}

// Unwrap returns the inner handler. This is useful for handler chains
// that need to inspect or replace the underlying handler.
func (h *ContextHandler) Unwrap() slog.Handler {
	return h.inner
}

// compile-time check that ContextHandler implements slog.Handler
var _ slog.Handler = (*ContextHandler)(nil)

// NewModuleHandler creates a new ModuleHandler with per-module log level filtering.
func NewModuleHandler(inner slog.Handler, moduleConfig *ModuleConfig, commonFields ...slog.Attr) *ModuleHandler {
	return &ModuleHandler{
		ContextHandler: ContextHandler{
			inner:        inner,
			commonFields: commonFields,
		},
		moduleConfig: moduleConfig,
	}
}

// Enabled reports whether the handler handles records at the given level.
// It uses the module configuration to determine the level for the calling module.
func (h *ModuleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	module := getCallerModule()
	moduleLevel := h.moduleConfig.LevelFor(module)
	return level >= moduleLevel
}

// Handle processes the log record, adding the module name as an attribute.
//
//nolint:gocritic // slog.Record is passed by value per slog.Handler interface contract
func (h *ModuleHandler) Handle(ctx context.Context, r slog.Record) error {
	// Check if we should handle this record based on module level
	module := getCallerModuleFromPC(r.PC)
	moduleLevel := h.moduleConfig.LevelFor(module)
	if r.Level < moduleLevel {
		return nil
	}

	// Add fields directly to the passed record (value copy, safe to mutate)
	// to avoid allocating a new Record with slog.NewRecord.
	for _, attr := range h.commonFields {
		r.AddAttrs(attr)
	}
	if module != "" {
		r.AddAttrs(slog.String("logger", module))
	}
	h.addContextFields(ctx, &r)

	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes added.
func (h *ModuleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ModuleHandler{
		ContextHandler: ContextHandler{
			inner:        h.inner.WithAttrs(attrs),
			commonFields: h.commonFields,
		},
		moduleConfig: h.moduleConfig,
	}
}

// WithGroup returns a new handler with the given group name.
func (h *ModuleHandler) WithGroup(name string) slog.Handler {
	return &ModuleHandler{
		ContextHandler: ContextHandler{
			inner:        h.inner.WithGroup(name),
			commonFields: h.commonFields,
		},
		moduleConfig: h.moduleConfig,
	}
}

// getCallerModule returns the module name of the calling code.
// It walks up the stack to find the first frame outside the logger package.
func getCallerModule() string {
	// Skip: getCallerModule, Enabled, slog internals, and the logger wrapper
	// We need to go deep enough to get out of the logger package
	const maxDepth = 10
	var pcs [maxDepth]uintptr
	//nolint:mnd // 3 is the number of stack frames to skip (getCallerModule, Enabled, slog)
	n := runtime.Callers(3, pcs[:])
	if n == 0 {
		return ""
	}

	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		module := extractModuleFromFunction(frame.Function)
		if module != "" && !strings.HasPrefix(module, "logger") {
			return module
		}
		if !more {
			break
		}
	}
	return ""
}

// getCallerModuleFromPC extracts the module name from a program counter.
func getCallerModuleFromPC(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frames := runtime.CallersFrames([]uintptr{pc})
	frame, _ := frames.Next()
	return extractModuleFromFunction(frame.Function)
}

// extractModuleFromFunction extracts a module name from a fully qualified function name.
// For example, "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline.(*Executor).Run"
// becomes "runtime.pipeline".
func extractModuleFromFunction(fn string) string {
	if fn == "" {
		return ""
	}

	// Find the package path after the module root
	const moduleRoot = "github.com/AltairaLabs/PromptKit/"
	idx := strings.Index(fn, moduleRoot)
	if idx == -1 {
		// Not in our module, return empty
		return ""
	}

	// Extract the path after the module root
	path := fn[idx+len(moduleRoot):]

	// Find the function name (after the last dot before any method receiver)
	// e.g., "runtime/pipeline.(*Executor).Run" -> "runtime/pipeline"
	if parenIdx := strings.Index(path, "("); parenIdx != -1 {
		path = path[:parenIdx]
	}
	if dotIdx := strings.LastIndex(path, "."); dotIdx != -1 {
		path = path[:dotIdx]
	}

	// Drop the major-version segment. Go puts /v2 in the module path, so
	// "runtime/v2/pipeline" would otherwise log as "runtime.v2.pipeline" and
	// every level filter keyed on a module name would stop matching the moment
	// the major version moved. The version is a distribution detail, not part
	// of what a reader means by "the pipeline module".
	segments := strings.Split(path, "/")
	kept := segments[:0]
	for _, seg := range segments {
		if isMajorVersionSegment(seg) {
			continue
		}
		kept = append(kept, seg)
	}

	// Convert slashes to dots for hierarchical module names
	return strings.Join(kept, ".")
}

// isMajorVersionSegment reports whether a path segment is a Go major-version
// suffix — "v2", "v3", … . "v1" never appears in a module path, and a package
// legitimately named e.g. "v1alpha1" is not one, because it is not all digits
// after the v.
func isMajorVersionSegment(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for _, r := range seg[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// compile-time check that ModuleHandler implements slog.Handler
var _ slog.Handler = (*ModuleHandler)(nil)
