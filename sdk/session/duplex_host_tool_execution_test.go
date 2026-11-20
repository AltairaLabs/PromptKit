package session

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// oneCallSession answers its first text with one lookup call and the tool
// result with "done".
type oneCallSession struct {
	*mock.MockStreamSession
	mu   sync.Mutex
	sent bool
}

func (s *oneCallSession) SendText(ctx context.Context, text string) error {
	s.mu.Lock()
	first := !s.sent
	s.sent = true
	s.mu.Unlock()
	if !first {
		return s.MockStreamSession.SendText(ctx, text)
	}
	finish := "tool_calls"
	go s.EmitChunk(&providers.StreamChunk{
		ToolCalls:    []types.MessageToolCall{{ID: "call-1", Name: "lookup", Args: json.RawMessage(`{}`)}},
		FinishReason: &finish,
	})
	return nil
}

func (s *oneCallSession) SendToolResponses(context.Context, []providers.ToolResponse) error {
	stop := "stop"
	go s.EmitChunk(&providers.StreamChunk{Content: "done", Delta: "done", FinishReason: &stop})
	return nil
}

func (s *oneCallSession) SendToolResponse(ctx context.Context, id, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{{ToolCallID: id, Result: result}})
}

// oneCallProvider creates oneCallSessions.
type oneCallProvider struct{ *mock.StreamingProvider }

func (p *oneCallProvider) CreateStreamSession(
	context.Context, *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	return &oneCallSession{MockStreamSession: mock.NewMockStreamSession()}, nil
}

// countingExecutor counts the calls it runs.
type countingExecutor struct {
	mu    sync.Mutex
	calls int
}

func (e *countingExecutor) Name() string { return "counting" }

func (e *countingExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return json.RawMessage(`{"ok":true}`), nil
}

func (e *countingExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// A host that builds its own pipeline, giving its DuplexProviderStage the
// tool registry (SetToolExecution) and the session the same registry, runs
// each tool call once: the session learns from the pipeline that a stage
// runs the calls, and runs none itself.
func TestDuplexSession_HostPipelineRunningToolsRunsEachCallOnce(t *testing.T) {
	ctx := context.Background()
	exec := &countingExecutor{}
	registry := tools.NewRegistry()
	registry.RegisterExecutor(exec)
	require.NoError(t, registry.Register(&tools.ToolDescriptor{
		Name: "lookup", Description: "look up", Mode: "counting",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}))
	builder := func(_ context.Context, _ providers.Provider, sp providers.StreamInputSupport,
		cfg *providers.StreamingInputConfig, _ string, _ statestore.Store) (*stage.StreamPipeline, error) {
		duplex := stage.NewDuplexProviderStage(sp, cfg)
		duplex.SetToolExecution(registry, nil, stage.DuplexToolConfig{}, nil)
		pc := stage.DefaultPipelineConfig()
		pc.ExecutionTimeout = 0
		return stage.NewPipelineBuilderWithConfig(pc).Chain(duplex).Build()
	}
	sess, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider: &oneCallProvider{StreamingProvider: mock.NewStreamingProvider("rt", "rt-model", false)},
		Config: &providers.StreamingInputConfig{
			Tools: []providers.StreamingToolDefinition{{Name: "lookup", Description: "look up"}},
		},
		PipelineBuilder: builder,
		ToolRegistry:    registry,
	})
	require.NoError(t, err)
	defer sess.Close()
	responses := sess.Response()

	require.NoError(t, sess.SendText(ctx, "look it up"))
	deadline := time.After(3 * time.Second)
	for done := false; !done; {
		select {
		case c := <-responses:
			require.NoError(t, c.Error)
			done = c.Content == "done" || c.Delta == "done"
		case <-deadline:
			t.Fatal("no reply after the tool call")
		}
	}
	// A second run, by the session, would follow the first.
	assert.Never(t, func() bool { return exec.count() > 1 }, 100*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, 1, exec.count())
}
