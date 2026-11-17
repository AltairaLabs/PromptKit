package tools

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestErrorResult(t *testing.T) {
	got := ErrorResult("c1", "lookup", errors.New("backend down"))
	want := types.NewTextToolResult("c1", "lookup", "Error: backend down")
	want.Error = "backend down"
	want.ErrorType = types.ToolErrorExecution
	assert.Equal(t, want, got)
}

func TestClassifyError(t *testing.T) {
	assert.Equal(t, types.ToolErrorExecution, ClassifyError(errors.New("boom")))
	wrapped := fmt.Errorf("calling: %w", &ValidationError{Type: "args_invalid", Tool: "lookup"})
	assert.Equal(t, types.ToolErrorValidation, ClassifyError(wrapped))
}
