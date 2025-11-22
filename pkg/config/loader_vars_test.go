package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPromptConfigs_WithVars(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a prompt config file
	promptContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: restaurant-support
spec:
  task_type: restaurant-support
  version: v1.0.0
  description: Restaurant support prompt
  system_template: "You are a support assistant for {{restaurant_name}}, a {{cuisine_type}} restaurant."
  required_vars:
    - restaurant_name
    - cuisine_type
  optional_vars:
    business_hours: "11 AM - 10 PM"
    dress_code: "Smart casual"
`
	promptPath := filepath.Join(tmpDir, "restaurant-support.yaml")
	if err := os.WriteFile(promptPath, []byte(promptContent), 0600); err != nil {
		t.Fatalf("Failed to write test prompt config: %v", err)
	}

	tests := []struct {
		name         string
		arenaYAML    string
		expectVars   bool
		expectedVars map[string]string
	}{
		{
			name: "with vars in prompt_configs",
			arenaYAML: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: restaurant-support
      file: restaurant-support.yaml
      vars:
        restaurant_name: "Sushi Haven"
        cuisine_type: "Japanese"
        business_hours: "12 PM - 11 PM, closed Mondays"
        dress_code: "Casual"
`,
			expectVars: true,
			expectedVars: map[string]string{
				"restaurant_name": "Sushi Haven",
				"cuisine_type":    "Japanese",
				"business_hours":  "12 PM - 11 PM, closed Mondays",
				"dress_code":      "Casual",
			},
		},
		{
			name: "without vars in prompt_configs",
			arenaYAML: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: restaurant-support
      file: restaurant-support.yaml
`,
			expectVars:   false,
			expectedVars: nil,
		},
		{
			name: "with empty vars",
			arenaYAML: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: restaurant-support
      file: restaurant-support.yaml
      vars: {}
`,
			expectVars:   false,
			expectedVars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write arena config
			configPath := filepath.Join(tmpDir, "arena.yaml")
			if err := os.WriteFile(configPath, []byte(tt.arenaYAML), 0600); err != nil {
				t.Fatalf("Failed to write arena config: %v", err)
			}

			// Load config
			config, err := LoadConfig(configPath)
			if err != nil {
				t.Fatalf("LoadConfig failed: %v", err)
			}

			// Check LoadedPromptConfigs
			if len(config.LoadedPromptConfigs) != 1 {
				t.Fatalf("Expected 1 loaded prompt config, got %d", len(config.LoadedPromptConfigs))
			}

			promptConfig, exists := config.LoadedPromptConfigs["restaurant-support"]
			if !exists {
				t.Fatal("Expected restaurant-support prompt config to be loaded")
			}

			// Verify Vars field
			if tt.expectVars {
				if len(promptConfig.Vars) != len(tt.expectedVars) {
					t.Errorf("Expected %d vars, got %d", len(tt.expectedVars), len(promptConfig.Vars))
				}

				for key, expectedVal := range tt.expectedVars {
					if actualVal, ok := promptConfig.Vars[key]; !ok {
						t.Errorf("Missing var %s", key)
					} else if actualVal != expectedVal {
						t.Errorf("Var %s: expected %s, got %s", key, expectedVal, actualVal)
					}
				}
			} else {
				if len(promptConfig.Vars) > 0 && tt.expectedVars == nil {
					t.Errorf("Expected no vars, got %d", len(promptConfig.Vars))
				}
			}
		})
	}
}

func TestLoadPromptConfigs_VarsMerging(t *testing.T) {
	tmpDir := t.TempDir()

	// Create prompt config file
	promptContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: product-support
spec:
  task_type: product-support
  version: v1.0.0
  description: Product support prompt
  system_template: "Support for {{product_name}}"
  required_vars:
    - product_name
  optional_vars:
    support_hours: "9 AM - 5 PM"
`
	promptPath := filepath.Join(tmpDir, "product-support.yaml")
	if err := os.WriteFile(promptPath, []byte(promptContent), 0600); err != nil {
		t.Fatalf("Failed to write test prompt config: %v", err)
	}

	arenaYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: product-support
      file: product-support.yaml
      vars:
        product_name: "CloudSync Enterprise"
        support_hours: "24/7"
        warranty_period: "3 years"
`

	configPath := filepath.Join(tmpDir, "arena.yaml")
	if err := os.WriteFile(configPath, []byte(arenaYAML), 0600); err != nil {
		t.Fatalf("Failed to write arena config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	promptConfig, exists := config.LoadedPromptConfigs["product-support"]
	if !exists {
		t.Fatal("Expected product-support prompt config to be loaded")
	}

	// Verify all vars are present (required + optional + extra)
	expected := map[string]string{
		"product_name":    "CloudSync Enterprise",
		"support_hours":   "24/7",
		"warranty_period": "3 years",
	}

	if len(promptConfig.Vars) != len(expected) {
		t.Errorf("Expected %d vars, got %d", len(expected), len(promptConfig.Vars))
	}

	for key, expectedVal := range expected {
		if actualVal, ok := promptConfig.Vars[key]; !ok {
			t.Errorf("Missing var %s", key)
		} else if actualVal != expectedVal {
			t.Errorf("Var %s: expected %s, got %s", key, expectedVal, actualVal)
		}
	}
}

func TestLoadPromptConfigs_MultipleConfigsWithDifferentVars(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two different prompt config files
	prompt1Content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: restaurant-support
spec:
  task_type: restaurant-support
  version: v1.0.0
  system_template: "{{restaurant_name}}"
  required_vars: ["restaurant_name"]
`
	prompt1Path := filepath.Join(tmpDir, "restaurant.yaml")
	if err := os.WriteFile(prompt1Path, []byte(prompt1Content), 0600); err != nil {
		t.Fatalf("Failed to write prompt1: %v", err)
	}

	prompt2Content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: product-support
spec:
  task_type: product-support
  version: v1.0.0
  system_template: "{{product_name}}"
  required_vars: ["product_name"]
`
	prompt2Path := filepath.Join(tmpDir, "product.yaml")
	if err := os.WriteFile(prompt2Path, []byte(prompt2Content), 0600); err != nil {
		t.Fatalf("Failed to write prompt2: %v", err)
	}

	arenaYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: restaurant-support
      file: restaurant.yaml
      vars:
        restaurant_name: "Sushi Haven"
        cuisine_type: "Japanese"
    - id: product-support
      file: product.yaml
      vars:
        product_name: "CloudSync"
        version: "2.0"
`

	configPath := filepath.Join(tmpDir, "arena.yaml")
	if err := os.WriteFile(configPath, []byte(arenaYAML), 0600); err != nil {
		t.Fatalf("Failed to write arena config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify both configs loaded with correct vars
	if len(config.LoadedPromptConfigs) != 2 {
		t.Fatalf("Expected 2 loaded prompt configs, got %d", len(config.LoadedPromptConfigs))
	}

	// Check restaurant config
	restaurantConfig, exists := config.LoadedPromptConfigs["restaurant-support"]
	if !exists {
		t.Fatal("Expected restaurant-support config")
	}
	if restaurantConfig.Vars["restaurant_name"] != "Sushi Haven" {
		t.Errorf("Expected Sushi Haven, got %s", restaurantConfig.Vars["restaurant_name"])
	}
	if restaurantConfig.Vars["cuisine_type"] != "Japanese" {
		t.Errorf("Expected Japanese, got %s", restaurantConfig.Vars["cuisine_type"])
	}

	// Check product config
	productConfig, exists := config.LoadedPromptConfigs["product-support"]
	if !exists {
		t.Fatal("Expected product-support config")
	}
	if productConfig.Vars["product_name"] != "CloudSync" {
		t.Errorf("Expected CloudSync, got %s", productConfig.Vars["product_name"])
	}
	if productConfig.Vars["version"] != "2.0" {
		t.Errorf("Expected 2.0, got %s", productConfig.Vars["version"])
	}

	// Ensure vars from one config don't leak to another
	if _, exists := restaurantConfig.Vars["product_name"]; exists {
		t.Error("restaurant config should not have product_name var")
	}
	if _, exists := productConfig.Vars["restaurant_name"]; exists {
		t.Error("product config should not have restaurant_name var")
	}
}
