package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestPopulateDefaults_TemplateEngine(t *testing.T) {
	registry := createTestRegistry()
	config := &Config{
		Spec: Spec{
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
	config := &Config{
		Spec: Spec{
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
	config := &Config{
		Spec: Spec{
			TaskType: "test",
			Validators: []ValidatorConfig{
				{
					Type: "banned_words",
					Params: map[string]interface{}{
						"words": []string{"bad", "worse"},
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

	config, err := ParseConfig(data)
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
	spec := &Spec{
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
	t.Run("sets fields with nil metadata", func(t *testing.T) {
		spec := &Spec{
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
	})

	t.Run("sets fields with existing metadata", func(t *testing.T) {
		spec := &Spec{
			TaskType: "test",
			Metadata: &Metadata{
				Domain: "old-domain",
			},
		}
		builder := NewMetadataBuilder(spec)

		builder.SetLanguage("fr")
		builder.SetTags([]string{"staging"})

		if spec.Metadata.Domain != "old-domain" {
			t.Errorf("Expected domain 'old-domain' to be preserved, got '%s'", spec.Metadata.Domain)
		}
		if spec.Metadata.Language != "fr" {
			t.Errorf("Expected language 'fr', got '%s'", spec.Metadata.Language)
		}
		if len(spec.Metadata.Tags) != 1 {
			t.Errorf("Expected 1 tag, got %d", len(spec.Metadata.Tags))
		}
	})
}

func TestMetadataBuilder_AddChangelogEntry(t *testing.T) {
	spec := &Spec{
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

func TestMetadataBuilder_BuildMetadata(t *testing.T) {
	spec := &Spec{
		TaskType: "test",
	}
	builder := NewMetadataBuilder(spec)

	t.Run("with test results", func(t *testing.T) {
		results := []TestResultSummary{
			{Success: true, Cost: 0.01, LatencyMs: 800, Tokens: 400},
			{Success: true, Cost: 0.02, LatencyMs: 1000, Tokens: 500},
			{Success: false, Cost: 0.015, LatencyMs: 900, Tokens: 450},
			{Success: true, Cost: 0.03, LatencyMs: 1200, Tokens: 600},
		}

		metadata := builder.BuildMetadata("customer-support", "en", []string{"production", "v1"}, results)

		if metadata.Domain != "customer-support" {
			t.Errorf("Expected domain 'customer-support', got '%s'", metadata.Domain)
		}
		if metadata.Language != "en" {
			t.Errorf("Expected language 'en', got '%s'", metadata.Language)
		}
		if len(metadata.Tags) != 2 {
			t.Errorf("Expected 2 tags, got %d", len(metadata.Tags))
		}

		if metadata.CostEstimate == nil {
			t.Fatal("Expected cost estimate")
		}
		if metadata.CostEstimate.MinCostUSD != 0.01 {
			t.Errorf("Expected min cost 0.01, got %.2f", metadata.CostEstimate.MinCostUSD)
		}
		if metadata.CostEstimate.MaxCostUSD != 0.03 {
			t.Errorf("Expected max cost 0.03, got %.2f", metadata.CostEstimate.MaxCostUSD)
		}
		expectedAvg := (0.01 + 0.02 + 0.015 + 0.03) / 4.0
		if metadata.CostEstimate.AvgCostUSD != expectedAvg {
			t.Errorf("Expected avg cost %.4f, got %.4f", expectedAvg, metadata.CostEstimate.AvgCostUSD)
		}

		if metadata.Performance == nil {
			t.Fatal("Expected performance metrics")
		}
		expectedAvgLatency := (800 + 1000 + 900 + 1200) / 4
		if metadata.Performance.AvgLatencyMs != expectedAvgLatency {
			t.Errorf("Expected avg latency %d, got %d", expectedAvgLatency, metadata.Performance.AvgLatencyMs)
		}
		expectedAvgTokens := (400 + 500 + 450 + 600) / 4
		if metadata.Performance.AvgTokens != expectedAvgTokens {
			t.Errorf("Expected avg tokens %d, got %d", expectedAvgTokens, metadata.Performance.AvgTokens)
		}
		expectedSuccessRate := 3.0 / 4.0
		if metadata.Performance.SuccessRate != expectedSuccessRate {
			t.Errorf("Expected success rate %.2f, got %.2f", expectedSuccessRate, metadata.Performance.SuccessRate)
		}
		if metadata.Performance.P95LatencyMs != 1200 {
			t.Errorf("Expected P95 latency 1200, got %d", metadata.Performance.P95LatencyMs)
		}
	})

	t.Run("with empty test results", func(t *testing.T) {
		metadata := builder.BuildMetadata("support", "en", []string{"test"}, []TestResultSummary{})

		if metadata.Domain != "support" {
			t.Errorf("Expected domain 'support', got '%s'", metadata.Domain)
		}
		if metadata.CostEstimate != nil {
			t.Error("Expected nil cost estimate for empty results")
		}
		if metadata.Performance != nil {
			t.Error("Expected nil performance metrics for empty results")
		}
	})
}

func TestCalculateP95Latency(t *testing.T) {
	t.Run("with multiple values", func(t *testing.T) {
		results := []TestResultSummary{
			{LatencyMs: 100},
			{LatencyMs: 200},
			{LatencyMs: 300},
			{LatencyMs: 400},
			{LatencyMs: 500},
			{LatencyMs: 600},
			{LatencyMs: 700},
			{LatencyMs: 800},
			{LatencyMs: 900},
			{LatencyMs: 1000},
		}
		p95 := calculateP95Latency(results)
		// For 10 values, P95 should be at index 9 (95% of 10 = 9.5, rounded to 9)
		if p95 != 1000 {
			t.Errorf("Expected P95 latency 1000, got %d", p95)
		}
	})

	t.Run("with single value", func(t *testing.T) {
		results := []TestResultSummary{{LatencyMs: 500}}
		p95 := calculateP95Latency(results)
		if p95 != 500 {
			t.Errorf("Expected P95 latency 500, got %d", p95)
		}
	})

	t.Run("with empty results", func(t *testing.T) {
		p95 := calculateP95Latency([]TestResultSummary{})
		if p95 != 0 {
			t.Errorf("Expected P95 latency 0 for empty results, got %d", p95)
		}
	})
}

func TestMetadataBuilder_ValidateMetadata(t *testing.T) {
	t.Run("with no metadata", func(t *testing.T) {
		spec := &Spec{TaskType: "test"}
		builder := NewMetadataBuilder(spec)
		warnings := builder.ValidateMetadata()
		if len(warnings) != 1 {
			t.Errorf("Expected 1 warning, got %d", len(warnings))
		}
		if warnings[0] != "no metadata defined" {
			t.Errorf("Expected 'no metadata defined' warning, got '%s'", warnings[0])
		}
	})

	t.Run("with complete metadata", func(t *testing.T) {
		spec := &Spec{
			TaskType: "test",
			Metadata: &Metadata{
				Domain:   "customer-support",
				Language: "en",
				Tags:     []string{"production"},
			},
		}
		builder := NewMetadataBuilder(spec)
		warnings := builder.ValidateMetadata()
		if len(warnings) != 0 {
			t.Errorf("Expected no warnings, got %d: %v", len(warnings), warnings)
		}
	})

	t.Run("with incomplete metadata", func(t *testing.T) {
		spec := &Spec{
			TaskType: "test",
			Metadata: &Metadata{
				Domain: "customer-support",
				// Missing language and tags
			},
		}
		builder := NewMetadataBuilder(spec)
		warnings := builder.ValidateMetadata()
		if len(warnings) != 2 {
			t.Errorf("Expected 2 warnings, got %d: %v", len(warnings), warnings)
		}
	})
}

func TestMetadataBuilder_UpdateFromCostInfo(t *testing.T) {
	t.Run("with cost info", func(t *testing.T) {
		spec := &Spec{TaskType: "test"}
		builder := NewMetadataBuilder(spec)

		costs := []types.CostInfo{
			{InputCostUSD: 0.01, OutputCostUSD: 0.02},
			{InputCostUSD: 0.015, OutputCostUSD: 0.025},
			{InputCostUSD: 0.008, OutputCostUSD: 0.012},
		}

		builder.UpdateFromCostInfo(costs)

		if spec.Metadata == nil {
			t.Fatal("Metadata should be initialized")
		}
		if spec.Metadata.CostEstimate == nil {
			t.Fatal("CostEstimate should be populated")
		}

		if spec.Metadata.CostEstimate.MinCostUSD != 0.02 {
			t.Errorf("Expected min cost 0.02, got %.3f", spec.Metadata.CostEstimate.MinCostUSD)
		}
		if spec.Metadata.CostEstimate.MaxCostUSD != 0.04 {
			t.Errorf("Expected max cost 0.04, got %.3f", spec.Metadata.CostEstimate.MaxCostUSD)
		}
	})

	t.Run("with empty costs", func(t *testing.T) {
		spec := &Spec{TaskType: "test"}
		builder := NewMetadataBuilder(spec)

		builder.UpdateFromCostInfo([]types.CostInfo{})

		if spec.Metadata == nil {
			t.Fatal("Metadata should be initialized")
		}
		if spec.Metadata.CostEstimate != nil {
			t.Error("CostEstimate should be nil for empty costs")
		}
	})

	t.Run("with existing metadata", func(t *testing.T) {
		spec := &Spec{
			TaskType: "test",
			Metadata: &Metadata{
				Domain: "existing-domain",
			},
		}
		builder := NewMetadataBuilder(spec)

		costs := []types.CostInfo{
			{InputCostUSD: 0.01, OutputCostUSD: 0.01},
		}

		builder.UpdateFromCostInfo(costs)

		if spec.Metadata.Domain != "existing-domain" {
			t.Error("Existing metadata fields should be preserved")
		}
		if spec.Metadata.CostEstimate == nil {
			t.Fatal("CostEstimate should be populated")
		}
	})
}
