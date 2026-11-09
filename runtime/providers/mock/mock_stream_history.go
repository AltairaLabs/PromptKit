package mock

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// SeedHistory records the prior conversation the session was given, reduced
// the way every realtime session replays it (providers.HistoryTurns).
func (m *MockStreamSession) SeedHistory(_ context.Context, messages []types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeCalled {
		return errors.New(errSessionClosed)
	}
	m.history = append(m.history, providers.HistoryTurns(messages)...)
	return nil
}

// GetHistory returns the prior-conversation turns SeedHistory recorded.
func (m *MockStreamSession) GetHistory() []providers.HistoryTurn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]providers.HistoryTurn(nil), m.history...)
}

// SupportsStreamHistory reports that mock realtime sessions take prior
// conversation.
func (p *StreamingProvider) SupportsStreamHistory() bool { return true }

var (
	_ providers.StreamHistorySession = (*MockStreamSession)(nil)
	_ providers.StreamHistorySupport = (*StreamingProvider)(nil)
)
