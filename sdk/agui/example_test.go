package agui_test

import (
	"fmt"

	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/agui"
)

// ExampleMessageToAGUI converts a PromptKit Message to the AG-UI wire format
// used by AG-UI-compatible frontends.
func ExampleMessageToAGUI() {
	msg := types.NewTextMessage("user", "Hello!")
	aguiMsg := agui.MessageToAGUI(&msg)
	fmt.Println(aguiMsg.Role, "->", aguiMsg.Content)
	// Output: user -> Hello!
}

// ExampleMessageFromAGUI converts an AG-UI Message back to a PromptKit
// Message, the inverse of [agui.MessageToAGUI].
func ExampleMessageFromAGUI() {
	aguiMsg := &aguitypes.Message{
		Role:    aguitypes.RoleAssistant,
		Content: "Hi there!",
	}
	msg := agui.MessageFromAGUI(aguiMsg)
	fmt.Println(msg.Role, "->", msg.GetContent())
	// Output: assistant -> Hi there!
}
