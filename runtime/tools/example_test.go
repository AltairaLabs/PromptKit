package tools_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// ExampleRegistry shows registering a tool descriptor and executing it via
// the static mock executor — no network or live backend required. This is
// the same registry/executor pair Arena uses to run assertions against
// canned tool responses.
func ExampleRegistry() {
	reg := tools.NewRegistry()

	_ = reg.Register(&tools.ToolDescriptor{
		Name:       "get_weather",
		Mode:       "mock",
		MockResult: json.RawMessage(`{"temp_f":72}`),
	})

	result, err := tools.NewMockStaticExecutor().Execute(context.Background(), reg.Get("get_weather"), nil)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(result))
	// Output: {"temp_f":72}
}
