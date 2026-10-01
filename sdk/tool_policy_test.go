package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover #2104: the SDK never applied a prompt's tool_policy, so
// max_rounds always fell back to 50 and blocklist / tool_choice were inert.
// Each test opens a conversation from a pack and observes the provider.

// alwaysCallProvider calls lookup_order on every round it is offered tools,
// with fresh arguments so the identical-call breaker never fires; offered none,
// it answers. Only a round limit or a spent call budget stops it.
type alwaysCallProvider struct {
	*mock.ToolProvider

	mu     sync.Mutex
	rounds int // PredictWithTools calls, i.e. rounds offered tools
}

func newAlwaysCallProvider() *alwaysCallProvider {
	return &alwaysCallProvider{ToolProvider: mock.NewToolProvider("loop", "loop-model", false, nil)}
}

// Pin to the unary tool loop for determinism.
func (p *alwaysCallProvider) SupportsStreaming() bool { return false }

func (p *alwaysCallProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, tools providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if tools == nil {
		return providers.PredictionResponse{Content: "here is your answer"}, nil, nil
	}
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()
	calls := []types.MessageToolCall{{
		ID: fmt.Sprintf("call-%d", round), Name: "lookup_order", Args: []byte(fmt.Sprintf(`{"id":"ORD-%d"}`, round)),
	}}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func (p *alwaysCallProvider) roundsSeen() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rounds
}

func toolPolicyTestPack(policy string) string {
	return `{
	"id": "tool-policy-test",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a support agent.",
			"tools": ["lookup_order"],
			"tool_policy": ` + policy + `
		}
	},
	"tools": {
		"lookup_order": {
			"name": "lookup_order",
			"description": "Look up an order",
			"mode": "local",
			"parameters": {"type": "object", "properties": {"id": {"type": "string"}}}
		}
	}
}`
}

func openToolPolicyConv(t *testing.T, policy string, provider providers.Provider) (*Conversation, *int) {
	t.Helper()
	conv, err := Open(writeWorkflowTestPack(t, toolPolicyTestPack(policy)), "chat",
		WithProvider(provider),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	var mu sync.Mutex
	executed := 0
	conv.OnTool("lookup_order", func(map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		executed++
		return "ok", nil
	})
	return conv, &executed
}

func TestToolPolicy_PackMaxRoundsBoundsTheTurn(t *testing.T) {
	provider := newAlwaysCallProvider()
	conv, _ := openToolPolicyConv(t, `{"max_rounds": 3}`, provider)

	_, err := conv.Send(context.Background(), "where is my order?")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max rounds (3) exceeded")
	assert.Equal(t, 3, provider.roundsSeen(), "the pack's max_rounds, not the default 50")
}

func TestToolPolicy_PackBlocklistStopsExecution(t *testing.T) {
	provider := newAlwaysCallProvider()
	conv, executed := openToolPolicyConv(t, `{"max_rounds": 2, "blocklist": ["lookup_order"]}`, provider)

	_, _ = conv.Send(context.Background(), "where is my order?")
	assert.Equal(t, 2, provider.roundsSeen())
	assert.Zero(t, *executed, "a blocklisted tool is never run")
}

func TestToolPolicy_PackMaxToolCallsPerTurn(t *testing.T) {
	provider := newAlwaysCallProvider()
	conv, executed := openToolPolicyConv(t, `{"max_rounds": 5, "max_tool_calls_per_turn": 2}`, provider)

	_, err := conv.Send(context.Background(), "where is my order?")
	require.NoError(t, err, "a spent budget ends the turn with an answer, not a max-rounds error")
	assert.Equal(t, 2, *executed, "calls beyond max_tool_calls_per_turn are not run")
	assert.Equal(t, 2, provider.roundsSeen(), "once the budget is spent the next round is offered no tools")
}
