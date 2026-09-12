package streaming

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// ExampleProcessResponseElement shows the response state machine deciding
// what to do with a completed, non-tool-call stream element: an
// EndOfStream element carrying a message with content is a finished turn.
func ExampleProcessResponseElement() {
	elem := &stage.StreamElement{
		EndOfStream: true,
		Message: &types.Message{
			Role:    "assistant",
			Content: "Hello there!",
		},
	}

	action, err := ProcessResponseElement(elem, "Example")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(action)
	// Output: complete
}
