package assertions

import (
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// NewArenaAssertionRegistry creates a new registry with arena-specific assertion validators
func NewArenaAssertionRegistry() *runtimeValidators.Registry {
	registry := runtimeValidators.NewRegistry()

	// Register arena-specific assertion validators
	registry.Register("tools_called", NewToolsCalledValidator)
	registry.Register("tools_not_called", NewToolsNotCalledValidator)
	registry.Register("content_includes", NewContentIncludesValidator)
	registry.Register("content_matches", NewContentMatchesValidator)
	registry.Register("guardrail_triggered", NewGuardrailTriggeredValidator)

	return registry
}
