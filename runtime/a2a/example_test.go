package a2a_test

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
)

// ExampleMessageToMessage shows converting an A2A protocol Message into
// PromptKit's internal types.Message. This is a pure conversion — no
// network call or running A2A server required. The "agent" role maps to
// PromptKit's "assistant" role.
func ExampleMessageToMessage() {
	text := "hello from agent"
	msg := &a2a.Message{
		MessageID: "m1",
		Role:      a2a.RoleAgent,
		Parts:     []a2a.Part{{Text: &text}},
	}

	out, err := a2a.MessageToMessage(msg)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(out.Role, "->", out.GetContent())
	// Output: assistant -> hello from agent
}
