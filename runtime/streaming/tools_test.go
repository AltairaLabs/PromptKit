package streaming

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
