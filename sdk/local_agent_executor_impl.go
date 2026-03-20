package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// resolveMemberName extracts the bare member name from a potentially qualified
// tool descriptor name. For example, "a2a__worker" → "worker".
func resolveMemberName(descriptorName string) string {
	_, local := tools.ParseToolName(descriptorName)
	return local
}

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

	// Find the member conversation by bare agent name.
	// Tool descriptors use qualified names (e.g. "a2a__worker"), but the
	// members map is keyed by bare name (e.g. "worker").
	memberName := resolveMemberName(descriptor.Name)
	conv, ok := e.members[memberName]
	if !ok {
		return nil, fmt.Errorf("unknown agent member: %s (resolved from %s)", memberName, descriptor.Name)
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
		return nil, fmt.Errorf("agent %s failed: %w", memberName, err)
	}

	// Return the response
	result := map[string]string{"response": resp.Text()}
	return json.Marshal(result)
}
