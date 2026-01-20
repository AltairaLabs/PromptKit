package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonaStyle_Structure(t *testing.T) {
	style := PersonaStyle{
		Verbosity:      "medium",
		ChallengeLevel: "high",
		FrictionTags:   []string{"skeptical", "detail-oriented"},
	}

	if style.Verbosity != "medium" {
		t.Error("Verbosity mismatch")
	}

	if style.ChallengeLevel != "high" {
		t.Error("ChallengeLevel mismatch")
	}

	if len(style.FrictionTags) != 2 {
		t.Error("FrictionTags count mismatch")
	}
}

func TestPersonaDefaults_Structure(t *testing.T) {
	temp := float32(0.8)
	seed := 123
	defaults := PersonaDefaults{
		Temperature: &temp,
		Seed:        &seed,
	}

	if defaults.Temperature == nil || *defaults.Temperature != 0.8 {
		t.Error("Temperature mismatch")
	}

	if defaults.Seed == nil || *defaults.Seed != 123 {
		t.Error("Seed mismatch")
	}
}

func TestValidatePersonaStyle_ValidVerbosity(t *testing.T) {
	validVerbosities := []string{"short", "medium", "long"}

	for _, verbosity := range validVerbosities {
		style := PersonaStyle{Verbosity: verbosity}
		err := validatePersonaStyle(style)
		if err != nil {
			t.Errorf("Expected no error for valid verbosity '%s', got: %v", verbosity, err)
		}
	}
}

func TestValidatePersonaStyle_InvalidVerbosity(t *testing.T) {
	style := PersonaStyle{Verbosity: "invalid"}
	err := validatePersonaStyle(style)

	if err == nil {
		t.Error("Expected error for invalid verbosity")
	}
}

func TestValidatePersonaStyle_ValidChallengeLevel(t *testing.T) {
	validLevels := []string{"low", "medium", "high"}

	for _, level := range validLevels {
		style := PersonaStyle{ChallengeLevel: level}
		err := validatePersonaStyle(style)
		if err != nil {
			t.Errorf("Expected no error for valid challenge_level '%s', got: %v", level, err)
		}
	}
}

func TestValidatePersonaStyle_InvalidChallengeLevel(t *testing.T) {
	style := PersonaStyle{ChallengeLevel: "extreme"}
	err := validatePersonaStyle(style)

	if err == nil {
		t.Error("Expected error for invalid challenge_level")
	}
}

func TestValidatePersonaStyle_TooManyFrictionTags(t *testing.T) {
	tags := make([]string, 11) // More than max of 10
	for i := range tags {
		tags[i] = "tag"
	}

	style := PersonaStyle{FrictionTags: tags}
	err := validatePersonaStyle(style)

	if err == nil {
		t.Error("Expected error for too many friction tags")
	}
}

func TestValidatePersonaStyle_ExactlyTenFrictionTags(t *testing.T) {
	tags := make([]string, 10) // Exactly max of 10
	for i := range tags {
		tags[i] = "tag"
	}

	style := PersonaStyle{FrictionTags: tags}
	err := validatePersonaStyle(style)

	if err != nil {
		t.Errorf("Expected no error for 10 friction tags, got: %v", err)
	}
}

func TestValidatePersonaStyle_EmptyValues(t *testing.T) {
	style := PersonaStyle{
		Verbosity:      "",
		ChallengeLevel: "",
		FrictionTags:   []string{},
	}

	err := validatePersonaStyle(style)
	if err != nil {
		t.Errorf("Expected no error for empty values, got: %v", err)
	}
}

func TestLoadPersona_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  description: Test persona
  system_prompt: "You are a helpful assistant"
  goals:
    - Be helpful
    - Be accurate
  constraints:
    - Be respectful
  style:
    verbosity: medium
    challenge_level: high
    friction_tags:
      - skeptical
  defaults:
    temperature: 0.7
    seed: 42
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona(personaFile)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if persona.ID != "test-persona" {
		t.Error("ID mismatch")
	}

	if len(persona.Goals) != 2 {
		t.Error("Goals count mismatch")
	}

	if persona.Style.Verbosity != "medium" {
		t.Error("Verbosity mismatch")
	}

	if persona.Defaults.Temperature == nil || *persona.Defaults.Temperature != 0.7 {
		t.Error("Temperature mismatch")
	}
}

func TestLoadPersona_WithPromptActivity(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  description: Test persona
  prompt_activity: user_persona
  goals:
    - Be helpful
  constraints:
    - Be respectful
  style:
    verbosity: short
    challenge_level: low
  defaults:
    temperature: 0.8
    seed: 100
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona(personaFile)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if persona.PromptActivity != "user_persona" {
		t.Error("PromptActivity mismatch")
	}

	if persona.SystemPrompt != "" {
		t.Error("SystemPrompt should be empty when using prompt_activity")
	}
}

func TestLoadPersona_MissingID(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `description: Test persona
system_prompt: "Test"
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPersona(personaFile)

	if err == nil {
		t.Error("Expected error for missing ID")
	}
}

func TestLoadPersona_MissingPromptAndActivity(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  description: Test persona
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPersona(personaFile)

	if err == nil {
		t.Error("Expected error for missing both prompt_activity and system_prompt")
	}
}

func TestLoadPersona_DefaultValues(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  system_prompt: "Test"
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona(personaFile)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Check defaults are applied
	if persona.Defaults.Temperature == nil {
		t.Error("Expected default temperature to be set")
	} else if *persona.Defaults.Temperature != 0.7 {
		t.Errorf("Expected default temperature 0.7, got %f", *persona.Defaults.Temperature)
	}

	if persona.Defaults.Seed == nil {
		t.Error("Expected default seed to be set")
	} else if *persona.Defaults.Seed != 42 {
		t.Errorf("Expected default seed 42, got %d", *persona.Defaults.Seed)
	}
}

// TestLoadPersona_ExplicitZeroTemperature verifies that explicit temperature=0
// is preserved and not overwritten by the default (fixes the bug where
// temperature=0 was silently changed to 0.7)
func TestLoadPersona_ExplicitZeroTemperature(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: deterministic-persona
spec:
  description: Persona with deterministic output
  system_prompt: Be deterministic
  goals:
    - Be predictable
  constraints:
    - No randomness
  style:
    verbosity: short
    challenge_level: low
  defaults:
    temperature: 0
    seed: 0
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona(personaFile)
	if err != nil {
		t.Fatalf("Failed to load persona: %v", err)
	}

	// Verify explicit temperature=0 is preserved (not overwritten to 0.7)
	if persona.Defaults.Temperature == nil {
		t.Fatal("Expected temperature to be set")
	}
	if *persona.Defaults.Temperature != 0 {
		t.Errorf("Expected temperature 0 to be preserved, got %f", *persona.Defaults.Temperature)
	}

	// Verify explicit seed=0 is preserved (not overwritten to 42)
	if persona.Defaults.Seed == nil {
		t.Fatal("Expected seed to be set")
	}
	if *persona.Defaults.Seed != 0 {
		t.Errorf("Expected seed 0 to be preserved, got %d", *persona.Defaults.Seed)
	}
}

func TestLoadPersona_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	invalidContent := `invalid: yaml: content: here:`

	if err := os.WriteFile(personaFile, []byte(invalidContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPersona(personaFile)

	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLoadPersona_NonexistentFile(t *testing.T) {
	_, err := LoadPersona("/nonexistent/file.yaml")

	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadPersona_JSONFormat(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.json")

	jsonContent := `{
  "id": "test-persona",
  "system_prompt": "Test",
  "goals": ["goal1"],
  "style": {
    "verbosity": "short"
  }
}`

	if err := os.WriteFile(personaFile, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona(personaFile)

	if err != nil {
		t.Fatalf("Expected no error for JSON format, got: %v", err)
	}

	if persona.ID != "test-persona" {
		t.Error("ID mismatch")
	}
}

func TestLoadPersona_InvalidStyleValidation(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  system_prompt: "Test"
  style:
    verbosity: invalid_verbosity
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPersona(personaFile)

	if err == nil {
		t.Error("Expected error for invalid style validation")
	}
}

func TestUserPersonaPack_BuildSystemPrompt_Legacy(t *testing.T) {
	persona := &UserPersonaPack{
		ID:           "test",
		SystemPrompt: "Legacy prompt",
	}

	prompt, err := persona.BuildSystemPrompt("us", nil)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if prompt != "Legacy prompt" {
		t.Error("Expected legacy system prompt")
	}
}

func TestUserPersonaPack_BuildSystemPrompt_NoPromptOrActivity(t *testing.T) {
	persona := &UserPersonaPack{
		ID: "test",
		// No SystemPrompt or PromptActivity
	}

	_, err := persona.BuildSystemPrompt("us", nil)

	if err == nil {
		t.Error("Expected error when no prompt_activity or system_prompt specified")
	}
}

func TestPersonaStyle_AllFields(t *testing.T) {
	style := PersonaStyle{
		Verbosity:      "long",
		ChallengeLevel: "low",
		FrictionTags:   []string{"patient", "thorough", "analytical"},
	}

	if style.Verbosity != "long" {
		t.Error("Verbosity mismatch")
	}

	if style.ChallengeLevel != "low" {
		t.Error("ChallengeLevel mismatch")
	}

	if len(style.FrictionTags) != 3 {
		t.Errorf("Expected 3 friction tags, got %d", len(style.FrictionTags))
	}
}

func TestLoadPersona_MultipleGoalsAndConstraints(t *testing.T) {
	tmpDir := t.TempDir()
	personaFile := filepath.Join(tmpDir, "persona.yaml")

	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  system_prompt: "Test"
  goals:
    - Goal 1
    - Goal 2
    - Goal 3
  constraints:
    - Constraint 1
    - Constraint 2
  style:
    verbosity: medium
`

	if err := os.WriteFile(personaFile, []byte(personaContent), 0644); err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona(personaFile)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(persona.Goals) != 3 {
		t.Errorf("Expected 3 goals, got %d", len(persona.Goals))
	}

	if len(persona.Constraints) != 2 {
		t.Errorf("Expected 2 constraints, got %d", len(persona.Constraints))
	}
}

func TestUserPersonaPack_BuildTemplatedPrompt(t *testing.T) {
	persona := &UserPersonaPack{
		ID:             "test-persona",
		SystemTemplate: "Hello {{name}}, you are {{verbosity}} verbose. Goals: {{persona_goals}}",
		Goals:          []string{"Be helpful", "Be accurate"},
		Style: PersonaStyle{
			Verbosity: "very",
		},
		RequiredVars: []string{"name"},
		OptionalVars: map[string]string{
			"optional": "default",
		},
	}

	// Test successful rendering
	contextVars := map[string]string{
		"name":     "Alice",
		"optional": "overridden",
	}

	result, err := persona.buildTemplatedPrompt("us", contextVars)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expected := "Hello Alice, you are very verbose. Goals: Be helpful, Be accurate"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestUserPersonaPack_BuildTemplatedPrompt_MissingRequiredVar(t *testing.T) {
	persona := &UserPersonaPack{
		ID:             "test-persona",
		SystemTemplate: "Hello {{name}}",
		RequiredVars:   []string{"name"},
	}

	_, err := persona.buildTemplatedPrompt("us", nil)
	if err == nil {
		t.Error("Expected error for missing required variable")
	}
}

func TestUserPersonaPack_BuildTemplatedPrompt_WithFragments(t *testing.T) {
	persona := &UserPersonaPack{
		ID:             "test-persona",
		SystemTemplate: "Template text",
		Fragments: []FragmentRef{
			{Name: "frag1", Path: "path1", Required: true},
		},
	}

	_, err := persona.buildTemplatedPrompt("us", nil)
	if err == nil {
		t.Error("Expected error for fragments (not implemented)")
	}

	if !strings.Contains(err.Error(), "fragment loading for personas not yet fully implemented") {
		t.Errorf("Expected fragment error message, got: %v", err)
	}
}

func TestConvertFragmentRefs(t *testing.T) {
	configRefs := []FragmentRef{
		{Name: "frag1", Path: "path1", Required: true},
		{Name: "frag2", Path: "path2", Required: false},
	}

	promptRefs := convertFragmentRefs(configRefs)

	if len(promptRefs) != 2 {
		t.Errorf("Expected 2 refs, got %d", len(promptRefs))
	}

	if promptRefs[0].Name != "frag1" || promptRefs[0].Path != "path1" || !promptRefs[0].Required {
		t.Error("First ref conversion incorrect")
	}

	if promptRefs[1].Name != "frag2" || promptRefs[1].Path != "path2" || promptRefs[1].Required {
		t.Error("Second ref conversion incorrect")
	}
}
