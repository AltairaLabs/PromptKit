package sdk

import "context"

// StreamEvent is the interface for all stream event types.
// Use a type switch to handle specific events.
type StreamEvent interface {
	streamEvent() // marker method
}

// TextDeltaEvent is emitted for each text delta during streaming.
type TextDeltaEvent struct {
	// Delta is the new text content in this chunk.
	Delta string
	// ReplacesText marks a Delta that replaces all the text streamed before it
	// in this turn (StreamChunk.ReplacesText): discard what was shown and
	// show Delta instead.
	ReplacesText bool
}

func (TextDeltaEvent) streamEvent() { /* marker method — intentionally empty */ }

// ClientToolRequestEvent is emitted when the pipeline encounters a client tool
// that needs caller fulfillment.
type ClientToolRequestEvent struct {
	// CallID is the provider-assigned ID for this tool invocation.
	CallID string

	// ToolName is the tool's name as defined in the pack.
	ToolName string

	// Args contains the parsed arguments from the LLM.
	Args map[string]any

	// ConsentMsg is the human-readable consent message.
	ConsentMsg string

	// Categories are the semantic consent categories.
	Categories []string
}

func (ClientToolRequestEvent) streamEvent() { /* marker method — intentionally empty */ }

// ToolApprovalEvent is emitted, before StreamDoneEvent, for each call an
// OnToolAsync check holds for approval: the turn is paused, not finished.
// Resolve the call with ResolveTool or RejectTool, then Continue.
type ToolApprovalEvent struct {
	// Tool is the held call, as Response.PendingTools reports it.
	Tool PendingTool
}

func (ToolApprovalEvent) streamEvent() { /* marker method — intentionally empty */ }

// StreamDoneEvent is emitted when the stream completes.
type StreamDoneEvent struct {
	// Response contains the complete response with metadata.
	Response *Response
}

func (StreamDoneEvent) streamEvent() { /* marker method — intentionally empty */ }

// StreamEventHandler is called for each event during streaming.
type StreamEventHandler func(event StreamEvent)

// OnStreamEvent registers a handler that will be called for each stream event
// during [Conversation.StreamWithCallback].
//
// Example:
//
//	conv.OnStreamEvent(func(event sdk.StreamEvent) {
//	    switch e := event.(type) {
//	    case sdk.TextDeltaEvent:
//	        fmt.Print(e.Delta)
//	    case sdk.ClientToolRequestEvent:
//	        fmt.Printf("Tool %s needs fulfillment\n", e.ToolName)
//	    case sdk.ToolApprovalEvent:
//	        fmt.Printf("Tool %s awaits approval\n", e.Tool.Name)
//	    case sdk.StreamDoneEvent:
//	        fmt.Println("\nDone!")
//	    }
//	})
func (c *Conversation) OnStreamEvent(handler StreamEventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamEventHandler = handler
}

// StreamWithCallback sends a message and invokes the registered [StreamEventHandler]
// for each chunk. This is a convenience wrapper around [Conversation.Stream] that
// translates chunks into typed events.
//
// If no handler has been registered via [Conversation.OnStreamEvent], this behaves
// like Stream() but discards all chunks and returns the final response.
//
// Returns the complete Response or an error.
func (c *Conversation) StreamWithCallback(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	c.mu.RLock()
	handler := c.streamEventHandler
	c.mu.RUnlock()

	var finalResp *Response

	for chunk := range c.Stream(ctx, message, opts...) {
		if chunk.Error != nil {
			return nil, chunk.Error
		}

		if handler == nil {
			if chunk.Type == ChunkDone {
				finalResp = chunk.Message
			}
			continue
		}

		if chunk.Type == ChunkDone {
			finalResp = chunk.Message
		}
		dispatchStreamEvents(handler, &chunk)
	}

	return finalResp, nil
}

// dispatchStreamEvents hands the handler the events one chunk carries.
func dispatchStreamEvents(handler StreamEventHandler, chunk *StreamChunk) {
	switch chunk.Type {
	case ChunkText:
		handler(TextDeltaEvent{Delta: chunk.Text, ReplacesText: chunk.ReplacesText})
	case ChunkClientTool:
		if chunk.ClientTool != nil {
			handler(ClientToolRequestEvent{
				CallID:     chunk.ClientTool.CallID,
				ToolName:   chunk.ClientTool.ToolName,
				Args:       chunk.ClientTool.Args,
				ConsentMsg: chunk.ClientTool.ConsentMsg,
				Categories: chunk.ClientTool.Categories,
			})
		}
	case ChunkDone:
		// A call held for approval leaves the turn paused, not done.
		if chunk.Message != nil {
			for _, held := range chunk.Message.PendingTools() {
				handler(ToolApprovalEvent{Tool: held})
			}
		}
		handler(StreamDoneEvent{Response: chunk.Message})
	case ChunkToolCall, ChunkToolResult, ChunkMedia, ChunkReasoning, ChunkProviderCallEnd:
		// No event type defined yet.
	}
}
