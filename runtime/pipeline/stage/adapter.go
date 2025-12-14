package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MiddlewareAdapter wraps a legacy pipeline.Middleware as a Stage.
// This provides backward compatibility, allowing existing middleware to work
// in the new streaming architecture without modification.
//
// The adapter:
// 1. Accumulates input elements into an ExecutionContext
// 2. Calls the middleware's Process() method with a no-op next() function
// 3. Emits the resulting state as output elements
//
// Note: This adapter is for request/response middleware. Streaming middleware
// (like VAD or TTS) should be converted to native stages for optimal performance.
type MiddlewareAdapter struct {
	BaseStage
	middleware pipeline.Middleware
}

// NewMiddlewareAdapter creates a new adapter wrapping the given middleware.
func NewMiddlewareAdapter(name string, middleware pipeline.Middleware) *MiddlewareAdapter {
	return &MiddlewareAdapter{
		BaseStage:  NewBaseStage(name, StageTypeTransform),
		middleware: middleware,
	}
}

// Process implements the Stage interface by wrapping the middleware execution.
//
//nolint:lll // Channel signature cannot be shortened
func (a *MiddlewareAdapter) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Accumulate input into ExecutionContext
	execCtx, err := a.accumulateInput(ctx, input)
	if err != nil {
		output <- NewErrorElement(err)
		return err
	}

	// Call the middleware's Process() method with a no-op next() function
	// Since we're wrapping a single middleware, next() does nothing
	noOpNext := func() error { return nil }
	err = a.middleware.Process(execCtx, noOpNext)
	if err != nil {
		output <- NewErrorElement(err)
		return err
	}

	// Emit the resulting state as output elements
	return a.emitOutput(ctx, execCtx, output)
}

// accumulateInput collects all input elements and builds an ExecutionContext.
//
//nolint:gocognit,lll // Complexity inherent to adapter logic, channel signature cannot be shortened
func (a *MiddlewareAdapter) accumulateInput(ctx context.Context, input <-chan StreamElement) (*pipeline.ExecutionContext, error) {
	execCtx := &pipeline.ExecutionContext{
		Context:  ctx,
		Messages: []types.Message{},
		Metadata: make(map[string]interface{}),
		Trace: pipeline.ExecutionTrace{
			LLMCalls: []pipeline.LLMCall{},
			Events:   []pipeline.TraceEvent{},
		},
	}

	// Collect all input elements
	for elem := range input {
		// Handle errors
		if elem.Error != nil {
			execCtx.Error = elem.Error
			return execCtx, elem.Error
		}

		// Accumulate messages
		if elem.Message != nil {
			execCtx.Messages = append(execCtx.Messages, *elem.Message)
		}

		// Accumulate text as a user message
		if elem.Text != nil {
			execCtx.Messages = append(execCtx.Messages, types.Message{
				Role:    "user",
				Content: *elem.Text,
			})
		}

		// Merge metadata
		for k, v := range elem.Metadata {
			// Special handling for known metadata keys
			switch k {
			case "system_prompt":
				if sp, ok := v.(string); ok {
					execCtx.SystemPrompt = sp
				}
			case "variables":
				if vars, ok := v.(map[string]string); ok {
					execCtx.Variables = vars
				}
			case "allowed_tools":
				if tools, ok := v.([]string); ok {
					execCtx.AllowedTools = tools
				}
			default:
				execCtx.Metadata[k] = v
			}
		}
	}

	return execCtx, nil
}

// emitOutput converts the ExecutionContext state to output elements.
//
//nolint:gocognit,gocritic,lll // Complexity inherent to adapter logic, rangeValCopy acceptable for clarity, channel signature cannot be shortened
func (a *MiddlewareAdapter) emitOutput(ctx context.Context, execCtx *pipeline.ExecutionContext, output chan<- StreamElement) error {
	// Emit all messages
	for _, msg := range execCtx.Messages {
		msgCopy := msg
		elem := StreamElement{
			Message:   &msgCopy,
			Timestamp: timeNow(),
			Priority:  PriorityNormal,
			Metadata:  make(map[string]interface{}),
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Emit response as a separate element (if different from last message)
	if execCtx.Response != nil && len(execCtx.Messages) > 0 {
		lastMsg := execCtx.Messages[len(execCtx.Messages)-1]
		if lastMsg.Role != execCtx.Response.Role || lastMsg.Content != execCtx.Response.Content {
			// Response is different from last message, emit it
			responseMsg := types.Message{
				Role:    execCtx.Response.Role,
				Content: execCtx.Response.Content,
				Parts:   execCtx.Response.Parts,
			}
			elem := StreamElement{
				Message:   &responseMsg,
				Timestamp: timeNow(),
				Priority:  PriorityNormal,
				Metadata:  make(map[string]interface{}),
			}

			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Emit metadata element with context state
	metadataElem := StreamElement{
		Timestamp: timeNow(),
		Priority:  PriorityLow,
		Metadata:  make(map[string]interface{}),
	}

	// Copy relevant context fields to metadata
	if execCtx.SystemPrompt != "" {
		metadataElem.Metadata["system_prompt"] = execCtx.SystemPrompt
	}
	if len(execCtx.Variables) > 0 {
		metadataElem.Metadata["variables"] = execCtx.Variables
	}
	if len(execCtx.AllowedTools) > 0 {
		metadataElem.Metadata["allowed_tools"] = execCtx.AllowedTools
	}
	if len(execCtx.Trace.LLMCalls) > 0 {
		metadataElem.Metadata["trace"] = execCtx.Trace
	}
	if execCtx.CostInfo.TotalCost > 0 {
		metadataElem.Metadata["cost_info"] = execCtx.CostInfo
	}

	// Merge user metadata
	for k, v := range execCtx.Metadata {
		metadataElem.Metadata[k] = v
	}

	// Only emit if there's actually metadata to send
	if len(metadataElem.Metadata) > 0 {
		select {
		case output <- metadataElem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// WrapMiddleware is a convenience function to wrap a middleware as a stage.
func WrapMiddleware(name string, middleware pipeline.Middleware) Stage {
	return NewMiddlewareAdapter(name, middleware)
}
