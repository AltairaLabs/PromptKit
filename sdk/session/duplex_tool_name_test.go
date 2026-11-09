package session

import (
	"context"
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

// toolCallingStage answers the first input with an assistant turn that calls
// get_weather, then records the tool result messages it is sent back.
type toolCallingStage struct {
	mu      sync.Mutex
	results []types.MessageToolResult
}

func (*toolCallingStage) Name() string          { return "tool-calling" }
func (*toolCallingStage) Type() stage.StageType { return stage.StageTypeTransform }

func (s *toolCallingStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	defer close(out)
	called := false
	for elem := range in {
		for i := range elem.Meta.ToolResultMessages {
			if tr := elem.Meta.ToolResultMessages[i].ToolResult; tr != nil {
				s.mu.Lock()
				s.results = append(s.results, *tr)
				s.mu.Unlock()
			}
		}
		if called {
			continue
		}
		called = true
		call := stage.NewMessageElement(&types.Message{
			Role:      "assistant",
			ToolCalls: []types.MessageToolCall{{ID: "call-1", Name: "get_weather", Args: []byte(`{}`)}},
		})
		call.EndOfStream = true
		select {
		case out <- call:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *toolCallingStage) recorded() []types.MessageToolResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.MessageToolResult(nil), s.results...)
}

// A client answers a duplex tool call with SubmitToolResults, which carries
// only the call ID. The tool result put into the conversation must carry the
// called tool's name, or a provider that keys results by name (Gemini's
// functionResponse.name) gets an invalid request (#2589).
func TestDuplexSession_SubmittedToolResultsCarryTheCallName(t *testing.T) {
	st := &toolCallingStage{}
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(st).Build()
	}
	// The call is held for the client, the way a client tool or an approval
	// gate holds it, so the client answers it with SubmitToolResults.
	hold := func(context.Context, string, string, map[string]any) *AsyncToolCheckResult {
		return &AsyncToolCheckResult{ShouldWait: true, PendingInfo: &tools.PendingToolInfo{Reason: "client"}}
	}
	sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:         mock.NewProvider("mock", "mock-model", false),
		PipelineBuilder:  builder,
		ToolRegistry:     tools.NewRegistry(),
		AsyncToolChecker: hold,
	})
	require.NoError(t, err)
	defer sess.Close()
	resp := sess.Response()

	require.NoError(t, sess.SendText(context.Background(), "weather?"))
	select {
	case chunk := <-resp:
		require.Len(t, chunk.PendingTools, 1, "test premise: the held call reaches the caller")
		assert.Equal(t, "get_weather", chunk.PendingTools[0].ToolName)
	case <-time.After(lifecycleWait):
		t.Fatal("the tool call never reached the caller")
	}

	require.NoError(t, sess.SubmitToolResults(context.Background(), []providers.ToolResponse{
		{ToolCallID: "call-1", Result: `{"temp":21}`},
	}))
	require.Eventually(t, func() bool { return len(st.recorded()) == 1 }, lifecycleWait, 5*time.Millisecond)
	got := st.recorded()[0]
	assert.Equal(t, "call-1", got.ID)
	assert.Equal(t, "get_weather", got.Name)

	// An answered call is forgotten, so a long session does not accumulate
	// every call it ever made.
	ds := sess.(*duplexSession)
	ds.callNamesMu.Lock()
	defer ds.callNamesMu.Unlock()
	assert.Empty(t, ds.callNames)
}
