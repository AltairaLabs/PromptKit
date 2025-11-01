package validators

import (
	"sync"
)

// ValidatorConfig defines a validator configuration from a prompt pack.
// This is just configuration data - validators are instantiated by the registry.
type ValidatorConfig struct {
	Type   string                 `json:"type" yaml:"type"`
	Params map[string]interface{} `json:"params" yaml:"params"`
}

// ValidatorFactory creates a validator instance from configuration params.
// Params from the config are passed at construction time to allow validators
// to pre-compile patterns, build state, etc.
type ValidatorFactory func(params map[string]interface{}) Validator

// Registry maps validator type names to factory functions.
// This allows dynamic instantiation of validators from configuration.
type Registry struct {
	factories map[string]ValidatorFactory
	mu        sync.RWMutex
}

// NewRegistry creates a new validator registry with built-in validators.
func NewRegistry() *Registry {
	r := &Registry{
		factories: make(map[string]ValidatorFactory),
	}

	// Register built-in validators
	r.Register("banned_words", func(params map[string]interface{}) Validator {
		// Extract banned words from params
		var words []string
		if wordsParam, ok := params["words"]; ok {
			switch w := wordsParam.(type) {
			case []string:
				words = w
			case []interface{}:
				for _, item := range w {
					if str, ok := item.(string); ok {
						words = append(words, str)
					}
				}
			}
		}
		return NewBannedWordsValidator(words)
	})
	r.Register("max_sentences", func(params map[string]interface{}) Validator {
		return NewMaxSentencesValidator()
	})
	r.Register("required_fields", func(params map[string]interface{}) Validator {
		return NewRequiredFieldsValidator()
	})
	r.Register("commit", func(params map[string]interface{}) Validator {
		return NewCommitValidator()
	})
	r.Register("length", func(params map[string]interface{}) Validator {
		return NewLengthValidator()
	})
	r.Register("max_length", func(params map[string]interface{}) Validator {
		return NewLengthValidator()
	}) // Alias

	return r
}

// Register adds a validator factory to the registry.
func (r *Registry) Register(validatorType string, factory ValidatorFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[validatorType] = factory
}

// Get retrieves a validator factory by type.
func (r *Registry) Get(validatorType string) (ValidatorFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[validatorType]
	return factory, ok
}

// HasValidator returns true if a validator type is registered.
func (r *Registry) HasValidator(validatorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[validatorType]
	return ok
}

// DefaultRegistry is the global validator registry.
var DefaultRegistry = NewRegistry()
