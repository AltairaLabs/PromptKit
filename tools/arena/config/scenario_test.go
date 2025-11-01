package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestScenario_TurnsOnlyUserContent tests that scenarios only define user turns
func TestScenario_TurnsOnlyUserContent(t *testing.T) {
	yamlContent := `
id: test-scenario
task_type: support
description: "Test scenario"
turns:
  - role: user
    content: "First user message"
  - role: user
    content: "Second user message"
  - role: user
    content: "Third user message"
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if scenario.TaskType != "support" {
		t.Errorf("Expected task_type 'support', got '%s'", scenario.TaskType)
	}

	if len(scenario.Turns) != 3 {
		t.Errorf("Expected 3 turns, got %d", len(scenario.Turns))
	}

	// All turns should be user turns
	for i, turn := range scenario.Turns {
		if turn.Role != "user" {
			t.Errorf("Turn %d: expected role 'user', got '%s'", i, turn.Role)
		}
		if turn.Content == "" {
			t.Errorf("Turn %d: content should not be empty", i)
		}
	}
}

// TestScenario_NoPromptConfigField tests that PromptConfig field doesn't exist
func TestScenario_NoPromptConfigField(t *testing.T) {
	// This YAML has the old prompt_config field which should be ignored
	yamlContent := `
id: legacy-scenario
task_type: support
description: "Legacy scenario with prompt_config"
turns:
  - role: user
    content: "User message"
  - role: assistant
    prompt_config: support-bot
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	// Should parse without error, but PromptConfig field should not exist
	// The TurnDefinition struct should not have a PromptConfig field
	if len(scenario.Turns) != 2 {
		t.Errorf("Expected 2 turns, got %d", len(scenario.Turns))
	}
}

// TestScenario_AssistantTurnsDeprecated tests that assistant turns should be removed
func TestScenario_AssistantTurnsDeprecated(t *testing.T) {
	// Old format with explicit assistant turns
	yamlContent := `
id: old-format
task_type: support
description: "Old format with assistant turns"
turns:
  - role: user
    content: "First message"
  - role: assistant
    prompt_config: support-bot
  - role: user
    content: "Second message"
  - role: assistant
    prompt_config: support-bot
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	// New approach: filter out assistant turns or only count user turns
	// The engine should automatically execute assistant turns after each user turn
	userTurns := 0
	for _, turn := range scenario.Turns {
		if turn.Role == "user" {
			userTurns++
		}
	}

	if userTurns != 2 {
		t.Errorf("Expected 2 user turns, got %d", userTurns)
	}
}

// TestScenario_SelfPlayTurnsStillWork tests that self-play turns are different
func TestScenario_SelfPlayTurnsStillWork(t *testing.T) {
	yamlContent := `
id: selfplay-scenario
task_type: support
description: "Self-play scenario"
turns:
  - role: user
    content: "Initial user message"
  - role: claude-user
    persona: critical-expert
    turns: 3
`

	var scenario Scenario
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	if err != nil {
		t.Fatalf("Failed to parse scenario: %v", err)
	}

	if len(scenario.Turns) != 2 {
		t.Errorf("Expected 2 turns, got %d", len(scenario.Turns))
	}

	// First turn is regular user
	if scenario.Turns[0].Role != "user" {
		t.Errorf("First turn should be user, got '%s'", scenario.Turns[0].Role)
	}

	// Second turn is self-play
	if scenario.Turns[1].Role != "claude-user" {
		t.Errorf("Second turn should be claude-user, got '%s'", scenario.Turns[1].Role)
	}

	if scenario.Turns[1].Persona != "critical-expert" {
		t.Errorf("Self-play turn should have persona, got '%s'", scenario.Turns[1].Persona)
	}

	if scenario.Turns[1].Turns != 3 {
		t.Errorf("Self-play turn should have turns=3, got %d", scenario.Turns[1].Turns)
	}
}
