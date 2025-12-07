# Streaming Example

Real-time response streaming with SDK v2.

## What You'll Learn

- Using `Stream()` for real-time responses
- Processing chunks as they arrive
- Handling stream completion and errors
- Progress tracking during generation

## Prerequisites

- Go 1.21+
- OpenAI API key

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## Code Overview

```go
conv, err := sdk.Open("./streaming.pack.json", "storyteller")
if err != nil {
    log.Fatal(err)
}
defer conv.Close()

ctx := context.Background()

// Stream responses in real-time
for chunk := range conv.Stream(ctx, "Tell me a short story") {
    if chunk.Error != nil {
        log.Printf("Error: %v", chunk.Error)
        break
    }
    if chunk.Type == sdk.ChunkDone {
        fmt.Println("\n[Complete]")
        break
    }
    // Print text as it arrives
    fmt.Print(chunk.Text)
}
```

## Chunk Types

```go
const (
    ChunkText     // Text content arrived
    ChunkToolCall // Tool is being called
    ChunkDone     // Stream completed
)
```

## Pack File Structure

The `streaming.pack.json` defines:

- **Provider**: OpenAI with `gpt-4o-mini`
- **Prompt**: A creative storyteller with higher temperature

```json
{
  "prompts": {
    "storyteller": {
      "system_template": "You are a creative storyteller...",
      "parameters": {
        "temperature": 0.9,
        "max_tokens": 500
      }
    }
  }
}
```

## Progress Tracking

Track generation progress while streaming:

```go
var charCount int

for chunk := range conv.Stream(ctx, "Tell me about AI") {
    if chunk.Type == sdk.ChunkDone {
        fmt.Printf("\n[Complete - %d characters]\n", charCount)
        break
    }
    fmt.Print(chunk.Text)
    charCount += len(chunk.Text)
}
```

## Key Concepts

1. **Channel-Based** - `Stream()` returns a channel of chunks
2. **Non-Blocking** - Print responses as they arrive
3. **Error Handling** - Check `chunk.Error` for issues
4. **Completion** - `ChunkDone` signals end of stream

## Next Steps

- [Hello Example](../hello/) - Basic conversation
- [Tools Example](../tools/) - Function calling
- [HITL Example](../hitl/) - Human-in-the-loop approval
