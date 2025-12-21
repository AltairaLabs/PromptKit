package streaming

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ToolExecutor executes tool calls and returns results.
// Implementations provide the actual tool registry integration.
type ToolExecutor interface {
	// Execute runs the given tool calls and returns their results.
	// The implementation is responsible for handling execution errors
	// and formatting them appropriately in the result.
	Execute(ctx context.Context, toolCalls []types.MessageToolCall) (*ToolExecutionResult, error)
}

// ToolExecutionResult contains the results of executing tool calls.
type ToolExecutionResult struct {
	// ProviderResponses are formatted for sending back to the streaming provider.
	ProviderResponses []providers.ToolResponse

	// ResultMessages are formatted for state store capture,
	// matching the behavior of non-streaming tool execution.
	ResultMessages []types.Message
}

// SendToolResults sends tool execution results back through the pipeline to the provider,
// and includes tool result messages for state store capture.
//
// This matches the behavior of non-streaming mode where tool results are stored as messages.
// The tool result messages are sent via inputChan with metadata, and DuplexProviderStage
// forwards them to output for state store capture.
func SendToolResults(
	ctx context.Context,
	result *ToolExecutionResult,
	inputChan chan<- stage.StreamElement,
) error {
	if result == nil {
		return nil
	}

	elem := BuildToolResponseElement(result)

	select {
	case inputChan <- elem:
		logger.Debug("SendToolResults: sent tool results to pipeline",
			"provider_responses", len(result.ProviderResponses),
			"result_messages", len(result.ResultMessages))
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BuildToolResponseElement creates a stream element containing tool results.
// This element can be sent through the pipeline to:
// 1. Forward tool responses to the provider (via metadata["tool_responses"])
// 2. Capture tool results in the state store (via metadata["tool_result_messages"])
func BuildToolResponseElement(result *ToolExecutionResult) stage.StreamElement {
	return stage.StreamElement{
		Metadata: map[string]interface{}{
			"tool_responses":       result.ProviderResponses,
			"tool_result_messages": result.ResultMessages,
		},
	}
}

// ExecuteAndSend is a convenience function that executes tool calls and sends
// the results through the pipeline in one operation.
//
// If the executor is nil, this function returns nil (no-op).
func ExecuteAndSend(
	ctx context.Context,
	executor ToolExecutor,
	toolCalls []types.MessageToolCall,
	inputChan chan<- stage.StreamElement,
) error {
	if executor == nil {
		logger.Warn("ExecuteAndSend: no tool executor configured")
		return nil
	}

	result, err := executor.Execute(ctx, toolCalls)
	if err != nil {
		return err
	}

	return SendToolResults(ctx, result, inputChan)
}
