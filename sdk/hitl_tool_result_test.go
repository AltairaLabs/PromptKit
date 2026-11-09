package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// orchLastToolResults returns every tool result in the last request the model
// was given.
func orchLastToolResults(p *recordingToolProvider) []types.MessageToolResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seen) == 0 {
		return nil
	}
	var out []types.MessageToolResult
	for _, m := range p.seen[len(p.seen)-1] {
		if m.ToolResult != nil {
			out = append(out, *m.ToolResult)
		}
	}
	return out
}

// orchAssistantCallID returns the ID of the single tool call in the last
// request's assistant message.
func orchAssistantCallID(t *testing.T, p *recordingToolProvider) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.seen)
	for _, m := range p.seen[len(p.seen)-1] {
		if m.Role == "assistant" && len(m.ToolCalls) == 1 {
			return m.ToolCalls[0].ID
		}
	}
	t.Fatal("no assistant message with one tool call in the last request")
	return ""
}
