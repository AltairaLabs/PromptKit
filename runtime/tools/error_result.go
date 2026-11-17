package tools

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ClassifyError is the class of a tool call's error: a validation error when
// err wraps a *ValidationError, an execution error otherwise.
func ClassifyError(err error) types.ToolErrorType {
	var valErr *ValidationError
	if errors.As(err, &valErr) {
		return types.ToolErrorValidation
	}
	return types.ToolErrorExecution
}

// ErrorResult is the tool result the model is given for call id of tool name
// that failed with err: "Error: <err>" as its text, err as its error, and
// err's class (ClassifyError). The provider stage answers a failed call with
// it, and the SDK answers a result a host settled outside the turn with it
// when the result cannot be prepared.
func ErrorResult(id, name string, err error) types.MessageToolResult {
	result := types.NewTextToolResult(id, name, fmt.Sprintf("Error: %v", err))
	result.Error = err.Error()
	result.ErrorType = ClassifyError(err)
	return result
}
