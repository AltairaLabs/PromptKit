package evals

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// EvalTypeHandler defines the interface for eval type implementations.
// Each handler covers a single eval type (e.g. "contains", "llm_judge").
// Handlers are stateless; params are passed per invocation.
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
//  1. When a guardrail hook is constructed, so a pack validator with unusable
//     params is surfaced at SDK load time instead of failing every turn.
//  2. By ValidateEvalTypes, so build-time checks and the SDK's eval middleware
//     see param errors the same way.
//
// Params passed to ValidateParams have already been normalized via
// ApplyDefaults and NormalizeParams, so implementations should check
// for canonical key names only.
type ParamValidator interface {
	// ValidateParams returns nil if params are usable by this handler,
	// or an error describing what is missing or wrong. The error message
	// should be self-contained: it is logged or surfaced in a
	// ValidateEvalTypes result without a wrapping prefix.
	ValidateParams(params map[string]any) error
}

// ParamNamer is an optional interface for EvalTypeHandler implementations
// that list the params they read. A handler that implements it has its params
// checked for unknown keys wherever ParamValidator runs (ValidateEvalTypes, the
// guardrail factory, and the assertion and guardrail wrappers' ValidateParams),
// so a misspelled key fails at load instead of being silently ignored. Every
// built-in handler implements it; a custom handler that does not is not checked.
//
// ParamNames returns the canonical names, after ApplyDefaults and
// NormalizeParams: a legacy name NormalizeParams rewrites need not be listed.
// The threshold keys min_score and max_score belong to the wrappers and are
// never reported as unknown, so they are not listed either.
type ParamNamer interface {
	ParamNames() []string
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

// NewEvalTypeRegistry creates a registry pre-populated with the handlers and
// aliases added through RegisterDefault and RegisterDefaultAlias, plus the
// "assertion" and "guardrail" wrappers. The evals/handlers package registers
// the built-in handlers in its init function, so import it for its side effects:
//
//	import _ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
func NewEvalTypeRegistry() *EvalTypeRegistry {
	r := NewEmptyEvalTypeRegistry()
	defaultsMu.RLock()
	handlers := append([]EvalTypeHandler(nil), defaultHandlers...)
	aliases := append([][2]string(nil), defaultAliases...)
	defaultsMu.RUnlock()
	for _, h := range handlers {
		r.Register(h)
	}
	for _, pair := range aliases {
		_ = r.RegisterAlias(pair[0], pair[1])
	}
	// Register wrapper handlers that resolve inner evals from the registry.
	r.Register(&AssertionEvalHandler{registry: r})
	r.Register(&GuardrailEvalHandler{registry: r})
	return r
}

// defaultsMu guards defaultHandlers and defaultAliases.
var defaultsMu sync.RWMutex

// defaultHandlers holds handlers registered via RegisterDefault.
// This avoids a circular import between evals and handlers.
var defaultHandlers []EvalTypeHandler

// defaultAliases holds alias→target pairs registered via RegisterDefaultAlias.
var defaultAliases [][2]string

// RegisterDefault adds a handler to the default set used by
// NewEvalTypeRegistry. It affects only registries created afterward. It is
// safe to call concurrently, though it is usually called from an init function.
func RegisterDefault(h EvalTypeHandler) {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	defaultHandlers = append(defaultHandlers, h)
	recordDeclaredGroups(h)
}

// RegisterDefaultAlias registers an alias mapping applied by NewEvalTypeRegistry.
// The target must be registered with RegisterDefault before the registry is
// created; an alias whose target is missing is skipped. It is safe to call
// concurrently.
func RegisterDefaultAlias(aliasType, targetType string) {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	defaultAliases = append(defaultAliases, [2]string{aliasType, targetType})
	recordGroupAlias(aliasType, targetType)
}

// DefaultAliases returns a copy of the pairs registered with RegisterDefaultAlias,
// as alias and target, sorted by alias.
func DefaultAliases() [][2]string {
	defaultsMu.RLock()
	out := make([][2]string, len(defaultAliases))
	copy(out, defaultAliases)
	defaultsMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// Register adds a handler to the registry. If a handler with the same
// type is already registered, it is replaced. A handler that implements
// GroupDeclarer has its groups recorded for DefaultGroupsForType.
func (r *EvalTypeRegistry) Register(handler EvalTypeHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[handler.Type()] = handler
	recordDeclaredGroups(handler)
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
	recordGroupAlias(aliasType, targetType)
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

// ParamNames returns the params the handler registered for evalType (or an
// alias of it) reads, sorted, and true. It returns nil and false when the type
// is not registered or its handler does not implement ParamNamer, which means
// its params are not known, not that it takes none.
func (r *EvalTypeRegistry) ParamNames(evalType string) ([]string, bool) {
	h, err := r.Get(evalType)
	if err != nil {
		return nil, false
	}
	pn, ok := h.(ParamNamer)
	if !ok {
		return nil, false
	}
	names := append([]string{}, pn.ParamNames()...)
	sort.Strings(names)
	return names, true
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
