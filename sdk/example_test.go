package sdk_test

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// ExampleAsValidationError shows unwrapping a validation failure returned by
// [sdk.Conversation.Send] when a pack-defined validator rejects a response.
// AsValidationError works on any error chain, so it is demonstrated here
// against a manually constructed error rather than a live Send call.
func ExampleAsValidationError() {
	err := fmt.Errorf("turn failed: %w", &sdk.ValidationError{
		ValidatorType: "banned_words",
		Message:       "response contained a banned word",
	})

	if vErr, ok := sdk.AsValidationError(err); ok {
		fmt.Println(vErr.ValidatorType, "-", vErr.Message)
	}

	var other error = errors.New("not a validation error")
	_, ok := sdk.AsValidationError(other)
	fmt.Println("matched:", ok)

	// Output:
	// banned_words - response contained a banned word
	// matched: false
}
