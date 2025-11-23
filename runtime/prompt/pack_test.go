package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	registry := createTestRegistryWithRepo()
	compiler := NewPackCompiler(registry)

	require.NotNil(t, compiler)
	assert.NotNil(t, compiler.loader)
	assert.NotNil(t, compiler.timeProvider)
	assert.NotNil(t, compiler.fileWriter)
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

// mockTimeProvider for deterministic tests
type mockTimeProvider struct {
	fixedTime time.Time
}

func (m mockTimeProvider) Now() time.Time {
	return m.fixedTime
}

// mockFileWriter for testing without actual file I/O
type mockFileWriter struct {
	writtenFiles map[string][]byte
	writeError   error
}

func newMockFileWriter() *mockFileWriter {
	return &mockFileWriter{
		writtenFiles: make(map[string][]byte),
	}
}

func (m *mockFileWriter) WriteFile(path string, data []byte, perm os.FileMode) error {
	if m.writeError != nil {
		return m.writeError
	}
	m.writtenFiles[path] = data
	return nil
}

func TestPackCompiler_Compile(t *testing.T) {
	// Create mock repository with test config
	repo := newMockPromptRepository()
	testConfig := &PromptConfig{
		Spec: PromptSpec{
			TaskType:       "test-task",
			Version:        "1.0.0",
			Description:    "Test prompt for compilation",
			SystemTemplate: "Hello {{name}}",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
			Variables: []VariableMetadata{
				{Name: "name", Required: true, Type: "string"},
			},
			AllowedTools: []string{"search"},
		},
		Metadata: metav1.ObjectMeta{
			Name: "Test Task",
		},
	}
	repo.prompts["test-task"] = testConfig

	// Create registry and compiler
	registry := NewRegistryWithRepository(repo)
	_ = registry.RegisterConfig("test-task", testConfig)

	fixedTime := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)
	timeProvider := mockTimeProvider{fixedTime: fixedTime}
	fileWriter := newMockFileWriter()

	compiler := NewPackCompilerWithDeps(registry, timeProvider, fileWriter)

	t.Run("compiles valid config successfully", func(t *testing.T) {
		pack, err := compiler.Compile("test-task", "packc v1.0.0")

		require.NoError(t, err)
		assert.Equal(t, "test-task", pack.ID)
		assert.Equal(t, "Test Task", pack.Name)
		assert.Equal(t, "1.0.0", pack.Version)
		assert.Equal(t, "Test prompt for compilation", pack.Description)
		assert.NotNil(t, pack.TemplateEngine)
		assert.Equal(t, "v1", pack.TemplateEngine.Version)

		// Check prompt
		require.Contains(t, pack.Prompts, "test-task")
		prompt := pack.Prompts["test-task"]
		assert.Equal(t, "test-task", prompt.ID)
		assert.Equal(t, "Test Task", prompt.Name)
		assert.Equal(t, "Hello {{name}}", prompt.SystemTemplate)
		assert.Equal(t, []string{"search"}, prompt.Tools)
		assert.Len(t, prompt.Variables, 1)
		assert.Equal(t, "name", prompt.Variables[0].Name)

		// Check compilation info
		assert.NotNil(t, pack.Compilation)
		assert.Equal(t, "packc v1.0.0", pack.Compilation.CompiledWith)
		assert.Equal(t, "v1", pack.Compilation.Schema)
	})

	t.Run("returns error for non-existent task", func(t *testing.T) {
		pack, err := compiler.Compile("non-existent", "packc v1.0.0")

		assert.Error(t, err)
		assert.Nil(t, pack)
		assert.Contains(t, err.Error(), "failed to load config")
	})

	t.Run("handles config with fragments", func(t *testing.T) {
		configWithFragments := &PromptConfig{
			Spec: PromptSpec{
				TaskType:    "task-with-fragments",
				Version:     "1.0.0",
				Description: "Task with fragments",
				Fragments: []FragmentRef{
					{Name: "intro", Required: true},
					{Name: "outro", Required: false},
				},
				TemplateEngine: &TemplateEngineInfo{
					Version: "v1",
					Syntax:  "handlebars",
				},
			},
			Metadata: metav1.ObjectMeta{
				Name: "Task With Fragments",
			},
		}
		repo.prompts["task-with-fragments"] = configWithFragments
		_ = registry.RegisterConfig("task-with-fragments", configWithFragments)

		pack, err := compiler.Compile("task-with-fragments", "packc v1.0.0")

		require.NoError(t, err)
		assert.Len(t, pack.Fragments, 2)
		assert.Contains(t, pack.Fragments, "intro")
		assert.Contains(t, pack.Fragments, "outro")
	})
}

func TestPackCompiler_CompileFromRegistry(t *testing.T) {
	// Create mock repository with multiple configs
	repo := newMockPromptRepository()

	config1 := &PromptConfig{
		Spec: PromptSpec{
			TaskType:       "task1",
			Version:        "1.0.0",
			Description:    "First task",
			SystemTemplate: "Task 1: {{input}}",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
		},
		Metadata: metav1.ObjectMeta{Name: "Task One"},
	}

	config2 := &PromptConfig{
		Spec: PromptSpec{
			TaskType:       "task2",
			Version:        "2.0.0",
			Description:    "Second task",
			SystemTemplate: "Task 2: {{output}}",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
		},
		Metadata: metav1.ObjectMeta{Name: "Task Two"},
	}

	repo.prompts["task1"] = config1
	repo.prompts["task2"] = config2

	// Create registry and compiler
	registry := NewRegistryWithRepository(repo)
	_ = registry.RegisterConfig("task1", config1)
	_ = registry.RegisterConfig("task2", config2)

	fixedTime := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)
	timeProvider := mockTimeProvider{fixedTime: fixedTime}
	fileWriter := newMockFileWriter()

	compiler := NewPackCompilerWithDeps(registry, timeProvider, fileWriter)

	t.Run("compiles all prompts from registry", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistry("multi-pack", "packc v1.0.0")

		require.NoError(t, err)
		assert.Equal(t, "multi-pack", pack.ID)
		assert.Equal(t, "multi-pack", pack.Name)
		assert.Equal(t, "v1.0.0", pack.Version)
		assert.Contains(t, pack.Description, "2 prompts")

		// Check both prompts are included
		assert.Len(t, pack.Prompts, 2)
		assert.Contains(t, pack.Prompts, "task1")
		assert.Contains(t, pack.Prompts, "task2")

		// Verify prompt details
		assert.Equal(t, "Task 1: {{input}}", pack.Prompts["task1"].SystemTemplate)
		assert.Equal(t, "Task 2: {{output}}", pack.Prompts["task2"].SystemTemplate)

		// Check compilation info
		assert.NotNil(t, pack.Compilation)
		assert.Equal(t, "packc v1.0.0", pack.Compilation.CompiledWith)
		assert.Equal(t, fixedTime.Format(time.RFC3339), pack.Compilation.CreatedAt)
		assert.Equal(t, "v1", pack.Compilation.Schema)
	})

	t.Run("returns error for empty registry", func(t *testing.T) {
		emptyRepo := newMockPromptRepository()
		emptyRegistry := NewRegistryWithRepository(emptyRepo)
		emptyCompiler := NewPackCompilerWithDeps(emptyRegistry, timeProvider, fileWriter)

		pack, err := emptyCompiler.CompileFromRegistry("empty-pack", "packc v1.0.0")

		assert.Error(t, err)
		assert.Nil(t, pack)
		assert.Contains(t, err.Error(), "no prompts found in registry")
	})
}

func TestPackCompiler_CompileToFile(t *testing.T) {
	// Create mock repository with test config
	repo := newMockPromptRepository()
	testConfig := &PromptConfig{
		Spec: PromptSpec{
			TaskType:       "test-task",
			Version:        "1.0.0",
			SystemTemplate: "Test template",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
		},
		Metadata: metav1.ObjectMeta{Name: "Test"},
	}
	repo.prompts["test-task"] = testConfig

	registry := NewRegistryWithRepository(repo)
	_ = registry.RegisterConfig("test-task", testConfig)

	fixedTime := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)
	timeProvider := mockTimeProvider{fixedTime: fixedTime}
	fileWriter := newMockFileWriter()

	compiler := NewPackCompilerWithDeps(registry, timeProvider, fileWriter)

	t.Run("compiles to file successfully", func(t *testing.T) {
		outputPath := "/tmp/test.pack.json"

		err := compiler.CompileToFile("test-task", outputPath, "packc v1.0.0")

		require.NoError(t, err)
		assert.Contains(t, fileWriter.writtenFiles, outputPath)

		// Verify the written content is valid JSON
		var pack Pack
		err = json.Unmarshal(fileWriter.writtenFiles[outputPath], &pack)
		require.NoError(t, err)
		assert.Equal(t, "test-task", pack.ID)
	})

	t.Run("returns error on compilation failure", func(t *testing.T) {
		err := compiler.CompileToFile("non-existent", "/tmp/test.pack.json", "packc v1.0.0")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "compilation failed")
	})

	t.Run("returns error on write failure", func(t *testing.T) {
		fileWriter.writeError = fmt.Errorf("disk full")

		err := compiler.CompileToFile("test-task", "/tmp/test.pack.json", "packc v1.0.0")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write pack file")

		fileWriter.writeError = nil // Reset for other tests
	})
}

func TestPackCompiler_MarshalPack(t *testing.T) {
	compiler := NewPackCompiler(createTestRegistryWithRepo())

	pack := &Pack{
		ID:      "test-pack",
		Version: "v1.0.0",
		Prompts: map[string]*PackPrompt{
			"test": {
				ID:             "test",
				SystemTemplate: "Hello",
			},
		},
	}

	t.Run("marshals pack to JSON", func(t *testing.T) {
		data, err := compiler.MarshalPack(pack)

		require.NoError(t, err)
		assert.NotEmpty(t, data)

		// Verify it's valid JSON
		var unmarshaled Pack
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, "test-pack", unmarshaled.ID)
	})
}

func TestPackCompiler_WritePack(t *testing.T) {
	fileWriter := newMockFileWriter()
	compiler := NewPackCompilerWithDeps(
		createTestRegistryWithRepo(),
		realTimeProvider{},
		fileWriter,
	)

	pack := &Pack{
		ID:      "test-pack",
		Version: "v1.0.0",
		Prompts: map[string]*PackPrompt{
			"test": {
				ID:             "test",
				SystemTemplate: "Hello",
			},
		},
	}

	t.Run("writes pack successfully", func(t *testing.T) {
		err := compiler.WritePack(pack, "/tmp/output.pack.json")

		require.NoError(t, err)
		assert.Contains(t, fileWriter.writtenFiles, "/tmp/output.pack.json")
	})
}
