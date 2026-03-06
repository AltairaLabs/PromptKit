package sdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// transitionInternal is the shared transition logic used by both explicit
// Transition() and LLM-initiated transitions. Caller must hold wc.mu.
//
// This function calls Open() to create a new conversation for the target state,
// so it requires a valid pack file and is tested via integration tests.
func (wc *WorkflowConversation) transitionInternal(event, contextSummary string) (string, error) {
	fromState := wc.machine.CurrentState()

	if err := wc.machine.ProcessEvent(event); err != nil {
		return "", err
	}

	toState := wc.machine.CurrentState()

	// Close old conversation
	if wc.activeConv != nil {
		_ = wc.activeConv.Close()
	}

	// Build options, injecting context as a template variable if available
	opts := wc.opts
	if contextSummary != "" {
		opts = append(append([]Option{}, wc.opts...), WithVariables(map[string]string{
			"workflow_context": contextSummary,
		}))
	}

	// Open new conversation for the new state
	promptName := wc.machine.CurrentPromptTask()
	conv, err := Open(wc.packPath, promptName, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			toState, promptName, err)
	}
	wc.activeConv = conv

	// Register workflow tools for the new state
	wc.registerWorkflowTools()

	// Persist workflow context if state store is configured and state is not transient
	if wc.stateStore != nil && wc.workflowID != "" {
		wc.persistWorkflowContext()
	}

	// Emit transition event
	if wc.emitter != nil {
		wc.emitter.WorkflowTransitioned(fromState, toState, event, promptName)
		if wc.machine.IsTerminal() {
			wc.emitter.WorkflowCompleted(toState, wc.machine.Context().TransitionCount())
		}
	}

	return toState, nil
}

// maxSummaryContentLen is the max characters per message in a carry-forward summary.
const maxSummaryContentLen = 200

// buildContextSummary creates a text summary of the previous state's conversation
// for injection into the next state. It includes the state name, turn count,
// and the last few message exchanges.
//
// This function requires a live Conversation with an active session (calls conv.Messages),
// so it is tested via integration tests rather than unit tests.
func buildContextSummary(stateName string, conv *Conversation) string {
	ctx := context.Background()
	messages := conv.Messages(ctx)
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[Previous state: %s, %d messages]\n", stateName, len(messages))

	// Collect the last N non-system messages by iterating from the end,
	// avoiding a full filter + slice allocation for long histories.
	relevant := make([]types.Message, 0, defaultMaxSummaryMessages)
	for i := len(messages) - 1; i >= 0 && len(relevant) < defaultMaxSummaryMessages; i-- {
		if messages[i].Role != roleSystem {
			relevant = append(relevant, messages[i])
		}
	}
	// Reverse to restore chronological order
	for i, j := 0, len(relevant)-1; i < j; i, j = i+1, j-1 {
		relevant[i], relevant[j] = relevant[j], relevant[i]
	}

	for i := 0; i < len(relevant); i++ {
		content := extractMessageText(&relevant[i])
		if content == "" {
			continue
		}
		if len(content) > maxSummaryContentLen {
			content = content[:maxSummaryContentLen] + "..."
		}
		fmt.Fprintf(&sb, "%s: %s\n", relevant[i].Role, content)
	}

	return sb.String()
}
