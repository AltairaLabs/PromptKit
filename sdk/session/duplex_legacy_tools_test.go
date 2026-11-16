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
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// forwardingToolStage is a host's own pipeline stage that does not run tools:
// it answers the first input with an assistant turn calling get_weather and
// approve_refund, and records the tool responses the session sends back.
type forwardingToolStage struct {
	mu        sync.Mutex
	responses []providers.ToolResponse
}

func (*forwardingToolStage) Name() string          { return "forwarding-tools" }
func (*forwardingToolStage) Type() stage.StageType { return stage.StageTypeTransform }

func (s *forwardingToolStage) Process(
	ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement,
) error {
	defer close(out)
	called := false
	for elem := range in {
		s.mu.Lock()
		s.responses = append(s.responses, elem.Meta.ToolResponses...)
		s.mu.Unlock()
		if called {
			continue
		}
		called = true
		call := stage.NewMessageElement(&types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "call-1", Name: "get_weather", Args: json.RawMessage(`{"city":"Paris"}`)},
			{ID: "call-2", Name: "approve_refund", Args: json.RawMessage(`{"amount":900}`)},
		}})
		call.EndOfStream = true
		select {
		case out <- call:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *forwardingToolStage) recorded() []providers.ToolResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]providers.ToolResponse(nil), s.responses...)
}

// weatherExecutor runs every call with a fixed result.
type weatherExecutor struct{}

func (weatherExecutor) Name() string { return "weather" }
func (weatherExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"temp":21}`), nil
}

// A host that builds a duplex session itself, with a tool registry, an
// AsyncToolChecker and a pipeline that does not run tools, has the session
// run the calls, as before the pipeline could: the checker holds a call for
// approval, and the session runs the other and sends its result back.
func TestDuplexSession_RunsToolsWhenThePipelineDoesNot(t *testing.T) {
	reg := tools.NewRegistry()
	reg.RegisterExecutor(weatherExecutor{})
	for _, name := range []string{"get_weather", "approve_refund"} {
		require.NoError(t, reg.Register(&tools.ToolDescriptor{
			Name: name, Description: name, InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "weather",
		}))
	}
	hold := func(_ context.Context, _, name string, _ map[string]any) *AsyncToolCheckResult {
		if name != "approve_refund" {
			return nil
		}
		return &AsyncToolCheckResult{
			ShouldWait:  true,
			PendingInfo: &tools.PendingToolInfo{Reason: "high_value", ToolName: name},
		}
	}
	st := &forwardingToolStage{}
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(st).Build()
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:         mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder:  builder,
		ToolRegistry:     reg,
		AsyncToolChecker: hold,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()

	require.NoError(t, sess.SendText(context.Background(), "weather and refund"))
	select {
	case chunk := <-resp:
		require.Len(t, chunk.PendingTools, 1, "the gated call is held for the caller")
		assert.Equal(t, "call-2", chunk.PendingTools[0].CallID)
		assert.Equal(t, "approve_refund", chunk.PendingTools[0].ToolName)
		assert.Equal(t, map[string]any{"amount": float64(900)}, chunk.PendingTools[0].Args)
		require.NotNil(t, chunk.PendingTools[0].PendingInfo)
		assert.Equal(t, "high_value", chunk.PendingTools[0].PendingInfo.Reason)
	case <-time.After(lifecycleWait):
		t.Fatal("the held call never reached the caller")
	}
	require.Eventually(t, func() bool { return len(st.recorded()) == 1 }, lifecycleWait, 5*time.Millisecond,
		"the ungated call's result never reached the pipeline")
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "call-1", Result: `{"temp":21}`}}, st.recorded())
}
