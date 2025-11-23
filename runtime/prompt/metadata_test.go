package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

func TestPopulateDefaults_TemplateEngine(t *testing.T) {
	registry := createTestRegistry()
	config := &PromptConfig{
		Spec: PromptSpec{
			TaskType: "test",
		},
	}

	registry.populateDefaults(config)

	if config.Spec.TemplateEngine == nil {
		t.Fatal("TemplateEngine should be populated with defaults")
	}
	if config.Spec.TemplateEngine.Version != "v1" {
		t.Errorf("Expected template engine version v1, got %s", config.Spec.TemplateEngine.Version)
	}
	if config.Spec.TemplateEngine.Syntax != "{{variable}}" {
		t.Errorf("Expected syntax {{variable}}, got %s", config.Spec.TemplateEngine.Syntax)
	}
}

func TestPopulateDefaults_Variables(t *testing.T) {
	registry := createTestRegistry()
	config := &PromptConfig{
		Spec: PromptSpec{
			TaskType: "test",
			Variables: []VariableMetadata{
				{Name: "name", Required: true, Type: "string"},
				{Name: "email", Required: true, Type: "string"},
				{Name: "greeting", Required: false, Type: "string", Default: "Hello"},
				{Name: "title", Required: false, Type: "string", Default: "Mr."},
			},
		},
	}

	registry.populateDefaults(config)

	// populateDefaults should not modify Variables anymore
	if len(config.Spec.Variables) != 4 {
		t.Fatalf("Expected 4 variables, got %d", len(config.Spec.Variables))
	}

	// Verify variables are unchanged
	for _, v := range config.Spec.Variables {
		if v.Name == "name" && !v.Required {
			t.Error("Variable 'name' should be required")
		}
		if v.Name == "email" && !v.Required {
			t.Error("Variable 'email' should be required")
		}
		if v.Name == "greeting" {
			if v.Required {
				t.Error("Variable 'greeting' should be optional")
			}
			if v.Default != "Hello" {
				t.Errorf("Expected default 'Hello', got '%v'", v.Default)
			}
		}
	}
}

func TestPopulateDefaults_ValidatorFlags(t *testing.T) {
	registry := createTestRegistry()
	config := &PromptConfig{
		Spec: PromptSpec{
			TaskType: "test",
			Validators: []ValidatorConfig{
				{
					ValidatorConfig: validators.ValidatorConfig{
						Type: "banned_words",
						Params: map[string]interface{}{
							"words": []string{"bad", "worse"},
						},
					},
				},
			},
		},
	}

	registry.populateDefaults(config)

	if config.Spec.Validators[0].Enabled == nil {
		t.Fatal("Validator Enabled flag should be set")
	}
	if !*config.Spec.Validators[0].Enabled {
		t.Error("Validator should be enabled by default")
	}

	if config.Spec.Validators[0].FailOnViolation == nil {
		t.Fatal("Validator FailOnViolation flag should be set")
	}
	if !*config.Spec.Validators[0].FailOnViolation {
		t.Error("Validator should fail on violation by default")
	}
}

func TestPopulateDefaults_Integration(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "prompt-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create prompts subdirectory
	promptsDir := filepath.Join(tmpDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test YAML file
	yamlContent := `apiVersion: promptkit.altaira.ai/v1
kind: PromptConfig
metadata:
  name: test-prompt
spec:
  task_type: customer-support
  version: "1.0.0"
  description: Test prompt
  system_template: "You are a {{role}}. Help {{customer_name}}."
  variables:
    - name: role
      type: string
      required: true
    - name: customer_name
      type: string
      required: true
    - name: greeting
      type: string
      required: false
      default: "Hello"
  validators:
    - type: banned_words
      params:
        words: ["test"]
`

	testFile := filepath.Join(promptsDir, "customer-support.yaml")
	if err := os.WriteFile(testFile, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create registry - we'll manually register the config instead of using file-based loading
	registry := createTestRegistry()

	// Read and parse the file
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	config, err := ParsePromptConfig(data)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Populate defaults
	registry.populateDefaults(config)

	// Register the config
	if err := registry.RegisterConfig("customer-support", config); err != nil {
		t.Fatalf("Failed to register config: %v", err)
	}

	// Verify defaults were populated directly from config
	if config.Spec.TemplateEngine == nil {
		t.Error("TemplateEngine should be populated")
	}

	if len(config.Spec.Variables) != 3 {
		t.Errorf("Expected 3 variables, got %d", len(config.Spec.Variables))
	}

	if config.Spec.Validators[0].Enabled == nil || !*config.Spec.Validators[0].Enabled {
		t.Error("Validator should be enabled by default")
	}
}

func TestMetadataBuilder_BuildCompilationInfo(t *testing.T) {
	spec := &PromptSpec{
		TaskType: "test",
	}
	builder := NewMetadataBuilder(spec)

	compInfo := builder.BuildCompilationInfo("packc-v0.1.0")

	if compInfo.CompiledWith != "packc-v0.1.0" {
		t.Errorf("Expected compiler version 'packc-v0.1.0', got '%s'", compInfo.CompiledWith)
	}
	if compInfo.Schema != "v1" {
		t.Errorf("Expected schema 'v1', got '%s'", compInfo.Schema)
	}
	if compInfo.CreatedAt == "" {
		t.Error("CreatedAt should be populated")
	}
}

func TestAggregateTestResults(t *testing.T) {
	results := []TestResultSummary{
		{Success: true, Cost: 0.02, LatencyMs: 1000, Tokens: 500},
		{Success: true, Cost: 0.03, LatencyMs: 1200, Tokens: 600},
		{Success: false, Cost: 0.01, LatencyMs: 800, Tokens: 300},
	}

	testResult := AggregateTestResults(results, "openai", "gpt-4")

	if testResult == nil {
		t.Fatal("Expected non-nil result")
	}

	if testResult.Provider != "openai" {
		t.Errorf("Expected provider 'openai', got '%s'", testResult.Provider)
	}
	if testResult.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", testResult.Model)
	}

	expectedSuccessRate := 2.0 / 3.0
	if testResult.SuccessRate != expectedSuccessRate {
		t.Errorf("Expected success rate %.2f, got %.2f", expectedSuccessRate, testResult.SuccessRate)
	}

	expectedAvgTokens := (500 + 600 + 300) / 3
	if testResult.AvgTokens != expectedAvgTokens {
		t.Errorf("Expected avg tokens %d, got %d", expectedAvgTokens, testResult.AvgTokens)
	}

	expectedAvgLatency := (1000 + 1200 + 800) / 3
	if testResult.AvgLatencyMs != expectedAvgLatency {
		t.Errorf("Expected avg latency %d, got %d", expectedAvgLatency, testResult.AvgLatencyMs)
	}
}

func TestExtractVariablesFromTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		expected []string
	}{
		{
			name:     "simple variables",
			template: "Hello {{name}}, your email is {{email}}",
			expected: []string{"name", "email"},
		},
		{
			name:     "no variables",
			template: "Hello world",
			expected: []string{},
		},
		{
			name:     "duplicate variables",
			template: "{{name}} is {{name}}",
			expected: []string{"name"},
		},
		{
			name:     "multiple line template",
			template: "Hi {{name}},\nYour role is {{role}}.\nWelcome!",
			expected: []string{"name", "role"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := ExtractVariablesFromTemplate(tt.template)
			if len(vars) != len(tt.expected) {
				t.Errorf("Expected %d variables, got %d: %v", len(tt.expected), len(vars), vars)
				return
			}
			for i, v := range vars {
				if v != tt.expected[i] {
					t.Errorf("Expected variable '%s', got '%s'", tt.expected[i], v)
				}
			}
		})
	}
}

func TestMetadataBuilder_SetMethods(t *testing.T) {
	spec := &PromptSpec{
		TaskType: "test",
	}
	builder := NewMetadataBuilder(spec)

	builder.SetDomain("customer-support")
	builder.SetLanguage("en")
	builder.SetTags([]string{"production", "v1"})

	if spec.Metadata == nil {
		t.Fatal("Metadata should be initialized")
	}
	if spec.Metadata.Domain != "customer-support" {
		t.Errorf("Expected domain 'customer-support', got '%s'", spec.Metadata.Domain)
	}
	if spec.Metadata.Language != "en" {
		t.Errorf("Expected language 'en', got '%s'", spec.Metadata.Language)
	}
	if len(spec.Metadata.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(spec.Metadata.Tags))
	}
}

func TestMetadataBuilder_AddChangelogEntry(t *testing.T) {
	spec := &PromptSpec{
		TaskType: "test",
	}
	builder := NewMetadataBuilder(spec)

	builder.AddChangelogEntry("1.0.0", "test-author", "Initial release")
	builder.AddChangelogEntry("1.1.0", "test-author", "Bug fixes")

	if spec.Metadata == nil {
		t.Fatal("Metadata should be initialized")
	}
	if len(spec.Metadata.Changelog) != 2 {
		t.Fatalf("Expected 2 changelog entries, got %d", len(spec.Metadata.Changelog))
	}

	first := spec.Metadata.Changelog[0]
	if first.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", first.Version)
	}
	if first.Author != "test-author" {
		t.Errorf("Expected author 'test-author', got '%s'", first.Author)
	}
	if first.Description != "Initial release" {
		t.Errorf("Expected description 'Initial release', got '%s'", first.Description)
	}
}

func TestGetDefaultPipelineConfig(t *testing.T) {
	config := GetDefaultPipelineConfig()

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	stages, ok := config["stages"].([]string)
	if !ok {
		t.Fatal("Expected stages to be []string")
	}
	if len(stages) != 3 {
		t.Errorf("Expected 3 stages, got %d", len(stages))
	}

	middleware, ok := config["middleware"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected middleware to be []map[string]interface{}")
	}
	if len(middleware) != 3 {
		t.Errorf("Expected 3 middleware configs, got %d", len(middleware))
	}
}
