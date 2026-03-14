package session

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/streaming"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// duplexToolExecutionResult extends the streaming result with pending tool info.
type duplexToolExecutionResult struct {
	// Completed contains results for tools that executed synchronously.
	Completed *streaming.ToolExecutionResult
	// Pending contains tools that need to be fulfilled by the caller.
	Pending []tools.PendingToolExecution
}

// executeDuplexToolCalls processes tool calls from the LLM in duplex mode.
// It uses ExecuteAsync to distinguish between sync handlers (ToolStatusComplete)
// and deferred client tools (ToolStatusPending).
func executeDuplexToolCalls(
	registry *tools.Registry,
	toolCalls []types.MessageToolCall,
) *duplexToolExecutionResult {
	result := &duplexToolExecutionResult{
		Completed: &streaming.ToolExecutionResult{
			ProviderResponses: make([]providers.ToolResponse, 0, len(toolCalls)),
			ResultMessages:    make([]types.Message, 0, len(toolCalls)),
		},
	}

	for _, tc := range toolCalls {
		logger.Debug("duplexToolExecutor: executing tool",
			"name", tc.Name, "id", tc.ID)

		callCtx := tools.WithCallID(context.Background(), tc.ID)
		asyncResult, err := registry.ExecuteAsync(callCtx, tc.Name, tc.Args)
		if err != nil {
			addToolError(result, tc, err)
			continue
		}

		switch asyncResult.Status { //nolint:exhaustive // default handles Complete+Failed
		case tools.ToolStatusPending:
			addPendingTool(result, tc, asyncResult)
		default:
			addCompletedTool(result, tc, asyncResult)
		}
	}

	return result
}

// addToolError appends an error result for a tool that failed to execute.
func addToolError(result *duplexToolExecutionResult, tc types.MessageToolCall, err error) {
	logger.Error("duplexToolExecutor: tool execution failed",
		"name", tc.Name, "error", err)
	errMsg := fmt.Sprintf("tool execution failed: %s", err.Error())
	result.Completed.ProviderResponses = append(result.Completed.ProviderResponses, providers.ToolResponse{
		ToolCallID: tc.ID,
		Result:     fmt.Sprintf(`{"error": %q}`, errMsg),
		IsError:    true,
	})
	errResult := types.NewTextToolResult(tc.ID, tc.Name, errMsg)
	errResult.Error = errMsg
	result.Completed.ResultMessages = append(
		result.Completed.ResultMessages, types.NewToolResultMessage(errResult),
	)
}

// addPendingTool appends a pending tool execution to the result.
func addPendingTool(
	result *duplexToolExecutionResult, tc types.MessageToolCall, ar *tools.ToolExecutionResult,
) {
	var argsMap map[string]any
	if tc.Args != nil {
		_ = json.Unmarshal(tc.Args, &argsMap)
	}
	result.Pending = append(result.Pending, tools.PendingToolExecution{
		CallID:      tc.ID,
		ToolName:    tc.Name,
		Args:        argsMap,
		PendingInfo: ar.PendingInfo,
	})
}

// addCompletedTool appends a completed (or failed) tool result.
func addCompletedTool(
	result *duplexToolExecutionResult, tc types.MessageToolCall, ar *tools.ToolExecutionResult,
) {
	isError := ar.Status == tools.ToolStatusFailed
	resultStr := string(ar.Content)
	if isError && ar.Error != "" {
		resultStr = ar.Error
	}

	result.Completed.ProviderResponses = append(result.Completed.ProviderResponses, providers.ToolResponse{
		ToolCallID: tc.ID,
		Result:     resultStr,
		IsError:    isError,
	})

	toolResult := types.NewTextToolResult(tc.ID, tc.Name, resultStr)
	if isError {
		toolResult.Error = resultStr
	}
	if len(ar.Parts) > 0 {
		toolResult.Parts = ar.Parts
	}
	result.Completed.ResultMessages = append(
		result.Completed.ResultMessages, types.NewToolResultMessage(toolResult),
	)
}
