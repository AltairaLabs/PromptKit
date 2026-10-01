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
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// roundsProvider answers each model call with the next scripted round and
// records what each call was given.
type roundsProvider struct {
	*mock.ToolProvider
	mu     sync.Mutex
	rounds []providers.PredictionResponse
	seen   [][]types.Message
}

func (p *roundsProvider) SupportsStreaming() bool { return false }

func (p *roundsProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
	if len(p.rounds) == 0 {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	r := p.rounds[0]
	p.rounds = p.rounds[1:]
	return r, r.ToolCalls, nil
}

const twoToolPackJSON = `{
	"id": "resume-answers", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You help.", "tools": ["lookup", "locate"]}},
	"tools": {
		"lookup": {"name": "lookup", "description": "Server tool",
			"parameters": {"type": "object", "properties": {}}},
		"locate": {"name": "locate", "description": "Client tool",
			"parameters": {"type": "object", "properties": {}}}
	}
}`

func resultCounts(msgs []types.Message) map[string]int {
	counts := map[string]int{}
	for i := range msgs {
		if msgs[i].ToolResult != nil {
			counts[msgs[i].ToolResult.ID]++
		}
	}
	return counts
}

// A round that called a server tool and a client tool: the server tool's
// result is already in the history when the turn suspends. An answer for it
// supplied again on resume (as an AG-UI input carries one) must not reach the
// model a second time; providers reject a call answered twice.
func TestResume_SkipsCallsTheHistoryAlreadyAnswers(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "c-srv", Name: "lookup", Args: []byte(`{}`)},
		{ID: "c-cli", Name: "locate", Args: []byte(`{}`)},
	}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds:       []providers.PredictionResponse{{Content: "Checking.", ToolCalls: calls}},
	}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })
	desc, err := conv.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"

	resp, err := conv.Send(context.Background(), "go")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())

	ctx := context.Background()
	require.NoError(t, conv.SendToolResult(ctx, "c-srv", "shipped"))
	require.NoError(t, conv.SendToolResult(ctx, "c-cli", "Paris"))
	_, err = conv.Resume(ctx)
	require.NoError(t, err)

	last := provider.seen[len(provider.seen)-1]
	assert.Equal(t, map[string]int{"c-srv": 1, "c-cli": 1}, resultCounts(last))
}

// Two resolutions for one call keep the first.
func TestBuildToolResultMessages_OneAnswerPerCall(t *testing.T) {
	conv := newTestConversation()
	conv.resolvedStore = sdktools.NewResolvedStore()
	ctx := context.Background()
	require.NoError(t, conv.SendToolResult(ctx, "c1", "first"))
	require.NoError(t, conv.SendToolResult(ctx, "c1", "second"))
	require.NoError(t, conv.SendToolResult(ctx, "c2", "other"))

	msgs, err := conv.buildToolResultMessages(ctx)
	require.NoError(t, err)

	require.Len(t, msgs, 2)
	assert.Equal(t, "c1", msgs[0].ToolResult.ID)
	assert.Equal(t, `"first"`, msgs[0].ToolResult.GetTextContent())
	assert.Equal(t, "c2", msgs[1].ToolResult.ID)
}

// Continue feeds each resolved call to the model in turn. TurnMessages must
// hold all of them and the replies between, not only the last execution's.
func TestContinue_TurnMessagesCoverEveryResolvedCall(t *testing.T) {
	calls := []types.MessageToolCall{
		{ID: "h1", Name: "lookup", Args: []byte(`{}`)},
		{ID: "h2", Name: "lookup", Args: []byte(`{}`)},
	}
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds: []providers.PredictionResponse{
			{Content: "Both.", ToolCalls: calls},
			{Content: "First done."},
			{Content: "Both done."},
		},
	}
	conv, err := Open(writeWorkflowTestPack(t, twoToolPackJSON), "chat", WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.OnToolAsync("lookup",
		func(map[string]any) sdktools.PendingResult { return sdktools.PendingResult{Reason: "approve"} },
		func(map[string]any) (any, error) { return "ok", nil })

	ctx := context.Background()
	resp, err := conv.Send(ctx, "go")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 2)
	for _, pt := range resp.PendingTools() {
		_, err = conv.ResolveTool(ctx, pt.ID)
		require.NoError(t, err)
	}

	resp, err = conv.Continue(ctx)
	require.NoError(t, err)

	assert.Equal(t, map[string]int{"h1": 1, "h2": 1}, resultCounts(resp.TurnMessages()))
	var replies []string
	for _, m := range resp.TurnMessages() {
		if m.Role == roleAssistant {
			replies = append(replies, m.Content)
		}
	}
	assert.Equal(t, []string{"First done.", "Both done."}, replies)
}
