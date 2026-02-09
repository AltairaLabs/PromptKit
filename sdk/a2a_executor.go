package sdk

import "github.com/AltairaLabs/PromptKit/runtime/a2a"

// newA2AExecutor returns the shared A2A executor from runtime/a2a.
func newA2AExecutor() *a2a.Executor {
	return a2a.NewExecutor()
}
