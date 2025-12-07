// Package hooks provides convenience methods for subscribing to SDK events.
//
// This package wraps the runtime's [events.EventBus] with ergonomic helpers
// for common observability patterns. All hooks use the runtime's event types
// directly - this package adds no new event types.
//
// # Basic Usage
//
// Use [OnEvent] to subscribe to all events:
//
//	hooks.OnEvent(conv, func(e *events.Event) {
//	    log.Printf("[%s] %s", e.Type, e.Timestamp)
//	})
//
// Use [On] to subscribe to specific event types:
//
//	hooks.On(conv, events.EventToolCallStarted, func(e *events.Event) {
//	    data := e.Data.(*events.ToolCallStartedData)
//	    log.Printf("Tool: %s", data.ToolName)
//	})
//
// # Convenience Hooks
//
// For common patterns, use the typed convenience methods:
//
//	hooks.OnToolCall(conv, func(name string, args map[string]any) {
//	    log.Printf("Calling tool: %s", name)
//	})
//
//	hooks.OnValidationFailed(conv, func(validator string, err error) {
//	    log.Printf("Validation %s failed: %v", validator, err)
//	})
package hooks
