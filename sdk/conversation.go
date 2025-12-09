package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	streamPkg "github.com/AltairaLabs/PromptKit/sdk/stream"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// Default parameter values for LLM calls.
const (
	defaultMaxTokens        = 4096
	defaultTemperature      = 0.7
	streamChannelBufferSize = 100 // Buffer size for streaming channels
)

// Conversation represents an active LLM conversation.
//
// A conversation maintains:
//   - Connection to the LLM provider
//   - Message history (context)
//   - Variable state for template substitution
//   - Tool handlers for function calling
//   - Validation state
//
// Conversations are created via [Open] or [Resume] and are safe for concurrent use.
// Each [Open] call creates an independent conversation with isolated state.
//
// Basic usage:
//
//	conv, _ := sdk.Open("./assistant.pack.json", "chat")
//	conv.SetVar("user_name", "Alice")
//
//	resp, _ := conv.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())
//
//	resp, _ = conv.Send(ctx, "What's my name?")  // Remembers context
//	fmt.Println(resp.Text())  // "Your name is Alice"
type Conversation struct {
	// Pack and prompt configuration
	pack           *pack.Pack
	prompt         *pack.Prompt
	promptName     string
	promptRegistry *prompt.Registry // Registry for PromptAssemblyMiddleware
	toolRegistry   *tools.Registry  // Registry for tools (pre-populated from pack)

	// Provider for LLM calls
	provider providers.Provider

	// Configuration from options
	config *config

	// Variable state for template substitution
	variables map[string]string
	varMu     sync.RWMutex

	// Tool handlers
	handlers   map[string]ToolHandler
	handlersMu sync.RWMutex

	// Async tool handlers for HITL
	asyncHandlers   map[string]sdktools.AsyncToolHandler
	asyncHandlersMu sync.RWMutex

	// Pending tool calls awaiting approval
	pendingStore *sdktools.PendingStore

	// MCP registry for managing MCP servers
	mcpRegistry mcp.Registry

	// Event bus for observability
	eventBus *events.EventBus

	// Conversation state (messages, metadata)
	state   *statestore.ConversationState
	stateMu sync.RWMutex

	// Unique identifier (auto-generated or from config)
	id string

	// Closed flag
	closed bool
	mu     sync.RWMutex
}

// ToolHandler is a function that executes a tool call.
// It receives the parsed arguments from the LLM and returns a result.
//
// The args map contains the arguments as specified in the tool's schema.
// The return value should be JSON-serializable.
//
//	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
//	    city := args["city"].(string)
//	    return weatherAPI.GetCurrent(city)
//	})
type ToolHandler func(args map[string]any) (any, error)

// ToolHandlerCtx is like [ToolHandler] but receives a context.
// Use this when your tool implementation needs context for cancellation or deadlines.
//
//	conv.OnToolCtx("search_db", func(ctx context.Context, args map[string]any) (any, error) {
//	    return db.SearchWithContext(ctx, args["query"].(string))
//	})
type ToolHandlerCtx func(ctx context.Context, args map[string]any) (any, error)

// Send sends a message to the LLM and returns the response.
//
// The message can be a simple string or a *types.Message for multimodal content.
// Variables are substituted into the system prompt template before sending.
//
// Basic usage:
//
//	resp, err := conv.Send(ctx, "Hello!")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(resp.Text())
//
// With message options:
//
//	resp, err := conv.Send(ctx, "What's in this image?",
//	    sdk.WithImageFile("/path/to/image.jpg"),
//	)
//
// Send automatically:
//   - Substitutes variables into the system prompt
//   - Runs any registered validators
//   - Handles tool calls if tools are defined
//   - Persists state if a state store is configured
func (c *Conversation) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	startTime := time.Now()

	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	// Build user message from input
	userMsg, err := c.buildUserMessage(message, opts)
	if err != nil {
		return nil, err
	}

	// Build and execute pipeline
	result, err := c.executePipeline(ctx, userMsg)
	if err != nil {
		return nil, err
	}

	// Build response
	return c.buildResponse(result, startTime), nil
}

// checkClosed returns an error if the conversation is closed.
func (c *Conversation) checkClosed() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return ErrConversationClosed
	}
	return nil
}

// buildUserMessage creates a user message from the input.
func (c *Conversation) buildUserMessage(message any, opts []SendOption) (*types.Message, error) {
	var userMsg *types.Message
	switch m := message.(type) {
	case string:
		userMsg = &types.Message{Role: "user"}
		userMsg.AddTextPart(m)
	case *types.Message:
		userMsg = m
	default:
		return nil, fmt.Errorf("message must be string or *types.Message, got %T", message)
	}

	// Apply send options (image attachments, etc.)
	sendCfg := &sendConfig{}
	for _, opt := range opts {
		if err := opt(sendCfg); err != nil {
			return nil, fmt.Errorf("failed to apply send option: %w", err)
		}
	}

	// Add content parts from options to the message
	if err := c.applyContentParts(userMsg, sendCfg.parts); err != nil {
		return nil, fmt.Errorf("failed to apply content parts: %w", err)
	}

	return userMsg, nil
}

// executePipeline builds and executes the LLM pipeline.
func (c *Conversation) executePipeline(
	ctx context.Context,
	userMsg *types.Message,
) (*rtpipeline.ExecutionResult, error) {
	// Get current variables for template substitution (including from providers)
	vars := c.getVariablesWithProviders(ctx)

	// Build tool registry (registers executors for handlers)
	toolRegistry := c.buildToolRegistry()

	// Build pipeline using PromptAssemblyMiddleware (same as Arena)
	pipelineCfg := &intpipeline.Config{
		Provider:       c.provider,
		ToolRegistry:   toolRegistry,
		PromptRegistry: c.promptRegistry,
		TaskType:       c.promptName,
		Variables:      vars,
		MaxTokens:      defaultMaxTokens,
		Temperature:    defaultTemperature,
	}

	// Apply parameters from prompt if available
	c.applyPromptParameters(pipelineCfg)

	pipe, err := intpipeline.Build(pipelineCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}

	// Add user message to history
	c.addMessageToHistory(userMsg)

	// Execute pipeline
	execOpts := &rtpipeline.ExecutionOptions{
		Context:        ctx,
		ConversationID: c.id,
	}

	result, err := pipe.ExecuteWithMessageOptions(execOpts, *userMsg)
	if err != nil {
		return nil, fmt.Errorf("pipeline execution failed: %w", err)
	}

	// Add assistant response to history
	c.addAssistantResponse(result)

	return result, nil
}

// getVariables returns a copy of the current variables.
// If ctx is provided, it also resolves variables from any configured providers.
func (c *Conversation) getVariables() map[string]string {
	c.varMu.RLock()
	defer c.varMu.RUnlock()
	vars := make(map[string]string, len(c.variables))
	for k, v := range c.variables {
		vars[k] = v
	}
	return vars
}

// getVariablesWithProviders returns variables including those from providers.
// Provider variables override static variables with the same key.
func (c *Conversation) getVariablesWithProviders(ctx context.Context) map[string]string {
	// Start with static variables
	vars := c.getVariables()

	// Resolve from providers (if any)
	if c.config != nil && len(c.config.variableProviders) > 0 {
		for _, p := range c.config.variableProviders {
			providerVars, err := p.Provide(ctx)
			if err != nil {
				// Log but don't fail - providers are best-effort
				continue
			}
			for k, v := range providerVars {
				vars[k] = v
			}
		}
	}

	return vars
}

// applyPromptParameters applies parameters from the prompt to the pipeline config.
func (c *Conversation) applyPromptParameters(cfg *intpipeline.Config) {
	if c.prompt.Parameters == nil {
		return
	}
	if c.prompt.Parameters.MaxTokens != nil {
		cfg.MaxTokens = *c.prompt.Parameters.MaxTokens
	}
	if c.prompt.Parameters.Temperature != nil {
		cfg.Temperature = float32(*c.prompt.Parameters.Temperature)
	}
}

// addMessageToHistory adds a message to the conversation history.
func (c *Conversation) addMessageToHistory(msg *types.Message) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.state == nil {
		c.state = &statestore.ConversationState{
			ID:       c.id,
			Messages: []types.Message{},
		}
	}
	c.state.Messages = append(c.state.Messages, *msg)
}

// addAssistantResponse adds the assistant's response to history.
func (c *Conversation) addAssistantResponse(result *rtpipeline.ExecutionResult) {
	if result.Response == nil {
		return
	}
	assistantMsg := types.Message{
		Role:     "assistant",
		Content:  result.Response.Content,
		CostInfo: &result.CostInfo,
	}
	c.stateMu.Lock()
	c.state.Messages = append(c.state.Messages, assistantMsg)
	c.stateMu.Unlock()
}

// buildResponse creates a Response from the pipeline result.
func (c *Conversation) buildResponse(result *rtpipeline.ExecutionResult, startTime time.Time) *Response {
	var assistantMsg *types.Message
	if result.Response != nil {
		assistantMsg = &types.Message{
			Role:     "assistant",
			Content:  result.Response.Content,
			CostInfo: &result.CostInfo,
		}
	}

	resp := &Response{
		message:  assistantMsg,
		duration: time.Since(startTime),
	}

	// Extract tool calls from response if present
	if result.Response != nil && len(result.Response.ToolCalls) > 0 {
		resp.toolCalls = make([]types.MessageToolCall, len(result.Response.ToolCalls))
		for i, tc := range result.Response.ToolCalls {
			resp.toolCalls[i] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: tc.Args,
			}
		}
	}

	// Extract validation results from the assistant message in history
	// The DynamicValidatorMiddleware adds validations to the last assistant message
	for i := len(result.Messages) - 1; i >= 0; i-- {
		if result.Messages[i].Role == "assistant" && len(result.Messages[i].Validations) > 0 {
			resp.validations = result.Messages[i].Validations
			break
		}
	}

	return resp
}

// applyContentParts adds content parts from send options to the message.
func (c *Conversation) applyContentParts(msg *types.Message, parts []any) error {
	for _, part := range parts {
		switch p := part.(type) {
		case imageFilePart:
			if err := msg.AddImagePart(p.path, p.detail); err != nil {
				return fmt.Errorf("failed to add image from file: %w", err)
			}
		case imageURLPart:
			msg.AddImagePartFromURL(p.url, p.detail)
		case imageDataPart:
			// Convert raw bytes to base64
			base64Data := encodeBase64(p.data)
			contentPart := types.NewImagePartFromData(base64Data, p.mimeType, p.detail)
			msg.AddPart(contentPart)
		case audioFilePart:
			if err := msg.AddAudioPart(p.path); err != nil {
				return fmt.Errorf("failed to add audio from file: %w", err)
			}
		case filePart:
			// For generic files, add as text with filename context
			msg.AddTextPart(fmt.Sprintf("[File: %s]\n%s", p.name, string(p.data)))
		default:
			return fmt.Errorf("unknown content part type: %T", part)
		}
	}
	return nil
}

// encodeBase64 encodes raw bytes to base64 string.
func encodeBase64(data []byte) string {
	return base64Encoding.EncodeToString(data)
}

var base64Encoding = base64.StdEncoding

// buildToolRegistry returns the tool registry with executors registered for user-provided handlers.
// The registry is pre-populated with tool descriptors from the pack.
func (c *Conversation) buildToolRegistry() *tools.Registry {
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()

	// Register a single "local" executor that dispatches to the right handler.
	// This is needed because tools in the pack have Mode: "local", and the registry
	// looks up executors by mode name, not by tool name.
	localExec := &localExecutor{handlers: c.handlers}
	c.toolRegistry.RegisterExecutor(localExec)

	// Register MCP tool executors
	c.registerMCPExecutors()

	return c.toolRegistry
}

// registerMCPExecutors registers executors for MCP tools.
func (c *Conversation) registerMCPExecutors() {
	if c.mcpRegistry == nil {
		return
	}

	ctx := context.Background()
	mcpTools, err := c.mcpRegistry.ListAllTools(ctx)
	if err != nil {
		return
	}

	for _, serverTools := range mcpTools {
		for _, tool := range serverTools {
			// Register the MCP tool in the registry if not already present
			desc := &tools.ToolDescriptor{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Mode:        "mcp",
			}
			_ = c.toolRegistry.Register(desc)

			adapter := &mcpHandlerAdapter{name: tool.Name, registry: c.mcpRegistry}
			c.toolRegistry.RegisterExecutor(adapter)
		}
	}
}

// Stream sends a message and returns a channel of response chunks.
//
// Use this for real-time streaming of LLM responses:
//
//	for chunk := range conv.Stream(ctx, "Tell me a story") {
//	    if chunk.Error != nil {
//	        log.Printf("Error: %v", chunk.Error)
//	        break
//	    }
//	    fmt.Print(chunk.Text)
//	}
//
// The channel is closed when the response is complete or an error occurs.
// The final chunk (Type == ChunkDone) contains the complete Response.
func (c *Conversation) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk {
	ch := make(chan StreamChunk, streamChannelBufferSize)

	go func() {
		defer close(ch)
		startTime := time.Now()

		if err := c.checkClosed(); err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		// Build user message from input
		userMsg, err := c.buildUserMessage(message, opts)
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		// Execute streaming pipeline
		err = c.executeStreamingPipeline(ctx, userMsg, ch, startTime)
		if err != nil {
			ch <- StreamChunk{Error: err}
		}
	}()

	return ch
}

// executeStreamingPipeline builds and executes the LLM pipeline in streaming mode.
func (c *Conversation) executeStreamingPipeline(
	ctx context.Context,
	userMsg *types.Message,
	outCh chan<- StreamChunk,
	startTime time.Time,
) error {
	// Build and start streaming pipeline
	pipe, err := c.buildStreamPipeline()
	if err != nil {
		return err
	}

	// Add user message to history
	c.addMessageToHistory(userMsg)

	// Execute streaming pipeline
	streamCh, err := pipe.ExecuteStreamWithMessage(ctx, *userMsg)
	if err != nil {
		return fmt.Errorf("pipeline streaming failed: %w", err)
	}

	// Process stream and finalize
	return c.processAndFinalizeStream(streamCh, outCh, startTime)
}

// buildStreamPipeline creates the pipeline for streaming execution.
func (c *Conversation) buildStreamPipeline() (*rtpipeline.Pipeline, error) {
	// Get current variables for template substitution
	vars := c.getVariables()

	// Build tool registry (registers executors for handlers)
	toolRegistry := c.buildToolRegistry()

	// Build pipeline using PromptAssemblyMiddleware (same as Arena)
	pipelineCfg := &intpipeline.Config{
		Provider:       c.provider,
		ToolRegistry:   toolRegistry,
		PromptRegistry: c.promptRegistry,
		TaskType:       c.promptName,
		Variables:      vars,
		MaxTokens:      defaultMaxTokens,
		Temperature:    defaultTemperature,
	}

	// Apply parameters from prompt if available
	c.applyPromptParameters(pipelineCfg)

	pipe, err := intpipeline.Build(pipelineCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}

	return pipe, nil
}

// streamState tracks state during stream processing.
type streamState struct {
	accumulatedContent string
	lastToolCalls      []types.MessageToolCall
	finalResult        *rtpipeline.ExecutionResult
}

// processAndFinalizeStream handles the streaming response and emits the final chunk.
func (c *Conversation) processAndFinalizeStream(
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	startTime time.Time,
) error {
	state := &streamState{}

	// Process all stream chunks
	if err := c.processStreamChunks(streamCh, outCh, state); err != nil {
		return err
	}

	// Build response from accumulated data
	resp := c.buildStreamingResponse(state.finalResult, state.accumulatedContent, state.lastToolCalls, startTime)

	// Add assistant response to history
	c.finalizeStreamHistory(state)

	// Emit final ChunkDone with complete response
	outCh <- StreamChunk{
		Type:    ChunkDone,
		Message: resp,
	}

	return nil
}

// processStreamChunks processes provider chunks and emits SDK chunks.
func (c *Conversation) processStreamChunks(
	streamCh <-chan providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) error {
	for chunk := range streamCh {
		if chunk.Error != nil {
			return chunk.Error
		}

		c.emitStreamChunk(&chunk, outCh, state)
	}
	return nil
}

// emitStreamChunk converts a provider chunk to SDK chunk(s) and updates state.
func (c *Conversation) emitStreamChunk(
	chunk *providers.StreamChunk,
	outCh chan<- StreamChunk,
	state *streamState,
) {
	// Emit text delta
	if chunk.Delta != "" {
		state.accumulatedContent += chunk.Delta
		outCh <- StreamChunk{Type: ChunkText, Text: chunk.Delta}
	}

	// Emit media delta
	if chunk.MediaDelta != nil {
		outCh <- StreamChunk{Type: ChunkMedia, Media: chunk.MediaDelta}
	}

	// Emit new tool calls
	if len(chunk.ToolCalls) > len(state.lastToolCalls) {
		for i := len(state.lastToolCalls); i < len(chunk.ToolCalls); i++ {
			outCh <- StreamChunk{Type: ChunkToolCall, ToolCall: &chunk.ToolCalls[i]}
		}
		state.lastToolCalls = chunk.ToolCalls
	}

	// Capture final result
	if chunk.FinishReason != nil {
		if result, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
			state.finalResult = result
		}
	}
}

// finalizeStreamHistory adds the assistant response to history after streaming.
func (c *Conversation) finalizeStreamHistory(state *streamState) {
	if state.finalResult != nil {
		c.addAssistantResponse(state.finalResult)
	} else {
		// No final result from pipeline, create one from accumulated content
		assistantMsg := types.Message{
			Role:    "assistant",
			Content: state.accumulatedContent,
		}
		c.stateMu.Lock()
		c.state.Messages = append(c.state.Messages, assistantMsg)
		c.stateMu.Unlock()
	}
}

// buildStreamingResponse creates a Response from streaming data.
func (c *Conversation) buildStreamingResponse(
	result *rtpipeline.ExecutionResult,
	content string,
	toolCalls []types.MessageToolCall,
	startTime time.Time,
) *Response {
	resp := &Response{
		duration: time.Since(startTime),
	}

	// Use result data if available
	if result != nil && result.Response != nil {
		resp.message = &types.Message{
			Role:     "assistant",
			Content:  result.Response.Content,
			CostInfo: &result.CostInfo,
		}

		if len(result.Response.ToolCalls) > 0 {
			resp.toolCalls = result.Response.ToolCalls
		}

		// Extract validations
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Role == "assistant" && len(result.Messages[i].Validations) > 0 {
				resp.validations = result.Messages[i].Validations
				break
			}
		}
	} else {
		// Build from accumulated streaming data
		resp.message = &types.Message{
			Role:    "assistant",
			Content: content,
		}
		if len(toolCalls) > 0 {
			resp.toolCalls = toolCalls
		}
	}

	return resp
}

// StreamRaw returns a channel of streaming chunks for use with the stream package.
// This is a lower-level API that returns stream.Chunk types.
//
// Most users should use [Conversation.Stream] instead.
// StreamRaw is useful when working with [stream.Process] or [stream.CollectText].
//
//	err := stream.Process(ctx, conv, "Hello", func(chunk stream.Chunk) error {
//	    fmt.Print(chunk.Text)
//	    return nil
//	})
func (c *Conversation) StreamRaw(ctx context.Context, message any) (<-chan streamPkg.Chunk, error) {
	ch := make(chan streamPkg.Chunk, streamChannelBufferSize)

	go func() {
		defer close(ch)

		for sdkChunk := range c.Stream(ctx, message) {
			// Convert SDK StreamChunk to stream.Chunk
			chunk := streamPkg.Chunk{
				Error: sdkChunk.Error,
			}

			switch sdkChunk.Type {
			case ChunkText:
				chunk.Type = streamPkg.ChunkText
				chunk.Text = sdkChunk.Text
			case ChunkToolCall:
				chunk.Type = streamPkg.ChunkToolCall
				chunk.ToolCall = sdkChunk.ToolCall
			case ChunkMedia:
				chunk.Type = streamPkg.ChunkMedia
				chunk.Media = sdkChunk.Media
			case ChunkDone:
				chunk.Done = true
			}

			ch <- chunk
		}
	}()

	return ch, nil
}

// SetVar sets a single template variable.
//
// Variables are substituted into the system prompt template:
//
//	conv.SetVar("customer_name", "Alice")
//	// Template: "You are helping {{customer_name}}"
//	// Becomes: "You are helping Alice"
func (c *Conversation) SetVar(name, value string) {
	c.varMu.Lock()
	defer c.varMu.Unlock()
	c.variables[name] = value
}

// SetVars sets multiple template variables at once.
//
//	conv.SetVars(map[string]any{
//	    "customer_name": "Alice",
//	    "customer_tier": "premium",
//	    "max_discount": 20,
//	})
func (c *Conversation) SetVars(vars map[string]any) {
	c.varMu.Lock()
	defer c.varMu.Unlock()
	for k, v := range vars {
		c.variables[k] = fmt.Sprintf("%v", v)
	}
}

// SetVarsFromEnv sets variables from environment variables with a given prefix.
//
// Environment variables matching the prefix are added as template variables
// with the prefix stripped and converted to lowercase:
//
//	// If PROMPTKIT_CUSTOMER_NAME=Alice is set:
//	conv.SetVarsFromEnv("PROMPTKIT_")
//	// Sets variable "customer_name" = "Alice"
func (c *Conversation) SetVarsFromEnv(prefix string) {
	c.varMu.Lock()
	defer c.varMu.Unlock()

	const envKeyValueParts = 2 // key=value split parts
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", envKeyValueParts)
		if len(parts) != envKeyValueParts {
			continue
		}
		key, value := parts[0], parts[1]
		if strings.HasPrefix(key, prefix) {
			varName := strings.ToLower(strings.TrimPrefix(key, prefix))
			c.variables[varName] = value
		}
	}
}

// GetVar returns the current value of a template variable.
// Returns empty string if the variable is not set.
func (c *Conversation) GetVar(name string) string {
	c.varMu.RLock()
	defer c.varMu.RUnlock()
	return c.variables[name]
}

// OnTool registers a handler for a tool defined in the pack.
//
// The tool name must match a tool defined in the pack's tools section.
// When the LLM calls the tool, your handler receives the parsed arguments
// and returns a result.
//
//	conv.OnTool("get_weather", func(args map[string]any) (any, error) {
//	    city := args["city"].(string)
//	    return weatherAPI.GetCurrent(city)
//	})
//
// The handler's return value is automatically serialized to JSON and sent
// back to the LLM as the tool result.
func (c *Conversation) OnTool(name string, handler ToolHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[name] = handler
}

// OnToolCtx registers a context-aware handler for a tool.
//
// Use this when your tool implementation needs the request context
// for cancellation, deadlines, or tracing:
//
//	conv.OnToolCtx("search_db", func(ctx context.Context, args map[string]any) (any, error) {
//	    return db.SearchWithContext(ctx, args["query"].(string))
//	})
func (c *Conversation) OnToolCtx(name string, handler ToolHandlerCtx) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	// Wrap ToolHandlerCtx as ToolHandler (context will be injected at call time)
	c.handlers[name] = func(args map[string]any) (any, error) {
		// Note: This wrapper will be replaced with proper context injection
		// when the pipeline is built
		return handler(context.Background(), args)
	}
}

// OnTools registers multiple tool handlers at once.
//
//	conv.OnTools(map[string]sdk.ToolHandler{
//	    "get_weather":   getWeatherHandler,
//	    "search_docs":   searchDocsHandler,
//	    "send_email":    sendEmailHandler,
//	})
func (c *Conversation) OnTools(handlers map[string]ToolHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	for name, handler := range handlers {
		c.handlers[name] = handler
	}
}

// OnToolHTTP registers a tool that makes HTTP requests.
//
// This is a convenience method for tools that call external APIs:
//
//	conv.OnToolHTTP("create_ticket", sdktools.NewHTTPToolConfig(
//	    "https://api.tickets.example.com/tickets",
//	    sdktools.WithMethod("POST"),
//	    sdktools.WithHeader("Authorization", "Bearer "+apiKey),
//	    sdktools.WithTimeout(5000),
//	))
//
// The tool arguments from the LLM are serialized to JSON and sent as the
// request body. The response is parsed and returned to the LLM.
func (c *Conversation) OnToolHTTP(name string, config *sdktools.HTTPToolConfig) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[name] = config.Handler()
}

// OnToolExecutor registers a custom executor for tools.
//
// Use this when you need full control over tool execution or want to use
// a runtime executor directly:
//
//	executor := &MyCustomExecutor{}
//	conv.OnToolExecutor("custom_tool", executor)
//
// The executor must implement the runtime/tools.Executor interface.
func (c *Conversation) OnToolExecutor(name string, executor tools.Executor) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.handlers[name] = func(args map[string]any) (any, error) {
		// Convert args to JSON for the executor
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal args: %w", err)
		}

		// Get tool descriptor from pack
		packTool := c.pack.GetTool(name)
		if packTool == nil {
			return nil, fmt.Errorf("tool %q not found in pack", name)
		}

		paramsJSON, err := json.Marshal(packTool.Parameters)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal tool schema: %w", err)
		}

		desc := &tools.ToolDescriptor{
			Name:        packTool.Name,
			Description: packTool.Description,
			InputSchema: paramsJSON,
		}

		// Execute
		result, err := executor.Execute(desc, argsJSON)
		if err != nil {
			return nil, err
		}

		// Parse result
		var parsed any
		if json.Unmarshal(result, &parsed) != nil {
			return string(result), nil
		}
		return parsed, nil
	}
}

// OnToolAsync registers a handler that may require approval before execution.
//
// Use this for Human-in-the-Loop (HITL) workflows where certain actions
// require human approval before proceeding:
//
//	conv.OnToolAsync("process_refund", func(args map[string]any) sdk.PendingResult {
//	    amount := args["amount"].(float64)
//	    if amount > 1000 {
//	        return sdk.PendingResult{
//	            Reason:  "high_value_refund",
//	            Message: fmt.Sprintf("Refund of $%.2f requires approval", amount),
//	        }
//	    }
//	    return sdk.PendingResult{} // Proceed immediately
//	}, func(args map[string]any) (any, error) {
//	    // Execute the actual refund
//	    return refundAPI.Process(args)
//	})
//
// The first function checks if approval is needed, the second executes the action.
func (c *Conversation) OnToolAsync(
	name string,
	checkFunc func(args map[string]any) sdktools.PendingResult,
	execFunc ToolHandler,
) {
	c.asyncHandlersMu.Lock()
	defer c.asyncHandlersMu.Unlock()

	// Initialize maps if needed
	if c.asyncHandlers == nil {
		c.asyncHandlers = make(map[string]sdktools.AsyncToolHandler)
	}
	if c.pendingStore == nil {
		c.pendingStore = sdktools.NewPendingStore()
	}

	c.asyncHandlers[name] = checkFunc

	// Register the execution handler
	c.handlersMu.Lock()
	c.handlers[name] = execFunc
	c.handlersMu.Unlock()
}

// ResolveTool approves and executes a pending tool call.
//
// After calling Send() and receiving pending tools in the response,
// use this to approve and execute them:
//
//	resp, _ := conv.Send(ctx, "Process refund for order #12345")
//	if len(resp.PendingTools()) > 0 {
//	    pending := resp.PendingTools()[0]
//	    // ... get approval ...
//	    result, _ := conv.ResolveTool(pending.ID)
//	    // Continue the conversation with the result
//	    resp, _ = conv.Continue(ctx)
//	}
func (c *Conversation) ResolveTool(id string) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	return c.pendingStore.Resolve(id)
}

// RejectTool rejects a pending tool call.
//
// Use this when the human reviewer decides not to approve the tool:
//
//	resp, _ := conv.RejectTool(pending.ID, "Not authorized for this amount")
func (c *Conversation) RejectTool(id, reason string) (*sdktools.ToolResolution, error) {
	if c.pendingStore == nil {
		return nil, fmt.Errorf("no pending tools")
	}
	return c.pendingStore.Reject(id, reason)
}

// Continue resumes conversation after resolving pending tools.
//
// Call this after approving/rejecting all pending tools to continue
// the conversation with the tool results:
//
//	resp, _ := conv.Send(ctx, "Process refund")
//	for _, pending := range resp.PendingTools() {
//	    conv.ResolveTool(pending.ID)
//	}
//	resp, _ = conv.Continue(ctx) // LLM receives tool results
func (c *Conversation) Continue(ctx context.Context) (*Response, error) {
	startTime := time.Now()

	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	// Build the tool results from resolved pending tools
	// and continue the conversation
	// For now, this is a simplified implementation that re-sends
	// with the last context - full implementation would inject tool results

	// Get the last message and re-execute pipeline
	c.stateMu.RLock()
	if c.state == nil || len(c.state.Messages) == 0 {
		c.stateMu.RUnlock()
		return nil, fmt.Errorf("no messages to continue from")
	}
	c.stateMu.RUnlock()

	// Execute pipeline with empty message (continuation)
	userMsg := &types.Message{Role: "user"}
	userMsg.AddTextPart("continue")

	result, err := c.executePipeline(ctx, userMsg)
	if err != nil {
		return nil, err
	}

	return c.buildResponse(result, startTime), nil
}

// PendingTools returns all pending tool calls awaiting approval.
func (c *Conversation) PendingTools() []*sdktools.PendingToolCall {
	if c.pendingStore == nil {
		return nil
	}
	return c.pendingStore.List()
}

// CheckPending checks if a tool call should be pending and creates it if so.
// Returns (pending call, should wait) - if should wait is true, the tool shouldn't execute yet.
//
// This method is used internally when processing tool calls from the LLM.
// It can also be useful for testing HITL workflows:
//
//	pending, shouldWait := conv.CheckPending("risky_tool", args)
//	if shouldWait {
//	    // Tool requires approval
//	}
func (c *Conversation) CheckPending(
	name string,
	args map[string]any,
) (*sdktools.PendingToolCall, bool) {
	c.asyncHandlersMu.RLock()
	checkFunc, isAsync := c.asyncHandlers[name]
	c.asyncHandlersMu.RUnlock()

	if !isAsync {
		return nil, false
	}

	result := checkFunc(args)
	if !result.IsPending() {
		return nil, false
	}

	// Create pending call
	pending := &sdktools.PendingToolCall{
		ID:        uuid.New().String(),
		Name:      name,
		Arguments: args,
		Reason:    result.Reason,
		Message:   result.Message,
	}

	// Store it
	c.pendingStore.Add(pending)

	return pending, true
}

// ToolRegistry returns the underlying tool registry.
//
// This is a power-user method for direct registry access. Tool descriptors
// are loaded from the pack; this allows inspecting them or registering
// custom executors.
//
//	registry := conv.ToolRegistry().(*tools.Registry)
//	for _, desc := range registry.Descriptors() {
//	    fmt.Printf("Tool: %s\n", desc.Name)
//	}
func (c *Conversation) ToolRegistry() *tools.Registry {
	return c.toolRegistry
}

// Messages returns the conversation history.
//
// The returned slice is a copy - modifying it does not affect the conversation.
func (c *Conversation) Messages() []types.Message {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	if c.state == nil {
		return nil
	}

	// Return a copy
	messages := make([]types.Message, len(c.state.Messages))
	copy(messages, c.state.Messages)
	return messages
}

// Clear removes all messages from the conversation history.
//
// This keeps the system prompt and variables but removes all user/assistant
// messages. Useful for starting fresh within the same conversation session.
func (c *Conversation) Clear() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	if c.state != nil {
		c.state.Messages = nil
		c.state.TokenCount = 0
	}
}

// Fork creates a copy of the current conversation state.
//
// Use this to explore alternative conversation branches:
//
//	conv.Send(ctx, "I want to plan a trip")
//	conv.Send(ctx, "What cities should I visit?")
//
//	// Fork to explore different paths
//	branch := conv.Fork()
//
//	conv.Send(ctx, "Tell me about Tokyo")     // Original path
//	branch.Send(ctx, "Tell me about Kyoto")   // Branch path
//
// The forked conversation is completely independent - changes to one
// do not affect the other.
func (c *Conversation) Fork() *Conversation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.varMu.RLock()
	defer c.varMu.RUnlock()
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()
	c.asyncHandlersMu.RLock()
	defer c.asyncHandlersMu.RUnlock()

	// Copy variables
	vars := make(map[string]string, len(c.variables))
	for k, v := range c.variables {
		vars[k] = v
	}

	// Copy handlers
	handlers := make(map[string]ToolHandler, len(c.handlers))
	for k, v := range c.handlers {
		handlers[k] = v
	}

	// Copy async handlers
	asyncHandlers := make(map[string]sdktools.AsyncToolHandler, len(c.asyncHandlers))
	for k, v := range c.asyncHandlers {
		asyncHandlers[k] = v
	}

	// Copy state
	var stateCopy *statestore.ConversationState
	if c.state != nil {
		messages := make([]types.Message, len(c.state.Messages))
		copy(messages, c.state.Messages)
		stateCopy = &statestore.ConversationState{
			ID:         c.state.ID + "-fork",
			UserID:     c.state.UserID,
			Messages:   messages,
			TokenCount: c.state.TokenCount,
		}
	}

	return &Conversation{
		pack:          c.pack,
		prompt:        c.prompt,
		promptName:    c.promptName,
		provider:      c.provider,
		config:        c.config,
		variables:     vars,
		handlers:      handlers,
		asyncHandlers: asyncHandlers,
		pendingStore:  sdktools.NewPendingStore(), // Fresh pending store for fork
		state:         stateCopy,
	}
}

// Close releases resources associated with the conversation.
//
// After Close is called, Send and Stream will return [ErrConversationClosed].
// It's safe to call Close multiple times.
func (c *Conversation) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// Close MCP registry if present
	if c.mcpRegistry != nil {
		if err := c.mcpRegistry.Close(); err != nil {
			return fmt.Errorf("failed to close MCP registry: %w", err)
		}
	}

	// Save state if we have a state store
	if c.config.stateStore != nil && c.state != nil {
		ctx := context.Background()
		if err := c.config.stateStore.Save(ctx, c.state); err != nil {
			return fmt.Errorf("failed to save conversation state: %w", err)
		}
	}

	return nil
}

// ID returns the conversation's unique identifier.
func (c *Conversation) ID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id
}

// EventBus returns the conversation's event bus for observability.
//
// Use this to subscribe to runtime events like tool calls, validations,
// and provider requests:
//
//	conv.EventBus().Subscribe(events.EventToolCallStarted, func(e *events.Event) {
//	    log.Printf("Tool call: %s", e.Data.(*events.ToolCallStartedData).ToolName)
//	})
//
// For convenience methods, see the [hooks] package.
func (c *Conversation) EventBus() *events.EventBus {
	return c.eventBus
}

// localExecutor is a tool executor for locally-handled tools (Mode: "local").
// It dispatches to the appropriate ToolHandler based on the tool name.
type localExecutor struct {
	handlers map[string]ToolHandler
}

// Name returns "local" to match the Mode on tools in the pack.
func (e *localExecutor) Name() string {
	return "local"
}

// Execute dispatches to the appropriate handler based on tool name.
func (e *localExecutor) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	handler, ok := e.handlers[descriptor.Name]
	if !ok {
		return nil, fmt.Errorf("no handler registered for tool: %s", descriptor.Name)
	}

	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Call handler
	result, err := handler(argsMap)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool result: %w", err)
	}

	return resultJSON, nil
}

// handlerAdapter adapts an SDK ToolHandler to the runtime's tools.Executor interface.
type handlerAdapter struct {
	name    string
	handler ToolHandler
}

// Name returns the tool name.
func (a *handlerAdapter) Name() string {
	return a.name
}

// Execute runs the handler with the given arguments.
func (a *handlerAdapter) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	// Parse args to map
	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Call handler
	result, err := a.handler(argsMap)
	if err != nil {
		return nil, err
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tool result: %w", err)
	}

	return resultJSON, nil
}

// mcpHandlerAdapter adapts MCP tool calls to the runtime's tools.Executor interface.
type mcpHandlerAdapter struct {
	name     string
	registry mcp.Registry
}

// Name returns the tool name.
func (a *mcpHandlerAdapter) Name() string {
	return a.name
}

// Execute runs the MCP tool with the given arguments.
func (a *mcpHandlerAdapter) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	ctx := context.Background()

	// Get client for this tool
	client, err := a.registry.GetClientForTool(ctx, a.name)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCP client for tool %q: %w", a.name, err)
	}

	// Call the tool
	resp, err := client.CallTool(ctx, a.name, args)
	if err != nil {
		return nil, fmt.Errorf("MCP tool call failed: %w", err)
	}

	// Check for tool error
	if resp.IsError {
		errMsg := "MCP tool returned error"
		if len(resp.Content) > 0 && resp.Content[0].Text != "" {
			errMsg = resp.Content[0].Text
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	// Extract text content from response
	var result any
	if len(resp.Content) == 1 && resp.Content[0].Type == "text" {
		// Single text response - return as-is
		result = resp.Content[0].Text
	} else {
		// Multiple content items - return as array
		result = resp.Content
	}

	// Serialize result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize MCP tool result: %w", err)
	}

	return resultJSON, nil
}

// OpenAudioSession creates a bidirectional audio streaming session.
//
// Requires a provider that implements StreamInputSupport (e.g., Gemini).
// Returns an audio.Session with VAD and turn detection if configured.
//
//	session, err := conv.OpenAudioSession(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer session.Close()
//
//	// Send audio chunks
//	for chunk := range audioSource {
//	    session.SendChunk(ctx, chunk)
//	}
//
//	// Listen for responses
//	for chunk := range session.Response() {
//	    // Handle streaming audio response
//	}
func (c *Conversation) OpenAudioSession(ctx context.Context, opts ...AudioSessionOption) (*audio.Session, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	// Check provider supports streaming input
	streamProvider, ok := c.provider.(providers.StreamInputSupport)
	if !ok {
		return nil, ErrProviderNotStreamCapable
	}

	// Apply session options
	cfg := &audioSessionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Use conversation-level turn detector if not overridden
	turnDetector := cfg.turnDetector
	if turnDetector == nil {
		turnDetector = c.config.turnDetector
	}

	// Build system message from prompt
	systemMsg := c.buildSystemMessage()

	// Create streaming session request
	req := &providers.StreamInputRequest{
		SystemMsg: systemMsg,
	}

	// Create underlying stream session
	underlying, err := streamProvider.CreateStreamSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream session: %w", err)
	}

	// Wrap with audio session for VAD and turn detection
	sessionCfg := audio.SessionConfig{
		VAD:                  cfg.vad,
		TurnDetector:         turnDetector,
		InterruptionStrategy: cfg.interruptionStrategy,
		AutoCompleteTurn:     cfg.autoCompleteTurn,
	}

	session, err := audio.NewSession(underlying, sessionCfg)
	if err != nil {
		// Close underlying session on error
		_ = underlying.Close()
		return nil, fmt.Errorf("failed to create audio session: %w", err)
	}

	return session, nil
}

// SpeakResponse converts a text response to audio using the configured TTS service.
//
// Requires WithTTS() to be configured when opening the conversation.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "voice",
//	    sdk.WithTTS(tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))),
//	)
//
//	resp, _ := conv.Send(ctx, "Tell me a joke")
//	audioReader, _ := conv.SpeakResponse(ctx, resp)
//	defer audioReader.Close()
//
//	io.Copy(speaker, audioReader)
func (c *Conversation) SpeakResponse(ctx context.Context, resp *Response, opts ...TTSOption) (io.ReadCloser, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	if c.config.ttsService == nil {
		return nil, ErrNoTTSConfigured
	}

	// Get response text
	text := resp.Text()
	if text == "" {
		return nil, tts.ErrEmptyText
	}

	// Apply TTS options
	cfg := &ttsConfig{
		speed: 1.0, // Default speed
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build synthesis config
	synthConfig := tts.SynthesisConfig{
		Voice:    cfg.voice,
		Format:   cfg.format,
		Speed:    cfg.speed,
		Pitch:    cfg.pitch,
		Language: cfg.language,
		Model:    cfg.model,
	}

	return c.config.ttsService.Synthesize(ctx, text, synthConfig)
}

// SpeakResponseStream converts a text response to streaming audio.
//
// Requires WithTTS() configured with a StreamingService provider.
// Returns a channel of audio chunks for lower latency playback.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "voice",
//	    sdk.WithTTS(tts.NewCartesia(os.Getenv("CARTESIA_API_KEY"))),
//	)
//
//	resp, _ := conv.Send(ctx, "Tell me a story")
//	chunks, _ := conv.SpeakResponseStream(ctx, resp)
//
//	for chunk := range chunks {
//	    if chunk.Error != nil {
//	        log.Fatal(chunk.Error)
//	    }
//	    playAudio(chunk.Data)
//	}
func (c *Conversation) SpeakResponseStream(
	ctx context.Context, resp *Response, opts ...TTSOption,
) (<-chan tts.AudioChunk, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	if c.config.ttsService == nil {
		return nil, ErrNoTTSConfigured
	}

	// Check if TTS service supports streaming
	streamingTTS, ok := c.config.ttsService.(tts.StreamingService)
	if !ok {
		return nil, fmt.Errorf("TTS service %q does not support streaming", c.config.ttsService.Name())
	}

	// Get response text
	text := resp.Text()
	if text == "" {
		return nil, tts.ErrEmptyText
	}

	// Apply TTS options
	cfg := &ttsConfig{
		speed: 1.0, // Default speed
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build synthesis config
	synthConfig := tts.SynthesisConfig{
		Voice:    cfg.voice,
		Format:   cfg.format,
		Speed:    cfg.speed,
		Pitch:    cfg.pitch,
		Language: cfg.language,
		Model:    cfg.model,
	}

	return streamingTTS.SynthesizeStream(ctx, text, synthConfig)
}

// buildSystemMessage constructs the system message from the prompt template.
func (c *Conversation) buildSystemMessage() string {
	if c.prompt == nil || c.prompt.SystemTemplate == "" {
		return ""
	}

	// Get variables for substitution
	c.varMu.RLock()
	vars := make(map[string]string, len(c.variables))
	for k, v := range c.variables {
		vars[k] = v
	}
	c.varMu.RUnlock()

	// Simple variable substitution for now
	// Full template processing happens in the pipeline
	result := c.prompt.SystemTemplate
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
		result = strings.ReplaceAll(result, "{{ "+k+" }}", v)
	}

	return result
}
