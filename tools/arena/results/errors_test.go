package results_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/tools/arena/results"
)

func TestCompositeError(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		err := results.NewCompositeError("TestOp", []error{errors.New("single error")})

		assert.Equal(t, "TestOp: single error", err.Error())
		assert.Equal(t, "TestOp", err.Operation)
		assert.Len(t, err.Errors, 1)

		// Test Unwrap
		unwrapped := err.Unwrap()
		assert.Error(t, unwrapped)
		assert.Equal(t, "single error", unwrapped.Error())
	})

	t.Run("multiple errors", func(t *testing.T) {
		errs := []error{
			errors.New("first error"),
			errors.New("second error"),
			errors.New("third error"),
		}
		err := results.NewCompositeError("TestOp", errs)

		errorMsg := err.Error()
		assert.Contains(t, errorMsg, "TestOp: multiple errors:")
		assert.Contains(t, errorMsg, "first error")
		assert.Contains(t, errorMsg, "second error")
		assert.Contains(t, errorMsg, "third error")

		// Test Unwrap returns first error
		unwrapped := err.Unwrap()
		assert.Equal(t, "first error", unwrapped.Error())
	})

	t.Run("no errors", func(t *testing.T) {
		err := results.NewCompositeError("TestOp", []error{})

		assert.Equal(t, "TestOp: no errors", err.Error())
		assert.Nil(t, err.Unwrap())
	})
}

func TestUnsupportedOperationError(t *testing.T) {
	err := results.NewUnsupportedOperationError("LoadResults", "repository doesn't support loading")

	assert.Equal(t, "operation LoadResults not supported: repository doesn't support loading", err.Error())
	assert.Equal(t, "LoadResults", err.Operation)
	assert.Equal(t, "repository doesn't support loading", err.Reason)
}

func TestIsUnsupportedOperation(t *testing.T) {
	t.Run("is unsupported operation error", func(t *testing.T) {
		err := results.NewUnsupportedOperationError("SaveResult", "not supported")
		assert.True(t, results.IsUnsupportedOperation(err))
	})

	t.Run("is not unsupported operation error", func(t *testing.T) {
		err := errors.New("regular error")
		assert.False(t, results.IsUnsupportedOperation(err))
	})

	t.Run("wrapped unsupported operation error", func(t *testing.T) {
		unsupportedErr := results.NewUnsupportedOperationError("SaveResult", "not supported")
		wrappedErr := errors.Join(errors.New("wrapper"), unsupportedErr)
		assert.True(t, results.IsUnsupportedOperation(wrappedErr))
	})
}

func TestValidationError(t *testing.T) {
	err := results.NewValidationError("TestField", "test-value", "field is required")

	expectedMsg := "validation failed for field TestField (value: test-value): field is required"
	assert.Equal(t, expectedMsg, err.Error())
	assert.Equal(t, "TestField", err.Field)
	assert.Equal(t, "test-value", err.Value)
	assert.Equal(t, "field is required", err.Message)
}

func TestValidationError_WithComplexValue(t *testing.T) {
	complexValue := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
	}

	err := results.NewValidationError("ComplexField", complexValue, "invalid structure")

	assert.Contains(t, err.Error(), "ComplexField")
	assert.Contains(t, err.Error(), "invalid structure")
	assert.Equal(t, complexValue, err.Value)
}