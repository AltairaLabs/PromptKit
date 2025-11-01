package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSemanticVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
		errMsg  string
	}{
		// Valid versions
		{
			name:    "valid semver MAJOR.MINOR.PATCH",
			version: "1.0.0",
			wantErr: false,
		},
		{
			name:    "valid semver with v prefix",
			version: "v1.0.0",
			wantErr: false,
		},
		{
			name:    "valid semver with higher numbers",
			version: "2.15.3",
			wantErr: false,
		},
		{
			name:    "valid semver with v prefix and higher numbers",
			version: "v10.20.30",
			wantErr: false,
		},
		{
			name:    "valid pre-release version",
			version: "1.0.0-alpha",
			wantErr: false,
		},
		{
			name:    "valid pre-release with number",
			version: "1.0.0-beta.1",
			wantErr: false,
		},
		{
			name:    "valid version with build metadata",
			version: "1.0.0+20130313144700",
			wantErr: false,
		},
		{
			name:    "valid pre-release with build metadata",
			version: "1.0.0-alpha+001",
			wantErr: false,
		},
		{
			name:    "valid zero major version",
			version: "0.1.0",
			wantErr: false,
		},
		{
			name:    "valid all zeros",
			version: "0.0.0",
			wantErr: false,
		},

		// Invalid versions
		{
			name:    "empty version",
			version: "",
			wantErr: true,
			errMsg:  "empty",
		},
		{
			name:    "missing patch version",
			version: "1.0",
			wantErr: true,
		},
		{
			name:    "missing minor and patch",
			version: "v1",
			wantErr: true,
		},
		{
			name:    "non-numeric version",
			version: "version-1",
			wantErr: true,
		},
		{
			name:    "latest tag",
			version: "latest",
			wantErr: true,
		},
		{
			name:    "invalid format with dashes",
			version: "1-0-0",
			wantErr: true,
		},
		{
			name:    "invalid characters",
			version: "1.0.0abc",
			wantErr: true,
		},
		{
			name:    "missing minor version",
			version: "1..0",
			wantErr: true,
		},
		{
			name:    "negative version numbers",
			version: "-1.0.0",
			wantErr: true,
		},
		{
			name:    "spaces in version",
			version: "1.0.0 beta",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSemanticVersion(tt.version)

			if tt.wantErr {
				assert.Error(t, err, "expected error for version: %s", tt.version)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err, "expected no error for version: %s", tt.version)
			}
		})
	}
}

func TestPack_Validate_WithVersionValidation(t *testing.T) {
	tests := []struct {
		name            string
		pack            *Pack
		expectedStrings []string
		shouldHaveError bool
	}{
		{
			name: "valid pack with valid versions",
			pack: &Pack{
				ID:      "test-pack",
				Version: "1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {
						SystemTemplate: "Hello {{name}}",
						Version:        "1.0.0",
						Variables: []VariableMetadata{
							{Name: "name", Required: true},
						},
					},
					"test2": {
						SystemTemplate: "Goodbye {{name}}",
						Version:        "v2.1.0",
						Variables: []VariableMetadata{
							{Name: "name", Required: true},
						},
					},
				},
				Compilation: &CompilationInfo{
					CompiledWith: "packc v1.0.0",
				},
			},
			shouldHaveError: false,
		},
		{
			name: "invalid pack version",
			pack: &Pack{
				ID:      "test-pack",
				Version: "latest", // Invalid
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {
						SystemTemplate: "Hello {{name}}",
						Version:        "1.0.0",
					},
				},
			},
			shouldHaveError: true,
			expectedStrings: []string{"invalid pack version"},
		},
		{
			name: "invalid prompt version",
			pack: &Pack{
				ID:      "test-pack",
				Version: "1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {
						SystemTemplate: "Hello {{name}}",
						Version:        "v1", // Invalid - missing minor and patch
						Variables: []VariableMetadata{
							{Name: "name", Required: true},
						},
					},
				},
			},
			shouldHaveError: true,
			expectedStrings: []string{"prompt 'test'", "invalid version"},
		},
		{
			name: "multiple invalid prompt versions",
			pack: &Pack{
				ID:      "test-pack",
				Version: "1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test1": {
						SystemTemplate: "Hello",
						Version:        "version-1", // Invalid
						Variables: []VariableMetadata{
							{Name: "test", Required: true},
						},
					},
					"test2": {
						SystemTemplate: "Goodbye",
						Version:        "2.0", // Invalid - missing patch
						Variables: []VariableMetadata{
							{Name: "test", Required: true},
						},
					},
				},
			},
			shouldHaveError: true,
			expectedStrings: []string{
				"prompt 'test1'",
				"prompt 'test2'",
			},
		},
		{
			name: "empty pack version",
			pack: &Pack{
				ID:      "test-pack",
				Version: "", // Empty - should be caught by existing validation
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {
						SystemTemplate: "Hello",
						Version:        "1.0.0",
						Variables: []VariableMetadata{
							{Name: "test", Required: true},
						},
					},
				},
			},
			shouldHaveError: true,
			expectedStrings: []string{"missing required field: version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.pack.Validate()

			if tt.shouldHaveError {
				assert.NotEmpty(t, warnings, "expected validation warnings")

				// Check that each expected string appears in AT LEAST ONE warning
				for _, expectedStr := range tt.expectedStrings {
					found := false
					for _, warning := range warnings {
						if strings.Contains(warning, expectedStr) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected warning containing '%s' not found in warnings: %v", expectedStr, warnings)
					}
				}
			} else {
				assert.Empty(t, warnings, "expected no validation warnings, got: %v", warnings)
			}
		})
	}
}

func TestPackPrompt_ValidateVersion(t *testing.T) {
	tests := []struct {
		name          string
		promptVersion string
		wantErr       bool
	}{
		{"valid v1.0.0", "v1.0.0", false},
		{"valid 2.5.1", "2.5.1", false},
		{"invalid v1", "v1", true},
		{"invalid latest", "latest", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pack := &Pack{
				ID:      "test",
				Version: "1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {
						SystemTemplate: "Hello",
						Version:        tt.promptVersion,
					},
				},
			}

			warnings := pack.Validate()

			if tt.wantErr {
				// Should have warnings about invalid version
				hasVersionError := false
				for _, w := range warnings {
					if strings.Contains(w, "invalid version") {
						hasVersionError = true
						break
					}
				}
				assert.True(t, hasVersionError, "expected validation warnings about invalid version: %s", tt.promptVersion)
			} else {
				// Should NOT have warnings about invalid version
				hasVersionError := false
				for _, w := range warnings {
					if strings.Contains(w, "invalid version") {
						hasVersionError = true
						break
					}
				}
				assert.False(t, hasVersionError, "expected no version warnings for: %s, got warnings: %v", tt.promptVersion, warnings)
			}
		})
	}
}
