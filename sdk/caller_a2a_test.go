package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// a2aCallerTool is the tool the remote test agent's "summarize" skill becomes.
const a2aCallerTool = "a2a__remote__summarize"

// a2aCallerProvider calls a2aCallerTool once, then answers.
type a2aCallerProvider struct {
	*mock.ToolProvider
}

func (p *a2aCallerProvider) SupportsStreaming() bool { return false }

func (p *a2aCallerProvider) PredictWithTools(
	_ context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	calls := []types.MessageToolCall{{ID: callerCallID, Name: a2aCallerTool, Args: json.RawMessage(`{"query":"hi"}`)}}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

// asyncCallerExecutor is a host AsyncCallExecutor that records every call it
// is given and completes it. Its synchronous entry point records nothing, so
// a call routed past ExecuteCallAsync shows up as a missing call.
type asyncCallerExecutor struct{ callerExecutor }

func (e *asyncCallerExecutor) ExecuteCall(
	context.Context, *tools.ToolDescriptor, tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	return json.RawMessage(`{"path":"sync"}`), nil, nil
}

func (e *asyncCallerExecutor) ExecuteCallAsync(
	ctx context.Context, d *tools.ToolDescriptor, call tools.ToolCall,
) (*tools.ToolExecutionResult, error) {
	result, _, err := e.callerExecutor.ExecuteCall(ctx, d, call)
	return &tools.ToolExecutionResult{Status: tools.ToolStatusComplete, Content: result}, err
}

// A host executor given through WithA2AToolExecutor runs remote agent calls
// with the provider's call ID and the conversation's caller, through the
// call-taking entry point it implements.
func TestCaller_A2AToolExecutorReceivesTheCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}
	for name, exec := range map[string]interface {
		tools.CallExecutor
		seen() []tools.ToolCall
	}{
		"CallExecutor":      &callerExecutor{},
		"AsyncCallExecutor": &asyncCallerExecutor{},
	} {
		t.Run(name, func(t *testing.T) {
			srv := a2aTestServer(t, "Remote", "summarize", "remote says hi")
			defer srv.Close()
			prov := &a2aCallerProvider{ToolProvider: mock.NewToolProvider("a2a", "a2a-model", false, nil)}
			conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
				WithProvider(prov), WithSkipSchemaValidation(), WithCaller(member),
				WithA2AAgent(NewA2AAgent(srv.URL)), WithA2AToolExecutor(exec))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "summarize this")
			require.NoError(t, err)

			calls := exec.seen()
			require.Len(t, calls, 1)
			assert.Equal(t, a2aCallerTool, calls[0].Name)
			assert.Equal(t, callerCallID, calls[0].ID)
			assert.Equal(t, member, calls[0].Caller)
			assert.JSONEq(t, `{"query":"hi"}`, string(calls[0].Args))
		})
	}
}

// entryRecorder records which of its entry points a call reached.
type entryRecorder struct {
	entry string
}

func (e *entryRecorder) Name() string { return "entry-recorder" }

func (e *entryRecorder) Execute(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, error) {
	e.entry = "Execute"
	return json.RawMessage(`{"ok":true}`), nil
}

// multimodalHost implements MultimodalExecutor and no call-taking entry point.
type multimodalHost struct{ entryRecorder }

func (e *multimodalHost) ExecuteMultimodal(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (json.RawMessage, []types.ContentPart, error) {
	e.entry = "ExecuteMultimodal"
	return json.RawMessage(`{"ok":true}`), nil, nil
}

// asyncHost implements AsyncToolExecutor and no call-taking entry point.
type asyncHost struct{ entryRecorder }

func (e *asyncHost) ExecuteAsync(
	_ context.Context, _ *tools.ToolDescriptor, _ json.RawMessage,
) (*tools.ToolExecutionResult, error) {
	e.entry = "ExecuteAsync"
	return &tools.ToolExecutionResult{Status: tools.ToolStatusComplete, Content: json.RawMessage(`{"ok":true}`)}, nil
}

// A host executor without a call-taking entry point still has its most
// specific one used: wrapping it under the "a2a" name hides none of them.
func TestWithA2AToolExecutor_KeepsTheHostsEntryPoints(t *testing.T) {
	for want, host := range map[string]interface {
		tools.Executor
		reached() string
	}{
		"Execute":           &plainHost{},
		"ExecuteMultimodal": &multimodalHost{},
		"ExecuteAsync":      &asyncHost{},
	} {
		t.Run(want, func(t *testing.T) {
			srv := a2aTestServer(t, "Remote", "summarize", "remote says hi")
			defer srv.Close()
			prov := &a2aCallerProvider{ToolProvider: mock.NewToolProvider("a2a", "a2a-model", false, nil)}
			conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
				WithProvider(prov), WithSkipSchemaValidation(),
				WithA2AAgent(NewA2AAgent(srv.URL)), WithA2AToolExecutor(host))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			_, err = conv.Send(context.Background(), "summarize this")
			require.NoError(t, err)
			assert.Equal(t, want, host.reached())
		})
	}
}

// plainHost implements only Executor.
type plainHost struct{ entryRecorder }

func (e *entryRecorder) reached() string { return e.entry }
