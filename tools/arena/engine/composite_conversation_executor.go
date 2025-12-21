package engine

import (
	"context"
)

// CompositeConversationExecutor routes conversation execution to the appropriate
// executor based on scenario configuration. It selects between:
// - DefaultConversationExecutor for standard turn-based conversations
// - DuplexConversationExecutor for bidirectional streaming scenarios
type CompositeConversationExecutor struct {
	defaultExecutor *DefaultConversationExecutor
	duplexExecutor  *DuplexConversationExecutor
}

// NewCompositeConversationExecutor creates a new composite executor.
func NewCompositeConversationExecutor(
	defaultExecutor *DefaultConversationExecutor,
	duplexExecutor *DuplexConversationExecutor,
) *CompositeConversationExecutor {
	return &CompositeConversationExecutor{
		defaultExecutor: defaultExecutor,
		duplexExecutor:  duplexExecutor,
	}
}

// ExecuteConversation routes to the appropriate executor based on scenario config.
// If the scenario has duplex configuration, uses DuplexConversationExecutor.
// Otherwise, uses DefaultConversationExecutor.
//
//nolint:gocritic // hugeParam: req passed by value for interface compliance
func (ce *CompositeConversationExecutor) ExecuteConversation(
	ctx context.Context,
	req ConversationRequest,
) *ConversationResult {
	// Check if scenario requires duplex mode
	if ce.isDuplexScenario(&req) {
		if ce.duplexExecutor == nil {
			return &ConversationResult{
				Failed: true,
				Error:  "duplex executor not configured but scenario requires duplex mode",
			}
		}
		return ce.duplexExecutor.ExecuteConversation(ctx, req)
	}

	// Use default executor for standard scenarios
	if ce.defaultExecutor == nil {
		return &ConversationResult{
			Failed: true,
			Error:  "default executor not configured",
		}
	}
	return ce.defaultExecutor.ExecuteConversation(ctx, req)
}

// ExecuteConversationStream routes streaming execution to the appropriate executor.
//
//nolint:gocritic // hugeParam: req passed by value for interface compliance
func (ce *CompositeConversationExecutor) ExecuteConversationStream(
	ctx context.Context,
	req ConversationRequest,
) (<-chan ConversationStreamChunk, error) {
	// Check if scenario requires duplex mode
	if ce.isDuplexScenario(&req) {
		if ce.duplexExecutor == nil {
			errChan := make(chan ConversationStreamChunk, 1)
			errChan <- ConversationStreamChunk{
				Result: &ConversationResult{
					Failed: true,
					Error:  "duplex executor not configured but scenario requires duplex mode",
				},
			}
			close(errChan)
			return errChan, nil
		}
		return ce.duplexExecutor.ExecuteConversationStream(ctx, req)
	}

	// Use default executor for standard scenarios
	if ce.defaultExecutor == nil {
		errChan := make(chan ConversationStreamChunk, 1)
		errChan <- ConversationStreamChunk{
			Result: &ConversationResult{
				Failed: true,
				Error:  "default executor not configured",
			},
		}
		close(errChan)
		return errChan, nil
	}
	return ce.defaultExecutor.ExecuteConversationStream(ctx, req)
}

// isDuplexScenario checks if a scenario requires duplex mode execution.
func (ce *CompositeConversationExecutor) isDuplexScenario(req *ConversationRequest) bool {
	return req.Scenario != nil && req.Scenario.Duplex != nil
}

// GetDefaultExecutor returns the default executor for direct access if needed.
func (ce *CompositeConversationExecutor) GetDefaultExecutor() *DefaultConversationExecutor {
	return ce.defaultExecutor
}

// GetDuplexExecutor returns the duplex executor for direct access if needed.
func (ce *CompositeConversationExecutor) GetDuplexExecutor() *DuplexConversationExecutor {
	return ce.duplexExecutor
}
