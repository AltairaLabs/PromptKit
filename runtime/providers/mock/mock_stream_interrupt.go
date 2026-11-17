package mock

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// InterruptResponse records that the caller stopped the reply in progress
// (providers.ResponseInterrupter). The mock emits nothing for it: a test
// scripts what the provider sends after a cancel.
func (m *MockStreamSession) InterruptResponse(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interrupts++
	return nil
}

// Interrupts returns how many times InterruptResponse was called.
func (m *MockStreamSession) Interrupts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.interrupts
}

var _ providers.ResponseInterrupter = (*MockStreamSession)(nil)
