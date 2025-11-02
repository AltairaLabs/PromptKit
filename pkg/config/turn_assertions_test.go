package config

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"gopkg.in/yaml.v3"
)

func TestTurnDefinition_AssertionsField(t *testing.T) {
	yamlContent := `
role: user
content: "Check my account status"
assertions:
  - type: tools_called
    params:
      tools:
        - get_customer_info
        - check_subscription_status
  - type: content_includes
    params:
      patterns:
        - account
        - status
`

	var turn TurnDefinition
	err := yaml.Unmarshal([]byte(yamlContent), &turn)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Verify basic fields
	if turn.Role != "user" {
		t.Errorf("Expected role 'user', got %q", turn.Role)
	}
	if turn.Content != "Check my account status" {
		t.Errorf("Expected content, got %q", turn.Content)
	}

	// Verify assertions
	if len(turn.Assertions) != 2 {
		t.Fatalf("Expected 2 assertions, got %d", len(turn.Assertions))
	}

	// Check first assertion
	if turn.Assertions[0].Type != "tools_called" {
		t.Errorf("Expected first assertion type 'tools_called', got %q", turn.Assertions[0].Type)
	}

	tools, ok := turn.Assertions[0].Params["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools param to be []interface{}, got %T", turn.Assertions[0].Params["tools"])
	}
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	// Check second assertion
	if turn.Assertions[1].Type != "content_includes" {
		t.Errorf("Expected second assertion type 'content_includes', got %q", turn.Assertions[1].Type)
	}

	patterns, ok := turn.Assertions[1].Params["patterns"].([]interface{})
	if !ok {
		t.Fatalf("Expected patterns param to be []interface{}, got %T", turn.Assertions[1].Params["patterns"])
	}
	if len(patterns) != 2 {
		t.Errorf("Expected 2 patterns, got %d", len(patterns))
	}
}

func TestTurnDefinition_NoAssertions(t *testing.T) {
	yamlContent := `
role: user
content: "Simple message"
`

	var turn TurnDefinition
	err := yaml.Unmarshal([]byte(yamlContent), &turn)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Assertions should be nil or empty
	if len(turn.Assertions) > 0 {
		t.Errorf("Expected no assertions, got %d", len(turn.Assertions))
	}
}

func TestTurnDefinition_EmptyAssertionsList(t *testing.T) {
	yamlContent := `
role: user
content: "Simple message"
assertions: []
`

	var turn TurnDefinition
	err := yaml.Unmarshal([]byte(yamlContent), &turn)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Assertions should be empty list
	if len(turn.Assertions) != 0 {
		t.Errorf("Expected empty assertions list, got %d", len(turn.Assertions))
	}
}

func TestTurnDefinition_MarshalWithAssertions(t *testing.T) {
	turn := TurnDefinition{
		Role:    "user",
		Content: "Check my account",
		Assertions: []validators.ValidatorConfig{
			{
				Type: "tools_called",
				Params: map[string]interface{}{
					"tools": []string{"get_customer_info"},
				},
			},
		},
	}

	data, err := yaml.Marshal(&turn)
	if err != nil {
		t.Fatalf("Failed to marshal YAML: %v", err)
	}

	// Should contain assertions field
	yamlStr := string(data)
	if !contains(yamlStr, "assertions") {
		t.Error("Expected marshaled YAML to contain 'assertions' field")
	}
	if !contains(yamlStr, "tools_called") {
		t.Error("Expected marshaled YAML to contain 'tools_called'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
