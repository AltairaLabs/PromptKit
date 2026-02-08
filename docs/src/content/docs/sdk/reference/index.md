---
title: SDK Reference
sidebar:
  order: 0
---
Complete reference documentation for the PromptKit SDK API.

## Overview

The SDK provides a pack-first API that reduces boilerplate by ~80%.

## Core Functions

### sdk.Open

Opens a conversation from a pack file.

```go
func Open(packPath string, promptName string, opts ...Option) (*Conversation, error)
```

**Parameters:**
- `packPath` - Path to the .pack.json file
- `promptName` - Name of the prompt from the pack
- `opts` - Optional configuration options

**Returns:**
- `*Conversation` - Ready-to-use conversation
- `error` - Error if pack or prompt not found

**Example:**
```go
conv, err := sdk.Open("./app.pack.json", "assistant")
```

## Options

### WithModel

Override the model.

```go
sdk.WithModel("gpt-4o")
```

### WithTemperature

Override temperature.

```go
sdk.WithTemperature(0.8)
```

### WithMaxTokens

Override max tokens.

```go
sdk.WithMaxTokens(2000)
```

### WithImageFile

Attach an image from a file.

```go
sdk.WithImageFile("/path/to/image.png")
```

### WithImageURL

Attach an image from a URL.

```go
sdk.WithImageURL("https://example.com/image.jpg")
```

### WithImageData

Attach image from raw bytes.

```go
sdk.WithImageData(imageBytes, "image/png")
```

### WithDocumentFile

Attach a PDF document from a file.

```go
sdk.WithDocumentFile("/path/to/document.pdf")
```

### WithDocumentData

Attach a PDF document from raw bytes.

```go
sdk.WithDocumentData(pdfBytes, "application/pdf")
```

### WithAudioFile

Attach an audio file.

```go
sdk.WithAudioFile("/path/to/audio.mp3")
```

### WithVideoFile

Attach a video file.

```go
sdk.WithVideoFile("/path/to/video.mp4")
```

## Conversation Type

### Send

Send a message and get a response.

```go
func (c *Conversation) Send(ctx context.Context, message any) (*Response, error)
```

### Stream

Stream a response.

```go
func (c *Conversation) Stream(ctx context.Context, message string) <-chan StreamChunk
```

### SetVar

Set a template variable.

```go
func (c *Conversation) SetVar(key string, value any)
```

### GetVar

Get a template variable.

```go
func (c *Conversation) GetVar(key string) any
```

### SetVars

Set multiple variables.

```go
func (c *Conversation) SetVars(vars map[string]any)
```

### OnTool

Register a tool handler.

```go
func (c *Conversation) OnTool(name string, handler ToolHandler)
```

### OnToolCtx

Register a tool handler with context.

```go
func (c *Conversation) OnToolCtx(name string, handler ToolHandlerCtx)
```

### OnTools

Register multiple tool handlers.

```go
func (c *Conversation) OnTools(handlers map[string]ToolHandler)
```

### OnToolAsync

Register a tool with approval workflow.

```go
func (c *Conversation) OnToolAsync(name string, check CheckFunc, execute ToolHandler)
```

### OnToolHTTP

Register an HTTP tool.

```go
func (c *Conversation) OnToolHTTP(name string, config *tools.HTTPToolConfig)
```

### Subscribe

Subscribe to events.

```go
func (c *Conversation) Subscribe(event string, handler func(hooks.Event))
```

### Messages

Get conversation history.

```go
func (c *Conversation) Messages() []types.Message
```

### Clear

Clear conversation history.

```go
func (c *Conversation) Clear()
```

### Fork

Create an isolated copy.

```go
func (c *Conversation) Fork() *Conversation
```

### Close

Close the conversation.

```go
func (c *Conversation) Close() error
```

### ID

Get conversation ID.

```go
func (c *Conversation) ID() string
```

### ResolveTool

Approve a pending tool.

```go
func (c *Conversation) ResolveTool(id string) (*ToolResult, error)
```

### RejectTool

Reject a pending tool.

```go
func (c *Conversation) RejectTool(id string, reason string) (*ToolResult, error)
```

## Response Type

### Text

Get response text.

```go
func (r *Response) Text() string
```

### HasToolCalls

Check for tool calls.

```go
func (r *Response) HasToolCalls() bool
```

### ToolCalls

Get tool calls.

```go
func (r *Response) ToolCalls() []ToolCall
```

### PendingTools

Get pending approvals.

```go
func (r *Response) PendingTools() []PendingTool
```

## StreamChunk Type

```go
type StreamChunk struct {
    Type  ChunkType // ChunkText, ChunkToolCall, ChunkDone
    Text  string    // Text content
    Error error     // Error if any
}
```

### ChunkType Constants

```go
const (
    ChunkText     ChunkType = "text"
    ChunkToolCall ChunkType = "tool_call"
    ChunkDone     ChunkType = "done"
)
```

## Handler Types

### ToolHandler

```go
type ToolHandler func(args map[string]any) (any, error)
```

### ToolHandlerCtx

```go
type ToolHandlerCtx func(ctx context.Context, args map[string]any) (any, error)
```

## Error Types

```go
var (
    ErrPackNotFound       = errors.New("pack file not found")
    ErrPromptNotFound     = errors.New("prompt not found in pack")
    ErrInvalidPack        = errors.New("invalid pack format")
    ErrProviderError      = errors.New("provider error")
    ErrConversationClosed = errors.New("conversation closed")
    ErrToolNotRegistered  = errors.New("tool not registered")
)
```

## Package Import

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/sdk/hooks"
    "github.com/AltairaLabs/PromptKit/sdk/tools"
)
```

## Additional References

### Audio & Voice

- **[Audio API](audio)** - VAD mode, ASM mode, turn detection, audio streaming
- **[TTS API](tts)** - Text-to-speech services, voices, formats, providers
- **[Streaming Package](streaming)** - Bidirectional streaming utilities, response collection, audio streaming

### Dynamic Variables

- **[Variable Providers](variables)** - Dynamic variable resolution, built-in providers, custom providers

### A2A Server

- **[A2A Server](a2a-server)** - A2AServer, A2ATaskStore, A2AConversationOpener

### Related

- **[ConversationManager](conversation-manager)** - Legacy conversation management

## See Also

- [Tutorials](../tutorials/)
- [How-To Guides](../how-to/)
- [Examples](/sdk/examples/)
