package sdk

import (
	"context"
)

// transitionInternal handles explicit (caller-initiated) transitions.
// Calls ProcessEvent directly, then applies the transition.
// Caller must hold wc.mu.
func (wc *WorkflowConversation) transitionInternal(event, contextSummary string) (string, error) {
	result, err := wc.machine.ProcessEvent(event)
	if err != nil {
		return "", err
	}
	return wc.applyTransition(result, contextSummary)
}

// buildContextSummary creates a text summary of the previous state's conversation
// for injection into the next state. It includes the state name, turn count,
// and the last few message exchanges.
func buildContextSummary(stateName string, conv *Conversation) string {
	ctx := context.Background()
	messages := conv.Messages(ctx)
	return summarizeMessages(stateName, messages)
}
