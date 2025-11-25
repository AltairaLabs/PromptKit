package templates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoader_LoadBuiltIn(t *testing.T) {
	loader := NewLoader("")

	tmpl, err := loader.LoadBuiltIn("quick-start")
	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, "promptkit.altairalabs.ai/v1alpha1", tmpl.APIVersion)
	assert.Equal(t, "Template", tmpl.Kind)
	assert.Equal(t, "quick-start", tmpl.Metadata.Name)
}

func TestLoader_ListBuiltIn(t *testing.T) {
	loader := NewLoader("")

	templates, err := loader.ListBuiltIn()
	require.NoError(t, err)
	assert.NotEmpty(t, templates)

	// Should include quick-start
	found := false
	for _, tmpl := range templates {
		if tmpl.Name == "quick-start" {
			found = true
			assert.Equal(t, SourceBuiltIn, tmpl.Source)
			assert.True(t, tmpl.Verified)
			break
		}
	}
	assert.True(t, found, "quick-start template should be in the list")
}

func TestGenerator_RenderTemplate(t *testing.T) {
	loader := NewLoader("")
	tmpl := &Template{}
	generator := NewGenerator(tmpl, loader)

	tests := []struct {
		name      string
		template  string
		vars      map[string]interface{}
		expected  string
		shouldErr bool
	}{
		{
			name:     "simple variable",
			template: "Hello {{.name}}!",
			vars:     map[string]interface{}{"name": "World"},
			expected: "Hello World!",
		},
		{
			name:     "multiple variables",
			template: "{{.greeting}} {{.name}}!",
			vars:     map[string]interface{}{"greeting": "Hi", "name": "Alice"},
			expected: "Hi Alice!",
		},
		{
			name:     "conditional",
			template: "{{if .enabled}}Enabled{{else}}Disabled{{end}}",
			vars:     map[string]interface{}{"enabled": true},
			expected: "Enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generator.renderTemplate("test", tt.template, tt.vars)
			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGenerator_Generate(t *testing.T) {
	// Create a simple template
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Variables: []Variable{
				{
					Name:    "project_name",
					Default: "test-project",
				},
			},
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "Hello {{.project_name}}!",
				},
				{
					Path:    "config.yaml",
					Content: "name: {{.project_name}}\nversion: 1.0.0",
				},
			},
		},
	}

	// Create temp directory for output
	tempDir := t.TempDir()

	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "my-test",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"project_name": "my-test",
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Len(t, result.FilesCreated, 2)
	assert.Empty(t, result.Errors)

	// Verify files were created
	projectPath := filepath.Join(tempDir, "my-test")
	testFile := filepath.Join(projectPath, "test.txt")
	configFile := filepath.Join(projectPath, "config.yaml")

	assert.FileExists(t, testFile)
	assert.FileExists(t, configFile)

	// Verify file contents
	testContent, err := os.ReadFile(testFile)
	require.NoError(t, err)
	assert.Equal(t, "Hello my-test!", string(testContent))

	configContent, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(configContent), "name: my-test")
}

func TestGenerator_Condition(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "always.txt",
					Content: "Always created",
				},
				{
					Path:      "conditional.txt",
					Content:   "Only when flag is true",
					Condition: "{{.create_optional}}",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	// Test with flag = true
	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"create_optional": true,
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Len(t, result.FilesCreated, 2)

	// Test with flag = false
	tempDir2 := t.TempDir()
	config2 := &TemplateConfig{
		ProjectName: "test2",
		OutputDir:   tempDir2,
		Variables: map[string]interface{}{
			"create_optional": false,
		},
		Template: tmpl,
	}

	result2, err := generator.Generate(config2)
	require.NoError(t, err)
	assert.True(t, result2.Success)
	assert.Len(t, result2.FilesCreated, 1)
}

func TestGenerator_ForEach(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "{{.provider}}.txt",
					Content: "Provider: {{.provider}}",
					ForEach: "providers",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"providers": []string{"openai", "anthropic", "google"},
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Len(t, result.FilesCreated, 3)

	// Verify each file was created
	for _, provider := range []string{"openai", "anthropic", "google"} {
		filePath := filepath.Join(tempDir, "test", provider+".txt")
		assert.FileExists(t, filePath)
		content, err := os.ReadFile(filePath)
		require.NoError(t, err)
		assert.Contains(t, string(content), provider)
	}
}

func TestGenerator_ExecutableFile(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:       "script.sh",
					Content:    "#!/bin/bash\necho 'test'",
					Executable: true,
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)

	scriptPath := filepath.Join(tempDir, "test", "script.sh")
	info, err := os.Stat(scriptPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestGenerator_Hooks(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "test",
				},
			},
			Hooks: &HookSet{
				PreCreate: []Hook{
					{
						Message: "Pre-create hook",
						Command: "echo 'before'",
					},
				},
				PostCreate: []Hook{
					{
						Message: "Post-create hook",
						Command: "echo 'after'",
					},
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
}

func TestLoader_Load(t *testing.T) {
	loader := NewLoader("")

	// Test loading built-in template
	tmpl, err := loader.Load("quick-start")
	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, "quick-start", tmpl.Metadata.Name)

	// Test loading non-existent template
	_, err = loader.Load("non-existent")
	assert.Error(t, err)
}

func TestLoader_ReadTemplateFile(t *testing.T) {
	loader := NewLoader("")

	// Test reading a template file
	data, err := loader.ReadTemplateFile("quick-start", "templates/arena.yaml.tmpl")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "apiVersion")

	// Test reading non-existent file
	_, err = loader.ReadTemplateFile("quick-start", "non-existent.txt")
	assert.Error(t, err)
}

func TestLoader_Validate(t *testing.T) {
	loader := NewLoader("")

	tests := []struct {
		name        string
		template    Template
		shouldError bool
		errorMsg    string
	}{
		{
			name: "valid template",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "Template",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Files: []FileSpec{
						{Path: "test.yaml", Content: "test"},
					},
				},
			},
			shouldError: false,
		},
		{
			name: "missing apiVersion",
			template: Template{
				Kind: "Template",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Files: []FileSpec{
						{Path: "test.yaml", Content: "test"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "apiVersion is required",
		},
		{
			name: "wrong kind",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "WrongKind",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Files: []FileSpec{
						{Path: "test.yaml", Content: "test"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "kind must be 'Template'",
		},
		{
			name: "missing metadata name",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "Template",
				Spec: TemplateSpec{
					Files: []FileSpec{
						{Path: "test.yaml", Content: "test"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "metadata.name is required",
		},
		{
			name: "no files",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "Template",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Files: []FileSpec{},
				},
			},
			shouldError: true,
			errorMsg:    "spec.files must not be empty",
		},
		{
			name: "variable without name",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "Template",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Variables: []Variable{
						{Description: "test var"},
					},
					Files: []FileSpec{
						{Path: "test.yaml", Content: "test"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "variables[0].name is required",
		},
		{
			name: "file without path",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "Template",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Files: []FileSpec{
						{Content: "test"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "files[0].path is required",
		},
		{
			name: "file without content or template",
			template: Template{
				APIVersion: "promptkit.altairalabs.ai/v1alpha1",
				Kind:       "Template",
				Metadata: TemplateMetadata{
					Name: "test",
				},
				Spec: TemplateSpec{
					Files: []FileSpec{
						{Path: "test.yaml"},
					},
				},
			},
			shouldError: true,
			errorMsg:    "must have either template or content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := loader.validate(&tt.template)
			if tt.shouldError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGenerator_EvaluateCondition(t *testing.T) {
	loader := NewLoader("")
	tmpl := &Template{}
	generator := NewGenerator(tmpl, loader)

	tests := []struct {
		name      string
		condition string
		vars      map[string]interface{}
		expected  bool
	}{
		{
			name:      "true condition",
			condition: "{{.enabled}}",
			vars:      map[string]interface{}{"enabled": true},
			expected:  true,
		},
		{
			name:      "false condition",
			condition: "{{.enabled}}",
			vars:      map[string]interface{}{"enabled": false},
			expected:  false,
		},
		{
			name:      "string true",
			condition: "{{if .enabled}}true{{end}}",
			vars:      map[string]interface{}{"enabled": true},
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := generator.evaluateCondition(tt.condition, tt.vars)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerator_ForEachWithInterface(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "{{.item}}.txt",
					Content: "Item: {{.item}}",
					ForEach: "items",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"items": []interface{}{"a", "b", "c"},
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Len(t, result.FilesCreated, 3)
}

func TestGenerator_TemplateFromFile(t *testing.T) {
	loader := NewLoader("")

	// Use the actual quick-start template
	tmpl, err := loader.LoadBuiltIn("quick-start")
	require.NoError(t, err)

	tempDir := t.TempDir()
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test-project",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"project_name": "test-project",
			"provider":     "mock",
			"include_env":  true,
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotEmpty(t, result.FilesCreated)

	// Verify arena.yaml was created from template
	arenaPath := filepath.Join(tempDir, "test-project", "arena.yaml")
	assert.FileExists(t, arenaPath)

	content, err := os.ReadFile(arenaPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test-project")
	assert.Contains(t, string(content), "promptkit.altairalabs.ai/v1alpha1")
}

func TestGenerator_InvalidTemplate(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "Hello {{.invalid_syntax",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	// Should not return error, but result should contain errors
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestGenerator_ConditionalWithError(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:      "test.txt",
					Content:   "test",
					Condition: "{{.invalid_syntax",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestGenerator_ForEachInvalidVariable(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "test",
					ForEach: "nonexistent",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestGenerator_ForEachInvalidType(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-template",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "test",
					ForEach: "notarray",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"notarray": "string value",
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestLoader_LoadFromFile(t *testing.T) {
	// Create a temporary template file
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "template.yaml")

	templateContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Template
metadata:
  name: custom-template
spec:
  files:
    - path: test.txt
      content: "Hello World"
`
	err := os.WriteFile(templatePath, []byte(templateContent), 0644)
	require.NoError(t, err)

	loader := NewLoader("")
	tmpl, err := loader.LoadFromFile(templatePath)
	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, "custom-template", tmpl.Metadata.Name)
}

func TestLoader_LoadFromFile_Invalid(t *testing.T) {
	tempDir := t.TempDir()

	// Test non-existent file
	loader := NewLoader("")
	_, err := loader.LoadFromFile(filepath.Join(tempDir, "nonexistent.yaml"))
	assert.Error(t, err)

	// Test invalid YAML
	invalidPath := filepath.Join(tempDir, "invalid.yaml")
	err = os.WriteFile(invalidPath, []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	_, err = loader.LoadFromFile(invalidPath)
	assert.Error(t, err)

	// Test invalid template structure
	invalidTemplatePath := filepath.Join(tempDir, "invalid-template.yaml")
	invalidTemplate := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: WrongKind
metadata:
  name: test
spec:
  files: []
`
	err = os.WriteFile(invalidTemplatePath, []byte(invalidTemplate), 0644)
	require.NoError(t, err)

	_, err = loader.LoadFromFile(invalidTemplatePath)
	assert.Error(t, err)
}

func TestLoader_Load_RemoteTemplate(t *testing.T) {
	loader := NewLoader("")

	// Test that non-existent remote template returns error
	_, err := loader.Load("nonexistent-remote-template")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template not found")
}

func TestLoader_Load_LocalTemplate(t *testing.T) {
	// Create a temporary template file
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, "template.yaml")

	templateContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Template
metadata:
  name: local-template
spec:
  files:
    - path: test.txt
      content: "Test"
`
	err := os.WriteFile(templatePath, []byte(templateContent), 0644)
	require.NoError(t, err)

	loader := NewLoader("")

	// Test loading with absolute path (starts with /)
	tmpl, err := loader.Load(templatePath)
	require.NoError(t, err)
	assert.Equal(t, "local-template", tmpl.Metadata.Name)
}

func TestGenerator_Generate_WithHooks(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-with-hooks",
		},
		Spec: TemplateSpec{
			Hooks: &HookSet{
				PreCreate: []Hook{
					{
						Command: "echo 'pre-create'",
						Message: "Running pre-create hook",
					},
				},
				PostCreate: []Hook{
					{
						Command: "echo 'post-create'",
						Message: "Running post-create hook",
					},
				},
			},
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "Hello",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test-hooks",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotEmpty(t, result.FilesCreated)
}

func TestGenerator_Generate_WithConditionalHooks(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-conditional-hooks",
		},
		Spec: TemplateSpec{
			Hooks: &HookSet{
				PreCreate: []Hook{
					{
						Command:   "echo 'should run'",
						Condition: "{{.run_hook}}",
					},
					{
						Command:   "echo 'should not run'",
						Condition: "{{.skip_hook}}",
					},
				},
			},
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "Test",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables: map[string]interface{}{
			"run_hook":  true,
			"skip_hook": false,
		},
		Template: tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.True(t, result.Success)
}

func TestGenerator_Generate_HookWithRenderError(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-hook-error",
		},
		Spec: TemplateSpec{
			Hooks: &HookSet{
				PreCreate: []Hook{
					{
						Command: "echo {{.undefined syntax",
						Message: "Hook with syntax error",
					},
				},
			},
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "Test",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	// Hook errors should be warnings, not failures
	assert.NotEmpty(t, result.Warnings)
}

func TestGenerator_Generate_HookConditionError(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-hook-condition-error",
		},
		Spec: TemplateSpec{
			Hooks: &HookSet{
				PostCreate: []Hook{
					{
						Command:   "echo 'test'",
						Condition: "{{.invalid condition syntax",
					},
				},
			},
			Files: []FileSpec{
				{
					Path:    "test.txt",
					Content: "Test",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	// Hook condition errors should be warnings
	assert.NotEmpty(t, result.Warnings)
}

func TestGenerator_GenerateSingleFile_RenderError(t *testing.T) {
	tmpl := &Template{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Template",
		Metadata: TemplateMetadata{
			Name: "test-render-error",
		},
		Spec: TemplateSpec{
			Files: []FileSpec{
				{
					Path:     "test.txt",
					Template: "builtin/nonexistent/template.txt",
				},
			},
		},
	}

	tempDir := t.TempDir()
	loader := NewLoader("")
	generator := NewGenerator(tmpl, loader)

	config := &TemplateConfig{
		ProjectName: "test",
		OutputDir:   tempDir,
		Variables:   map[string]interface{}{},
		Template:    tmpl,
	}

	result, err := generator.Generate(config)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestLoader_ListBuiltIn_Error(t *testing.T) {
	// Test with invalid base path
	loader := NewLoader("/nonexistent/path")

	templates, err := loader.ListBuiltIn()
	require.NoError(t, err) // Should still work as it uses embedded FS
	assert.NotEmpty(t, templates)
}

func TestGenerator_RenderTemplate_Error(t *testing.T) {
	loader := NewLoader("")
	tmpl := &Template{}
	generator := NewGenerator(tmpl, loader)

	// Test invalid template syntax
	_, err := generator.renderTemplate("test", "{{.invalid syntax", map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse template")
}
