package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
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
	repo := newMockRepository()
	testConfig := &Config{
		Spec: Spec{
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
		configWithFragments := &Config{
			Spec: Spec{
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
	repo := newMockRepository()

	config1 := &Config{
		Spec: Spec{
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

	config2 := &Config{
		Spec: Spec{
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
		emptyRepo := newMockRepository()
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
	repo := newMockRepository()
	testConfig := &Config{
		Spec: Spec{
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

func TestCreatePackPrompt_ToolPolicyParametersEvals(t *testing.T) {
	temp := 0.7
	maxTokens := 1024
	topP := 0.9

	repo := newMockRepository()
	testConfig := &Config{
		Spec: Spec{
			TaskType:       "test-v12",
			Version:        "1.0.0",
			Description:    "Test v1.2 fields",
			SystemTemplate: "Hello {{name}}",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
			Variables: []VariableMetadata{
				{Name: "name", Required: true, Type: "string"},
			},
			ToolPolicy: &ToolPolicyPack{
				ToolChoice:          "auto",
				MaxRounds:           5,
				MaxToolCallsPerTurn: 3,
				Blocklist:           []string{"dangerous_tool"},
			},
			Parameters: &ParametersPack{
				Temperature: &temp,
				MaxTokens:   &maxTokens,
				TopP:        &topP,
			},
			Evals: []evals.EvalDef{
				{
					ID:   "latency-check",
					Type: "latency",
					Params: map[string]any{
						"max_ms": 2000,
					},
				},
			},
		},
		Metadata: metav1.ObjectMeta{Name: "Test V12"},
	}
	repo.prompts["test-v12"] = testConfig

	registry := NewRegistryWithRepository(repo)
	_ = registry.RegisterConfig("test-v12", testConfig)

	fixedTime := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)
	compiler := NewPackCompilerWithDeps(registry, mockTimeProvider{fixedTime: fixedTime}, newMockFileWriter())

	t.Run("createPackPrompt populates ToolPolicy, Parameters, Evals", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistry("v12-pack", "packc-test")
		require.NoError(t, err)

		prompt := pack.Prompts["test-v12"]
		require.NotNil(t, prompt)

		// ToolPolicy
		require.NotNil(t, prompt.ToolPolicy)
		assert.Equal(t, "auto", prompt.ToolPolicy.ToolChoice)
		assert.Equal(t, 5, prompt.ToolPolicy.MaxRounds)
		assert.Equal(t, 3, prompt.ToolPolicy.MaxToolCallsPerTurn)
		assert.Equal(t, []string{"dangerous_tool"}, prompt.ToolPolicy.Blocklist)

		// Parameters
		require.NotNil(t, prompt.Parameters)
		require.NotNil(t, prompt.Parameters.Temperature)
		assert.InDelta(t, 0.7, *prompt.Parameters.Temperature, 0.001)
		require.NotNil(t, prompt.Parameters.MaxTokens)
		assert.Equal(t, 1024, *prompt.Parameters.MaxTokens)
		require.NotNil(t, prompt.Parameters.TopP)
		assert.InDelta(t, 0.9, *prompt.Parameters.TopP, 0.001)

		// Evals
		require.Len(t, prompt.Evals, 1)
		assert.Equal(t, "latency-check", prompt.Evals[0].ID)
		assert.Equal(t, "latency", prompt.Evals[0].Type)
	})

	t.Run("fields serialize to JSON correctly", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistry("v12-pack", "packc-test")
		require.NoError(t, err)

		data, err := json.MarshalIndent(pack, "", "  ")
		require.NoError(t, err)

		// Parse back and verify
		var loaded Pack
		require.NoError(t, json.Unmarshal(data, &loaded))

		prompt := loaded.Prompts["test-v12"]
		require.NotNil(t, prompt)
		require.NotNil(t, prompt.ToolPolicy)
		assert.Equal(t, "auto", prompt.ToolPolicy.ToolChoice)
		require.NotNil(t, prompt.Parameters)
		require.NotNil(t, prompt.Parameters.Temperature)
		assert.InDelta(t, 0.7, *prompt.Parameters.Temperature, 0.001)
		require.Len(t, prompt.Evals, 1)
		assert.Equal(t, "latency-check", prompt.Evals[0].ID)
	})
}

func TestCompileFromRegistryWithOptions_PackEvals(t *testing.T) {
	repo := newMockRepository()
	testConfig := &Config{
		Spec: Spec{
			TaskType:       "eval-test",
			Version:        "1.0.0",
			SystemTemplate: "Hello",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
		},
		Metadata: metav1.ObjectMeta{Name: "Eval Test"},
	}
	repo.prompts["eval-test"] = testConfig

	registry := NewRegistryWithRepository(repo)
	_ = registry.RegisterConfig("eval-test", testConfig)

	fixedTime := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)
	compiler := NewPackCompilerWithDeps(registry, mockTimeProvider{fixedTime: fixedTime}, newMockFileWriter())

	t.Run("sets pack-level evals", func(t *testing.T) {
		packEvals := []evals.EvalDef{
			{
				ID:   "global-latency",
				Type: "latency",
				Params: map[string]any{
					"max_ms": 5000,
				},
			},
			{
				ID:   "global-cost",
				Type: "cost",
				Params: map[string]any{
					"max_usd": 0.05,
				},
			},
		}

		pack, err := compiler.CompileFromRegistryWithOptions("eval-pack", "packc-test", nil, packEvals)
		require.NoError(t, err)

		require.Len(t, pack.Evals, 2)
		assert.Equal(t, "global-latency", pack.Evals[0].ID)
		assert.Equal(t, "global-cost", pack.Evals[1].ID)
	})

	t.Run("nil packEvals leaves Evals empty", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistryWithOptions("eval-pack", "packc-test", nil, nil)
		require.NoError(t, err)
		assert.Empty(t, pack.Evals)
	})

	t.Run("backward compat: CompileFromRegistryWithParsedTools still works", func(t *testing.T) {
		pack, err := compiler.CompileFromRegistryWithParsedTools("compat-pack", "packc-test", nil)
		require.NoError(t, err)
		assert.Empty(t, pack.Evals)
		assert.Contains(t, pack.Prompts, "eval-test")
	})
}

func TestBackwardCompat_NoNewFields(t *testing.T) {
	// Configs without ToolPolicy, Parameters, or Evals should compile normally
	repo := newMockRepository()
	testConfig := &Config{
		Spec: Spec{
			TaskType:       "simple",
			Version:        "1.0.0",
			Description:    "Simple prompt without v1.2 fields",
			SystemTemplate: "Just a template",
			TemplateEngine: &TemplateEngineInfo{
				Version: "v1",
				Syntax:  "handlebars",
			},
			Variables: []VariableMetadata{
				{Name: "input", Required: true},
			},
		},
		Metadata: metav1.ObjectMeta{Name: "Simple"},
	}
	repo.prompts["simple"] = testConfig

	registry := NewRegistryWithRepository(repo)
	_ = registry.RegisterConfig("simple", testConfig)

	fixedTime := time.Date(2025, 11, 6, 12, 0, 0, 0, time.UTC)
	compiler := NewPackCompilerWithDeps(registry, mockTimeProvider{fixedTime: fixedTime}, newMockFileWriter())

	pack, err := compiler.CompileFromRegistry("simple-pack", "packc-test")
	require.NoError(t, err)

	prompt := pack.Prompts["simple"]
	require.NotNil(t, prompt)

	// These should be nil/empty when not set
	assert.Nil(t, prompt.ToolPolicy)
	assert.Nil(t, prompt.Parameters)
	assert.Empty(t, prompt.Evals)

	// JSON round-trip should also work
	data, err := json.MarshalIndent(pack, "", "  ")
	require.NoError(t, err)

	var loaded Pack
	require.NoError(t, json.Unmarshal(data, &loaded))
	loadedPrompt := loaded.Prompts["simple"]
	require.NotNil(t, loadedPrompt)
	assert.Nil(t, loadedPrompt.ToolPolicy)
	assert.Nil(t, loadedPrompt.Parameters)
	assert.Empty(t, loadedPrompt.Evals)
}

func TestPack_ValidateAgents(t *testing.T) {
	basePack := func() *Pack {
		return &Pack{
			ID:      "test-pack",
			Version: "v1.0.0",
			Prompts: map[string]*PackPrompt{
				"chat":      {ID: "chat", SystemTemplate: "Hello"},
				"summarize": {ID: "summarize", SystemTemplate: "Summarize"},
			},
		}
	}

	t.Run("valid agents config passes", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat": {
					Description: "Chat agent",
					Tags:        []string{"conversational"},
					InputModes:  []string{"text/plain"},
					OutputModes: []string{"text/plain"},
				},
				"summarize": {
					Description: "Summarizer",
					InputModes:  []string{"application/json"},
					OutputModes: []string{"text/plain"},
				},
			},
		}

		errs, warnings := p.ValidateAgents()
		assert.Empty(t, errs)
		assert.Empty(t, warnings)
	})

	t.Run("nil agents passes", func(t *testing.T) {
		p := basePack()
		p.Agents = nil

		errs, warnings := p.ValidateAgents()
		assert.Empty(t, errs)
		assert.Empty(t, warnings)
	})

	t.Run("empty members fails", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry:   "chat",
			Members: map[string]*AgentDef{},
		}

		errs, warnings := p.ValidateAgents()
		assert.Len(t, errs, 1)
		assert.Contains(t, errs[0], "members must not be empty")
		assert.Empty(t, warnings)
	})

	t.Run("invalid entry reference fails", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry: "nonexistent",
			Members: map[string]*AgentDef{
				"chat": {Description: "Chat agent"},
			},
		}

		errs, _ := p.ValidateAgents()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if assert.ObjectsAreEqual("agents: entry \"nonexistent\" does not reference a valid member", e) {
				found = true
			}
		}
		assert.True(t, found, "expected entry reference error")
	})

	t.Run("member key not in prompts map fails", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat":    {Description: "Chat agent"},
				"unknown": {Description: "Not a prompt"},
			},
		}

		errs, warnings := p.ValidateAgents()
		assert.NotEmpty(t, errs)
		found := false
		for _, e := range errs {
			if assert.ObjectsAreEqual("agents: member \"unknown\" does not reference a valid prompt", e) {
				found = true
			}
		}
		assert.True(t, found, "expected member-prompt reference error")
		assert.Empty(t, warnings)
	})

	t.Run("invalid MIME types warn", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat": {
					Description: "Chat agent",
					InputModes:  []string{"plaintext"},
					OutputModes: []string{"json", "text/plain"},
				},
			},
		}

		errs, warnings := p.ValidateAgents()
		assert.Empty(t, errs)
		assert.Len(t, warnings, 2)
		assert.Contains(t, warnings[0], "is not a valid MIME type")
		assert.Contains(t, warnings[1], "is not a valid MIME type")
	})

	t.Run("empty tags warn", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry: "chat",
			Members: map[string]*AgentDef{
				"chat": {
					Description: "Chat agent",
					Tags:        []string{"valid", "", "  "},
					InputModes:  []string{"text/plain"},
					OutputModes: []string{"text/plain"},
				},
			},
		}

		errs, warnings := p.ValidateAgents()
		assert.Empty(t, errs)
		assert.Len(t, warnings, 2)
		assert.Contains(t, warnings[0], "tag[1] is empty")
		assert.Contains(t, warnings[1], "tag[2] is empty")
	})

	t.Run("agents validation wired into Pack.Validate", func(t *testing.T) {
		p := basePack()
		p.Agents = &AgentsConfig{
			Entry:   "chat",
			Members: map[string]*AgentDef{},
		}

		allWarnings := p.Validate()
		found := false
		for _, w := range allWarnings {
			if w == "agents: members must not be empty" {
				found = true
			}
		}
		assert.True(t, found, "expected agents error in Validate() output")
	})
}
