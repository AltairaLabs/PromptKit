package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetWrapperKeys(t *testing.T) {
	tests := []struct {
		name     string
		wrappers map[string]interface{}
		expected int // We just check the count since map iteration order is not guaranteed
	}{
		{
			name:     "empty map",
			wrappers: map[string]interface{}{},
			expected: 0,
		},
		{
			name: "single wrapper",
			wrappers: map[string]interface{}{
				"wrapper1": "value1",
			},
			expected: 1,
		},
		{
			name: "multiple wrappers",
			wrappers: map[string]interface{}{
				"wrapper1": "value1",
				"wrapper2": "value2",
				"wrapper3": "value3",
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getWrapperKeys(tt.wrappers)
			assert.Len(t, result, tt.expected)
			// Verify all keys are present
			for key := range tt.wrappers {
				assert.Contains(t, result, key)
			}
		})
	}
}

func TestGetConstraintKeys(t *testing.T) {
	tests := []struct {
		name        string
		constraints map[string]interface{}
		expected    int
	}{
		{
			name:        "empty map",
			constraints: map[string]interface{}{},
			expected:    0,
		},
		{
			name: "single constraint",
			constraints: map[string]interface{}{
				"max_length": 100,
			},
			expected: 1,
		},
		{
			name: "multiple constraints",
			constraints: map[string]interface{}{
				"max_length":   100,
				"min_length":   10,
				"required":     true,
				"pattern":      "^[a-z]+$",
				"custom_check": map[string]string{"type": "custom"},
			},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getConstraintKeys(tt.constraints)
			assert.Len(t, result, tt.expected)
			// Verify all keys are present
			for key := range tt.constraints {
				assert.Contains(t, result, key)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "string shorter than max length",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "string exactly at max length",
			input:    "hello world",
			maxLen:   11,
			expected: "hello world",
		},
		{
			name:     "string longer than max length",
			input:    "this is a very long string that needs to be truncated",
			maxLen:   20,
			expected: "this is a very lo...",
		},
		{
			name:     "max length of 5",
			input:    "hello world",
			maxLen:   5,
			expected: "he...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), tt.maxLen)
		})
	}
}
