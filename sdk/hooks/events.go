package hooks

import (
	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// EventSource is an interface for objects that provide an EventBus.
// This is typically a [sdk.Conversation].
type EventSource interface {
	EventBus() *events.EventBus
}

// OnEvent subscribes to all events from the source.
//
// This is useful for logging, debugging, or building custom dashboards:
//
//	hooks.OnEvent(conv, func(e *events.Event) {
//	    log.Printf("[%s] %s: %+v", e.Type, e.Timestamp, e.Data)
//	})
func OnEvent(source EventSource, handler func(*events.Event)) {
	if bus := source.EventBus(); bus != nil {
		bus.SubscribeAll(handler)
	}
}

// On subscribes to a specific event type from the source.
//
// Use this for targeted event handling:
//
//	hooks.On(conv, events.EventToolCallStarted, func(e *events.Event) {
//	    data := e.Data.(*events.ToolCallStartedData)
//	    log.Printf("Tool: %s", data.ToolName)
//	})
func On(source EventSource, eventType events.EventType, handler func(*events.Event)) {
	if bus := source.EventBus(); bus != nil {
		bus.Subscribe(eventType, handler)
	}
}

// ToolCallHandler is called when a tool is invoked.
type ToolCallHandler func(name string, args map[string]any)

// OnToolCall subscribes to tool call events.
//
// This provides a simplified interface for monitoring tool execution:
//
//	hooks.OnToolCall(conv, func(name string, args map[string]any) {
//	    log.Printf("Tool %s called with %v", name, args)
//	})
func OnToolCall(source EventSource, handler ToolCallHandler) {
	On(source, events.EventToolCallStarted, func(e *events.Event) {
		if data, ok := e.Data.(*events.ToolCallStartedData); ok {
			handler(data.ToolName, data.Args)
		}
	})
}

// ValidationHandler is called when validation fails.
type ValidationHandler func(validatorName string, err error)

// OnValidationFailed subscribes to validation failure events.
//
// Use this to monitor and log validation issues:
//
//	hooks.OnValidationFailed(conv, func(validator string, err error) {
//	    log.Printf("Validation %s failed: %v", validator, err)
//	})
func OnValidationFailed(source EventSource, handler ValidationHandler) {
	On(source, events.EventValidationFailed, func(e *events.Event) {
		if data, ok := e.Data.(*events.ValidationFailedData); ok {
			handler(data.ValidatorName, data.Error)
		}
	})
}

// ProviderCallHandler is called when a provider (LLM) call completes.
type ProviderCallHandler func(model string, inputTokens, outputTokens int, cost float64)

// OnProviderCall subscribes to provider call completion events.
//
// Use this to track costs and token usage:
//
//	hooks.OnProviderCall(conv, func(model string, in, out int, cost float64) {
//	    log.Printf("Model %s: %d in, %d out, $%.4f", model, in, out, cost)
//	})
func OnProviderCall(source EventSource, handler ProviderCallHandler) {
	On(source, events.EventProviderCallCompleted, func(e *events.Event) {
		if data, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			handler(data.Model, data.InputTokens, data.OutputTokens, data.Cost)
		}
	})
}

// PipelineHandler is called when a pipeline completes.
type PipelineHandler func(totalCost float64, inputTokens, outputTokens int)

// OnPipelineComplete subscribes to pipeline completion events.
//
// Use this for aggregate metrics:
//
//	hooks.OnPipelineComplete(conv, func(cost float64, in, out int) {
//	    metrics.RecordCost(cost)
//	    metrics.RecordTokens(in + out)
//	})
func OnPipelineComplete(source EventSource, handler PipelineHandler) {
	On(source, events.EventPipelineCompleted, func(e *events.Event) {
		if data, ok := e.Data.(*events.PipelineCompletedData); ok {
			handler(data.TotalCost, data.InputTokens, data.OutputTokens)
		}
	})
}
