package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// executeToolCalls executes the tool calls from a response and returns both:
// - Provider responses to send back to the streaming session
// - Tool result messages to capture in the state store (matching non-streaming behavior)
func (de *DuplexConversationExecutor) executeToolCalls(
	ctx context.Context,
	toolCalls []types.MessageToolCall,
) *toolExecutionResult {
	_ = ctx // Currently unused, but kept for future async tool execution

	if de.toolRegistry == nil {
		logger.Warn("executeToolCalls: no tool registry configured")
		return nil
	}

	result := &toolExecutionResult{
		providerResponses: make([]providers.ToolResponse, 0, len(toolCalls)),
		resultMessages:    make([]types.Message, 0, len(toolCalls)),
	}

	for _, tc := range toolCalls {
		logger.Debug("executeToolCalls: executing tool",
			"name", tc.Name,
			"id", tc.ID,
			"args", string(tc.Args))

		// Execute tool using registry - args are already json.RawMessage
		toolResult, err := de.toolRegistry.Execute(tc.Name, tc.Args)
		if err != nil {
			logger.Error("executeToolCalls: tool execution failed",
				"name", tc.Name, "error", err)
			errMsg := fmt.Sprintf("tool execution failed: %s", err.Error())
			result.providerResponses = append(result.providerResponses, providers.ToolResponse{
				ToolCallID: tc.ID,
				Result:     fmt.Sprintf(`{"error": %q}`, errMsg),
				IsError:    true,
			})
			result.resultMessages = append(result.resultMessages, types.Message{
				Role:    "tool",
				Content: errMsg,
				ToolResult: &types.MessageToolResult{
					ID:      tc.ID,
					Name:    tc.Name,
					Content: errMsg,
					Error:   errMsg,
				},
			})
			continue
		}

		// Check if the tool itself reported an error
		if toolResult.Error != "" {
			logger.Error("executeToolCalls: tool returned error",
				"name", tc.Name, "error", toolResult.Error)
			result.providerResponses = append(result.providerResponses, providers.ToolResponse{
				ToolCallID: tc.ID,
				Result:     fmt.Sprintf(`{"error": %q}`, toolResult.Error),
				IsError:    true,
			})
			result.resultMessages = append(result.resultMessages, types.Message{
				Role:    "tool",
				Content: toolResult.Error,
				ToolResult: &types.MessageToolResult{
					ID:        tc.ID,
					Name:      tc.Name,
					Content:   toolResult.Error,
					Error:     toolResult.Error,
					LatencyMs: toolResult.LatencyMs,
				},
			})
			continue
		}

		// Convert result to string
		resultStr := string(toolResult.Result)

		logger.Debug("executeToolCalls: tool executed successfully",
			"name", tc.Name,
			"result_length", len(resultStr),
			"latency_ms", toolResult.LatencyMs)

		result.providerResponses = append(result.providerResponses, providers.ToolResponse{
			ToolCallID: tc.ID,
			Result:     resultStr,
			IsError:    false,
		})
		result.resultMessages = append(result.resultMessages, types.Message{
			Role:    "tool",
			Content: resultStr, // Set Content for template rendering (matches UnmarshalJSON behavior)
			ToolResult: &types.MessageToolResult{
				ID:        tc.ID,
				Name:      tc.Name,
				Content:   resultStr,
				LatencyMs: toolResult.LatencyMs,
			},
		})
	}

	return result
}

// sendToolResults sends tool execution results back through the pipeline to the provider,
// and includes tool result messages for state store capture.
// This matches the behavior of non-streaming mode where tool results are stored as messages.
//
// The tool result messages are sent via inputChan with metadata, and DuplexProviderStage
// forwards them to output for state store capture.
func (de *DuplexConversationExecutor) sendToolResults(
	ctx context.Context,
	execResult *toolExecutionResult,
	inputChan chan<- stage.StreamElement,
) error {
	// Send both tool responses (for provider) and tool result messages (for state store)
	// via the input channel. DuplexProviderStage handles routing appropriately.
	elem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"tool_responses":       execResult.providerResponses,
			"tool_result_messages": execResult.resultMessages,
		},
	}

	select {
	case inputChan <- elem:
		logger.Debug("sendToolResults: sent tool results to pipeline",
			"provider_responses", len(execResult.providerResponses),
			"result_messages", len(execResult.resultMessages))
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
