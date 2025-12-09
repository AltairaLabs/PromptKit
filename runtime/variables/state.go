package variables

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// StateProvider resolves variables from conversation state metadata.
// It extracts key-value pairs from the state's Metadata field and
// converts them to string variables for template substitution.
type StateProvider struct {
	// KeyPrefix filters metadata keys. Only keys with this prefix are included.
	// If empty, all metadata keys are included.
	KeyPrefix string

	// StripPrefix removes the KeyPrefix from variable names when true.
	// For example, if KeyPrefix="user_" and StripPrefix=true,
	// metadata key "user_name" becomes variable "name".
	StripPrefix bool
}

// NewStateProvider creates a StateProvider that extracts all metadata as variables.
func NewStateProvider() *StateProvider {
	return &StateProvider{}
}

// NewStatePrefixProvider creates a StateProvider that only extracts metadata
// keys with the given prefix. If stripPrefix is true, the prefix is removed
// from the resulting variable names.
func NewStatePrefixProvider(prefix string, stripPrefix bool) *StateProvider {
	return &StateProvider{
		KeyPrefix:   prefix,
		StripPrefix: stripPrefix,
	}
}

// Name returns the provider identifier.
func (p *StateProvider) Name() string {
	if p.KeyPrefix != "" {
		return fmt.Sprintf("state[%s]", p.KeyPrefix)
	}
	return "state"
}

// Provide extracts variables from conversation state metadata.
// Returns nil if state is nil or has no metadata.
func (p *StateProvider) Provide(ctx context.Context, state *statestore.ConversationState) (map[string]string, error) {
	if state == nil || state.Metadata == nil {
		return nil, nil
	}

	result := make(map[string]string)
	for k, v := range state.Metadata {
		// Apply prefix filter if configured
		if p.KeyPrefix != "" && !strings.HasPrefix(k, p.KeyPrefix) {
			continue
		}

		// Determine the variable name
		varName := k
		if p.StripPrefix && p.KeyPrefix != "" {
			varName = strings.TrimPrefix(k, p.KeyPrefix)
		}

		// Convert value to string
		result[varName] = fmt.Sprintf("%v", v)
	}

	return result, nil
}
