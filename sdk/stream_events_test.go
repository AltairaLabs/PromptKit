package sdk

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnStreamEvent_TextDelta(t *testing.T) {
	ctx := context.Background()
	repo := mock.NewInMemoryMockRepository("Hello world")
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "System"},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	var textDeltas []string
	var doneReceived bool

	conv.OnStreamEvent(func(event StreamEvent) {
		switch e := event.(type) {
		case TextDeltaEvent:
			textDeltas = append(textDeltas, e.Delta)
		case StreamDoneEvent:
			doneReceived = true
			assert.NotNil(t, e.Response)
		}
	})

	resp, err := conv.StreamWithCallback(ctx, "Hello")
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, doneReceived, "should receive StreamDoneEvent")
	// Mock provider returns text, so we should get text deltas
	assert.NotEmpty(t, textDeltas)
}

func TestStreamWithCallback_NoHandler(t *testing.T) {
	ctx := context.Background()
	repo := mock.NewInMemoryMockRepository("Response text")
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "System"},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv-nh", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv-nh",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// No handler registered — should still work
	resp, err := conv.StreamWithCallback(ctx, "Hello")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestStreamWithCallback_ClientToolRequest(t *testing.T) {
	conv := newTestConversation()

	// Simulate a stream with client tool chunks
	outCh := make(chan StreamChunk, 10)
	state := &streamState{}

	// Simulate emitStreamChunk with pending tools
	finishReason := "pending_tools"
	providerChunk := &providers.StreamChunk{
		FinishReason: &finishReason,
		PendingTools: []tools.PendingToolExecution{
			{
				CallID:   "call-1",
				ToolName: "get_location",
				Args:     map[string]any{"accuracy": "fine"},
				PendingInfo: &tools.PendingToolInfo{
					Message: "Allow location access?",
					Metadata: map[string]any{
						"categories": []string{"location"},
					},
				},
			},
		},
	}

	conv.emitStreamChunk(context.Background(), providerChunk, outCh, state)
	close(outCh)

	// Verify ChunkClientTool was emitted
	var clientToolChunks []StreamChunk
	for chunk := range outCh {
		clientToolChunks = append(clientToolChunks, chunk)
	}

	require.Len(t, clientToolChunks, 1)
	assert.Equal(t, ChunkClientTool, clientToolChunks[0].Type)
	assert.NotNil(t, clientToolChunks[0].ClientTool)
	assert.Equal(t, "call-1", clientToolChunks[0].ClientTool.CallID)
	assert.Equal(t, "get_location", clientToolChunks[0].ClientTool.ToolName)
	assert.Equal(t, "Allow location access?", clientToolChunks[0].ClientTool.ConsentMsg)
	assert.Equal(t, []string{"location"}, clientToolChunks[0].ClientTool.Categories)
}

func TestStreamEventInterface(t *testing.T) {
	// Verify all event types implement StreamEvent
	var _ StreamEvent = TextDeltaEvent{}
	var _ StreamEvent = ClientToolRequestEvent{}
	var _ StreamEvent = StreamDoneEvent{}
}
