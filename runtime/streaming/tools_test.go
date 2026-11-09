package streaming

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildToolResponseElement_FieldMapping(t *testing.T) {
	provResponses := []providers.ToolResponse{
		{ToolCallID: "a", Result: "ok"},
		{ToolCallID: "b", Result: "err", IsError: true},
	}
	resultMsgs := []types.Message{
		{Role: "tool", Content: "msg-a"},
	}
	result := &ToolExecutionResult{
		ProviderResponses: provResponses,
		ResultMessages:    resultMsgs,
	}

	elem := BuildToolResponseElement(result)

	// ProviderResponses must map to Meta.ToolResponses (provider-bound), NOT
	// Meta.ToolResultMessages. This guards against the field-swap drop-risk.
	if len(elem.Meta.ToolResponses) != len(provResponses) {
		t.Fatalf("Meta.ToolResponses len = %d, want %d", len(elem.Meta.ToolResponses), len(provResponses))
	}
	if elem.Meta.ToolResponses[0].ToolCallID != "a" || elem.Meta.ToolResponses[1].ToolCallID != "b" {
		t.Errorf("Meta.ToolResponses not mapped from ProviderResponses: %+v", elem.Meta.ToolResponses)
	}
	if !elem.Meta.ToolResponses[1].IsError {
		t.Error("Meta.ToolResponses[1].IsError lost in mapping")
	}

	// ResultMessages must map to Meta.ToolResultMessages (state-store capture).
	if len(elem.Meta.ToolResultMessages) != len(resultMsgs) {
		t.Fatalf("Meta.ToolResultMessages len = %d, want %d", len(elem.Meta.ToolResultMessages), len(resultMsgs))
	}
	if elem.Meta.ToolResultMessages[0].Content != "msg-a" {
		t.Errorf("Meta.ToolResultMessages not mapped from ResultMessages: %+v", elem.Meta.ToolResultMessages)
	}
}

func TestSendToolResults_NilResult(t *testing.T) {
	// Nil result is a no-op that returns nil and sends nothing.
	inputChan := make(chan stage.StreamElement, 1)
	if err := SendToolResults(context.Background(), nil, inputChan); err != nil {
		t.Fatalf("SendToolResults(nil) = %v, want nil", err)
	}
	select {
	case <-inputChan:
		t.Fatal("expected no element sent for nil result")
	default:
	}
}

func TestSendToolResults_Success(t *testing.T) {
	inputChan := make(chan stage.StreamElement, 1)
	result := &ToolExecutionResult{
		ProviderResponses: []providers.ToolResponse{{ToolCallID: "x", Result: "r"}},
		ResultMessages:    []types.Message{{Role: "tool", Content: "m"}},
	}

	if err := SendToolResults(context.Background(), result, inputChan); err != nil {
		t.Fatalf("SendToolResults = %v, want nil", err)
	}

	elem := <-inputChan
	if len(elem.Meta.ToolResponses) != 1 || elem.Meta.ToolResponses[0].ToolCallID != "x" {
		t.Errorf("sent element missing tool responses: %+v", elem.Meta.ToolResponses)
	}
}

func TestSendToolResults_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inputChan := make(chan stage.StreamElement) // unbuffered: send blocks

	result := &ToolExecutionResult{
		ProviderResponses: []providers.ToolResponse{{ToolCallID: "x"}},
	}
	err := SendToolResults(ctx, result, inputChan)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("SendToolResults = %v, want context.Canceled", err)
	}
}

func TestExecuteAndSend_NilExecutor(t *testing.T) {
	// Nil executor is a no-op returning nil.
	inputChan := make(chan stage.StreamElement, 1)
	err := ExecuteAndSend(context.Background(), nil, nil, inputChan)
	if err != nil {
		t.Fatalf("ExecuteAndSend(nil executor) = %v, want nil", err)
	}
	select {
	case <-inputChan:
		t.Fatal("expected no element sent for nil executor")
	default:
	}
}

func TestExecuteAndSend_ExecutorError(t *testing.T) {
	execErr := errors.New("exec failed")
	exec := &stubToolExecutor{err: execErr}
	inputChan := make(chan stage.StreamElement, 1)

	err := ExecuteAndSend(context.Background(), exec, []types.MessageToolCall{{ID: "1"}}, inputChan)
	if !errors.Is(err, execErr) {
		t.Errorf("ExecuteAndSend = %v, want %v", err, execErr)
	}
}

func TestExecuteAndSend_Success(t *testing.T) {
	exec := &stubToolExecutor{
		result: &ToolExecutionResult{
			ProviderResponses: []providers.ToolResponse{{ToolCallID: "1", Result: "done"}},
		},
	}
	inputChan := make(chan stage.StreamElement, 1)
	calls := []types.MessageToolCall{{ID: "1", Name: "search"}}

	if err := ExecuteAndSend(context.Background(), exec, calls, inputChan); err != nil {
		t.Fatalf("ExecuteAndSend = %v, want nil", err)
	}
	if len(exec.gotCalls) != 1 || exec.gotCalls[0].ID != "1" {
		t.Errorf("executor did not receive the tool calls: %+v", exec.gotCalls)
	}
	elem := <-inputChan
	if len(elem.Meta.ToolResponses) != 1 {
		t.Errorf("expected tool responses in sent element, got %+v", elem.Meta.ToolResponses)
	}
}

// The element SendToolResults builds is what DuplexProviderStage consumes: the
// provider-bound responses reach the live session, and the result messages
// come out of the stage for the state store. Runs both sides for real.
func TestSendToolResults_ReachDuplexSessionAndOutput(t *testing.T) {
	provider := mock.NewStreamingProvider("p", "m", false)
	duplex := stage.NewDuplexProviderStage(provider, &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio, ChunkSize: 4096, SampleRate: 16000,
			Encoding: "pcm_linear16", Channels: 1},
	})

	input := make(chan stage.StreamElement, 4)
	output := make(chan stage.StreamElement, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- duplex.Process(ctx, input, output) }()

	toolMsg := types.Message{Role: "tool", Content: "72F", ToolResult: &types.MessageToolResult{
		ID: "call-1", Name: "get_weather", Parts: []types.ContentPart{types.NewTextPart("72F")},
	}}
	require.NoError(t, SendToolResults(ctx, &ToolExecutionResult{
		ProviderResponses: []providers.ToolResponse{{ToolCallID: "call-1", Result: "72F"}},
		ResultMessages:    []types.Message{toolMsg},
	}, input))

	select {
	case e := <-output:
		require.NotNil(t, e.Message)
		assert.Equal(t, "tool", e.Message.Role)
		assert.Equal(t, "call-1", e.Message.ToolResult.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("tool result message never left the duplex stage")
	}
	require.Eventually(t, func() bool {
		s := provider.GetSession()
		return s != nil && len(s.ReceivedToolResponses()) == 1
	}, 2*time.Second, time.Millisecond)
	got := provider.GetSession().ReceivedToolResponses()[0]
	assert.Equal(t, "call-1", got.ToolCallID)
	assert.Equal(t, "72F", got.Result)

	cancel()
	close(input)
	<-done
}
