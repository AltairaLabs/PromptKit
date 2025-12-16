package pipeline

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMCall_SetError(t *testing.T) {
	t.Run("sets error from error value", func(t *testing.T) {
		call := &LLMCall{}
		err := errors.New("test error")

		call.SetError(err)

		require.NotNil(t, call.Error)
		assert.Equal(t, "test error", *call.Error)
	})

	t.Run("clears error when nil", func(t *testing.T) {
		errMsg := "existing error"
		call := &LLMCall{Error: &errMsg}

		call.SetError(nil)

		assert.Nil(t, call.Error)
	})
}

func TestLLMCall_GetError(t *testing.T) {
	t.Run("returns error when set", func(t *testing.T) {
		errMsg := "test error"
		call := &LLMCall{Error: &errMsg}

		err := call.GetError()

		require.NotNil(t, err)
		assert.Equal(t, "test error", err.Error())
	})

	t.Run("returns nil when no error", func(t *testing.T) {
		call := &LLMCall{}

		err := call.GetError()

		assert.Nil(t, err)
	})
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Type:    "TestError",
		Details: "test details",
	}

	assert.Equal(t, "TestError: test details", err.Error())
}
