package validators

import (
	"testing"
)

func TestContentIncludesValidator(t *testing.T) {
	tests := []struct {
		name        string
		patterns    []string
		content     string
		wantOK      bool
		wantMissing []string
	}{
		{
			name:        "All patterns found",
			patterns:    []string{"account", "status"},
			content:     "Your account status is active",
			wantOK:      true,
			wantMissing: nil,
		},
		{
			name:        "One pattern missing",
			patterns:    []string{"account", "subscription", "active"},
			content:     "Your account is active",
			wantOK:      false,
			wantMissing: []string{"subscription"},
		},
		{
			name:        "All patterns missing",
			patterns:    []string{"password", "reset"},
			content:     "Your account is active",
			wantOK:      false,
			wantMissing: []string{"password", "reset"},
		},
		{
			name:        "Case insensitive match",
			patterns:    []string{"Account", "Status"},
			content:     "your account status is good",
			wantOK:      true,
			wantMissing: nil,
		},
		{
			name:        "No patterns specified",
			patterns:    []string{},
			content:     "any content",
			wantOK:      true,
			wantMissing: nil,
		},
		{
			name:        "Empty content",
			patterns:    []string{"something"},
			content:     "",
			wantOK:      false,
			wantMissing: []string{"something"},
		},
		{
			name:        "Partial word match",
			patterns:    []string{"count"},
			content:     "Your account is ready",
			wantOK:      true,
			wantMissing: nil,
		},
		{
			name:        "Multiple occurrences",
			patterns:    []string{"account"},
			content:     "Your account account account",
			wantOK:      true,
			wantMissing: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create validator with factory pattern
			params := map[string]interface{}{
				"patterns": tt.patterns,
			}
			validator := NewContentIncludesValidator(params)

			// Validate
			result := validator.Validate(tt.content, params)

			if result.OK != tt.wantOK {
				t.Errorf("Validate() OK = %v, want %v", result.OK, tt.wantOK)
			}

			// Check missing patterns
			if !result.OK {
				details, ok := result.Details.(map[string]interface{})
				if !ok {
					t.Fatalf("Expected details to be map[string]interface{}, got %T", result.Details)
				}

				missing, ok := details["missing_patterns"].([]string)
				if !ok {
					t.Fatalf("Expected missing_patterns to be []string, got %T", details["missing_patterns"])
				}

				if len(missing) != len(tt.wantMissing) {
					t.Errorf("Got %d missing patterns, want %d: %v", len(missing), len(tt.wantMissing), missing)
				}

				for i, want := range tt.wantMissing {
					if i >= len(missing) || missing[i] != want {
						t.Errorf("Missing pattern %d = %v, want %v", i, missing, tt.wantMissing)
					}
				}
			}
		})
	}
}

func TestContentIncludesValidator_FactoryWithSliceTypes(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "String slice",
			params: map[string]interface{}{
				"patterns": []string{"pattern1", "pattern2"},
			},
		},
		{
			name: "Interface slice",
			params: map[string]interface{}{
				"patterns": []interface{}{"pattern1", "pattern2"},
			},
		},
		{
			name:   "Missing patterns param",
			params: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewContentIncludesValidator(tt.params)

			// Test that it doesn't panic and basic functionality works
			result := validator.Validate("some content", tt.params)

			// Should pass if no patterns or patterns not in content
			if !result.OK && len(tt.params) == 0 {
				t.Error("Expected OK=true when no patterns specified")
			}
		})
	}
}
