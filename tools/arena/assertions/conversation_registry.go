package assertions

import (
	"context"
	"fmt"
	"sync"
)

// ConversationAssertionFactory creates new evaluator instances for conversation assertions.
// Using factories allows evaluators to be stateless and thread-safe.
type ConversationAssertionFactory func() ConversationValidator

// ConversationAssertionRegistry manages available conversation-level assertions.
// Provides registration and lookup of evaluators by type name.
// Thread-safe for concurrent access.
type ConversationAssertionRegistry struct {
	validators map[string]ConversationAssertionFactory
	mu         sync.RWMutex
}

// NewConversationAssertionRegistry creates a new registry with built-in assertions.
// Returns a registry pre-populated with all standard conversation assertions.
func NewConversationAssertionRegistry() *ConversationAssertionRegistry {
	registry := &ConversationAssertionRegistry{
		validators: make(map[string]ConversationAssertionFactory),
	}

	// Register built-in assertions (Phase 2)
	registry.Register("tools_called", NewToolsCalledConversationValidator)
	registry.Register("tools_not_called", NewToolsNotCalledConversationValidator)
	registry.Register("tools_not_called_with_args", NewToolsNotCalledWithArgsConversationValidator)
	registry.Register("content_not_includes", NewContentNotIncludesConversationValidator)
	registry.Register("content_includes_any", NewContentIncludesAnyConversationValidator)
	registry.Register("tool_calls_with_args", NewToolCallsWithArgsConversationValidator)
	registry.Register("llm_judge_conversation", NewLLMJudgeConversationValidator)

	// Register multi-agent conversation assertions
	registry.Register("agent_invoked", NewAgentInvokedConversationValidator)
	registry.Register("agent_not_invoked", NewAgentNotInvokedConversationValidator)

	return registry
}

// Register adds an assertion factory to the registry.
// The name must match the Type() returned by evaluators created by the factory.
// Panics if name is empty or factory is nil.
func (r *ConversationAssertionRegistry) Register(name string, factory ConversationAssertionFactory) {
	if name == "" {
		panic("conversation assertion type name cannot be empty")
	}
	if factory == nil {
		panic("conversation assertion factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.validators[name] = factory
}

// Get retrieves an evaluator by name, creating a new instance via its factory.
// Returns an error if the assertion type is not registered.
func (r *ConversationAssertionRegistry) Get(name string) (ConversationValidator, error) {
	r.mu.RLock()
	factory, ok := r.validators[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown conversation assertion type: %s", name)
	}

	return factory(), nil
}

// Has checks if an assertion type is registered.
func (r *ConversationAssertionRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.validators[name]
	return ok
}

// Types returns a list of all registered assertion type names.
// Useful for introspection and documentation.
func (r *ConversationAssertionRegistry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.validators))
	for name := range r.validators {
		types = append(types, name)
	}
	return types
}

// ValidateConversation evaluates a single conversation-level assertion.
// Looks up the evaluator, instantiates it, and runs validation.
func (r *ConversationAssertionRegistry) ValidateConversation(
	ctx context.Context,
	assertion ConversationAssertion,
	convCtx *ConversationContext,
) ConversationValidationResult {
	validator, err := r.Get(assertion.Type)
	if err != nil {
		return ConversationValidationResult{
			Type:    assertion.Type,
			Passed:  false,
			Message: fmt.Sprintf("Failed to load validator: %v", err),
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		}
	}

	result := validator.ValidateConversation(ctx, convCtx, assertion.Params)
	// Ensure Type field is always populated from the assertion config
	result.Type = assertion.Type
	// Use custom message from assertion config if provided
	if assertion.Message != "" {
		result.Message = assertion.Message
	}
	return result
}

// ValidateConversations evaluates multiple assertions against a conversation.
// Returns results for all assertions, continuing even if some fail.
func (r *ConversationAssertionRegistry) ValidateConversations(
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
