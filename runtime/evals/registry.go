package evals

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// EvalTypeHandler defines the interface for eval type implementations.
// Each handler covers a single eval type (e.g. "contains", "llm_judge").
// Handlers are stateless — params are passed per invocation.
type EvalTypeHandler interface {
	// Type returns the eval type identifier (e.g. "contains", "regex").
	Type() string

	// Eval executes the evaluation and returns a result.
	// The EvalContext carries messages, tool calls, and metadata.
	// Params come from the EvalDef.Params map.
	Eval(ctx context.Context, evalCtx *EvalContext, params map[string]any) (*EvalResult, error)
}

// StreamableEvalHandler is an opt-in extension for EvalTypeHandler.
// Handlers implementing this interface support incremental evaluation
// on partial (streaming) content, enabling early abort in guardrails.
type StreamableEvalHandler interface {
	EvalTypeHandler

	// EvalPartial evaluates partial content accumulated so far.
	// Called on each streaming chunk. Implementations should be efficient
	// and avoid expensive operations on every call.
	EvalPartial(ctx context.Context, content string, params map[string]any) (*EvalResult, error)
}

// ParamValidator is an optional interface for EvalTypeHandler implementations
// that have required or strictly-typed params. Handlers without required
// params need not implement it.
//
// ValidateParams is called at two points:
//  1. Guardrail hook construction in runtime/hooks/guardrails/factory.go,
//     so pack validators with unusable params are surfaced at SDK load
//     time instead of silently failing every turn.
//  2. runtime/evals/validate.go:ValidateEvalTypes, so Arena's fail-fast
//     build-time check and the SDK's warn-and-skip middleware both see
//     param errors the same way.
//
// Params passed to ValidateParams have already been normalised via
// ApplyDefaults and NormalizeParams, so implementations should check
// for canonical key names only.
type ParamValidator interface {
	// ValidateParams returns nil if params are usable by this handler,
	// or an error describing what is missing or wrong. The error message
	// should be self-contained — it is logged and/or surfaced in a
	// ValidateEvalTypes result without a wrapping prefix.
	ValidateParams(params map[string]any) error
}

// EvalTypeRegistry provides thread-safe registration and lookup of
// EvalTypeHandler implementations by type name.
type EvalTypeRegistry struct {
	handlers map[string]EvalTypeHandler
	mu       sync.RWMutex
}

// NewEmptyEvalTypeRegistry creates a registry with no handlers registered.
// Use this in tests to control exactly which handlers are available.
func NewEmptyEvalTypeRegistry() *EvalTypeRegistry {
	return &EvalTypeRegistry{
		handlers: make(map[string]EvalTypeHandler),
	}
}

// NewEvalTypeRegistry creates a registry pre-populated with all
// built-in eval handlers. Call this in production code.
// Handlers self-register via RegisterDefaults in the handlers package;
// import _ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
// or call handlers.RegisterDefaults(r) explicitly.
func NewEvalTypeRegistry() *EvalTypeRegistry {
	r := NewEmptyEvalTypeRegistry()
	for _, h := range defaultHandlers {
		r.Register(h)
	}
	for _, pair := range defaultAliases {
		_ = r.RegisterAlias(pair[0], pair[1])
	}
	// Register wrapper handlers that resolve inner evals from the registry.
	r.Register(&AssertionEvalHandler{registry: r})
	r.Register(&GuardrailEvalHandler{registry: r})
	return r
}

// defaultHandlers holds handlers registered via RegisterDefault.
// This avoids a circular import between evals and handlers.
var defaultHandlers []EvalTypeHandler

// defaultAliases holds alias→target pairs registered via RegisterDefaultAlias.
var defaultAliases [][2]string

// RegisterDefault adds a handler to the default set used by
// NewEvalTypeRegistry. Call this from handler init() functions
// or from handlers.RegisterDefaults().
func RegisterDefault(h EvalTypeHandler) {
	defaultHandlers = append(defaultHandlers, h)
}

// RegisterDefaultAlias registers an alias mapping applied by NewEvalTypeRegistry.
// The target handler must be registered (via RegisterDefault) before the registry is created.
func RegisterDefaultAlias(aliasType, targetType string) {
	defaultAliases = append(defaultAliases, [2]string{aliasType, targetType})
}

// Register adds a handler to the registry. If a handler with the same
// type is already registered, it is replaced.
func (r *EvalTypeRegistry) Register(handler EvalTypeHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[handler.Type()] = handler
}

// RegisterAlias maps an alias name to an existing handler type.
// Lookups for aliasType will resolve to the handler registered for targetType.
// Returns an error if targetType has no registered handler.
func (r *EvalTypeRegistry) RegisterAlias(aliasType, targetType string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.handlers[targetType]
	if !ok {
		return fmt.Errorf("cannot alias %q to %q: target type not registered", aliasType, targetType)
	}
	r.handlers[aliasType] = h
	return nil
}

// Get returns the handler for the given type, or an error if not found.
func (r *EvalTypeRegistry) Get(evalType string) (EvalTypeHandler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[evalType]
	if !ok {
		return nil, fmt.Errorf("unknown eval type: %q", evalType)
	}
	return h, nil
}

// Has returns true if a handler is registered for the given type.
func (r *EvalTypeRegistry) Has(evalType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[evalType]
	return ok
}

// Types returns a sorted list of all registered eval type names.
func (r *EvalTypeRegistry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
