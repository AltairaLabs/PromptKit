// Package guardrails provides built-in ProviderHook implementations that
// replace the legacy validator system with hook-based guardrails.
package guardrails

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// Guardrail type name constants shared between Name() methods and the factory.
const (
	nameBannedWords    = "banned_words"
	nameLength         = "length"
	nameMaxSentences   = "max_sentences"
	nameRequiredFields = "required_fields"
)

// NewGuardrailHookFromRegistry creates a guardrail ProviderHook using the eval registry.
// This is the unified path — any registered eval handler can be used as a guardrail.
func NewGuardrailHookFromRegistry(
	typeName string, params map[string]any, registry *evals.EvalTypeRegistry,
) (hooks.ProviderHook, error) {
	handler, err := registry.Get(typeName)
	if err != nil {
		// Fall back to legacy constructors for backward compatibility.
		return newLegacyGuardrailHook(typeName, params)
	}

	direction := directionOutput
	if d, ok := params["direction"].(string); ok {
		direction = d
	}

	return &GuardrailHookAdapter{
		handler:   handler,
		evalType:  typeName,
		params:    params,
		direction: direction,
	}, nil
}

// NewGuardrailHook creates a guardrail ProviderHook using the default eval registry.
// This maintains backward compatibility with existing callers.
func NewGuardrailHook(typeName string, params map[string]any) (hooks.ProviderHook, error) {
	return NewGuardrailHookFromRegistry(typeName, params, evals.NewEvalTypeRegistry())
}

// newLegacyGuardrailHook creates a guardrail from the hand-coded constructors.
// These will be removed in Phase 3 once all types have eval handlers.
func newLegacyGuardrailHook(
	typeName string, params map[string]any,
) (hooks.ProviderHook, error) {
	switch typeName {
	case nameBannedWords:
		return newBannedWordsFromParams(params), nil
	case nameLength, "max_length":
		return newLengthFromParams(params), nil
	case nameMaxSentences:
		return newMaxSentencesFromParams(params), nil
	case nameRequiredFields:
		return newRequiredFieldsFromParams(params), nil
	default:
		return nil, fmt.Errorf("unknown guardrail type: %q", typeName)
	}
}

func newBannedWordsFromParams(params map[string]any) *BannedWordsHook {
	var words []string
	if w, ok := params["words"]; ok {
		switch v := w.(type) {
		case []string:
			words = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					words = append(words, s)
				}
			}
		}
	}
	return NewBannedWordsHook(words)
}

func newLengthFromParams(params map[string]any) *LengthHook {
	maxChars := intParam(params, "max_characters")
	maxTokens := intParam(params, "max_tokens")
	return NewLengthHook(maxChars, maxTokens)
}

func newMaxSentencesFromParams(params map[string]any) *MaxSentencesHook {
	limit := intParam(params, "max_sentences")
	return NewMaxSentencesHook(limit)
}

func newRequiredFieldsFromParams(params map[string]any) *RequiredFieldsHook {
	var fields []string
	if f, ok := params["required_fields"]; ok {
		switch v := f.(type) {
		case []string:
			fields = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					fields = append(fields, s)
				}
			}
		}
	}
	return NewRequiredFieldsHook(fields)
}

// intParam extracts an int parameter from a map, handling both int and
// float64 (common from JSON unmarshaling). Returns 0 if not found.
func intParam(params map[string]any, key string) int {
	v, ok := params[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
