package sdk

import (
	"context"
	"encoding/json"
	"testing"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockUnarySession is a controllable UnarySession for testing lifecycle behavior.
type mockUnarySession struct {
	executeResult *rtpipeline.ExecutionResult
	executeErr    error
	resumeResult  *rtpipeline.ExecutionResult
	resumeErr     error

	// Track calls
	resumeCalled        bool
	resumeStreamCalled  bool
	resumeToolResults   []types.Message
	executeStreamResult chan providers.StreamChunk
	executeStreamErr    error
}

func (m *mockUnarySession) ID() string                                          { return "mock-session" }
func (m *mockUnarySession) Variables() map[string]string                        { return nil }
func (m *mockUnarySession) SetVar(_, _ string)                                  {}
func (m *mockUnarySession) GetVar(_ string) (string, bool)                      { return "", false }
func (m *mockUnarySession) Messages(_ context.Context) ([]types.Message, error) { return nil, nil }
func (m *mockUnarySession) Clear(_ context.Context) error                       { return nil }

func (m *mockUnarySession) Execute(_ context.Context, _, _ string) (*rtpipeline.ExecutionResult, error) {
	return m.executeResult, m.executeErr
}

//nolint:gocritic
func (m *mockUnarySession) ExecuteWithMessage(_ context.Context, _ types.Message) (*rtpipeline.ExecutionResult, error) {
	return m.executeResult, m.executeErr
}

func (m *mockUnarySession) ExecuteStream(_ context.Context, _, _ string) (<-chan providers.StreamChunk, error) {
	return m.executeStreamResult, m.executeStreamErr
}

//nolint:gocritic
func (m *mockUnarySession) ExecuteStreamWithMessage(_ context.Context, _ types.Message) (<-chan providers.StreamChunk, error) {
	return m.executeStreamResult, m.executeStreamErr
}

func (m *mockUnarySession) ResumeWithToolResults(_ context.Context, toolResults []types.Message) (*rtpipeline.ExecutionResult, error) {
	m.resumeCalled = true
	m.resumeToolResults = toolResults
	return m.resumeResult, m.resumeErr
}

func (m *mockUnarySession) ResumeStreamWithToolResults(_ context.Context, toolResults []types.Message) (<-chan providers.StreamChunk, error) {
	m.resumeStreamCalled = true
	m.resumeToolResults = toolResults
	ch := make(chan providers.StreamChunk, 1)
	if m.resumeResult != nil {
		fin := "stop"
		ch <- providers.StreamChunk{
			Content:      m.resumeResult.Response.Content,
			FinishReason: &fin,
			FinalResult:  &stage.ExecutionResult{Response: &stage.Response{Role: "assistant", Content: m.resumeResult.Response.Content}},
		}
	}
	close(ch)
	return ch, m.resumeErr
}

func (m *mockUnarySession) ForkSession(_ context.Context, _ string, _ *stage.StreamPipeline) (session.UnarySession, error) {
	return nil, nil
}

// newTestConvWithMockSession creates a Conversation wired to a mock session
// for precise control of pipeline results.
func newTestConvWithMockSession(sess *mockUnarySession) *Conversation {
	return &Conversation{
		config:         &config{},
		handlers:       make(map[string]ToolHandler),
		ctxHandlers:    make(map[string]ToolHandlerCtx),
		clientHandlers: make(map[string]ClientToolHandler),
		mode:           UnaryMode,
		unarySession:   sess,
		toolRegistry:   tools.NewRegistry(),
		resolvedStore:  sdktools.NewResolvedStore(),
		sessionHooks:   newSessionHookDispatcher(nil, nil),
	}
}

// resultWithPendingTools builds an ExecutionResult that contains pending client tools.
func resultWithPendingTools() *rtpipeline.ExecutionResult {
	return &rtpipeline.ExecutionResult{
		Messages: []types.Message{
			{Role: "assistant", Content: "I need your location"},
		},
		Response: &rtpipeline.Response{
			Role:    "assistant",
			Content: "I need your location",
		},
		PendingTools: []tools.PendingToolExecution{
			{
				CallID:   "call-1",
				ToolName: "get_location",
				Args:     map[string]any{"accuracy": "fine"},
				PendingInfo: &tools.PendingToolInfo{
					Reason:   "client_tool_deferred",
					Message:  "Allow location?",
					ToolName: "get_location",
					Args:     json.RawMessage(`{"accuracy":"fine"}`),
					Metadata: map[string]any{
						"categories": []string{"location"},
					},
				},
			},
		},
		Metadata: make(map[string]any),
	}
}

// resultWithoutPendingTools builds a normal ExecutionResult.
func resultWithoutPendingTools() *rtpipeline.ExecutionResult {
	return &rtpipeline.ExecutionResult{
		Messages: []types.Message{
			{Role: "assistant", Content: "Your location is San Francisco"},
		},
		Response: &rtpipeline.Response{
			Role:    "assistant",
			Content: "Your location is San Francisco",
		},
		Metadata: make(map[string]any),
	}
}

// ---------------------------------------------------------------------------
// Bug 1: Send() should not fire lifecycle hooks when pending tools exist
// ---------------------------------------------------------------------------

func TestSend_SkipsHooksWhenPendingClientTools(t *testing.T) {
	sess := &mockUnarySession{
		executeResult: resultWithPendingTools(),
	}
	conv := newTestConvWithMockSession(sess)

	resp, err := conv.Send(context.Background(), "Where am I?")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools(), "response should have pending client tools")

	// Turn counter should NOT have been incremented
	assert.Equal(t, 0, conv.sessionHooks.TurnIndex(),
		"IncrementTurn should not fire when pending client tools exist")
}

func TestSend_FiresHooksWhenNoPendingClientTools(t *testing.T) {
	sess := &mockUnarySession{
		executeResult: resultWithoutPendingTools(),
	}
	conv := newTestConvWithMockSession(sess)

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.False(t, resp.HasPendingClientTools())

	// Turn counter SHOULD have been incremented
	assert.Equal(t, 1, conv.sessionHooks.TurnIndex(),
		"IncrementTurn should fire for normal responses")
}

// ---------------------------------------------------------------------------
// Bug 2a: Resume() should call dispatchTurnEvals
// Bug 2b: Resume() should skip hooks when nested pending tools exist
// ---------------------------------------------------------------------------

func TestResume_SkipsHooksWhenNestedPendingTools(t *testing.T) {
	sess := &mockUnarySession{
		// Resume returns MORE pending tools (nested)
		resumeResult: resultWithPendingTools(),
	}
	conv := newTestConvWithMockSession(sess)

	// Supply a tool result so Resume has something to work with
	err := conv.SendToolResult(context.Background(), "call-prev", map[string]any{"data": "ok"})
	require.NoError(t, err)

	resp, err := conv.Resume(context.Background())
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools(), "resume should return nested pending tools")

	// Turn counter should NOT have been incremented
	assert.Equal(t, 0, conv.sessionHooks.TurnIndex(),
		"IncrementTurn should not fire when resume returns pending tools")
}

func TestResume_FiresHooksWhenNoPendingTools(t *testing.T) {
	sess := &mockUnarySession{
		resumeResult: resultWithoutPendingTools(),
	}
	conv := newTestConvWithMockSession(sess)

	err := conv.SendToolResult(context.Background(), "call-1", map[string]any{"lat": 37.7})
	require.NoError(t, err)

	resp, err := conv.Resume(context.Background())
	require.NoError(t, err)
	require.False(t, resp.HasPendingClientTools())

	// Turn counter SHOULD have been incremented
	assert.Equal(t, 1, conv.sessionHooks.TurnIndex(),
		"IncrementTurn should fire when resume completes normally")
}
