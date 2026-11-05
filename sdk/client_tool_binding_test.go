package sdk

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The pack declares get_location with no execution mode: a tool's mode is a
// hosting concern, so the pack loader registers it as "local" and the host
// binds it. These tests bind it with OnClientTool / DeferClientTool only.
const bindingPackJSON = `{
	"id": "client-binding", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You help.", "tools": ["get_location"]}},
	"tools": {
		"get_location": {"name": "get_location", "description": "Device GPS",
			"parameters": {"type": "object", "properties": {"accuracy": {"type": "string"}}}}
	}
}`

const bindingFinalText = "You are in the city you told me."

// locationScript calls get_location on the first round and answers in text
// once a tool result is in the history.
type locationScript struct{}

func (locationScript) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	turn, err := locationScript{}.GetTurn(ctx, p)
	return turn.Content, err
}

func (locationScript) GetTurn(_ context.Context, p mock.ResponseParams) (*mock.Turn, error) {
	if p.TurnNumber <= 1 {
		return &mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{
			{Name: "get_location", Arguments: map[string]any{"accuracy": "high"}},
		}}, nil
	}
	return &mock.Turn{Type: "text", Content: bindingFinalText}, nil
}

// recordingToolProvider is a mock that actually calls the tool, and records
// every request so a test can see the tool result the model was given.
type recordingToolProvider struct {
	*mock.ToolProvider
	mu   sync.Mutex
	seen [][]types.Message
}

func newRecordingToolProvider() *recordingToolProvider {
	return &recordingToolProvider{
		ToolProvider: mock.NewToolProviderWithRepository("rec", "rec-model", false, locationScript{}),
	}
}

func (p *recordingToolProvider) record(req providers.PredictionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, append([]types.Message(nil), req.Messages...))
}

func (p *recordingToolProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, t providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.record(req)
	return p.ToolProvider.PredictWithTools(ctx, req, t, choice)
}

func (p *recordingToolProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, t interface{}, choice string,
) (<-chan providers.StreamChunk, error) {
	p.record(req)
	return p.ToolProvider.PredictStreamWithTools(ctx, req, t, choice)
}

// toolResults returns the text of every tool result the model was last given.
func (p *recordingToolProvider) toolResults() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seen) == 0 {
		return nil
	}
	var out []string
	for _, m := range p.seen[len(p.seen)-1] {
		if m.ToolResult != nil {
			out = append(out, m.ToolResult.GetTextContent())
		}
	}
	return out
}

// assertToolResultJSON asserts the model was last given exactly one tool
// result, equal to want as JSON.
func assertToolResultJSON(t *testing.T, want string, p *recordingToolProvider) {
	t.Helper()
	got := p.toolResults()
	require.Len(t, got, 1, "the model must be given the tool's result")
	assert.JSONEq(t, want, got[0])
}

// recordingClientHandler records every call it serves.
type recordingClientHandler struct {
	mu   sync.Mutex
	reqs []ClientToolRequest
}

func (h *recordingClientHandler) handle(_ context.Context, req ClientToolRequest) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs = append(h.reqs, req)
	return map[string]any{"city": "New York"}, nil
}

func (h *recordingClientHandler) calls() []ClientToolRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ClientToolRequest(nil), h.reqs...)
}

func openBindingConv(t *testing.T, opts ...Option) (*Conversation, *recordingToolProvider) {
	t.Helper()
	provider := newRecordingToolProvider()
	opts = append([]Option{WithProvider(provider), WithSkipSchemaValidation()}, opts...)
	conv, err := Open(writeWorkflowTestPack(t, bindingPackJSON), "chat", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, provider
}

func assertHandlerServedCall(t *testing.T, h *recordingClientHandler) {
	t.Helper()
	calls := h.calls()
	require.Len(t, calls, 1, "the OnClientTool handler must serve the model's call")
	assert.Equal(t, "get_location", calls[0].ToolName)
	assert.Equal(t, "high", calls[0].Args["accuracy"])
	assert.NotEmpty(t, calls[0].CallID)
	require.NotNil(t, calls[0].Descriptor)
	assert.Equal(t, "client", calls[0].Descriptor.Mode, "the handler sees the tool as a client tool")
}

func TestOnClientTool_BindsPackTool_Sync(t *testing.T) {
	conv, provider := openBindingConv(t)
	h := &recordingClientHandler{}
	conv.OnClientTool("get_location", h.handle)

	resp, err := conv.Send(context.Background(), "Where am I?")
	require.NoError(t, err)

	assertHandlerServedCall(t, h)
	assert.False(t, resp.HasPendingClientTools())
	assert.Equal(t, bindingFinalText, resp.Text())
	assertToolResultJSON(t, `{"city":"New York"}`, provider)
}

func TestOnClientTool_BindsPackTool_Stream(t *testing.T) {
	conv, provider := openBindingConv(t)
	h := &recordingClientHandler{}
	conv.OnClientTool("get_location", h.handle)

	var text strings.Builder
	for chunk := range conv.Stream(context.Background(), "Where am I?") {
		require.NoError(t, chunk.Error)
		require.NotEqual(t, ChunkClientTool, chunk.Type, "a bound handler serves the call; nothing is deferred")
		if chunk.Type == ChunkText {
			text.WriteString(chunk.Text)
		}
	}

	assertHandlerServedCall(t, h)
	assert.Equal(t, bindingFinalText, text.String())
	assertToolResultJSON(t, `{"city":"New York"}`, provider)
}

func TestDeferClientTool_SuspendsAndResumes(t *testing.T) {
	conv, provider := openBindingConv(t)
	conv.DeferClientTool("get_location")
	ctx := context.Background()

	resp, err := conv.Send(ctx, "Where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools(), "a deferred client tool suspends the turn")
	pending := resp.ClientTools()
	require.Len(t, pending, 1)
	assert.Equal(t, "get_location", pending[0].ToolName)
	assert.Equal(t, "high", pending[0].Args["accuracy"])

	require.NoError(t, conv.SendToolResult(ctx, pending[0].CallID, map[string]any{"city": "Paris"}))
	resp, err = conv.Resume(ctx)
	require.NoError(t, err)
	assert.Equal(t, bindingFinalText, resp.Text())
	assertToolResultJSON(t, `{"city":"Paris"}`, provider)
}

func TestDeferClientTool_StreamSuspendsAndResumes(t *testing.T) {
	conv, provider := openBindingConv(t)
	conv.DeferClientTool("get_location")
	ctx := context.Background()

	var pending *PendingClientTool
	for chunk := range conv.Stream(ctx, "Where am I?") {
		require.NoError(t, chunk.Error)
		if chunk.Type == ChunkClientTool {
			pending = chunk.ClientTool
		}
	}
	require.NotNil(t, pending, "a deferred client tool surfaces as a ChunkClientTool")
	assert.Equal(t, "get_location", pending.ToolName)

	require.NoError(t, conv.SendToolResult(ctx, pending.CallID, map[string]any{"city": "London"}))
	var text strings.Builder
	for chunk := range conv.ResumeStream(ctx) {
		require.NoError(t, chunk.Error)
		if chunk.Type == ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	assert.Equal(t, bindingFinalText, text.String())
	assertToolResultJSON(t, `{"city":"London"}`, provider)
}

// A nil handler is a deferred binding, not a panic at call time.
func TestOnClientTool_NilHandlerDefers(t *testing.T) {
	conv, _ := openBindingConv(t)
	conv.OnClientTool("get_location", nil)

	resp, err := conv.Send(context.Background(), "Where am I?")
	require.NoError(t, err)
	assert.True(t, resp.HasPendingClientTools())
}

// The binding is per conversation. Two conversations over one host registry
// share its get_location descriptor; binding it as a client tool in one must
// leave the other's local handler serving its calls, and must not rewrite the
// shared descriptor.
func TestOnClientTool_BindingIsPerConversation(t *testing.T) {
	host := tools.NewRegistry()
	require.NoError(t, host.Register(&tools.ToolDescriptor{
		Name: "get_location", Description: "Device GPS", Mode: "local",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"accuracy":{"type":"string"}}}`),
	}))

	clientConv, _ := openBindingConv(t, WithToolRegistry(host))
	h := &recordingClientHandler{}
	clientConv.OnClientTool("get_location", h.handle)

	localConv, localProvider := openBindingConv(t, WithToolRegistry(host))
	var localCalls int
	var mu sync.Mutex
	localConv.OnTool("get_location", func(map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		localCalls++
		return "server-side", nil
	})

	ctx := context.Background()
	_, err := clientConv.Send(ctx, "Where am I?")
	require.NoError(t, err)
	_, err = localConv.Send(ctx, "Where am I?")
	require.NoError(t, err)

	assert.Len(t, h.calls(), 1, "the client conversation's handler serves its own call only")
	mu.Lock()
	assert.Equal(t, 1, localCalls, "the other conversation's local handler still serves its call")
	mu.Unlock()
	assert.Equal(t, []string{"server-side"}, localProvider.toolResults())
	assert.Equal(t, "local", host.Get("get_location").Mode, "the shared descriptor is not rewritten")
}
