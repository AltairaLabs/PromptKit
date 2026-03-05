package variables

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

// StateProvider resolves variables from conversation state metadata.
// It extracts key-value pairs from the state's Metadata field and
// converts them to string variables for template substitution.
//
// The StateStore is injected via constructor, allowing the provider
// to look up state for the current conversation.
type StateProvider struct {
	// store is the state store to retrieve conversation state from.
	store statestore.Store

	// conversationID is the ID of the conversation to get state for.
	conversationID string

	// KeyPrefix filters metadata keys. Only keys with this prefix are included.
	// If empty, all metadata keys are included.
	KeyPrefix string

	// StripPrefix removes the KeyPrefix from variable names when true.
	// For example, if KeyPrefix="user_" and StripPrefix=true,
	// metadata key "user_name" becomes variable "name".
	StripPrefix bool
}

// NewStateProvider creates a StateProvider that extracts all metadata as variables
// from the given conversation's state.
func NewStateProvider(store statestore.Store, conversationID string) *StateProvider {
	return &StateProvider{
		store:          store,
		conversationID: conversationID,
	}
}

// NewStatePrefixProvider creates a StateProvider that only extracts metadata
// keys with the given prefix. If stripPrefix is true, the prefix is removed
// from the resulting variable names.
func NewStatePrefixProvider(store statestore.Store, conversationID, prefix string, stripPrefix bool) *StateProvider {
	return &StateProvider{
		store:          store,
		conversationID: conversationID,
		KeyPrefix:      prefix,
		StripPrefix:    stripPrefix,
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
// Returns nil if store is nil, conversation not found, or has no metadata.
func (p *StateProvider) Provide(ctx context.Context) (map[string]string, error) {
	// Handle both nil interface and nil pointer
	if p.store == nil || reflect.ValueOf(p.store).IsNil() {
		return nil, nil
	}

	// Use MetadataAccessor if available to avoid deep-copying the entire message
	// history. This is significantly cheaper for conversations with many messages.
	metadata, err := p.loadMetadata(ctx)
	if err != nil {
		// Conversation not found is not an error - just return no variables
		return nil, nil
	}

	if metadata == nil {
		return nil, nil
	}

	result := make(map[string]string)
	for k, v := range metadata {
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

// loadMetadata returns the metadata map for the current conversation.
// It uses MetadataAccessor if available to avoid deep-copying the full state,
// otherwise falls back to Store.Load.
func (p *StateProvider) loadMetadata(ctx context.Context) (map[string]interface{}, error) {
	// Try MetadataAccessor first (avoids deep-copying messages)
	if accessor, ok := p.store.(statestore.MetadataAccessor); ok {
		metadata, err := accessor.LoadMetadata(ctx, p.conversationID)
		if err != nil {
			if errors.Is(err, statestore.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return metadata, nil
	}

	// Fall back to full Load
	state, err := p.store.Load(ctx, p.conversationID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, nil
	}
	return state.Metadata, nil
}
