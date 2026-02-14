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
func NewEvalTypeRegistry() *EvalTypeRegistry {
	r := NewEmptyEvalTypeRegistry()
	// Built-in handlers will be registered here as they are implemented
	// in subsequent issues (#303, #304, #305).
	return r
}

// Register adds a handler to the registry. If a handler with the same
// type is already registered, it is replaced.
func (r *EvalTypeRegistry) Register(handler EvalTypeHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[handler.Type()] = handler
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
