package assertions

import (
	"context"
	"fmt"
	"sync"
)

// ConversationValidatorFactory is a factory function that creates new validator instances.
// Using factories allows validators to be stateless and thread-safe.
type ConversationValidatorFactory func() ConversationValidator

// ConversationValidatorRegistry manages available conversation-level validators.
// Provides registration and lookup of validators by type name.
// Thread-safe for concurrent access.
type ConversationValidatorRegistry struct {
	validators map[string]ConversationValidatorFactory
	mu         sync.RWMutex
}

// NewConversationValidatorRegistry creates a new registry with built-in validators.
// Returns a registry pre-populated with all standard conversation validators.
func NewConversationValidatorRegistry() *ConversationValidatorRegistry {
	registry := &ConversationValidatorRegistry{
		validators: make(map[string]ConversationValidatorFactory),
	}

	// Register built-in validators (Phase 2)
	registry.Register("tools_called", NewToolsCalledConversationValidator)
	registry.Register("tools_not_called", NewToolsNotCalledConversationValidator)
	registry.Register("tools_not_called_with_args", NewToolsNotCalledWithArgsConversationValidator)
	registry.Register("content_not_includes", NewContentNotIncludesConversationValidator)

	return registry
}

// Register adds a validator factory to the registry.
// The name must match the Type() returned by validators created by the factory.
// Panics if name is empty or factory is nil.
func (r *ConversationValidatorRegistry) Register(name string, factory ConversationValidatorFactory) {
	if name == "" {
		panic("conversation validator name cannot be empty")
	}
	if factory == nil {
		panic("conversation validator factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.validators[name] = factory
}

// Get retrieves a validator by name, creating a new instance via its factory.
// Returns an error if the validator type is not registered.
func (r *ConversationValidatorRegistry) Get(name string) (ConversationValidator, error) {
	r.mu.RLock()
	factory, ok := r.validators[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown conversation validator type: %s", name)
	}

	return factory(), nil
}

// Has checks if a validator type is registered.
func (r *ConversationValidatorRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.validators[name]
	return ok
}

// Types returns a list of all registered validator type names.
// Useful for introspection and documentation.
func (r *ConversationValidatorRegistry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.validators))
	for name := range r.validators {
		types = append(types, name)
	}
	return types
}

// ValidateConversation is a convenience function that evaluates a single assertion.
// Looks up the validator, instantiates it, and runs validation.
func (r *ConversationValidatorRegistry) ValidateConversation(
	ctx context.Context,
	assertion ConversationAssertion,
	convCtx *ConversationContext,
) ConversationValidationResult {
	validator, err := r.Get(assertion.Type)
	if err != nil {
		return ConversationValidationResult{
			Passed:  false,
			Message: fmt.Sprintf("Failed to load validator: %v", err),
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	return validator.ValidateConversation(ctx, convCtx, assertion.Params)
}

// ValidateConversations evaluates multiple assertions against a conversation.
// Returns results for all assertions, continuing even if some fail.
func (r *ConversationValidatorRegistry) ValidateConversations(
	ctx context.Context,
	assertions []ConversationAssertion,
	convCtx *ConversationContext,
) []ConversationValidationResult {
	results := make([]ConversationValidationResult, len(assertions))

	for i, assertion := range assertions {
		results[i] = r.ValidateConversation(ctx, assertion, convCtx)
	}

	return results
}
