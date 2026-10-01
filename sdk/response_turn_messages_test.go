package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// lookupThenAnswerProvider calls lookup_order once per turn, then answers.
type lookupThenAnswerProvider struct {
	*mock.ToolProvider
	mu    sync.Mutex
	calls int
}

func (p *lookupThenAnswerProvider) SupportsStreaming() bool { return false }

func (p *lookupThenAnswerProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if last := req.Messages[len(req.Messages)-1]; last.Role == "tool" {
		return providers.PredictionResponse{Content: "Found it."}, nil, nil
	}
	p.mu.Lock()
	p.calls++
	id := []string{"", "call-a", "call-b"}[p.calls]
	p.mu.Unlock()
	calls := []types.MessageToolCall{{ID: id, Name: "lookup_order", Args: []byte(`{"id":"1"}`)}}
	return providers.PredictionResponse{Content: "Let me look.", ToolCalls: calls}, calls, nil
}

// TurnMessages holds every round of the turn — the first assistant message
// and its call are invisible to Text() and ToolCalls() — and none of the
// history the turn was run with.
func TestResponse_TurnMessages(t *testing.T) {
	provider := &lookupThenAnswerProvider{ToolProvider: mock.NewToolProvider("x", "m", false, nil)}
	conv, err := Open(writeWorkflowTestPack(t, toolPolicyTestPack(`{"max_rounds": 5}`)), "chat",
		WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup_order", func(map[string]any) (any, error) { return "shipped", nil })

	_, err = conv.Send(context.Background(), "first")
	require.NoError(t, err)
	resp, err := conv.Send(context.Background(), "second")
	require.NoError(t, err)

	assert.Empty(t, resp.ToolCalls(), "the final round made no calls")
	msgs := resp.TurnMessages()
	require.Len(t, msgs, 4, "user, assistant with call, tool result, assistant; no history")
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "second", msgs[0].GetContent())
	assert.Equal(t, "Let me look.", msgs[1].Content)
	require.Len(t, msgs[1].ToolCalls, 1)
	assert.Equal(t, "call-b", msgs[1].ToolCalls[0].ID)
	require.NotNil(t, msgs[2].ToolResult)
	assert.Equal(t, "call-b", msgs[2].ToolResult.ID)
	assert.Equal(t, "Found it.", msgs[3].Content)
}

func TestTurnMessagesOf_SkipsLoadedContext(t *testing.T) {
	msgs := []types.Message{
		{Role: "user", Content: "old", Source: "statestore"},
		{Role: "system", Content: "summary", Source: "summary"},
		{Role: "user", Content: "fact", Source: "retrieved"},
		{Role: "user", Content: "new"},
		{Role: "assistant", Content: "reply", Source: "pipeline"},
	}

	got := turnMessagesOf(msgs)

	require.Len(t, got, 2)
	assert.Equal(t, "new", got[0].Content)
	assert.Equal(t, "reply", got[1].Content)
	assert.Nil(t, turnMessagesOf(nil))
}

func TestResponseTestOptions(t *testing.T) {
	turn := []types.Message{{Role: "assistant", Content: "hi"}}
	held := []PendingTool{{ID: "c1", Name: "send"}}

	r := NewResponseForTest("hi", nil, WithTurnMessagesForTest(turn), WithPendingToolsForTest(held))

	assert.Equal(t, turn, r.TurnMessages())
	assert.Equal(t, held, r.PendingTools())
}
