package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// defaultAgentSendTimeout is the default timeout applied to member agent Send
// calls when the caller's context has no deadline.
const defaultAgentSendTimeout = 5 * time.Minute

// Execute routes a tool call to the corresponding member conversation.
// It parses {"query":"..."} from args, calls member.Send(), and returns {"response":"..."}.
func (e *LocalAgentExecutor) Execute(
	ctx context.Context,
	descriptor *tools.ToolDescriptor,
	args json.RawMessage,
) (json.RawMessage, error) {
	// Parse query from args
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("failed to parse agent tool args: %w", err)
	}

	// Find the member conversation by tool name (which is the agent name)
	conv, ok := e.members[descriptor.Name]
	if !ok {
		return nil, fmt.Errorf("unknown agent member: %s", descriptor.Name)
	}

	// If the caller's context has no deadline, wrap with a default timeout
	// to prevent member agent calls from hanging indefinitely.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultAgentSendTimeout)
		defer cancel()
	}

	// Send the query to the member conversation.
	resp, err := conv.Send(ctx, input.Query)
	if err != nil {
		return nil, fmt.Errorf("agent %s failed: %w", descriptor.Name, err)
	}

	// Return the response
	result := map[string]string{"response": resp.Text()}
	return json.Marshal(result)
}
