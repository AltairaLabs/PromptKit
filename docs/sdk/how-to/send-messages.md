---
layout: default
title: Send Messages
nav_order: 3
parent: SDK How-To Guides
grand_parent: SDK
---

# How to Send Messages

Learn how to send user messages and receive LLM responses.

## Basic Message Sending

### Simple Send

```go
resp, err := conv.Send(ctx, "What is the capital of France?")
if err != nil {
    log.Fatal(err)
}

fmt.Println(resp.Content)  // "The capital of France is Paris."
fmt.Printf("Cost: $%.4f\n", resp.Cost)
```

### With Context

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

resp, err := conv.Send(ctx, "Tell me a long story")
if err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        log.Println("Request timed out")
    }
    return err
}
```

## Response Structure

### Accessing Response Data

```go
resp, _ := conv.Send(ctx, "Hello")

// Content
fmt.Println(resp.Content)  // LLM response text

// Metadata
fmt.Printf("Model: %s\n", resp.Model)
fmt.Printf("Tokens: %d in, %d out\n", resp.InputTokens, resp.OutputTokens)
fmt.Printf("Cost: $%.6f\n", resp.Cost)
fmt.Printf("Latency: %v\n", resp.Latency)

// Tool calls (if any)
if len(resp.ToolCalls) > 0 {
    fmt.Printf("Tools called: %d\n", len(resp.ToolCalls))
}
```

## Multi-Turn Conversations

### Context Retention

The SDK automatically manages conversation history:

```go
// Turn 1
resp1, _ := conv.Send(ctx, "My name is Alice")
fmt.Println(resp1.Content)  // "Nice to meet you, Alice!"

// Turn 2 - LLM remembers previous context
resp2, _ := conv.Send(ctx, "What's my name?")
fmt.Println(resp2.Content)  // "Your name is Alice."

// Turn 3
resp3, _ := conv.Send(ctx, "What did we talk about?")
fmt.Println(resp3.Content)  // References previous turns
```

### Multiple Exchanges

```go
questions := []string{
    "What is machine learning?",
    "How is it different from traditional programming?",
    "Can you give me an example?",
}

for _, q := range questions {
    fmt.Printf("Q: %s\n", q)
    
    resp, err := conv.Send(ctx, q)
    if err != nil {
        log.Printf("Error: %v\n", err)
        continue
    }
    
    fmt.Printf("A: %s\n\n", resp.Content)
}
```

## Streaming Responses

### Basic Streaming

```go
ch, err := conv.SendStream(ctx, "Tell me a long story")
if err != nil {
    log.Fatal(err)
}

for event := range ch {
    if event.Error != nil {
        log.Printf("Error: %v\n", event.Error)
        break
    }
    
    if event.Chunk != nil {
        fmt.Print(event.Chunk.Text)  // Print as it arrives
    }
}
fmt.Println() // Newline after complete response
```

### Collecting Streamed Content

```go
var fullContent strings.Builder

ch, _ := conv.SendStream(ctx, "Write a poem")

for event := range ch {
    if event.Error != nil {
        return event.Error
    }
    
    if event.Chunk != nil {
        fullContent.WriteString(event.Chunk.Text)
        fmt.Print(event.Chunk.Text) // Real-time display
    }
}

// Save complete response
complete := fullContent.String()
```

### Streaming with Metadata

```go
var totalTokens int

ch, _ := conv.SendStream(ctx, "Explain quantum computing")

for event := range ch {
    if event.Error != nil {
        break
    }
    
    if event.Chunk != nil {
        fmt.Print(event.Chunk.Text)
        
        // Track tokens
        if event.Chunk.TokenCount > 0 {
            totalTokens += event.Chunk.TokenCount
        }
    }
}

fmt.Printf("\nTotal tokens: %d\n", totalTokens)
```

## Send Options

### With Options

```go
resp, err := conv.Send(ctx, "Search for latest news", sdk.SendOptions{
    MaxToolCalls: 3,  // Limit tool iterations
    Metadata: map[string]interface{}{
        "request_id": "req-123",
        "source":     "web_ui",
    },
})
```

### Streaming with Options

```go
ch, err := conv.SendStream(ctx, "Generate report", sdk.SendOptions{
    Stream:       true,
    MaxToolCalls: 5,
})
```

## Error Handling

### Basic Error Handling

```go
resp, err := conv.Send(ctx, "Hello")
if err != nil {
    log.Printf("Send failed: %v", err)
    return err
}
```

### Checking Error Types

```go
resp, err := conv.Send(ctx, "Hello")
if err != nil {
    if sdk.IsRetryableError(err) {
        // Retry with exponential backoff
        time.Sleep(time.Second)
        resp, err = conv.Send(ctx, "Hello")
    } else if sdk.IsTemporaryError(err) {
        // Wait briefly and retry
        time.Sleep(500 * time.Millisecond)
        resp, err = conv.Send(ctx, "Hello")
    } else {
        // Fatal error
        return err
    }
}
```

### Retry Pattern

```go
func sendWithRetry(conv *sdk.Conversation, ctx context.Context, message string, maxRetries int) (*sdk.Response, error) {
    var resp *sdk.Response
    var err error
    
    for attempt := 0; attempt <= maxRetries; attempt++ {
        resp, err = conv.Send(ctx, message)
        if err == nil {
            return resp, nil
        }
        
        if !sdk.IsRetryableError(err) {
            return nil, err  // Don't retry
        }
        
        if attempt < maxRetries {
            backoff := time.Duration(attempt+1) * time.Second
            time.Sleep(backoff)
        }
    }
    
    return nil, fmt.Errorf("max retries exceeded: %w", err)
}
```

## Advanced Patterns

### Conditional Responses

```go
resp, _ := conv.Send(ctx, "Is Python good for ML?")

if strings.Contains(strings.ToLower(resp.Content), "yes") {
    // Follow-up question
    resp2, _ := conv.Send(ctx, "What are the best Python ML libraries?")
    fmt.Println(resp2.Content)
}
```

### Response Validation

```go
resp, _ := conv.Send(ctx, "What's 2+2?")

// Validate response
if !strings.Contains(resp.Content, "4") {
    log.Println("Warning: Unexpected response")
}

// Check confidence (if available in metadata)
if resp.Metadata != nil {
    if confidence, ok := resp.Metadata["confidence"].(float64); ok {
        if confidence < 0.8 {
            log.Println("Low confidence response")
        }
    }
}
```

### Batch Processing

```go
func processBatch(conv *sdk.Conversation, messages []string) ([]string, error) {
    responses := make([]string, len(messages))
    
    for i, msg := range messages {
        resp, err := conv.Send(ctx, msg)
        if err != nil {
            return nil, fmt.Errorf("message %d failed: %w", i, err)
        }
        responses[i] = resp.Content
    }
    
    return responses, nil
}
```

### Concurrent Conversations

```go
func handleMultipleUsers(manager *sdk.ConversationManager, pack *sdk.Pack, users []string) {
    var wg sync.WaitGroup
    
    for _, userID := range users {
        wg.Add(1)
        go func(uid string) {
            defer wg.Done()
            
            conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
                UserID:     uid,
                PromptName: "assistant",
            })
            
            resp, _ := conv.Send(ctx, "Hello!")
            fmt.Printf("[%s] %s\n", uid, resp.Content)
        }(userID)
    }
    
    wg.Wait()
}
```

## Cost Tracking

### Per-Message Costs

```go
var totalCost float64

messages := []string{"Question 1?", "Question 2?", "Question 3?"}

for _, msg := range messages {
    resp, err := conv.Send(ctx, msg)
    if err != nil {
        continue
    }
    
    totalCost += resp.Cost
    fmt.Printf("Message: $%.6f\n", resp.Cost)
}

fmt.Printf("Total: $%.4f\n", totalCost)
```

### Budget Limiting

```go
const maxBudget = 1.00  // $1.00 limit

var spent float64

for {
    if spent >= maxBudget {
        log.Println("Budget exceeded")
        break
    }
    
    resp, err := conv.Send(ctx, getNextMessage())
    if err != nil {
        break
    }
    
    spent += resp.Cost
    fmt.Printf("Spent: $%.4f / $%.2f\n", spent, maxBudget)
}
```

## Performance Tips

### Reuse Conversations

```go
// ✅ Good: Reuse conversation for same user/session
conv, _ := manager.NewConversation(ctx, pack, config)

for msg := range userMessages {
    resp, _ := conv.Send(ctx, msg)
    // Process response
}
```

### Parallel Processing

```go
// Process multiple independent messages concurrently
func processParallel(manager *sdk.ConversationManager, pack *sdk.Pack, messages []string) {
    results := make(chan string, len(messages))
    
    for _, msg := range messages {
        go func(m string) {
            conv, _ := manager.NewConversation(ctx, pack, config)
            resp, _ := conv.Send(ctx, m)
            results <- resp.Content
        }(msg)
    }
    
    for range messages {
        result := <-results
        fmt.Println(result)
    }
}
```

### Context Management

```go
// Configure token budget to control costs
conv, _ := manager.NewConversation(ctx, pack, sdk.ConversationConfig{
    UserID:     "user123",
    PromptName: "assistant",
    ContextPolicy: &middleware.ContextBuilderPolicy{
        MaxInputTokens: 4000,  // Limit context size
        Strategy:       middleware.StrategyTruncateOldest,
    },
})
```

## Common Mistakes

### ❌ Not Checking Errors

```go
resp, _ := conv.Send(ctx, "Hello")  // Ignoring error
fmt.Println(resp.Content)           // May panic if resp is nil
```

### ✅ Always Check Errors

```go
resp, err := conv.Send(ctx, "Hello")
if err != nil {
    log.Printf("Error: %v", err)
    return err
}
fmt.Println(resp.Content)
```

### ❌ Blocking on Streams

```go
ch, _ := conv.SendStream(ctx, "Story")
// Don't do other work while stream is active
time.Sleep(time.Second)  // Blocks streaming
```

### ✅ Process Stream Immediately

```go
ch, _ := conv.SendStream(ctx, "Story")
for event := range ch {
    // Process immediately
    if event.Chunk != nil {
        fmt.Print(event.Chunk.Text)
    }
}
```

## Troubleshooting

### Empty Responses

```go
resp, _ := conv.Send(ctx, "Hello")
if resp.Content == "" {
    log.Println("Empty response - check prompt configuration")
}
```

### Timeouts

```go
// Increase timeout for long operations
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

resp, err := conv.Send(ctx, "Complex task")
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("Operation timed out - consider increasing timeout")
}
```

### High Costs

```go
// Monitor token usage
resp, _ := conv.Send(ctx, msg)
if resp.InputTokens > 10000 {
    log.Printf("Warning: High input tokens (%d) - check context size", resp.InputTokens)
}
```

## Next Steps

- **[Stream Responses](stream-responses.md)** - Real-time streaming
- **[Handle Tool Calls](handle-tool-calls.md)** - Function calling
- **[Error Handling](error-handling.md)** - Robust error handling
- **[Tutorial: Conversation Handling](../tutorials/02-conversation-handling.md)** - Complete guide

## See Also

- [Conversation Reference](../reference/conversation.md)
- [Response Type Reference](../reference/types.md#response)
- [SendOptions Reference](../reference/types.md#sendoptions)
