package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBasePromptRepository(t *testing.T) {
	t.Run("with nil taskTypeToFile", func(t *testing.T) {
		repo := NewBasePromptRepository("/tmp", nil, []string{".json"}, json.Unmarshal)
		assert.NotNil(t, repo.TaskTypeToFile)
		assert.Equal(t, "/tmp", repo.BasePath)
		assert.NotNil(t, repo.Cache)
	})

	t.Run("with taskTypeToFile", func(t *testing.T) {
		mapping := map[string]string{"task1": "file1.json"}
		repo := NewBasePromptRepository("/tmp", mapping, []string{".json"}, json.Unmarshal)
		assert.Equal(t, mapping, repo.TaskTypeToFile)
	})
}

func TestLoadPrompt(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "test-base-repo")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test config
	config := prompt.Config{
		APIVersion: "v1",
		Kind:       "PromptConfig",
		Spec: prompt.Spec{
			TaskType: "test-task",
		},
	}
	configData, _ := json.Marshal(config)
	configPath := filepath.Join(tmpDir, "test-task.json")
	os.WriteFile(configPath, configData, 0644)

	repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)

	t.Run("loads and caches config", func(t *testing.T) {
		loaded, err := repo.LoadPrompt("test-task")
		require.NoError(t, err)
		assert.Equal(t, "test-task", loaded.Spec.TaskType)

		// Check cache
		assert.Contains(t, repo.Cache, "test-task")
	})

	t.Run("returns cached config", func(t *testing.T) {
		loaded1, _ := repo.LoadPrompt("test-task")
		loaded2, _ := repo.LoadPrompt("test-task")
		assert.Equal(t, loaded1, loaded2)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := repo.LoadPrompt("nonexistent")
		assert.Error(t, err)
	})
}

func TestResolveFilePath(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-resolve")
	defer os.RemoveAll(tmpDir)

	t.Run("with explicit mapping relative path", func(t *testing.T) {
		mapping := map[string]string{"task1": "prompts/task1.json"}
		repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, json.Unmarshal)

		resolved, err := repo.ResolveFilePath("task1")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(tmpDir, "prompts/task1.json"), resolved)
	})

	t.Run("with explicit mapping absolute path", func(t *testing.T) {
		absPath := "/absolute/path/task1.json"
		mapping := map[string]string{"task1": absPath}
		repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, json.Unmarshal)

		resolved, err := repo.ResolveFilePath("task1")
		require.NoError(t, err)
		assert.Equal(t, absPath, resolved)
	})

	t.Run("falls back to search", func(t *testing.T) {
		// Create a file to find
		config := prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "searchable"},
		}
		configData, _ := json.Marshal(config)
		os.WriteFile(filepath.Join(tmpDir, "searchable.json"), configData, 0644)

		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)
		resolved, err := repo.ResolveFilePath("searchable")
		require.NoError(t, err)
		assert.Contains(t, resolved, "searchable.json")
	})
}

func TestSearchByFilename(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-search-filename")
	defer os.RemoveAll(tmpDir)

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "task1.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "task2.v1.json"), []byte("{}"), 0644)

	repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)

	t.Run("finds exact match", func(t *testing.T) {
		found := repo.SearchByFilename("task1")
		assert.Contains(t, found, "task1.json")
	})

	t.Run("finds versioned file", func(t *testing.T) {
		found := repo.SearchByFilename("task2")
		assert.Contains(t, found, "task2.v1.json")
	})

	t.Run("returns empty for not found", func(t *testing.T) {
		found := repo.SearchByFilename("nonexistent")
		assert.Empty(t, found)
	})
}

func TestSearchByContent(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-search-content")
	defer os.RemoveAll(tmpDir)

	// Create test config with specific task type
	config := prompt.Config{
		APIVersion: "v1",
		Kind:       "PromptConfig",
		Spec:       prompt.Spec{TaskType: "content-task"},
	}
	configData, _ := json.Marshal(config)
	os.WriteFile(filepath.Join(tmpDir, "somefile.json"), configData, 0644)

	repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)

	t.Run("finds by content", func(t *testing.T) {
		found := repo.SearchByContent("content-task")
		assert.Contains(t, found, "somefile.json")
	})

	t.Run("returns empty for not found", func(t *testing.T) {
		found := repo.SearchByContent("nonexistent-task")
		assert.Empty(t, found)
	})
}

func TestHasValidExtension(t *testing.T) {
	repo := NewBasePromptRepository("/tmp", nil, []string{".json", ".yaml", ".yml"}, json.Unmarshal)

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.json", true},
		{"file.JSON", true}, // case insensitive
		{"file.yaml", true},
		{"file.yml", true},
		{"file.txt", false},
		{"file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, repo.HasValidExtension(tt.path))
		})
	}
}

func TestHasMatchingTaskType(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-matching-task")
	defer os.RemoveAll(tmpDir)

	// Create matching file
	config := prompt.Config{
		APIVersion: "v1",
		Kind:       "PromptConfig",
		Spec:       prompt.Spec{TaskType: "matching"},
	}
	configData, _ := json.Marshal(config)
	matchingPath := filepath.Join(tmpDir, "matching.json")
	os.WriteFile(matchingPath, configData, 0644)

	// Create non-matching file
	config2 := prompt.Config{
		APIVersion: "v1",
		Kind:       "PromptConfig",
		Spec:       prompt.Spec{TaskType: "other"},
	}
	configData2, _ := json.Marshal(config2)
	otherPath := filepath.Join(tmpDir, "other.json")
	os.WriteFile(otherPath, configData2, 0644)

	// Create invalid file
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	os.WriteFile(invalidPath, []byte("not json"), 0644)

	repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)

	t.Run("matches correct task type", func(t *testing.T) {
		assert.True(t, repo.HasMatchingTaskType(matchingPath, "matching"))
	})

	t.Run("does not match different task type", func(t *testing.T) {
		assert.False(t, repo.HasMatchingTaskType(matchingPath, "other"))
	})

	t.Run("returns false for invalid file", func(t *testing.T) {
		assert.False(t, repo.HasMatchingTaskType(invalidPath, "matching"))
	})

	t.Run("returns false for nonexistent file", func(t *testing.T) {
		assert.False(t, repo.HasMatchingTaskType("/nonexistent", "matching"))
	})
}

func TestListPrompts(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-list-prompts")
	defer os.RemoveAll(tmpDir)

	t.Run("with explicit mappings", func(t *testing.T) {
		mapping := map[string]string{
			"task1": "file1.json",
			"task2": "file2.json",
		}
		repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, json.Unmarshal)

		taskTypes, err := repo.ListPrompts()
		require.NoError(t, err)
		assert.Len(t, taskTypes, 2)
		assert.Contains(t, taskTypes, "task1")
		assert.Contains(t, taskTypes, "task2")
	})

	t.Run("by scanning directory", func(t *testing.T) {
		// Create test files
		config1 := prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "scanned1"},
		}
		configData1, _ := json.Marshal(config1)
		os.WriteFile(filepath.Join(tmpDir, "scanned1.json"), configData1, 0644)

		config2 := prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "scanned2"},
		}
		configData2, _ := json.Marshal(config2)
		os.WriteFile(filepath.Join(tmpDir, "scanned2.json"), configData2, 0644)

		// Create invalid file (should be skipped)
		os.WriteFile(filepath.Join(tmpDir, "invalid.json"), []byte("not json"), 0644)

		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)

		taskTypes, err := repo.ListPrompts()
		require.NoError(t, err)
		assert.Contains(t, taskTypes, "scanned1")
		assert.Contains(t, taskTypes, "scanned2")
	})

	t.Run("empty directory", func(t *testing.T) {
		emptyDir, _ := os.MkdirTemp("", "test-empty")
		defer os.RemoveAll(emptyDir)

		repo := NewBasePromptRepository(emptyDir, nil, []string{".json"}, json.Unmarshal)
		taskTypes, err := repo.ListPrompts()
		require.NoError(t, err)
		assert.Empty(t, taskTypes)
	})
}

func TestValidatePromptConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := &prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "test"},
		}
		err := ValidatePromptConfig(config)
		assert.NoError(t, err)
	})

	t.Run("missing apiVersion", func(t *testing.T) {
		config := &prompt.Config{
			Kind: "PromptConfig",
			Spec: prompt.Spec{TaskType: "test"},
		}
		err := ValidatePromptConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing apiVersion")
	})

	t.Run("invalid kind", func(t *testing.T) {
		config := &prompt.Config{
			APIVersion: "v1",
			Kind:       "InvalidKind",
			Spec:       prompt.Spec{TaskType: "test"},
		}
		err := ValidatePromptConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid kind")
	})

	t.Run("missing task_type", func(t *testing.T) {
		config := &prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: ""},
		}
		err := ValidatePromptConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing spec.task_type")
	})
}

func TestSearchForPrompt(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-search-prompt")
	defer os.RemoveAll(tmpDir)

	t.Run("not found", func(t *testing.T) {
		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)
		_, err := repo.SearchForPrompt("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no file found")
	})

	t.Run("found by filename", func(t *testing.T) {
		os.WriteFile(filepath.Join(tmpDir, "by-filename.json"), []byte("{}"), 0644)
		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)

		found, err := repo.SearchForPrompt("by-filename")
		require.NoError(t, err)
		assert.Contains(t, found, "by-filename.json")
	})

	t.Run("found by content", func(t *testing.T) {
		config := prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "by-content"},
		}
		configData, _ := json.Marshal(config)
		os.WriteFile(filepath.Join(tmpDir, "random-name.json"), configData, 0644)

		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)
		found, err := repo.SearchForPrompt("by-content")
		require.NoError(t, err)
		assert.Contains(t, found, "random-name.json")
	})
}

func TestLoadPromptWithUnmarshalError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-unmarshal-error")
	defer os.RemoveAll(tmpDir)

	// Create invalid file
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	os.WriteFile(invalidPath, []byte("not valid json"), 0644)

	mapping := map[string]string{"invalid": "invalid.json"}
	repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, json.Unmarshal)

	_, err := repo.LoadPrompt("invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse file")
}

func TestLoadPromptWithValidationError(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-validation-error")
	defer os.RemoveAll(tmpDir)

	// Create config missing required fields
	config := prompt.Config{
		APIVersion: "", // Missing
		Kind:       "PromptConfig",
		Spec:       prompt.Spec{TaskType: "test"},
	}
	configData, _ := json.Marshal(config)
	configPath := filepath.Join(tmpDir, "invalid-config.json")
	os.WriteFile(configPath, configData, 0644)

	mapping := map[string]string{"test": "invalid-config.json"}
	repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, json.Unmarshal)

	_, err := repo.LoadPrompt("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing apiVersion")
}

func TestCustomUnmarshalFunc(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "test-custom-unmarshal")
	defer os.RemoveAll(tmpDir)

	// Create a custom unmarshal function that always returns an error
	errorUnmarshal := func(data []byte, v interface{}) error {
		return fmt.Errorf("custom unmarshal error")
	}

	config := prompt.Config{
		APIVersion: "v1",
		Kind:       "PromptConfig",
		Spec:       prompt.Spec{TaskType: "test"},
	}
	configData, _ := json.Marshal(config)
	configPath := filepath.Join(tmpDir, "test.json")
	os.WriteFile(configPath, configData, 0644)

	mapping := map[string]string{"test": "test.json"}
	repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, errorUnmarshal)

	_, err := repo.LoadPrompt("test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "custom unmarshal error")
}

func TestSavePrompt(t *testing.T) {
	t.Run("saves and loads config", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "test-save-prompt")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)
		repo.Marshal = func(v interface{}) ([]byte, error) {
			return json.MarshalIndent(v, "", "  ")
		}

		config := &prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "save-test"},
		}

		err = repo.SavePrompt(config)
		require.NoError(t, err)

		// Verify file exists
		filePath := filepath.Join(tmpDir, "save-test.json")
		_, err = os.Stat(filePath)
		assert.NoError(t, err)

		// Verify we can load it back
		loaded, err := repo.LoadPrompt("save-test")
		require.NoError(t, err)
		assert.Equal(t, "save-test", loaded.Spec.TaskType)
	})

	t.Run("error without marshal function", func(t *testing.T) {
		repo := NewBasePromptRepository("/tmp", nil, []string{".json"}, json.Unmarshal)
		// Marshal is not set

		config := &prompt.Config{
			Spec: prompt.Spec{TaskType: "test"},
		}

		err := repo.SavePrompt(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "marshal function not configured")
	})

	t.Run("error for nil config", func(t *testing.T) {
		repo := NewBasePromptRepository("/tmp", nil, []string{".json"}, json.Unmarshal)
		repo.Marshal = func(v interface{}) ([]byte, error) {
			return json.Marshal(v)
		}

		err := repo.SavePrompt(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config cannot be nil")
	})

	t.Run("error for empty task_type", func(t *testing.T) {
		repo := NewBasePromptRepository("/tmp", nil, []string{".json"}, json.Unmarshal)
		repo.Marshal = func(v interface{}) ([]byte, error) {
			return json.Marshal(v)
		}

		config := &prompt.Config{
			Spec: prompt.Spec{TaskType: ""},
		}

		err := repo.SavePrompt(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "task_type cannot be empty")
	})

	t.Run("uses explicit mapping for path", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "test-save-mapping")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		mapping := map[string]string{"mapped-task": "custom/path/config.json"}
		repo := NewBasePromptRepository(tmpDir, mapping, []string{".json"}, json.Unmarshal)
		repo.Marshal = func(v interface{}) ([]byte, error) {
			return json.Marshal(v)
		}

		config := &prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "mapped-task"},
		}

		err = repo.SavePrompt(config)
		require.NoError(t, err)

		// Verify file was created at mapped path
		expectedPath := filepath.Join(tmpDir, "custom/path/config.json")
		_, err = os.Stat(expectedPath)
		assert.NoError(t, err)
	})

	t.Run("updates cache and mapping", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "test-save-cache")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		repo := NewBasePromptRepository(tmpDir, nil, []string{".json"}, json.Unmarshal)
		repo.Marshal = func(v interface{}) ([]byte, error) {
			return json.Marshal(v)
		}

		config := &prompt.Config{
			APIVersion: "v1",
			Kind:       "PromptConfig",
			Spec:       prompt.Spec{TaskType: "cache-test"},
		}

		err = repo.SavePrompt(config)
		require.NoError(t, err)

		// Verify cache was updated
		cached, ok := repo.Cache["cache-test"]
		assert.True(t, ok)
		assert.Equal(t, "cache-test", cached.Spec.TaskType)

		// Verify mapping was added
		_, hasMapping := repo.TaskTypeToFile["cache-test"]
		assert.True(t, hasMapping)
	})
}
