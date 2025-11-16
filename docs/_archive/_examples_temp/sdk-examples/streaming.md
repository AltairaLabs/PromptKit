---
layout: default
title: streaming
parent: SDK Examples
grand_parent: Guides
---

# Streaming Example

This example demonstrates how to use streaming responses with the PromptKit SDK. Streaming is essential for building responsive user interfaces, especially for WebSocket-based real-time applications.

## Features Demonstrated

- ✅ **Real-time streaming**: Content arrives as it's generated
- ✅ **Event-based API**: Handle content, tool calls, errors, and completion separately
- ✅ **Conversation state**: Maintains history across streaming turns
- ✅ **Cost tracking**: Monitor tokens and costs in real-time
- ✅ **Error handling**: Graceful error handling during streaming

## Running the Example

1. Set your OpenAI API key:
```bash
export OPENAI_API_KEY=your-api-key-here
```

2. Run the example:
```bash
go run main.go
```

## Expected Output

```
🤖 Streaming conversation started (ID: conv_1730073600000000000)

User: Tell me a short story about a robot.
Assistant: [Full response displayed at once]

💰 Cost: $0.0032 | ⏱️  Latency: 1234ms | 🎫 Tokens: 156

User: Now tell me another one, but shorter.
Assistant: [Content streams character by character in real-time]

💰 Cost: $0.0021 | ⏱️  Latency: 892ms | 🎫 Tokens: 98

📜 Conversation History:
  1. [user] Tell me a short story about a robot.
  2. [assistant] Once upon a time, in a world powered by circuits and cod...
  3. [user] Now tell me another one, but shorter.
  4. [assistant] A tiny robot discovered a flower growing through concret...
```

## Stream Event Types

The SDK emits the following event types:

### `"content"`
- **When**: New content delta arrives from the LLM
- **Data**: `event.Content` contains the text delta
- **Use**: Print/append to display in real-time

```go
case "content":
    fmt.Print(event.Content)
```

### `"tool_call"`
- **When**: The LLM invokes a tool
- **Data**: `event.ToolCall` contains the tool name and arguments
- **Use**: Execute tools, show UI indicators

```go
case "tool_call":
    fmt.Printf("[Tool: %s]\n", event.ToolCall.Name)
```

### `"error"`
- **When**: An error occurs during streaming
- **Data**: `event.Error` contains the error
- **Use**: Show error messages, retry logic

```go
case "error":
    log.Printf("Error: %v", event.Error)
```

### `"done"`
- **When**: Stream completes successfully
- **Data**: `event.Final` contains the complete Response
- **Use**: Update UI, show final stats, save to database

```go
case "done":
    if event.Final != nil {
        fmt.Printf("Cost: $%.4f\n", event.Final.Cost)
    }
```

## Integration with WebSocket APIs

This pattern works perfectly with WebSocket servers:

```go
// In your WebSocket handler
streamChan, err := conv.SendStream(ctx, userMessage)
if err != nil {
    websocket.WriteJSON(map[string]interface{}{
        "type": "error",
        "error": err.Error(),
    })
    return
}

// Forward stream events to WebSocket
for event := range streamChan {
    websocket.WriteJSON(map[string]interface{}{
        "type":    event.Type,
        "content": event.Content,
        "final":   event.Final,
    })
}
```

## React Native Integration

On the client side, you can process these events to update the UI:

```javascript
const ws = new WebSocket('wss://api.example.com/chat');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  
  switch (data.type) {
    case 'content':
      // Append to message buffer
      setMessage(prev => prev + data.content);
      break;
      
    case 'tool_call':
      // Show tool execution indicator
      setToolExecuting(data.tool_call.name);
      break;
      
    case 'done':
      // Finalize message, update stats
      setComplete(true);
      setStats(data.final);
      break;
      
    case 'error':
      // Show error to user
      showError(data.error);
      break;
  }
};

ws.send(JSON.stringify({
  action: 'send_message',
  message: userInput,
}));
```

## Performance Considerations

- **Buffer Size**: The SDK uses a default buffer size of 10 events. This is configurable via pipeline configuration.
- **Backpressure**: If the client can't keep up, the pipeline will naturally apply backpressure.
- **Cancellation**: Use context cancellation to stop streaming mid-flight:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

streamChan, err := conv.SendStream(ctx, message)
// Stream will automatically stop after 30 seconds
```

## Comparison: Streaming vs Non-Streaming

| Aspect | Non-Streaming (`Send`) | Streaming (`SendStream`) |
|--------|------------------------|--------------------------|
| **Latency** | Waits for full response | First token arrives quickly |
| **UX** | All-or-nothing | Progressive loading |
| **Bandwidth** | Single response | Multiple chunks |
| **Complexity** | Simple | Event handling required |
| **Best For** | Batch processing | Interactive chat |

## Next Steps

- Check out `../tools/` for tool execution examples
- See `../custom-middleware/` for advanced pipeline customization
- Read the SDK documentation for more configuration options
