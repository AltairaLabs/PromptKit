package sdk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// mcpToolCallProvider calls a named tool on its first round, then answers.
type mcpToolCallProvider struct {
	*toolGrantProvider
	toolName string
}

func newMCPToolCallProvider() *mcpToolCallProvider {
	return &mcpToolCallProvider{toolGrantProvider: newToolGrantProvider()}
}

func (p *mcpToolCallProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()

	if round == 1 {
		calls := []types.MessageToolCall{{ID: "m1", Name: p.toolName, Args: []byte(`{}`)}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// newRecordingMCPRegistry returns a registry serving one "ping" tool that
// records the tag of whichever registry actually handled the call.
func newRecordingMCPRegistry(tag string, hits *[]string) *mockMCPRegistry {
	reg := newMockMCPRegistry()
	_ = reg.RegisterServer(mcp.ServerConfig{Name: "srv", URL: "http://" + tag})
	reg.tools = map[string][]mcp.Tool{
		"srv": {{
			Name:        "ping",
			Description: "ping",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}
	reg.callFunc = func(string, json.RawMessage) (*mcp.ToolCallResponse, error) {
		*hits = append(*hits, tag)
		return &mcp.ToolCallResponse{Content: []mcp.Content{{Type: "text", Text: tag}}}, nil
	}
	return reg
}

// Two conversations sharing one tools.Registry must route their MCP tool calls
// through their OWN mcp.Registry.
//
// Each conversation builds its own mcp.Registry from its configured servers
// (initMCPRegistry), then registers a runtime MCPExecutor holding it. The tools
// registry keeps one executor per name, so the second conversation's executor
// -- and its server routing -- served the first as well. tools.WithMCPRegistry
// has existed for exactly this since the runtime side of it was written; the
// SDK simply never called it. See #2011.
func TestMCPRouting_DoesNotLeakAcrossASharedRegistry(t *testing.T) {
	shared := sharedPackRegistry(t)
	packPath := writeWorkflowTestPack(t, toolGrantPackJSON)

	var hits []string

	open := func(tag string) (*Conversation, *mcpToolCallProvider) {
		prov := newMCPToolCallProvider()
		conv, err := Open(packPath, "chat",
			WithProvider(prov),
			WithSkipSchemaValidation(),
			WithToolRegistry(shared),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		conv.mcpRegistry = newRecordingMCPRegistry(tag, &hits)
		conv.mcpExecutorsRegistered = false
		conv.registerMCPExecutors()
		return conv, prov
	}

	alice, aliceProv := open("alice")
	open("bob") // registers second, overwriting the "mcp" executor by name

	aliceProv.toolName = firstMCPTool(t, shared)

	_, err := alice.Send(context.Background(), "ping it")
	require.NoError(t, err)

	require.Len(t, hits, 1, "exactly one MCP registry should have handled the call")
	assert.Equal(t, "alice", hits[0],
		"alice's MCP call must route through alice's own server registry")
}

func firstMCPTool(t *testing.T, reg *tools.Registry) string {
	t.Helper()
	for _, name := range reg.List() {
		if strings.HasPrefix(name, "mcp__") {
			return name
		}
	}
	t.Fatalf("no mcp__ tool registered; registry holds %v", reg.List())
	return ""
}
