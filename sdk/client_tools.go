package sdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// ClientToolRequest contains information about a client-side tool invocation.
// It is passed to handlers registered via [Conversation.OnClientTool].
type ClientToolRequest struct {
	// ToolName is the tool's name as defined in the pack.
	ToolName string

	// CallID is the provider-assigned ID for this particular invocation.
	CallID string

	// Args contains the parsed arguments from the LLM.
	Args map[string]any

	// ConsentMsg is the human-readable consent message from the pack's
	// client.consent.message field. Empty when no consent is configured.
	ConsentMsg string

	// Categories are the semantic consent categories (e.g., ["location"]).
	Categories []string

	// Descriptor provides the full tool descriptor for advanced use cases.
	Descriptor *tools.ToolDescriptor
}

// ClientToolHandler is a function that fulfills a client-side tool call.
// It receives a context (carrying the tool timeout from ClientConfig.TimeoutMs)
// and a [ClientToolRequest] with the invocation details.
//
// The return value should be JSON-serializable and will be sent back to the
// LLM as the tool result.
type ClientToolHandler func(ctx context.Context, req ClientToolRequest) (any, error)

// OnClientTool registers a handler for a client-side tool.
//
// Client tools (mode: "client") are tools that must be fulfilled on the
// caller's device — for example GPS, camera, or biometric sensors. The
// handler is invoked synchronously when the LLM calls the tool.
//
// Example:
//
//	conv.OnClientTool("get_location", func(ctx context.Context, req sdk.ClientToolRequest) (any, error) {
//	    coords, err := deviceGPS(ctx, req.Args["accuracy"].(string))
//	    if err != nil {
//	        return nil, err
//	    }
//	    return map[string]any{"lat": coords.Lat, "lng": coords.Lng}, nil
//	})
func (c *Conversation) OnClientTool(name string, handler ClientToolHandler) {
	c.clientHandlersMu.Lock()
	defer c.clientHandlersMu.Unlock()
	if c.clientHandlers == nil {
		c.clientHandlers = make(map[string]ClientToolHandler)
	}
	c.clientHandlers[name] = handler
}

// OnClientTools registers multiple client tool handlers at once.
//
//	conv.OnClientTools(map[string]sdk.ClientToolHandler{
//	    "get_location": locationHandler,
//	    "read_contacts": contactsHandler,
//	})
func (c *Conversation) OnClientTools(handlers map[string]ClientToolHandler) {
	c.clientHandlersMu.Lock()
	defer c.clientHandlersMu.Unlock()
	if c.clientHandlers == nil {
		c.clientHandlers = make(map[string]ClientToolHandler)
	}
	for name, handler := range handlers {
		c.clientHandlers[name] = handler
	}
}

// clientExecutor dispatches client-mode tool calls to registered
// ClientToolHandler functions. It implements tools.Executor.
type clientExecutor struct {
	handlers   map[string]ClientToolHandler
	handlersMu *clientHandlersMuAccessor
}

// clientHandlersMuAccessor provides read access to the conversation's
// client handlers under its mutex. This avoids exposing the full
// Conversation to the executor.
type clientHandlersMuAccessor struct {
	conv *Conversation
}

func (a *clientHandlersMuAccessor) getHandler(name string) (ClientToolHandler, bool) {
	a.conv.clientHandlersMu.RLock()
	defer a.conv.clientHandlersMu.RUnlock()
	h, ok := a.conv.clientHandlers[name]
	return h, ok
}

// Name returns "client" to match mode: "client" tools.
func (e *clientExecutor) Name() string {
	return "client"
}

// Execute dispatches to the registered ClientToolHandler for the tool.
func (e *clientExecutor) Execute(
	ctx context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	// Parse args
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse client tool arguments: %w", err)
	}

	// Look up handler — first from snapshot, then live via mutex accessor
	handler, ok := e.handlers[descriptor.Name]
	if !ok && e.handlersMu != nil {
		handler, ok = e.handlersMu.getHandler(descriptor.Name)
	}
	if !ok {
		return nil, fmt.Errorf("no client handler registered for tool: %s", descriptor.Name)
	}

	// Build request
	req := ClientToolRequest{
		ToolName:   descriptor.Name,
		Args:       argsMap,
		Descriptor: descriptor,
	}

	// Populate consent fields from ClientConfig
	if descriptor.ClientConfig != nil {
		if descriptor.ClientConfig.Consent != nil {
			req.ConsentMsg = descriptor.ClientConfig.Consent.Message
		}
		req.Categories = descriptor.ClientConfig.Categories
	}

	// Execute handler
	result, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize client tool result: %w", err)
	}

	return resultJSON, nil
}
