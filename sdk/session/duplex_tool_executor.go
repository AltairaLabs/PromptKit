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

// hitlAction describes the outcome of an HITL check for a single tool call.
type hitlAction int

const (
	hitlNone    hitlAction = iota // Not an async tool — proceed to registry
	hitlGated                     // Tool requires human approval
	hitlHandled                   // HITL checker executed the handler directly
)

// checkHITLGate consults the checker for a single tool call and returns
// the action to take plus the check result (if any).
func checkHITLGate(
	checker AsyncToolChecker, tc types.MessageToolCall,
) (hitlAction, map[string]any, *AsyncToolCheckResult) {
	var argsMap map[string]any
	if tc.Args != nil {
		_ = json.Unmarshal(tc.Args, &argsMap)
	}
	checkResult := checker(tc.ID, tc.Name, argsMap)
	if checkResult == nil {
		return hitlNone, argsMap, nil
	}
	if checkResult.ShouldWait {
		return hitlGated, argsMap, checkResult
	}
	if checkResult.Handled {
		return hitlHandled, argsMap, checkResult
	}
	return hitlNone, argsMap, nil
}

// executeDuplexToolCalls processes tool calls from the LLM in duplex mode.
// It uses ExecuteAsync to distinguish between sync handlers (ToolStatusComplete)
// and deferred client tools (ToolStatusPending).
// If checker is non-nil, it is consulted before execution to support HITL gating.
func executeDuplexToolCalls(
	registry *tools.Registry,
	toolCalls []types.MessageToolCall,
	checker AsyncToolChecker,
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

		if checker != nil {
			action, argsMap, checkResult := checkHITLGate(checker, tc)
			switch action { //nolint:exhaustive // hitlNone falls through to registry below
			case hitlGated:
				logger.Debug("duplexToolExecutor: tool gated by HITL",
					"name", tc.Name, "id", tc.ID, "reason", checkResult.PendingInfo.Reason)
				result.Pending = append(result.Pending, tools.PendingToolExecution{
					CallID:      tc.ID,
					ToolName:    tc.Name,
					Args:        argsMap,
					PendingInfo: checkResult.PendingInfo,
				})
				continue
			case hitlHandled:
				addHandledResult(result, tc, checkResult)
				continue
			default:
				// hitlNone — fall through to registry
			}
		}

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

// addHandledResult appends a result for a tool that was handled directly by the HITL checker
// (i.e., the check passed and the handler executed immediately).
func addHandledResult(result *duplexToolExecutionResult, tc types.MessageToolCall, cr *AsyncToolCheckResult) {
	isError := cr.HandlerError != nil
	var resultStr string
	if isError {
		resultStr = cr.HandlerError.Error()
	} else {
		resultStr = string(cr.HandlerResult)
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
	result.Completed.ResultMessages = append(
		result.Completed.ResultMessages, types.NewToolResultMessage(toolResult),
	)
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
