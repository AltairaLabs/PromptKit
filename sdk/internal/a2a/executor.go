// Package a2a provides the A2A task store and executor for the SDK.
package a2a

import rta2a "github.com/AltairaLabs/PromptKit/runtime/v2/a2a"

// NewExecutor returns the shared A2A executor from runtime/a2a.
func NewExecutor() *rta2a.Executor {
	return rta2a.NewExecutor()
}
