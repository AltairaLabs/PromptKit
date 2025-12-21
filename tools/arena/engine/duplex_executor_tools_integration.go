package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// arenaToolExecutor implements streaming.ToolExecutor using Arena's tool registry.
type arenaToolExecutor struct {
	registry *tools.Registry
}

// newArenaToolExecutor creates a new tool executor with the given registry.
func newArenaToolExecutor(registry *tools.Registry) streaming.ToolExecutor {
	if registry == nil {
		return nil
	}
	return &arenaToolExecutor{registry: registry}
}

// Execute implements streaming.ToolExecutor.
func (e *arenaToolExecutor) Execute(
	ctx context.Context,
	toolCalls []types.MessageToolCall,
) (*streaming.ToolExecutionResult, error) {
	_ = ctx // Currently unused, but kept for future async tool execution

	result := &streaming.ToolExecutionResult{
		ProviderResponses: make([]providers.ToolResponse, 0, len(toolCalls)),
		ResultMessages:    make([]types.Message, 0, len(toolCalls)),
	}

	for _, tc := range toolCalls {
		logger.Debug("arenaToolExecutor: executing tool",
			"name", tc.Name,
			"id", tc.ID,
			"args", string(tc.Args))

		// Execute tool using registry - args are already json.RawMessage
		toolResult, err := e.registry.Execute(tc.Name, tc.Args)
		if err != nil {
			logger.Error("arenaToolExecutor: tool execution failed",
				"name", tc.Name, "error", err)
			errMsg := fmt.Sprintf("tool execution failed: %s", err.Error())
			result.ProviderResponses = append(result.ProviderResponses, providers.ToolResponse{
				ToolCallID: tc.ID,
				Result:     fmt.Sprintf(`{"error": %q}`, errMsg),
				IsError:    true,
			})
			result.ResultMessages = append(result.ResultMessages, types.Message{
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
			logger.Error("arenaToolExecutor: tool returned error",
				"name", tc.Name, "error", toolResult.Error)
			result.ProviderResponses = append(result.ProviderResponses, providers.ToolResponse{
				ToolCallID: tc.ID,
				Result:     fmt.Sprintf(`{"error": %q}`, toolResult.Error),
				IsError:    true,
			})
			result.ResultMessages = append(result.ResultMessages, types.Message{
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

		logger.Debug("arenaToolExecutor: tool executed successfully",
			"name", tc.Name,
			"result_length", len(resultStr),
			"latency_ms", toolResult.LatencyMs)

		result.ProviderResponses = append(result.ProviderResponses, providers.ToolResponse{
			ToolCallID: tc.ID,
			Result:     resultStr,
			IsError:    false,
		})
		result.ResultMessages = append(result.ResultMessages, types.Message{
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

	return result, nil
}
