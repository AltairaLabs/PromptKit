package mock

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// InterruptResponse records that the caller stopped the reply numbered
// generation (providers.ResponseInterrupter). The mock emits nothing for it:
// a test scripts what the provider sends after a cancel.
func (m *MockStreamSession) InterruptResponse(_ context.Context, generation int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interrupts = append(m.interrupts, generation)
	return nil
}

// Interrupts returns how many times InterruptResponse was called.
func (m *MockStreamSession) Interrupts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.interrupts)
}

// InterruptedGenerations returns the generation each InterruptResponse call
// named, in order.
func (m *MockStreamSession) InterruptedGenerations() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int64(nil), m.interrupts...)
}

var _ providers.ResponseInterrupter = (*MockStreamSession)(nil)
