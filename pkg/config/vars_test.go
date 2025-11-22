package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPromptConfigRef_VarsFieldParsing(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected map[string]string
		wantErr  bool
	}{
		{
			name: "simple vars",
			yaml: `
task_type: "restaurant-support"
vars:
  restaurant_name: "Sushi Haven"
  cuisine_type: "Japanese"
`,
			expected: map[string]string{
				"restaurant_name": "Sushi Haven",
				"cuisine_type":    "Japanese",
			},
			wantErr: false,
		},
		{
			name: "no vars field",
			yaml: `
task_type: "restaurant-support"
`,
			expected: nil,
			wantErr:  false,
		},
		{
			name: "empty vars",
			yaml: `
task_type: "restaurant-support"
vars: {}
`,
			expected: map[string]string{},
			wantErr:  false,
		},
		{
			name: "complex business hours",
			yaml: `
task_type: "restaurant-support"
vars:
  restaurant_name: "Sushi Haven"
  business_hours: "12 PM - 11 PM, closed Mondays"
  dress_code: "Casual"
`,
			expected: map[string]string{
				"restaurant_name": "Sushi Haven",
				"business_hours":  "12 PM - 11 PM, closed Mondays",
				"dress_code":      "Casual",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ref PromptConfigRef
			err := yaml.Unmarshal([]byte(tt.yaml), &ref)

			if (err != nil) != tt.wantErr {
				t.Errorf("yaml.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(tt.expected) == 0 && len(ref.Vars) == 0 {
					return // Both empty/nil, ok
				}

				if len(ref.Vars) != len(tt.expected) {
					t.Errorf("Expected %d vars, got %d", len(tt.expected), len(ref.Vars))
					return
				}

				for key, expectedVal := range tt.expected {
					if actualVal, ok := ref.Vars[key]; !ok {
						t.Errorf("Missing var %s", key)
					} else if actualVal != expectedVal {
						t.Errorf("Var %s: expected %s, got %s", key, expectedVal, actualVal)
					}
				}
			}
		})
	}
}

func TestPromptConfigData_VarsField(t *testing.T) {
	tests := []struct {
		name     string
		data     PromptConfigData
		expected map[string]string
	}{
		{
			name: "vars populated",
			data: PromptConfigData{
				TaskType: "support",
				Vars: map[string]string{
					"company_name": "Acme Corp",
					"industry":     "Technology",
				},
			},
			expected: map[string]string{
				"company_name": "Acme Corp",
				"industry":     "Technology",
			},
		},
		{
			name: "no vars",
			data: PromptConfigData{
				TaskType: "support",
				Vars:     nil,
			},
			expected: nil,
		},
		{
			name: "empty vars",
			data: PromptConfigData{
				TaskType: "support",
				Vars:     map[string]string{},
			},
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.expected) == 0 && len(tt.data.Vars) == 0 {
				return // Both empty/nil, ok
			}

			if len(tt.data.Vars) != len(tt.expected) {
				t.Errorf("Expected %d vars, got %d", len(tt.expected), len(tt.data.Vars))
				return
			}

			for key, expectedVal := range tt.expected {
				if actualVal, ok := tt.data.Vars[key]; !ok {
					t.Errorf("Missing var %s", key)
				} else if actualVal != expectedVal {
					t.Errorf("Var %s: expected %s, got %s", key, expectedVal, actualVal)
				}
			}
		})
	}
}
