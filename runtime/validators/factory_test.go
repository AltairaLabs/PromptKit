package validators

import (
	"testing"
)

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()

	if registry == nil {
		t.Fatal("NewRegistry() returned nil")
	}

	// Verify built-in validators are registered
	builtins := []string{
		"banned_words",
		"max_sentences",
		"required_fields",
		"commit",
		"length",
		"max_length", // alias
	}

	for _, validatorType := range builtins {
		if !registry.HasValidator(validatorType) {
			t.Errorf("Built-in validator %q not registered", validatorType)
		}
	}
}

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry()

	// Register a custom validator
	customFactory := func(params map[string]interface{}) Validator {
		return NewLengthValidator()
	}

	registry.Register("custom_test", customFactory)

	if !registry.HasValidator("custom_test") {
		t.Error("Custom validator not registered")
	}

	factory, ok := registry.Get("custom_test")
	if !ok {
		t.Error("Failed to get custom validator factory")
	}

	if factory == nil {
		t.Error("Got nil factory")
	}

	// Create validator using factory
	validator := factory(nil)
	if validator == nil {
		t.Error("Factory returned nil validator")
	}
}

func TestRegistry_Get(t *testing.T) {
	registry := NewRegistry()

	tests := []struct {
		name          string
		validatorType string
		shouldExist   bool
	}{
		{
			name:          "Get existing validator",
			validatorType: "banned_words",
			shouldExist:   true,
		},
		{
			name:          "Get non-existent validator",
			validatorType: "nonexistent",
			shouldExist:   false,
		},
		{
			name:          "Get max_sentences validator",
			validatorType: "max_sentences",
			shouldExist:   true,
		},
		{
			name:          "Get length alias",
			validatorType: "max_length",
			shouldExist:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, ok := registry.Get(tt.validatorType)

			if ok != tt.shouldExist {
				t.Errorf("Get(%q) ok = %v, want %v", tt.validatorType, ok, tt.shouldExist)
			}

			if tt.shouldExist && factory == nil {
				t.Error("Expected non-nil factory for existing validator")
			}

			if !tt.shouldExist && factory != nil {
				t.Error("Expected nil factory for non-existent validator")
			}
		})
	}
}

func TestRegistry_HasValidator(t *testing.T) {
	registry := NewRegistry()

	tests := []struct {
		name          string
		validatorType string
		want          bool
	}{
		{
			name:          "Existing validator",
			validatorType: "banned_words",
			want:          true,
		},
		{
			name:          "Non-existent validator",
			validatorType: "not_a_validator",
			want:          false,
		},
		{
			name:          "Empty string",
			validatorType: "",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.HasValidator(tt.validatorType)
			if got != tt.want {
				t.Errorf("HasValidator(%q) = %v, want %v", tt.validatorType, got, tt.want)
			}
		})
	}
}

func TestRegistry_BannedWordsFactory(t *testing.T) {
	registry := NewRegistry()
	factory, ok := registry.Get("banned_words")
	if !ok {
		t.Fatal("banned_words factory not found")
	}

	tests := []struct {
		name   string
		params map[string]interface{}
		want   int // expected number of banned words
	}{
		{
			name: "String slice",
			params: map[string]interface{}{
				"words": []string{"bad", "evil", "wrong"},
			},
			want: 3,
		},
		{
			name: "Interface slice",
			params: map[string]interface{}{
				"words": []interface{}{"bad", "evil"},
			},
			want: 2,
		},
		{
			name:   "No words param",
			params: map[string]interface{}{},
			want:   0,
		},
		{
			name: "Mixed types in interface slice",
			params: map[string]interface{}{
				"words": []interface{}{"bad", 123, "evil"},
			},
			want: 2, // Only strings are added
		},
		{
			name: "Non-slice type",
			params: map[string]interface{}{
				"words": "not a slice",
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := factory(tt.params)
			if validator == nil {
				t.Fatal("Factory returned nil validator")
			}

			bwv, ok := validator.(*BannedWordsValidator)
			if !ok {
				t.Fatal("Factory did not return BannedWordsValidator")
			}

			if len(bwv.bannedWords) != tt.want {
				t.Errorf("Expected %d banned words, got %d", tt.want, len(bwv.bannedWords))
			}
		})
	}
}

func TestRegistry_OtherFactories(t *testing.T) {
	registry := NewRegistry()

	tests := []struct {
		validatorType string
		expectedType  string
	}{
		{"max_sentences", "*validators.MaxSentencesValidator"},
		{"required_fields", "*validators.RequiredFieldsValidator"},
		{"commit", "*validators.CommitValidator"},
		{"length", "*validators.LengthValidator"},
		{"max_length", "*validators.LengthValidator"}, // alias
	}

	for _, tt := range tests {
		t.Run(tt.validatorType, func(t *testing.T) {
			factory, ok := registry.Get(tt.validatorType)
			if !ok {
				t.Fatalf("Factory for %q not found", tt.validatorType)
			}

			validator := factory(nil)
			if validator == nil {
				t.Fatal("Factory returned nil validator")
			}

			// Just verify we can create validators - detailed testing is in other test files
		})
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Verify DefaultRegistry is initialized
	if DefaultRegistry == nil {
		t.Fatal("DefaultRegistry is nil")
	}

	// Verify it has built-in validators
	if !DefaultRegistry.HasValidator("banned_words") {
		t.Error("DefaultRegistry missing banned_words validator")
	}

	if !DefaultRegistry.HasValidator("length") {
		t.Error("DefaultRegistry missing length validator")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	// Test concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			registry.Register("concurrent_test", func(params map[string]interface{}) Validator {
				return NewLengthValidator()
			})
		}
		done <- true
	}()

	// Reader goroutines
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				registry.HasValidator("banned_words")
				registry.Get("length")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}

	// Verify registry is still functional
	if !registry.HasValidator("concurrent_test") {
		t.Error("concurrent_test validator not found after concurrent access")
	}
}

func TestValidatorConfig(t *testing.T) {
	config := ValidatorConfig{
		Type: "banned_words",
		Params: map[string]interface{}{
			"words": []string{"test"},
		},
	}

	if config.Type != "banned_words" {
		t.Errorf("Expected Type = 'banned_words', got %q", config.Type)
	}

	if config.Params == nil {
		t.Error("Expected non-nil Params")
	}
}
