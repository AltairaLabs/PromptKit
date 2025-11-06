package prompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPack_Validate(t *testing.T) {
	tests := []struct {
		name            string
		pack            *Pack
		expectedErrors  int
		expectedStrings []string
	}{
		{
			name: "valid pack",
			pack: &Pack{
				ID:      "test-pack",
				Version: "v1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {
						SystemTemplate: "Hello {{name}}",
						Version:        "1.0.0", // Add version
						Variables: []VariableMetadata{
							{Name: "name", Required: true},
						},
					},
				},
				Compilation: &CompilationInfo{
					CompiledWith: "packc v1.0.0",
				},
			},
			expectedErrors: 0,
		},
		{
			name: "missing ID",
			pack: &Pack{
				Version: "v1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {SystemTemplate: "test"},
				},
			},
			expectedErrors:  1,
			expectedStrings: []string{"missing required field: id"},
		},
		{
			name: "missing version",
			pack: &Pack{
				ID: "test-pack",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{
					"test": {SystemTemplate: "test"},
				},
			},
			expectedErrors:  1,
			expectedStrings: []string{"missing required field: version"},
		},
		{
			name: "no prompts",
			pack: &Pack{
				ID:      "test-pack",
				Version: "v1.0.0",
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
				Prompts: map[string]*PackPrompt{},
			},
			expectedErrors:  1,
			expectedStrings: []string{"no prompts defined in pack"},
		},
		{
			name: "missing template engine",
			pack: &Pack{
				ID:      "test-pack",
				Version: "v1.0.0",
				Prompts: map[string]*PackPrompt{
					"test": {SystemTemplate: "test"},
				},
			},
			expectedErrors:  1,
			expectedStrings: []string{"missing template_engine configuration"},
		},
		{
			name: "multiple errors",
			pack: &Pack{
				ID:             "",
				Version:        "",
				TemplateEngine: nil,
				Prompts:        map[string]*PackPrompt{},
			},
			expectedErrors: 4,
			expectedStrings: []string{
				"missing required field: id",
				"missing required field: version",
				"no prompts defined in pack",
				"missing template_engine configuration",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.pack.Validate()

			// Check that all expected warnings are present
			for _, expectedStr := range tt.expectedStrings {
				assert.Contains(t, warnings, expectedStr, "expected warning not found")
			}

			// For valid pack, should have no warnings
			if tt.expectedErrors == 0 {
				assert.Empty(t, warnings)
			} else {
				// For invalid packs, should have at least the expected warnings
				assert.GreaterOrEqual(t, len(warnings), tt.expectedErrors, "should have at least the expected warnings")
			}
		})
	}
}

func TestPack_GetPrompt(t *testing.T) {
	pack := &Pack{
		Prompts: map[string]*PackPrompt{
			"test": {
				ID:             "test",
				Name:           "Test Prompt",
				SystemTemplate: "Hello {{name}}",
			},
			"other": {
				ID:             "other",
				Name:           "Other Prompt",
				SystemTemplate: "Goodbye {{name}}",
			},
		},
	}

	t.Run("existing prompt", func(t *testing.T) {
		prompt := pack.GetPrompt("test")
		require.NotNil(t, prompt)
		assert.Equal(t, "test", prompt.ID)
		assert.Equal(t, "Test Prompt", prompt.Name)
	})

	t.Run("non-existing prompt", func(t *testing.T) {
		prompt := pack.GetPrompt("nonexistent")
		assert.Nil(t, prompt)
	})
}

func TestPack_ListPrompts(t *testing.T) {
	t.Run("multiple prompts", func(t *testing.T) {
		pack := &Pack{
			Prompts: map[string]*PackPrompt{
				"test1": {ID: "test1"},
				"test2": {ID: "test2"},
				"test3": {ID: "test3"},
			},
		}

		prompts := pack.ListPrompts()
		assert.Len(t, prompts, 3)
		assert.Contains(t, prompts, "test1")
		assert.Contains(t, prompts, "test2")
		assert.Contains(t, prompts, "test3")
	})

	t.Run("empty prompts", func(t *testing.T) {
		pack := &Pack{
			Prompts: map[string]*PackPrompt{},
		}

		prompts := pack.ListPrompts()
		assert.Len(t, prompts, 0)
	})
}

func TestPack_GetRequiredVariables(t *testing.T) {
	pack := &Pack{
		Prompts: map[string]*PackPrompt{
			"test": {
				Variables: []VariableMetadata{
					{Name: "name", Required: true},
					{Name: "age", Required: true},
					{Name: "city", Required: false},
				},
			},
		},
	}

	t.Run("existing prompt", func(t *testing.T) {
		vars := pack.GetRequiredVariables("test")
		assert.Len(t, vars, 2)
		assert.Contains(t, vars, "name")
		assert.Contains(t, vars, "age")
		assert.NotContains(t, vars, "city")
	})

	t.Run("non-existing prompt", func(t *testing.T) {
		vars := pack.GetRequiredVariables("nonexistent")
		assert.Empty(t, vars)
	})
}

func TestPack_GetOptionalVariables(t *testing.T) {
	pack := &Pack{
		Prompts: map[string]*PackPrompt{
			"test": {
				Variables: []VariableMetadata{
					{Name: "name", Required: true, Default: "John"},
					{Name: "city", Required: false, Default: "NYC"},
					{Name: "country", Required: false, Default: "USA"},
					{Name: "optional_no_default", Required: false, Default: ""},
				},
			},
		},
	}

	t.Run("existing prompt", func(t *testing.T) {
		vars := pack.GetOptionalVariables("test")
		assert.Len(t, vars, 2)
		assert.Equal(t, "NYC", vars["city"])
		assert.Equal(t, "USA", vars["country"])
		assert.NotContains(t, vars, "name")
		assert.NotContains(t, vars, "optional_no_default")
	})

	t.Run("non-existing prompt", func(t *testing.T) {
		vars := pack.GetOptionalVariables("nonexistent")
		assert.Empty(t, vars)
	})
}

func TestPack_GetToolNames(t *testing.T) {
	pack := &Pack{
		Prompts: map[string]*PackPrompt{
			"test": {
				Tools: []string{"search", "calculator", "weather"},
			},
			"no-tools": {
				Tools: []string{},
			},
		},
	}

	t.Run("prompt with tools", func(t *testing.T) {
		tools := pack.GetToolNames("test")
		assert.Len(t, tools, 3)
		assert.Contains(t, tools, "search")
		assert.Contains(t, tools, "calculator")
		assert.Contains(t, tools, "weather")
	})

	t.Run("prompt without tools", func(t *testing.T) {
		tools := pack.GetToolNames("no-tools")
		assert.Empty(t, tools)
	})

	t.Run("non-existing prompt", func(t *testing.T) {
		tools := pack.GetToolNames("nonexistent")
		assert.Empty(t, tools)
	})
}

func TestPack_Summary(t *testing.T) {
	pack := &Pack{
		Name:    "Customer Support",
		Version: "v1.2.3",
		Prompts: map[string]*PackPrompt{
			"greeting": {},
			"farewell": {},
			"help":     {},
		},
	}

	summary := pack.Summary()
	assert.Contains(t, summary, "Customer Support")
	assert.Contains(t, summary, "v1.2.3")
	assert.Contains(t, summary, "3 prompts")
}

func TestLoadPack(t *testing.T) {
	t.Run("valid pack file", func(t *testing.T) {
		tmpDir := t.TempDir()
		packFile := filepath.Join(tmpDir, "test.pack.json")

		pack := &Pack{
			ID:      "test-pack",
			Name:    "Test Pack",
			Version: "v1.0.0",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
			Prompts: map[string]*PackPrompt{
				"greeting": {
					ID:             "greeting",
					SystemTemplate: "Hello {{name}}",
					Variables: []VariableMetadata{
						{Name: "name", Required: true},
					},
				},
			},
		}

		data, err := json.MarshalIndent(pack, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(packFile, data, 0644)
		require.NoError(t, err)

		loaded, err := LoadPack(packFile)
		require.NoError(t, err)
		assert.Equal(t, "test-pack", loaded.ID)
		assert.Equal(t, "Test Pack", loaded.Name)
		assert.Equal(t, "v1.0.0", loaded.Version)
		assert.Len(t, loaded.Prompts, 1)
		assert.NotNil(t, loaded.Prompts["greeting"])
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := LoadPack("/nonexistent/path/pack.json")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read pack file")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		packFile := filepath.Join(tmpDir, "invalid.json")
		err := os.WriteFile(packFile, []byte("not valid json"), 0644)
		require.NoError(t, err)

		_, err = LoadPack(packFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse pack JSON")
	})
}

func TestNewPackCompiler(t *testing.T) {
	registry := createTestRegistry()
	compiler := NewPackCompiler(registry)

	require.NotNil(t, compiler)
	assert.Equal(t, registry, compiler.registry)
}

func TestAssembledPrompt_UsesTools(t *testing.T) {
	t.Run("with tools", func(t *testing.T) {
		ap := &AssembledPrompt{
			AllowedTools: []string{"search", "calculator"},
		}
		assert.True(t, ap.UsesTools())
	})

	t.Run("without tools", func(t *testing.T) {
		ap := &AssembledPrompt{
			AllowedTools: []string{},
		}
		assert.False(t, ap.UsesTools())
	})

	t.Run("nil tools", func(t *testing.T) {
		ap := &AssembledPrompt{}
		assert.False(t, ap.UsesTools())
	})
}

func TestLoadPackWithMediaConfig(t *testing.T) {
	t.Run("pack with media config", func(t *testing.T) {
		tmpDir := t.TempDir()
		packFile := filepath.Join(tmpDir, "multimodal.pack.json")

		pack := &Pack{
			ID:      "multimodal-pack",
			Name:    "Multimodal Pack",
			Version: "v1.0.0",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
			Prompts: map[string]*PackPrompt{
				"image-analysis": {
					ID:             "image-analysis",
					Name:           "Image Analyzer",
					SystemTemplate: "Analyze the provided image",
					MediaConfig: &MediaConfig{
						Enabled:        true,
						SupportedTypes: []string{"image"},
						Image: &ImageConfig{
							MaxSizeMB:      20,
							AllowedFormats: []string{"jpeg", "png", "webp"},
							DefaultDetail:  "high",
						},
					},
				},
			},
		}

		data, err := json.MarshalIndent(pack, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(packFile, data, 0644)
		require.NoError(t, err)

		loaded, err := LoadPack(packFile)
		require.NoError(t, err)
		assert.Equal(t, "multimodal-pack", loaded.ID)
		assert.Len(t, loaded.Prompts, 1)

		prompt := loaded.Prompts["image-analysis"]
		require.NotNil(t, prompt)
		require.NotNil(t, prompt.MediaConfig)
		assert.True(t, prompt.MediaConfig.Enabled)
		assert.Equal(t, []string{"image"}, prompt.MediaConfig.SupportedTypes)
		assert.NotNil(t, prompt.MediaConfig.Image)
		assert.Equal(t, 20, prompt.MediaConfig.Image.MaxSizeMB)
		assert.Equal(t, []string{"jpeg", "png", "webp"}, prompt.MediaConfig.Image.AllowedFormats)
		assert.Equal(t, "high", prompt.MediaConfig.Image.DefaultDetail)
	})

	t.Run("pack without media config", func(t *testing.T) {
		tmpDir := t.TempDir()
		packFile := filepath.Join(tmpDir, "simple.pack.json")

		pack := &Pack{
			ID:      "simple-pack",
			Version: "v1.0.0",
			Prompts: map[string]*PackPrompt{
				"greeting": {
					ID:             "greeting",
					SystemTemplate: "Hello {{name}}",
				},
			},
		}

		data, err := json.MarshalIndent(pack, "", "  ")
		require.NoError(t, err)
		err = os.WriteFile(packFile, data, 0644)
		require.NoError(t, err)

		loaded, err := LoadPack(packFile)
		require.NoError(t, err)
		assert.Equal(t, "simple-pack", loaded.ID)

		prompt := loaded.Prompts["greeting"]
		require.NotNil(t, prompt)
		assert.Nil(t, prompt.MediaConfig) // Should be nil for non-multimodal prompts
	})
}
