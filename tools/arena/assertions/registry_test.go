package assertions

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

func TestNewArenaAssertionRegistry(t *testing.T) {
	registry := NewArenaAssertionRegistry()

	// Test that all expected validators are registered
	expectedValidators := []string{
		"tools_called",
		"tools_not_called",
		"content_includes",
		"content_matches",
	}

	for _, validatorType := range expectedValidators {
		t.Run("Has_"+validatorType, func(t *testing.T) {
			factory, ok := registry.Get(validatorType)
			if !ok {
				t.Errorf("Expected validator %q to be registered", validatorType)
			}
			if factory == nil {
				t.Errorf("Expected factory for %q to be non-nil", validatorType)
			}
		})
	}
}

func TestArenaAssertionRegistry_CreateValidators(t *testing.T) {
	registry := NewArenaAssertionRegistry()

	tests := []struct {
		name          string
		validatorType string
		params        map[string]interface{}
		testContent   string
		testParams    map[string]interface{}
		wantPassed      bool
	}{
		{
			name:          "tools_called validator",
			validatorType: "tools_called",
			params: map[string]interface{}{
				"tools": []string{"get_customer_info"},
			},
			testContent: "",
			testParams: map[string]interface{}{
				"_message_tool_calls": []types.MessageToolCall{
					{Name: "get_customer_info"},
				},
			},
			wantPassed: true,
		},
		{
			name:          "tools_not_called validator",
			validatorType: "tools_not_called",
			params: map[string]interface{}{
				"tools": []string{"delete_account"},
			},
			testContent: "",
			testParams: map[string]interface{}{
				"_message_tool_calls": []types.MessageToolCall{
					{Name: "get_customer_info"},
				},
			},
			wantPassed: true,
		},
		{
			name:          "content_includes validator",
			validatorType: "content_includes",
			params: map[string]interface{}{
				"patterns": []string{"account", "status"},
			},
			testContent: "Your account status is active",
			testParams:  map[string]interface{}{},
			wantPassed:      true,
		},
		{
			name:          "content_matches validator",
			validatorType: "content_matches",
			params: map[string]interface{}{
				"pattern": "account.*status",
			},
			testContent: "Your account and status information",
			testParams:  map[string]interface{}{},
			wantPassed:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, ok := registry.Get(tt.validatorType)
			if !ok {
				t.Fatalf("Validator %q not registered", tt.validatorType)
			}

			validator := factory(tt.params)
			if validator == nil {
				t.Fatal("Factory returned nil validator")
			}

			result := validator.Validate(tt.testContent, tt.testParams)
			if result.Passed != tt.wantPassed {
				t.Errorf("Validate() OK = %v, want %v, details = %v",
					result.Passed, tt.wantPassed, result.Details)
			}
		})
	}
}

func TestArenaAssertionRegistry_UnknownValidator(t *testing.T) {
	registry := NewArenaAssertionRegistry()

	factory, ok := registry.Get("nonexistent_validator")
	if ok {
		t.Error("Expected Get() to return false for unknown validator")
	}
	if factory != nil {
		t.Error("Expected factory to be nil for unknown validator")
	}
}

func TestArenaAssertionRegistry_Register(t *testing.T) {
	registry := NewArenaAssertionRegistry()

	// Register a custom validator
	customCalled := false
	registry.Register("custom_test", func(params map[string]interface{}) runtimeValidators.Validator {
		customCalled = true
		return NewContentIncludesValidator(params)
	})

	// Verify it was registered
	factory, ok := registry.Get("custom_test")
	if !ok {
		t.Fatal("Expected custom validator to be registered")
	}

	// Create validator and verify our factory was called
	validator := factory(map[string]interface{}{})
	if !customCalled {
		t.Error("Expected custom factory to be called")
	}
	if validator == nil {
		t.Error("Expected validator to be non-nil")
	}
}
