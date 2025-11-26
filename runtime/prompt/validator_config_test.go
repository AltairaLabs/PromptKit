package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidatorConfig_Loading(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a prompt config with validators
	yamlContent := `apiVersion: prompt.promptkit.altairalabs.ai/v1
kind: PromptConfig
metadata:
  name: test-validators
  task_type: support
spec:
  system_prompt: |
    You are a helpful assistant.
  validators:
    - type: banned_words
      params:
        words: ["guarantee", "promise"]
    - type: max_length
      params:
        max_characters: 1000
        max_tokens: 250
    - type: max_sentences
      params:
        max_sentences: 8
`

	// Write the YAML file
	promptPath := filepath.Join(tmpDir, "test.yaml")
	err := os.WriteFile(promptPath, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test prompt file: %v", err)
	}

	// Load the prompt config
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("Failed to read prompt file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to unmarshal prompt config: %v", err)
	}

	// Verify validators were loaded
	if len(config.Spec.Validators) != 3 {
		t.Errorf("Expected 3 validators, got %d", len(config.Spec.Validators))
	}

	// Verify first validator (banned_words)
	if config.Spec.Validators[0].Type != "banned_words" {
		t.Errorf("Expected first validator type 'banned_words', got '%s'", config.Spec.Validators[0].Type)
	}
	if words, ok := config.Spec.Validators[0].Params["words"]; !ok {
		t.Errorf("Expected 'words' param in banned_words validator")
	} else {
		wordList := words.([]interface{})
		if len(wordList) != 2 {
			t.Errorf("Expected 2 banned words, got %d", len(wordList))
		}
	}

	// Verify second validator (max_length)
	if config.Spec.Validators[1].Type != "max_length" {
		t.Errorf("Expected second validator type 'max_length', got '%s'", config.Spec.Validators[1].Type)
	}
	if _, ok := config.Spec.Validators[1].Params["max_characters"]; !ok {
		t.Errorf("Expected 'max_characters' param in max_length validator")
	}
	if _, ok := config.Spec.Validators[1].Params["max_tokens"]; !ok {
		t.Errorf("Expected 'max_tokens' param in max_length validator")
	}

	// Verify third validator (max_sentences)
	if config.Spec.Validators[2].Type != "max_sentences" {
		t.Errorf("Expected third validator type 'max_sentences', got '%s'", config.Spec.Validators[2].Type)
	}
	if _, ok := config.Spec.Validators[2].Params["max_sentences"]; !ok {
		t.Errorf("Expected 'max_sentences' param in max_sentences validator")
	}
}

func TestValidatorConfig_InAssembledPrompt(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create prompts directory
	promptsDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("Failed to create prompts directory: %v", err)
	}

	// Create a prompt config with validators
	yamlContent := `apiVersion: prompt.promptkit.altairalabs.ai/v1
kind: PromptConfig
metadata:
  name: test-validators
spec:
  task_type: support
  system_template: |
    You are a helpful assistant.
  validators:
    - type: banned_words
      params:
        words: ["guarantee", "promise"]
`

	// Write the YAML file to prompts/support.yaml (task_type based naming)
	promptPath := filepath.Join(promptsDir, "support.yaml")
	err := os.WriteFile(promptPath, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test prompt file: %v", err)
	}

	// Create registry and load/register config
	registry := createTestRegistry()

	// Read and parse the file
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	config, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Register the config
	if err := registry.RegisterConfig("support", config); err != nil {
		t.Fatalf("Failed to register config: %v", err)
	}

	// Verify validators are in config directly
	if len(config.Spec.Validators) != 1 {
		t.Errorf("Expected 1 validator in config, got %d", len(config.Spec.Validators))
	}

	if len(config.Spec.Validators) > 0 && config.Spec.Validators[0].Type != "banned_words" {
		t.Errorf("Expected validator type 'banned_words', got '%s'", config.Spec.Validators[0].Type)
	}
}
