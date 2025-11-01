package main

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello, world!",
			expected: 3, // 13 chars / 4 = 3.25 -> 3
		},
		{
			name:     "longer text",
			text:     "This is a longer text with multiple words and punctuation.",
			expected: 14, // 59 chars / 4 = 14.75 -> 14
		},
		{
			name:     "exactly 4 characters",
			text:     "test",
			expected: 1,
		},
		{
			name:     "exactly 8 characters",
			text:     "testtext",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := estimateTokens(tt.text)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOutputPromptJSON(t *testing.T) {
	tests := []struct {
		name             string
		systemPrompt     string
		region           string
		taskType         string
		configFile       string
		variables        map[string]string
		promptPacksCount int
		providersCount   int
		scenariosCount   int
		expectError      bool
	}{
		{
			name:             "basic output",
			systemPrompt:     "You are a helpful assistant.",
			region:           "us-east-1",
			taskType:         "chat",
			configFile:       "config.yaml",
			variables:        map[string]string{"var1": "value1"},
			promptPacksCount: 2,
			providersCount:   3,
			scenariosCount:   5,
			expectError:      false,
		},
		{
			name:             "empty prompt",
			systemPrompt:     "",
			region:           "us-west-2",
			taskType:         "summarization",
			configFile:       "test.yaml",
			variables:        map[string]string{},
			promptPacksCount: 1,
			providersCount:   1,
			scenariosCount:   1,
			expectError:      false,
		},
		{
			name:             "nil variables",
			systemPrompt:     "Test prompt",
			region:           "eu-west-1",
			taskType:         "task1",
			configFile:       "arena.yaml",
			variables:        nil,
			promptPacksCount: 0,
			providersCount:   0,
			scenariosCount:   0,
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := outputPromptJSON(
				tt.systemPrompt,
				tt.region,
				tt.taskType,
				tt.configFile,
				tt.variables,
				tt.promptPacksCount,
				tt.providersCount,
				tt.scenariosCount,
			)

			// Restore stdout
			w.Close()
			os.Stdout = old

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Read captured output
				var buf bytes.Buffer
				_, _ = io.Copy(&buf, r) // Ignore error and bytes written in test
				output := buf.String()

				// Verify JSON contains expected fields
				assert.Contains(t, output, `"region"`)
				assert.Contains(t, output, `"task_type"`)
				assert.Contains(t, output, `"system_prompt"`)
				assert.Contains(t, output, tt.region)
				assert.Contains(t, output, tt.taskType)
			}
		})
	}
}
