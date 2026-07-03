package streaming

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stubToolExecutor is a configurable ToolExecutor for tests.
type stubToolExecutor struct {
	result   *ToolExecutionResult
	err      error
	gotCalls []types.MessageToolCall
}

func (s *stubToolExecutor) Execute(
	_ context.Context,
	toolCalls []types.MessageToolCall,
) (*ToolExecutionResult, error) {
	s.gotCalls = toolCalls
	return s.result, s.err
}

// recvWithTimeout reads a single value from responseDone or fails the test.
func recvWithTimeout(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for responseDone")
		return nil
	}
}

func TestResponseCollector_Complete(t *testing.T) {
	outputChan := make(chan stage.StreamElement, 1)
	inputChan := make(chan stage.StreamElement, 1)
	c := NewResponseCollector(ResponseCollectorConfig{})

	outputChan <- stage.StreamElement{
		EndOfStream: true,
		Message:     &types.Message{Content: "final"},
	}

	done := c.Start(context.Background(), outputChan, inputChan)
	if err := recvWithTimeout(t, done); err != nil {
		t.Errorf("expected nil on complete, got %v", err)
	}
}

func TestResponseCollector_Error(t *testing.T) {
	outputChan := make(chan stage.StreamElement, 1)
	inputChan := make(chan stage.StreamElement, 1)
	c := NewResponseCollector(ResponseCollectorConfig{LogPrefix: "test"})

	// Empty end-of-stream produces ErrEmptyResponse.
	outputChan <- stage.StreamElement{EndOfStream: true}

	done := c.Start(context.Background(), outputChan, inputChan)
	if err := recvWithTimeout(t, done); !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestResponseCollector_ContinuesThenCompletes(t *testing.T) {
	outputChan := make(chan stage.StreamElement, 3)
	inputChan := make(chan stage.StreamElement, 1)
	c := NewResponseCollector(ResponseCollectorConfig{})

	// Interrupted (continue), then a streaming chunk (continue), then complete.
	outputChan <- stage.StreamElement{Meta: stage.ElementMetadata{Interrupted: true}}
	outputChan <- stage.StreamElement{Text: strPtr("chunk")}
	outputChan <- stage.StreamElement{EndOfStream: true, Message: &types.Message{Content: "done"}}

	done := c.Start(context.Background(), outputChan, inputChan)
	if err := recvWithTimeout(t, done); err != nil {
		t.Errorf("expected nil after continues, got %v", err)
	}
}

func TestResponseCollector_ChannelClosed(t *testing.T) {
	outputChan := make(chan stage.StreamElement)
	inputChan := make(chan stage.StreamElement, 1)
	c := NewResponseCollector(ResponseCollectorConfig{})

	close(outputChan)

	done := c.Start(context.Background(), outputChan, inputChan)
	if err := recvWithTimeout(t, done); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("expected ErrSessionEnded, got %v", err)
	}
}

func TestResponseCollector_ContextCancelled(t *testing.T) {
	outputChan := make(chan stage.StreamElement) // never yields
	inputChan := make(chan stage.StreamElement, 1)
	c := NewResponseCollector(ResponseCollectorConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	done := c.Start(ctx, outputChan, inputChan)
	cancel()

	if err := recvWithTimeout(t, done); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestResponseCollector_ToolCallsThenComplete(t *testing.T) {
	outputChan := make(chan stage.StreamElement, 2)
	inputChan := make(chan stage.StreamElement, 1)

	exec := &stubToolExecutor{
		result: &ToolExecutionResult{
			ProviderResponses: []providers.ToolResponse{{ToolCallID: "1", Result: "ok"}},
		},
	}
	c := NewResponseCollector(ResponseCollectorConfig{ToolExecutor: exec})

	outputChan <- stage.StreamElement{
		EndOfStream: true,
		Message:     &types.Message{ToolCalls: []types.MessageToolCall{{ID: "1", Name: "search"}}},
	}
	outputChan <- stage.StreamElement{
		EndOfStream: true,
		Message:     &types.Message{Content: "final answer"},
	}

	done := c.Start(context.Background(), outputChan, inputChan)
	if err := recvWithTimeout(t, done); err != nil {
		t.Errorf("expected nil after tool execution then complete, got %v", err)
	}

	// Tool results should have been forwarded to inputChan.
	select {
	case elem := <-inputChan:
		if len(elem.Meta.ToolResponses) != 1 {
			t.Errorf("expected forwarded tool responses, got %+v", elem.Meta.ToolResponses)
		}
	default:
		t.Error("expected tool results forwarded to inputChan")
	}

	if len(exec.gotCalls) != 1 {
		t.Errorf("executor got %d calls, want 1", len(exec.gotCalls))
	}
}

func TestResponseCollector_ToolCallsNoExecutor(t *testing.T) {
	outputChan := make(chan stage.StreamElement, 1)
	inputChan := make(chan stage.StreamElement, 1)
	c := NewResponseCollector(ResponseCollectorConfig{}) // nil executor

	outputChan <- stage.StreamElement{
		EndOfStream: true,
		Message:     &types.Message{ToolCalls: []types.MessageToolCall{{ID: "1"}}},
	}

	done := c.Start(context.Background(), outputChan, inputChan)
	err := recvWithTimeout(t, done)
	if err == nil {
		t.Fatal("expected error for tool calls with no executor")
	}
}

func TestResponseCollector_ToolExecutionError(t *testing.T) {
	outputChan := make(chan stage.StreamElement, 1)
	inputChan := make(chan stage.StreamElement, 1)
	execErr := errors.New("tool blew up")
	exec := &stubToolExecutor{err: execErr}
	c := NewResponseCollector(ResponseCollectorConfig{ToolExecutor: exec})

	outputChan <- stage.StreamElement{
		EndOfStream: true,
		Message:     &types.Message{ToolCalls: []types.MessageToolCall{{ID: "1"}}},
	}

	done := c.Start(context.Background(), outputChan, inputChan)
	if err := recvWithTimeout(t, done); !errors.Is(err, execErr) {
		t.Errorf("expected tool execution error, got %v", err)
	}
}

// handleAction's default branch is unreachable through normal flow; exercise it
// directly to guarantee it signals "not done".
func TestResponseCollector_HandleAction_DefaultBranch(t *testing.T) {
	c := NewResponseCollector(ResponseCollectorConfig{})
	responseDone := make(chan error, 1)
	done := c.handleAction(
		context.Background(),
		ResponseAction(99),
		nil,
		&stage.StreamElement{},
		make(chan stage.StreamElement, 1),
		responseDone,
		"test",
	)
	if done {
		t.Error("default branch should return done=false")
	}
}

func TestResponseCollector_ExecuteAndSend_NoResponses(t *testing.T) {
	// Executor returns a nil/empty result: no send to inputChan, no error.
	exec := &stubToolExecutor{result: nil}
	c := NewResponseCollector(ResponseCollectorConfig{ToolExecutor: exec})
	inputChan := make(chan stage.StreamElement, 1)

	elem := &stage.StreamElement{
		Message: &types.Message{ToolCalls: []types.MessageToolCall{{ID: "1"}}},
	}
	if err := c.executeAndSendToolResults(context.Background(), elem, inputChan, "test"); err != nil {
		t.Fatalf("executeAndSendToolResults = %v, want nil", err)
	}
	select {
	case <-inputChan:
		t.Error("expected no forwarding when result is nil")
	default:
	}
}

func TestResponseCollector_ExecuteAndSend_SendError(t *testing.T) {
	// Executor returns responses, but the send fails because ctx is cancelled
	// and inputChan is unbuffered — the send error must propagate.
	exec := &stubToolExecutor{
		result: &ToolExecutionResult{
			ProviderResponses: []providers.ToolResponse{{ToolCallID: "1", Result: "ok"}},
		},
	}
	c := NewResponseCollector(ResponseCollectorConfig{ToolExecutor: exec})
	inputChan := make(chan stage.StreamElement) // unbuffered: send blocks

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	elem := &stage.StreamElement{
		Message: &types.Message{ToolCalls: []types.MessageToolCall{{ID: "1"}}},
	}
	err := c.executeAndSendToolResults(ctx, elem, inputChan, "test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("executeAndSendToolResults = %v, want context.Canceled", err)
	}
}

func TestDrainStaleMessages(t *testing.T) {
	t.Run("drains buffered then returns count", func(t *testing.T) {
		outputChan := make(chan stage.StreamElement, 3)
		outputChan <- stage.StreamElement{Text: strPtr("a")}
		outputChan <- stage.StreamElement{EndOfStream: true}

		count, err := DrainStaleMessages(outputChan)
		if err != nil {
			t.Fatalf("DrainStaleMessages err = %v", err)
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
	})

	t.Run("empty channel drains zero", func(t *testing.T) {
		outputChan := make(chan stage.StreamElement, 1)
		count, err := DrainStaleMessages(outputChan)
		if err != nil {
			t.Fatalf("DrainStaleMessages err = %v", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
	})

	t.Run("closed channel returns ErrSessionEnded", func(t *testing.T) {
		outputChan := make(chan stage.StreamElement)
		close(outputChan)
		count, err := DrainStaleMessages(outputChan)
		if !errors.Is(err, ErrSessionEnded) {
			t.Errorf("err = %v, want ErrSessionEnded", err)
		}
		if count != 0 {
			t.Errorf("count = %d, want 0", count)
		}
	})
}

func TestWaitForResponse(t *testing.T) {
	t.Run("receives result", func(t *testing.T) {
		done := make(chan error, 1)
		sentinel := errors.New("done result")
		done <- sentinel
		if err := WaitForResponse(context.Background(), done); !errors.Is(err, sentinel) {
			t.Errorf("err = %v, want %v", err, sentinel)
		}
	})

	t.Run("context cancellation wins", func(t *testing.T) {
		done := make(chan error) // never sends
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := WaitForResponse(ctx, done); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	})
}
