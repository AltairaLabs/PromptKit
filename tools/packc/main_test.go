package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFragmentNames(t *testing.T) {
	t.Run("empty fragments", func(t *testing.T) {
		fragments := map[string]string{}
		names := getFragmentNames(fragments)
		assert.Empty(t, names)
	})

	t.Run("multiple fragments", func(t *testing.T) {
		fragments := map[string]string{
			"intro":  "{{intro}}",
			"outro":  "{{outro}}",
			"middle": "{{middle}}",
		}
		names := getFragmentNames(fragments)
		assert.Len(t, names, 3)
		assert.Contains(t, names, "intro")
		assert.Contains(t, names, "outro")
		assert.Contains(t, names, "middle")
	})
}

func TestValidateMediaReferences(t *testing.T) {
	t.Run("no media config", func(t *testing.T) {
		config := &prompt.Config{
			Spec: prompt.Spec{
				TaskType: "test",
			},
		}
		warnings := validateMediaReferences(config, "/tmp")
		assert.Empty(t, warnings)
	})

	t.Run("disabled media config", func(t *testing.T) {
		config := &prompt.Config{
			Spec: prompt.Spec{
				TaskType: "test",
				MediaConfig: &prompt.MediaConfig{
					Enabled: false,
				},
			},
		}
		warnings := validateMediaReferences(config, "/tmp")
		assert.Empty(t, warnings)
	})

	t.Run("media config with URL references", func(t *testing.T) {
		config := &prompt.Config{
			Spec: prompt.Spec{
				TaskType: "test",
				MediaConfig: &prompt.MediaConfig{
					Enabled:        true,
					SupportedTypes: []string{"image"},
					Examples: []prompt.MultimodalExample{
						{
							Name: "url-example",
							Role: "user",
							Parts: []prompt.ExampleContentPart{
								{
									Media: &prompt.ExampleMedia{
										URL:      "https://example.com/image.jpg",
										MIMEType: "image/jpeg",
									},
								},
							},
						},
					},
				},
			},
		}
		warnings := validateMediaReferences(config, "/tmp")
		// URLs are not validated, only file paths
		assert.Empty(t, warnings)
	})

	t.Run("media config with missing file", func(t *testing.T) {
		config := &prompt.Config{
			Spec: prompt.Spec{
				TaskType: "test",
				MediaConfig: &prompt.MediaConfig{
					Enabled:        true,
					SupportedTypes: []string{"image"},
					Examples: []prompt.MultimodalExample{
						{
							Name: "file-example",
							Role: "user",
							Parts: []prompt.ExampleContentPart{
								{
									Media: &prompt.ExampleMedia{
										FilePath: "nonexistent.jpg",
										MIMEType: "image/jpeg",
									},
								},
							},
						},
					},
				},
			},
		}
		warnings := validateMediaReferences(config, "/tmp")
		assert.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "Media file not found")
		assert.Contains(t, warnings[0], "nonexistent.jpg")
	})

	t.Run("media config with existing file", func(t *testing.T) {
		// Create a temporary file
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.jpg")
		err := os.WriteFile(testFile, []byte("test"), 0644)
		require.NoError(t, err)

		config := &prompt.Config{
			Spec: prompt.Spec{
				TaskType: "test",
				MediaConfig: &prompt.MediaConfig{
					Enabled:        true,
					SupportedTypes: []string{"image"},
					Examples: []prompt.MultimodalExample{
						{
							Name: "file-example",
							Role: "user",
							Parts: []prompt.ExampleContentPart{
								{
									Media: &prompt.ExampleMedia{
										FilePath: "test.jpg",
										MIMEType: "image/jpeg",
									},
								},
							},
						},
					},
				},
			},
		}
		warnings := validateMediaReferences(config, tmpDir)
		assert.Empty(t, warnings)
	})
}

func TestValidateExampleMediaReferences(t *testing.T) {
	t.Run("no media in parts", func(t *testing.T) {
		example := prompt.MultimodalExample{
			Name: "text-only",
			Role: "user",
			Parts: []prompt.ExampleContentPart{
				{
					Text: "Hello world",
				},
			},
		}
		warnings := validateExampleMediaReferences(example, "/tmp")
		assert.Empty(t, warnings)
	})

	t.Run("media with empty filepath", func(t *testing.T) {
		example := prompt.MultimodalExample{
			Name: "empty-path",
			Role: "user",
			Parts: []prompt.ExampleContentPart{
				{
					Media: &prompt.ExampleMedia{
						FilePath: "",
						URL:      "https://example.com/image.jpg",
						MIMEType: "image/jpeg",
					},
				},
			},
		}
		warnings := validateExampleMediaReferences(example, "/tmp")
		assert.Empty(t, warnings)
	})

	t.Run("media with absolute path - file exists", func(t *testing.T) {
		// Create a temporary file
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "test.jpg")
		err := os.WriteFile(testFile, []byte("test"), 0644)
		require.NoError(t, err)

		example := prompt.MultimodalExample{
			Name: "absolute-path",
			Role: "user",
			Parts: []prompt.ExampleContentPart{
				{
					Media: &prompt.ExampleMedia{
						FilePath: testFile,
						MIMEType: "image/jpeg",
					},
				},
			},
		}
		warnings := validateExampleMediaReferences(example, "/tmp")
		assert.Empty(t, warnings)
	})

	t.Run("media with absolute path - file missing", func(t *testing.T) {
		example := prompt.MultimodalExample{
			Name: "missing-abs",
			Role: "user",
			Parts: []prompt.ExampleContentPart{
				{
					Media: &prompt.ExampleMedia{
						FilePath: "/nonexistent/path/image.jpg",
						MIMEType: "image/jpeg",
					},
				},
			},
		}
		warnings := validateExampleMediaReferences(example, "/tmp")
		assert.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "Media file not found")
		assert.Contains(t, warnings[0], "/nonexistent/path/image.jpg")
	})

	t.Run("multiple parts with mixed results", func(t *testing.T) {
		tmpDir := t.TempDir()
		existingFile := filepath.Join(tmpDir, "exists.jpg")
		err := os.WriteFile(existingFile, []byte("test"), 0644)
		require.NoError(t, err)

		example := prompt.MultimodalExample{
			Name: "mixed",
			Role: "user",
			Parts: []prompt.ExampleContentPart{
				{
					Media: &prompt.ExampleMedia{
						FilePath: "exists.jpg",
						MIMEType: "image/jpeg",
					},
				},
				{
					Media: &prompt.ExampleMedia{
						FilePath: "missing.jpg",
						MIMEType: "image/jpeg",
					},
				},
			},
		}
		warnings := validateExampleMediaReferences(example, tmpDir)
		assert.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "missing.jpg")
		assert.Contains(t, warnings[0], "part 1")
	})
}

func TestPrintPackInfo(t *testing.T) {
	// This is mainly for coverage - just ensure it doesn't panic
	pack := &prompt.Pack{
		ID:          "test-pack",
		Name:        "Test Pack",
		Version:     "1.0.0",
		Description: "Test description",
	}

	// Call the function - it prints to stdout but shouldn't panic
	printPackInfo(pack)
}

func TestPrintTemplateEngine(t *testing.T) {
	t.Run("with template engine", func(t *testing.T) {
		pack := &prompt.Pack{
			TemplateEngine: &prompt.TemplateEngineInfo{
				Version:  "v1",
				Syntax:   "handlebars",
				Features: []string{"loops", "conditionals"},
			},
		}
		printTemplateEngine(pack)
	})

	t.Run("without template engine", func(t *testing.T) {
		pack := &prompt.Pack{}
		printTemplateEngine(pack)
	})
}

func TestPrintFragments(t *testing.T) {
	t.Run("with fragments", func(t *testing.T) {
		pack := &prompt.Pack{
			Fragments: map[string]string{
				"intro": "{{intro}}",
				"outro": "{{outro}}",
			},
		}
		printFragments(pack)
	})

	t.Run("without fragments", func(t *testing.T) {
		pack := &prompt.Pack{}
		printFragments(pack)
	})
}

func TestPrintMetadata(t *testing.T) {
	t.Run("with metadata", func(t *testing.T) {
		pack := &prompt.Pack{
			Metadata: &prompt.Metadata{
				Domain:   "customer-support",
				Language: "en",
				Tags:     []string{"support", "predict"},
			},
		}
		printMetadata(pack)
	})

	t.Run("without metadata", func(t *testing.T) {
		pack := &prompt.Pack{}
		printMetadata(pack)
	})
}

func TestPrintCompilationInfo(t *testing.T) {
	t.Run("with compilation info", func(t *testing.T) {
		pack := &prompt.Pack{
			Compilation: &prompt.CompilationInfo{
				CompiledWith: "packc-v0.1.0",
				CreatedAt:    "2025-11-06T12:00:00Z",
				Schema:       "v1",
			},
		}
		printCompilationInfo(pack)
	})

	t.Run("without compilation info", func(t *testing.T) {
		pack := &prompt.Pack{}
		printCompilationInfo(pack)
	})
}

func TestPrintPromptVariables(t *testing.T) {
	t.Run("with mixed variables", func(t *testing.T) {
		p := &prompt.PackPrompt{
			Variables: []prompt.VariableMetadata{
				{Name: "required1", Required: true},
				{Name: "optional1", Required: false},
				{Name: "required2", Required: true},
			},
		}
		printPromptVariables(p)
	})

	t.Run("no variables", func(t *testing.T) {
		p := &prompt.PackPrompt{}
		printPromptVariables(p)
	})
}

func TestPrintPromptTools(t *testing.T) {
	t.Run("with tools", func(t *testing.T) {
		p := &prompt.PackPrompt{
			Tools: []string{"search", "calculator"},
		}
		printPromptTools(p)
	})

	t.Run("no tools", func(t *testing.T) {
		p := &prompt.PackPrompt{}
		printPromptTools(p)
	})
}

func TestPrintPromptValidators(t *testing.T) {
	enabled := true
	disabled := false

	t.Run("with validators", func(t *testing.T) {
		p := &prompt.PackPrompt{
			Validators: []prompt.ValidatorConfig{
				{
					ValidatorConfig: validators.ValidatorConfig{Type: "length"},
					Enabled:         &enabled,
				},
				{
					ValidatorConfig: validators.ValidatorConfig{Type: "sentiment"},
					Enabled:         &disabled,
				},
			},
		}
		printPromptValidators(p)
	})

	t.Run("no validators", func(t *testing.T) {
		p := &prompt.PackPrompt{}
		printPromptValidators(p)
	})
}

func TestPrintPromptsFunction(t *testing.T) {
	t.Run("with prompts", func(t *testing.T) {
		pack := &prompt.Pack{
			Prompts: map[string]*prompt.PackPrompt{
				"task1": {
					ID:          "task1",
					Name:        "Task 1",
					Description: "First task",
				},
				"task2": {
					ID:          "task2",
					Name:        "Task 2",
					Description: "Second task",
				},
			},
		}
		printPrompts(pack)
	})

	t.Run("no prompts", func(t *testing.T) {
		pack := &prompt.Pack{}
		printPrompts(pack)
	})
}

func TestMustLoadConfig_Error(t *testing.T) {
	// Test that the underlying function returns error for nonexistent file
	_, err := config.LoadConfig("nonexistent-file.yaml")
	assert.Error(t, err)
}

func TestBuildMemoryRepo(t *testing.T) {
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{
			"test": {
				FilePath: "test.yaml",
				Config: &prompt.Config{
					APIVersion: "promptkit.altairalabs.ai/v1alpha1",
					Kind:       "Config",
					Spec: prompt.Spec{
						TaskType:       "test",
						SystemTemplate: "Test prompt",
					},
				},
			},
		},
	}

	repo := buildMemoryRepo(cfg)
	assert.NotNil(t, repo)

	// Verify prompt was registered
	cfg2, err := repo.LoadPrompt("test")
	require.NoError(t, err)
	assert.Equal(t, "test", cfg2.Spec.TaskType)
}

func TestBuildMemoryRepo_InvalidConfig(t *testing.T) {
	// This would cause os.Exit in real code, but we test the logic
	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{
			"test": {
				FilePath: "test.yaml",
				Config:   nil, // nil config should be skipped
			},
		},
	}

	repo := buildMemoryRepo(cfg)
	assert.NotNil(t, repo)
}

func TestValidateLoadedMedia(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.jpg")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		LoadedPromptConfigs: map[string]*config.PromptConfigData{
			"test": {
				FilePath: "test.yaml",
				Config: &prompt.Config{
					Spec: prompt.Spec{
						TaskType: "test",
						MediaConfig: &prompt.MediaConfig{
							Enabled:        true,
							SupportedTypes: []string{"image"},
							Examples: []prompt.MultimodalExample{
								{
									Name: "example",
									Role: "user",
									Parts: []prompt.ExampleContentPart{
										{
											Media: &prompt.ExampleMedia{
												FilePath: "test.jpg",
												MIMEType: "image/jpeg",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Should not panic
	validateLoadedMedia(cfg, tmpDir)
}

func TestPrintPromptTestedModels(t *testing.T) {
	t.Run("with tested models", func(t *testing.T) {
		p := &prompt.PackPrompt{
			TestedModels: []prompt.ModelTestResultRef{
				{
					Provider:    "openai",
					Model:       "gpt-4",
					SuccessRate: 0.95,
					AvgTokens:   150,
				},
			},
		}
		printPromptTestedModels(p)
	})

	t.Run("no tested models", func(t *testing.T) {
		p := &prompt.PackPrompt{}
		printPromptTestedModels(p)
	})
}

func TestPrintPromptDetails(t *testing.T) {
	enabled := true
	p := &prompt.PackPrompt{
		ID:          "test-prompt",
		Name:        "Test Prompt",
		Description: "A test prompt",
		Version:     "1.0.0",
		Variables: []prompt.VariableMetadata{
			{Name: "var1", Required: true},
		},
		Tools: []string{"tool1"},
		Validators: []prompt.ValidatorConfig{
			{
				ValidatorConfig: validators.ValidatorConfig{Type: "length"},
				Enabled:         &enabled,
			},
		},
		TestedModels: []prompt.ModelTestResultRef{
			{Provider: "openai", Model: "gpt-4", SuccessRate: 0.95, AvgTokens: 100},
		},
	}

	printPromptDetails("test", p)
}

func TestPrintPrompts(t *testing.T) {
	pack := &prompt.Pack{
		Prompts: map[string]*prompt.PackPrompt{
			"task1": {
				ID:      "task1",
				Name:    "Task 1",
				Version: "1.0.0",
			},
		},
	}
	printPrompts(pack)
}

func TestPrintWorkflow(t *testing.T) {
	t.Run("with workflow", func(t *testing.T) {
		pack := &prompt.Pack{
			Workflow: &prompt.WorkflowConfig{
				Version: 1,
				Entry:   "start",
				States: map[string]*prompt.WorkflowState{
					"start": {
						PromptTask:  "greeting",
						Description: "Initial state",
						OnEvent: map[string]string{
							"next": "end",
						},
						Persistence:   "persistent",
						Orchestration: "internal",
					},
					"end": {
						PromptTask: "farewell",
						OnEvent:    map[string]string{},
					},
				},
			},
		}
		printWorkflow(pack)
	})

	t.Run("without workflow", func(t *testing.T) {
		pack := &prompt.Pack{}
		printWorkflow(pack)
	})
}

func TestPrintAgents(t *testing.T) {
	t.Run("with agents", func(t *testing.T) {
		pack := &prompt.Pack{
			Agents: &prompt.AgentsConfig{
				Entry: "triage",
				Members: map[string]*prompt.AgentDef{
					"triage": {
						Description: "Triage agent",
						Tags:        []string{"router"},
						InputModes:  []string{"text/plain"},
						OutputModes: []string{"text/plain"},
					},
					"billing": {
						Description: "Billing agent",
						Tags:        []string{"billing"},
					},
				},
			},
		}
		printAgents(pack)
	})

	t.Run("without agents", func(t *testing.T) {
		pack := &prompt.Pack{}
		printAgents(pack)
	})
}

func TestSanitizePackID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "MyProject",
			expected: "myproject",
		},
		{
			name:     "spaces to dashes",
			input:    "my project name",
			expected: "my-project-name",
		},
		{
			name:     "remove special characters",
			input:    "my_project!@#$%",
			expected: "myproject",
		},
		{
			name:     "mixed case with spaces and special chars",
			input:    "Customer Support Bot!",
			expected: "customer-support-bot",
		},
		{
			name:     "multiple consecutive dashes",
			input:    "my---project",
			expected: "my-project",
		},
		{
			name:     "trim leading/trailing dashes",
			input:    "-my-project-",
			expected: "my-project",
		},
		{
			name:     "already valid id",
			input:    "customer-support",
			expected: "customer-support",
		},
		{
			name:     "numbers preserved",
			input:    "project123",
			expected: "project123",
		},
		{
			name:     "complex mixed input",
			input:    "  My Cool Project (v2.0)  ",
			expected: "my-cool-project-v20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizePackID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
