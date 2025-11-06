package sdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPackManager_LoadPack(t *testing.T) {
	// Create temp pack file
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	// Create pack data without lock fields
	packData := map[string]interface{}{
		"id":          "test-pack",
		"name":        "Test Pack",
		"version":     "1.0.0",
		"description": "Test pack for SDK",
		"template_engine": map[string]interface{}{
			"version":  "v1",
			"syntax":   "{{variable}}",
			"features": []string{"basic_substitution"},
		},
		"prompts": map[string]interface{}{
			"support": map[string]interface{}{
				"id":              "support",
				"name":            "Support Bot",
				"description":     "Test support prompt",
				"version":         "1.0.0",
				"system_template": "You are a {{role}} assistant.",
				"variables": []map[string]interface{}{
					{
						"name":        "role",
						"type":        "string",
						"required":    true,
						"description": "The role of the assistant",
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal pack: %v", err)
	}

	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("failed to write pack file: %v", err)
	}

	// Test loading
	pm := NewPackManager()
	loadedPack, err := pm.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load pack: %v", err)
	}

	if loadedPack.ID != "test-pack" {
		t.Errorf("expected pack ID 'test-pack', got '%s'", loadedPack.ID)
	}

	if len(loadedPack.Prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(loadedPack.Prompts))
	}

	// Test caching
	cachedPack, err := pm.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load cached pack: %v", err)
	}

	if cachedPack != loadedPack {
		t.Error("expected cached pack to be same instance")
	}
}

func TestPackManager_ValidatePack(t *testing.T) {
	pm := NewPackManager()

	tests := []struct {
		name    string
		pack    *Pack
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid pack",
			pack: &Pack{
				ID:      "valid",
				Name:    "Valid Pack",
				Version: "1.0.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test",
						SystemTemplate: "Test template",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			pack: &Pack{
				Name:    "No ID Pack",
				Version: "1.0.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test",
						SystemTemplate: "Test",
					},
				},
			},
			wantErr: true,
			errMsg:  "id",
		},
		{
			name: "missing version",
			pack: &Pack{
				ID:   "test",
				Name: "No Version Pack",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test",
						SystemTemplate: "Test",
					},
				},
			},
			wantErr: true,
			errMsg:  "version",
		},
		{
			name: "no prompts",
			pack: &Pack{
				ID:      "empty",
				Name:    "Empty Pack",
				Version: "1.0.0",
				Prompts: map[string]*Prompt{},
			},
			wantErr: true,
			errMsg:  "no prompts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pm.validatePack(tt.pack)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestPack_GetPrompt(t *testing.T) {
	pack := &Pack{
		ID:      "test",
		Name:    "Test Pack",
		Version: "1.0.0",
		Prompts: map[string]*Prompt{
			"support": {
				ID:             "support",
				SystemTemplate: "Support template",
			},
			"sales": {
				ID:             "sales",
				SystemTemplate: "Sales template",
			},
		},
	}

	// Test existing prompt
	prompt, err := pack.GetPrompt("support")
	if err != nil {
		t.Fatalf("failed to get prompt: %v", err)
	}
	if prompt.ID != "support" {
		t.Errorf("expected prompt ID 'support', got '%s'", prompt.ID)
	}

	// Test non-existent prompt
	_, err = pack.GetPrompt("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent prompt")
	}
}

func TestPack_GetTools(t *testing.T) {
	pack := &Pack{
		ID:      "test",
		Name:    "Test Pack",
		Version: "1.0.0",
		Tools: map[string]*Tool{
			"search": {
				Name:        "search",
				Description: "Search for information",
			},
			"calculate": {
				Name:        "calculate",
				Description: "Perform calculations",
			},
		},
		Prompts: map[string]*Prompt{
			"support": {
				ID:             "support",
				SystemTemplate: "Support template",
				ToolNames:      []string{"search"},
			},
			"math": {
				ID:             "math",
				SystemTemplate: "Math template",
				ToolNames:      []string{"calculate", "search"},
			},
		},
	}

	// Test support prompt tools
	tools, err := pack.GetTools("support")
	if err != nil {
		t.Fatalf("failed to get tools: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "search" {
		t.Errorf("expected tool 'search', got '%s'", tools[0].Name)
	}

	// Test math prompt tools
	tools, err = pack.GetTools("math")
	if err != nil {
		t.Fatalf("failed to get tools: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestPack_CreateRegistry(t *testing.T) {
	pack := &Pack{
		ID:      "test",
		Name:    "Test Pack",
		Version: "1.0.0",
		TemplateEngine: TemplateEngine{
			Version:  "v1",
			Syntax:   "{{variable}}",
			Features: []string{"basic_substitution"},
		},
		Prompts: map[string]*Prompt{
			"support": {
				ID:             "support",
				Name:           "Support Bot",
				SystemTemplate: "You are a {{role}} assistant for {{company}}.",
				Variables: []*Variable{
					{
						Name:        "role",
						Type:        "string",
						Required:    true,
						Description: "The role of the assistant",
					},
					{
						Name:        "company",
						Type:        "string",
						Required:    false,
						Default:     "TechCo",
						Description: "Company name",
					},
				},
				ToolNames: []string{"search", "email"},
				Validators: []*Validator{
					{
						Type:    "length",
						Enabled: true,
						Params: map[string]interface{}{
							"min": 10,
							"max": 1000,
						},
					},
				},
			},
			"sales": {
				ID:             "sales",
				Name:           "Sales Bot",
				SystemTemplate: "You are a sales agent for {{product}}.",
				Variables: []*Variable{
					{
						Name:     "product",
						Type:     "string",
						Required: true,
					},
				},
			},
		},
	}

	// Create registry from pack
	registry, err := pack.CreateRegistry()
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Verify registry has correct task types
	taskTypes := registry.GetAvailableTaskTypes()
	if len(taskTypes) != 2 {
		t.Errorf("expected 2 task types, got %d", len(taskTypes))
	}

	// Check if both prompts are registered
	hasSales := false
	hasSupport := false
	for _, tt := range taskTypes {
		if tt == "sales" {
			hasSales = true
		}
		if tt == "support" {
			hasSupport = true
		}
	}
	if !hasSales {
		t.Error("expected 'sales' task type in registry")
	}
	if !hasSupport {
		t.Error("expected 'support' task type in registry")
	}

	// Test loading config from registry
	config, err := registry.LoadConfig("support")
	if err != nil {
		t.Fatalf("failed to load config from registry: %v", err)
	}

	// Verify basic config properties
	if config.Spec.TaskType != "support" {
		t.Errorf("expected task type 'support', got '%s'", config.Spec.TaskType)
	}
	if config.Spec.SystemTemplate != "You are a {{role}} assistant for {{company}}." {
		t.Errorf("system template mismatch: %s", config.Spec.SystemTemplate)
	}

	// Verify required vars
	if len(config.Spec.RequiredVars) != 1 {
		t.Errorf("expected 1 required var, got %d", len(config.Spec.RequiredVars))
	}
	if config.Spec.RequiredVars[0] != "role" {
		t.Errorf("expected required var 'role', got '%s'", config.Spec.RequiredVars[0])
	}

	// Verify optional vars
	if len(config.Spec.OptionalVars) != 1 {
		t.Errorf("expected 1 optional var, got %d", len(config.Spec.OptionalVars))
	}
	if _, ok := config.Spec.OptionalVars["company"]; !ok {
		t.Error("expected 'company' in optional vars")
	}
	if config.Spec.OptionalVars["company"] != "TechCo" {
		t.Errorf("expected default 'TechCo', got '%v'", config.Spec.OptionalVars["company"])
	}

	// Verify variable metadata
	if len(config.Spec.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(config.Spec.Variables))
	}

	// Verify validators
	if len(config.Spec.Validators) != 1 {
		t.Errorf("expected 1 validator, got %d", len(config.Spec.Validators))
	}
	if config.Spec.Validators[0].Type != "length" {
		t.Errorf("expected validator type 'length', got '%s'", config.Spec.Validators[0].Type)
	}
	if config.Spec.Validators[0].Enabled == nil || !*config.Spec.Validators[0].Enabled {
		t.Error("expected validator to be enabled")
	}

	// Verify allowed tools
	if len(config.Spec.AllowedTools) != 2 {
		t.Errorf("expected 2 allowed tools, got %d", len(config.Spec.AllowedTools))
	}

	// Test with prompt without variables (sales)
	salesConfig, err := registry.LoadConfig("sales")
	if err != nil {
		t.Fatalf("failed to load sales config: %v", err)
	}
	if salesConfig.Spec.TaskType != "sales" {
		t.Errorf("expected task type 'sales', got '%s'", salesConfig.Spec.TaskType)
	}
	if len(salesConfig.Spec.RequiredVars) != 1 {
		t.Errorf("expected 1 required var for sales, got %d", len(salesConfig.Spec.RequiredVars))
	}
}

func TestPackManager_GetPack(t *testing.T) {
	pm := NewPackManager()

	// Test pack doesn't exist
	pack, exists := pm.GetPack("/nonexistent/path.pack.json")
	if exists {
		t.Error("expected pack to not exist")
	}
	if pack != nil {
		t.Error("expected nil pack for nonexistent path")
	}

	// Load a pack
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packData := map[string]interface{}{
		"id":      "test-pack",
		"name":    "Test Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
			"syntax":  "{{variable}}",
		},
		"prompts": map[string]interface{}{
			"support": map[string]interface{}{
				"id":              "support",
				"name":            "Support Bot",
				"version":         "1.0.0",
				"system_template": "You are a support assistant.",
			},
		},
	}

	data, _ := json.MarshalIndent(packData, "", "  ")
	os.WriteFile(packPath, data, 0644)

	loadedPack, err := pm.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load pack: %v", err)
	}

	// Test pack exists
	pack, exists = pm.GetPack(packPath)
	if !exists {
		t.Error("expected pack to exist after loading")
	}
	if pack == nil {
		t.Error("expected non-nil pack")
	}
	if pack.ID != loadedPack.ID {
		t.Errorf("expected pack ID %s, got %s", loadedPack.ID, pack.ID)
	}
}

func TestPack_ListPrompts(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packData := map[string]interface{}{
		"id":      "test-pack",
		"name":    "Test Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
			"syntax":  "{{variable}}",
		},
		"prompts": map[string]interface{}{
			"support": map[string]interface{}{
				"id":              "support",
				"name":            "Support Bot",
				"version":         "1.0.0",
				"system_template": "You are a support assistant.",
			},
			"sales": map[string]interface{}{
				"id":              "sales",
				"name":            "Sales Bot",
				"version":         "1.0.0",
				"system_template": "You are a sales assistant.",
			},
			"analytics": map[string]interface{}{
				"id":              "analytics",
				"name":            "Analytics Bot",
				"version":         "1.0.0",
				"system_template": "You are an analytics assistant.",
			},
		},
	}

	data, _ := json.MarshalIndent(packData, "", "  ")
	os.WriteFile(packPath, data, 0644)

	pm := NewPackManager()
	pack, err := pm.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load pack: %v", err)
	}

	// Test ListPrompts
	prompts := pack.ListPrompts()
	if len(prompts) != 3 {
		t.Errorf("expected 3 prompts, got %d", len(prompts))
	}

	// Verify all expected prompts are in the list
	expectedPrompts := map[string]bool{
		"support":   false,
		"sales":     false,
		"analytics": false,
	}
	for _, promptName := range prompts {
		if _, ok := expectedPrompts[promptName]; ok {
			expectedPrompts[promptName] = true
		}
	}
	for name, found := range expectedPrompts {
		if !found {
			t.Errorf("expected prompt '%s' to be in list", name)
		}
	}

	// Test empty pack
	emptyPack := &Pack{
		Prompts: make(map[string]*Prompt),
	}
	emptyPrompts := emptyPack.ListPrompts()
	if len(emptyPrompts) != 0 {
		t.Errorf("expected 0 prompts for empty pack, got %d", len(emptyPrompts))
	}
}

func TestPackManager_ValidatePack_WithVersionValidation(t *testing.T) {
	pm := NewPackManager()

	tests := []struct {
		name        string
		pack        *Pack
		wantErr     bool
		errContains string
	}{
		{
			name: "valid pack with valid semver",
			pack: &Pack{
				ID:      "test-pack",
				Name:    "Test Pack",
				Version: "1.0.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test-prompt",
						SystemTemplate: "Test template",
						Version:        "1.0.0",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid pack with v prefix",
			pack: &Pack{
				ID:      "test-pack",
				Name:    "Test Pack",
				Version: "v1.0.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test-prompt",
						SystemTemplate: "Test template",
						Version:        "v2.0.0",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid pack version - incomplete",
			pack: &Pack{
				ID:      "test-pack",
				Name:    "Test Pack",
				Version: "1.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test-prompt",
						SystemTemplate: "Test template",
						Version:        "1.0.0",
					},
				},
			},
			wantErr:     true,
			errContains: "pack version '1.0' is invalid",
		},
		{
			name: "invalid prompt version",
			pack: &Pack{
				ID:      "test-pack",
				Name:    "Test Pack",
				Version: "1.0.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test-prompt",
						Name:           "Test Prompt",
						SystemTemplate: "Test template",
						Version:        "latest",
					},
				},
			},
			wantErr:     true,
			errContains: "version 'latest' is invalid",
		},
		{
			name: "valid pack with prompt without version",
			pack: &Pack{
				ID:      "test-pack",
				Name:    "Test Pack",
				Version: "1.0.0",
				Prompts: map[string]*Prompt{
					"test": {
						ID:             "test-prompt",
						SystemTemplate: "Test template",
						// No version specified - should be allowed
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pm.validatePack(tt.pack)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing '%s', got no error", tt.errContains)
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing '%s', got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestPackManager_LoadPackWithMediaConfig(t *testing.T) {
	// Create temp pack file with MediaConfig
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "multimodal.pack.json")

	// Create pack data with MediaConfig
	packData := map[string]interface{}{
		"id":      "multimodal-pack",
		"name":    "Multimodal Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
			"syntax":  "{{variable}}",
		},
		"prompts": map[string]interface{}{
			"image-analyzer": map[string]interface{}{
				"id":              "image-analyzer",
				"name":            "Image Analyzer",
				"system_template": "Analyze the provided image: {{task}}",
				"variables": []map[string]interface{}{
					{
						"name":     "task",
						"required": true,
					},
				},
				"media": map[string]interface{}{
					"enabled":         true,
					"supported_types": []string{"image"},
					"image": map[string]interface{}{
						"max_size_mb":        20,
						"allowed_formats":    []string{"jpeg", "png", "webp"},
						"default_detail":     "high",
						"max_images_per_msg": 5,
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal pack: %v", err)
	}

	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("failed to write pack file: %v", err)
	}

	// Load the pack
	pm := NewPackManager()
	pack, err := pm.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load pack: %v", err)
	}

	if pack.ID != "multimodal-pack" {
		t.Errorf("expected pack ID 'multimodal-pack', got '%s'", pack.ID)
	}

	// Get the prompt with MediaConfig
	promptConfig, err := pack.GetPrompt("image-analyzer")
	if err != nil {
		t.Fatalf("failed to get prompt: %v", err)
	}
	if promptConfig == nil {
		t.Fatal("Prompt not found")
	}

	// Verify MediaConfig is present and correct
	if promptConfig.MediaConfig == nil {
		t.Fatal("MediaConfig should not be nil")
	}

	t.Logf("MediaConfig: %+v", promptConfig.MediaConfig)
	if promptConfig.MediaConfig.Image != nil {
		t.Logf("Image config: %+v", promptConfig.MediaConfig.Image)
	}

	if !promptConfig.MediaConfig.Enabled {
		t.Error("MediaConfig should be enabled")
	}

	if len(promptConfig.MediaConfig.SupportedTypes) != 1 || promptConfig.MediaConfig.SupportedTypes[0] != "image" {
		t.Errorf("Expected supported types [image], got %v", promptConfig.MediaConfig.SupportedTypes)
	}

	if promptConfig.MediaConfig.Image == nil {
		t.Fatal("Image config should not be nil")
	}

	if promptConfig.MediaConfig.Image.MaxSizeMB != 20 {
		t.Errorf("Expected MaxSizeMB 20, got %d", promptConfig.MediaConfig.Image.MaxSizeMB)
	}

	expectedFormats := []string{"jpeg", "png", "webp"}
	if len(promptConfig.MediaConfig.Image.AllowedFormats) != len(expectedFormats) {
		t.Errorf("Expected %d formats, got %d", len(expectedFormats), len(promptConfig.MediaConfig.Image.AllowedFormats))
	}

	if promptConfig.MediaConfig.Image.DefaultDetail != "high" {
		t.Errorf("Expected DefaultDetail 'high', got '%s'", promptConfig.MediaConfig.Image.DefaultDetail)
	}
}
