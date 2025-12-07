package sdk

import (
"errors"
"testing"

"github.com/stretchr/testify/assert"
)

func TestValidationError(t *testing.T) {
	t.Run("Error method", func(t *testing.T) {
ve := &ValidationError{
			ValidatorType: "banned_words",
			Message:       "contains prohibited content",
		}
		errStr := ve.Error()
		assert.Contains(t, errStr, "banned_words")
		assert.Contains(t, errStr, "contains prohibited content")
	})

	t.Run("with details", func(t *testing.T) {
ve := &ValidationError{
			ValidatorType: "length",
			Message:       "too long",
			Details: map[string]any{
				"maxLength": 100,
				"actual":    150,
			},
		}
		assert.NotNil(t, ve.Details)
		assert.Equal(t, 100, ve.Details["maxLength"])
	})

	t.Run("AsValidationError with ValidationError", func(t *testing.T) {
ve := &ValidationError{ValidatorType: "test"}
		result, ok := AsValidationError(ve)
		assert.True(t, ok)
		assert.Equal(t, ve, result)
	})

	t.Run("AsValidationError with non-ValidationError", func(t *testing.T) {
err := errors.New("regular error")
result, ok := AsValidationError(err)
assert.False(t, ok)
assert.Nil(t, result)
})

	t.Run("AsValidationError with nil", func(t *testing.T) {
result, ok := AsValidationError(nil)
assert.False(t, ok)
assert.Nil(t, result)
})
}

func TestPackError(t *testing.T) {
	t.Run("Error method", func(t *testing.T) {
cause := errors.New("file not found")
pe := &PackError{
			Path:  "/path/to/pack.yaml",
			Cause: cause,
		}
		errStr := pe.Error()
		assert.Contains(t, errStr, "/path/to/pack.yaml")
		assert.Contains(t, errStr, "file not found")
	})

	t.Run("Unwrap method", func(t *testing.T) {
cause := errors.New("file not found")
pe := &PackError{
			Path:  "/path/to/pack.yaml",
			Cause: cause,
		}
		assert.Equal(t, cause, pe.Unwrap())
	})
}

func TestProviderError(t *testing.T) {
	t.Run("Error method without status code", func(t *testing.T) {
pe := &ProviderError{
			Provider: "openai",
			Message:  "rate limit exceeded",
		}
		errStr := pe.Error()
		assert.Contains(t, errStr, "openai")
		assert.Contains(t, errStr, "rate limit exceeded")
	})

	t.Run("Error method with status code", func(t *testing.T) {
pe := &ProviderError{
			Provider:   "anthropic",
			StatusCode: 429,
			Message:    "too many requests",
		}
		errStr := pe.Error()
		assert.Contains(t, errStr, "anthropic")
		assert.Contains(t, errStr, "too many requests")
	})

	t.Run("Unwrap method", func(t *testing.T) {
cause := errors.New("connection timeout")
pe := &ProviderError{
			Provider: "gemini",
			Message:  "failed",
			Cause:    cause,
		}
		assert.Equal(t, cause, pe.Unwrap())
	})
}

func TestToolError(t *testing.T) {
	t.Run("Error method", func(t *testing.T) {
cause := errors.New("connection timeout")
te := &ToolError{
			ToolName: "get_weather",
			Cause:    cause,
		}
		errStr := te.Error()
		assert.Contains(t, errStr, "get_weather")
		assert.Contains(t, errStr, "connection timeout")
	})

	t.Run("Unwrap method", func(t *testing.T) {
cause := errors.New("timeout")
te := &ToolError{
			ToolName: "get_weather",
			Cause:    cause,
		}
		assert.Equal(t, cause, te.Unwrap())
	})
}

func TestSentinelErrors(t *testing.T) {
	// Test that sentinel errors are defined and unique
	sentinelErrors := []error{
		ErrConversationClosed,
		ErrConversationNotFound,
		ErrNoStateStore,
		ErrPromptNotFound,
		ErrPackNotFound,
		ErrProviderNotDetected,
		ErrToolNotRegistered,
		ErrToolNotInPack,
	}

	for i, err := range sentinelErrors {
		assert.NotNil(t, err, "sentinel error %d should not be nil", i)
		assert.NotEmpty(t, err.Error(), "sentinel error %d should have a message", i)
	}

	// Ensure they are distinct
	seen := make(map[string]bool)
	for _, err := range sentinelErrors {
		msg := err.Error()
		assert.False(t, seen[msg], "duplicate error message: %s", msg)
		seen[msg] = true
	}
}
