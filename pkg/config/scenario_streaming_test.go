package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestScenario_StreamingDefault tests default streaming behavior
func TestScenario_StreamingDefault(t *testing.T) {
	yamlContent := `
id: test-scenario
task_type: support
description: "Test scenario without streaming specified"
turns:
  - role: user
    content: "First message"
  - role: user
    content: "Second message"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	// Default should be false
	if scenario.Streaming {
		t.Error("Expected default streaming to be false")
	}

	// ShouldStreamTurn should return false for all turns
	for i := range scenario.Turns {
		if scenario.ShouldStreamTurn(i) {
			t.Errorf("Turn %d: expected streaming=false, got true", i)
		}
	}
}

// TestScenario_StreamingEnabled tests scenario-level streaming
func TestScenario_StreamingEnabled(t *testing.T) {
	yamlContent := `
id: streaming-scenario
task_type: support
description: "Test scenario with streaming enabled"
streaming: true
turns:
  - role: user
    content: "First message"
  - role: user
    content: "Second message"
  - role: user
    content: "Third message"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if !scenario.Streaming {
		t.Error("Expected streaming to be true")
	}

	// All turns should use streaming
	for i := range scenario.Turns {
		if !scenario.ShouldStreamTurn(i) {
			t.Errorf("Turn %d: expected streaming=true, got false", i)
		}
	}
}

// TestScenario_StreamingPerTurnOverride tests per-turn streaming overrides
func TestScenario_StreamingPerTurnOverride(t *testing.T) {
	yamlContent := `
id: mixed-streaming
task_type: support
description: "Test scenario with mixed streaming"
streaming: true
turns:
  - role: user
    content: "First message - use default (true)"
  - role: user
    content: "Second message - disable"
    streaming: false
  - role: user
    content: "Third message - explicit enable"
    streaming: true
  - role: user
    content: "Fourth message - use default (true)"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if !scenario.Streaming {
		t.Error("Expected scenario-level streaming to be true")
	}

	// Check each turn
	expectedStreaming := []bool{true, false, true, true}
	for i, expected := range expectedStreaming {
		got := scenario.ShouldStreamTurn(i)
		if got != expected {
			t.Errorf("Turn %d: expected streaming=%v, got %v", i, expected, got)
		}
	}
}

// TestScenario_StreamingPerTurnOverrideWithDefaultFalse tests overrides when default is false
func TestScenario_StreamingPerTurnOverrideWithDefaultFalse(t *testing.T) {
	yamlContent := `
id: mostly-non-streaming
task_type: support
description: "Test scenario with streaming disabled by default"
streaming: false
turns:
  - role: user
    content: "First message - use default (false)"
  - role: user
    content: "Second message - enable streaming"
    streaming: true
  - role: user
    content: "Third message - use default (false)"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if scenario.Streaming {
		t.Error("Expected scenario-level streaming to be false")
	}

	// Check each turn
	expectedStreaming := []bool{false, true, false}
	for i, expected := range expectedStreaming {
		got := scenario.ShouldStreamTurn(i)
		if got != expected {
			t.Errorf("Turn %d: expected streaming=%v, got %v", i, expected, got)
		}
	}
}

// TestScenario_StreamingSelfPlay tests streaming with self-play turns
func TestScenario_StreamingSelfPlay(t *testing.T) {
	yamlContent := `
id: selfplay-streaming
task_type: support
description: "Test self-play with streaming"
streaming: true
turns:
  - role: user
    content: "Initial message"
  - role: claude-user
    persona: critical-expert
    turns: 3
    streaming: false
  - role: user
    content: "Follow-up message"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	// Check streaming configuration
	expectedStreaming := []bool{true, false, true}
	for i, expected := range expectedStreaming {
		got := scenario.ShouldStreamTurn(i)
		if got != expected {
			t.Errorf("Turn %d: expected streaming=%v, got %v", i, expected, got)
		}
	}
}

// TestScenario_ShouldStreamTurnInvalidIndex tests boundary conditions
func TestScenario_ShouldStreamTurnInvalidIndex(t *testing.T) {
	scenario := Scenario{
		ID:        "test",
		TaskType:  "support",
		Streaming: true,
		Turns: []TurnDefinition{
			{Role: "user", Content: "Message"},
		},
	}

	// Test negative index - should return scenario default
	if !scenario.ShouldStreamTurn(-1) {
		t.Error("Negative index should return scenario default (true)")
	}

	// Test out of bounds index - should return scenario default
	if !scenario.ShouldStreamTurn(999) {
		t.Error("Out of bounds index should return scenario default (true)")
	}

	// Test valid index
	if !scenario.ShouldStreamTurn(0) {
		t.Error("Valid index should return true")
	}
}
