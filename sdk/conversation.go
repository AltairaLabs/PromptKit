package sdk

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// Default parameter values for LLM calls.
const (
	defaultMaxTokens        = 4096
	defaultTemperature      = 0.7
	streamChannelBufferSize = 100 // Buffer size for streaming channels
)

// SessionMode represents the conversation's session mode.
type SessionMode int

const (
	// UnaryMode for request/response conversations.
	UnaryMode SessionMode = iota
	// DuplexMode for bidirectional streaming conversations.
	DuplexMode
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

	// Configuration from options (includes provider)
	config *config

	// Session management - mode-based approach
	mode SessionMode

	// Union: exactly one is populated based on mode
	unarySession  session.UnarySession
	duplexSession session.DuplexSession

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

	c.mu.RLock()
	if c.mode != UnaryMode {
		c.mu.RUnlock()
		return nil, fmt.Errorf("Send() only available in unary mode; use OpenDuplex() for duplex streaming")
	}
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	// Build user message from input
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
	for _, part := range sendCfg.parts {
		switch p := part.(type) {
		case imageFilePart:
			if err := userMsg.AddImagePart(p.path, p.detail); err != nil {
				return nil, fmt.Errorf("failed to add image from file: %w", err)
			}
		case imageURLPart:
			userMsg.AddImagePartFromURL(p.url, p.detail)
		case imageDataPart:
			base64Data := base64.StdEncoding.EncodeToString(p.data)
			contentPart := types.NewImagePartFromData(base64Data, p.mimeType, p.detail)
			userMsg.AddPart(contentPart)
		case audioFilePart:
			if err := userMsg.AddAudioPart(p.path); err != nil {
				return nil, fmt.Errorf("failed to add audio from file: %w", err)
			}
		case filePart:
			userMsg.AddTextPart(fmt.Sprintf("[File: %s]\n%s", p.name, string(p.data)))
		default:
			return nil, fmt.Errorf("unknown content part type: %T", part)
		}
	}

	// Build and execute pipeline
	result, err := c.executePipeline(ctx, userMsg)
	if err != nil {
		return nil, err
	}

	// Build response
	return c.buildResponse(result, startTime), nil
}

// buildPipelineWithParams builds a wrapped pipeline with explicit parameters.
// Used during initialization for unary sessions.
func (c *Conversation) buildPipelineWithParams(
	store statestore.Store,
	conversationID string,
	streamInputSession providers.StreamInputSession,
) (*rtpipeline.Pipeline, error) {
	// Get initial variables from config (required for prompt template resolution)
	vars := make(map[string]string)
	if c.config != nil && c.config.initialVariables != nil {
		for k, v := range c.config.initialVariables {
			vars[k] = v
		}
	}

	// Build tool registry
	c.handlersMu.RLock()
	localExec := &localExecutor{handlers: c.handlers}
	c.toolRegistry.RegisterExecutor(localExec)
	c.registerMCPExecutors()
	toolRegistry := c.toolRegistry
	c.handlersMu.RUnlock()

	// Build pipeline configuration
	pipelineCfg := &intpipeline.Config{
		Provider:           c.config.provider,
		ToolRegistry:       toolRegistry,
		PromptRegistry:     c.promptRegistry,
		TaskType:           c.promptName,
		Variables:          vars,
		VariableProviders:  c.config.variableProviders, // Pass to pipeline for dynamic resolution
		MaxTokens:          defaultMaxTokens,
		Temperature:        defaultTemperature,
		StateStore:         store,
		ConversationID:     conversationID,
		StreamInputSession: streamInputSession, // Pass session for duplex mode
	}

	// Apply parameters from prompt if available
	if c.prompt.Parameters != nil {
		if c.prompt.Parameters.MaxTokens != nil {
			pipelineCfg.MaxTokens = *c.prompt.Parameters.MaxTokens
		}
		if c.prompt.Parameters.Temperature != nil {
			pipelineCfg.Temperature = float32(*c.prompt.Parameters.Temperature)
		}
	}

	// Build the pipeline
	return intpipeline.Build(pipelineCfg)
}

// buildStreamPipelineWithParams builds a stage pipeline directly for duplex sessions.
// Returns *stage.StreamPipeline which DuplexSession uses directly without wrapping.
//
//nolint:dupl // Similar to buildPipelineWithParams but serves different purpose
func (c *Conversation) buildStreamPipelineWithParams(
	store statestore.Store,
	conversationID string,
	streamInputSession providers.StreamInputSession,
) (*stage.StreamPipeline, error) {
	// Get initial variables from config (required for prompt template resolution)
	vars := make(map[string]string)
	if c.config != nil && c.config.initialVariables != nil {
		for k, v := range c.config.initialVariables {
			vars[k] = v
		}
	}

	// Build tool registry
	c.handlersMu.RLock()
	localExec := &localExecutor{handlers: c.handlers}
	c.toolRegistry.RegisterExecutor(localExec)
	c.registerMCPExecutors()
	toolRegistry := c.toolRegistry
	c.handlersMu.RUnlock()

	// Build pipeline configuration
	pipelineCfg := &intpipeline.Config{
		Provider:           c.config.provider,
		ToolRegistry:       toolRegistry,
		PromptRegistry:     c.promptRegistry,
		TaskType:           c.promptName,
		Variables:          vars,
		VariableProviders:  c.config.variableProviders,
		MaxTokens:          defaultMaxTokens,
		Temperature:        defaultTemperature,
		StateStore:         store,
		ConversationID:     conversationID,
		StreamInputSession: streamInputSession,
	}

	// Apply parameters from prompt if available
	if c.prompt.Parameters != nil {
		if c.prompt.Parameters.MaxTokens != nil {
			pipelineCfg.MaxTokens = *c.prompt.Parameters.MaxTokens
		}
		if c.prompt.Parameters.Temperature != nil {
			pipelineCfg.Temperature = float32(*c.prompt.Parameters.Temperature)
		}
	}

	// Add VAD mode configuration if present
	if c.config.vadModeConfig != nil && c.config.sttService != nil && c.config.ttsService != nil {
		// Create shared interruption handler for barge-in support
		interruptionHandler := audio.NewInterruptionHandler(
			audio.InterruptionImmediate,
			nil, // VAD is managed by AudioTurnStage
		)

		// Convert SDK config to internal stage configs
		vadCfg := c.config.vadModeConfig
		audioTurnCfg := vadCfg.toAudioTurnConfig(interruptionHandler)
		sttCfg := vadCfg.toSTTStageConfig()
		ttsCfg := vadCfg.toTTSStageConfig(interruptionHandler)

		pipelineCfg.VADConfig = &audioTurnCfg
		pipelineCfg.STTService = c.config.sttService
		pipelineCfg.STTConfig = &sttCfg
		pipelineCfg.TTSService = c.config.ttsService
		pipelineCfg.TTSConfig = &ttsCfg
		pipelineCfg.InterruptionHandler = interruptionHandler
	}

	// Build the stage pipeline directly (for duplex sessions)
	return intpipeline.BuildStreamPipeline(pipelineCfg)
}

// executePipeline builds and executes the LLM pipeline.
func (c *Conversation) executePipeline(
	ctx context.Context,
	userMsg *types.Message,
) (*rtpipeline.ExecutionResult, error) {
	// Execute through the unary session (only called from Send which checks mode)
	return c.unarySession.ExecuteWithMessage(ctx, *userMsg)
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

// getBaseSession returns the active session as BaseSession.
func (c *Conversation) getBaseSession() session.BaseSession {
	if c.mode == UnaryMode {
		return c.unarySession
	}
	return c.duplexSession
}

// SendChunk sends a streaming chunk in duplex mode.
// Only available when the conversation was opened with OpenDuplex().
func (c *Conversation) SendChunk(ctx context.Context, chunk *providers.StreamChunk) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mode != DuplexMode {
		return fmt.Errorf("SendChunk() only available in duplex mode; use OpenDuplex()")
	}
	if c.closed {
		return ErrConversationClosed
	}

	return c.duplexSession.SendChunk(ctx, chunk)
}

// SendText sends text in duplex mode.
// Only available when the conversation was opened with OpenDuplex().
func (c *Conversation) SendText(ctx context.Context, text string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mode != DuplexMode {
		return fmt.Errorf("SendText() only available in duplex mode; use OpenDuplex()")
	}
	if c.closed {
		return ErrConversationClosed
	}

	return c.duplexSession.SendText(ctx, text)
}

// TriggerStart sends a text message to make the model initiate the conversation.
// Use this in ASM mode when you want the model to speak first (e.g., introducing itself).
// Only available when the conversation was opened with OpenDuplex().
//
// Example:
//
//	conv, _ := sdk.OpenDuplex("./assistant.pack.json", "interviewer", ...)
//	// Start processing responses first
//	go processResponses(conv.Response())
//	// Trigger the model to begin
//	conv.TriggerStart(ctx, "Please introduce yourself and begin the interview.")
func (c *Conversation) TriggerStart(ctx context.Context, message string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mode != DuplexMode {
		return fmt.Errorf("TriggerStart() only available in duplex mode; use OpenDuplex()")
	}
	if c.closed {
		return ErrConversationClosed
	}

	return c.duplexSession.SendText(ctx, message)
}

// Response returns the response channel for duplex streaming.
// Only available when the conversation was opened with OpenDuplex().
func (c *Conversation) Response() (<-chan providers.StreamChunk, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mode != DuplexMode {
		return nil, fmt.Errorf("Response() only available in duplex mode; use OpenDuplex()")
	}
	if c.closed {
		return nil, ErrConversationClosed
	}

	return c.duplexSession.Response(), nil
}

// Done returns a channel that's closed when the duplex session ends.
// Only available when the conversation was opened with OpenDuplex().
func (c *Conversation) Done() (<-chan struct{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mode != DuplexMode {
		return nil, fmt.Errorf("Done() only available in duplex mode; use OpenDuplex()")
	}
	if c.closed {
		return nil, ErrConversationClosed
	}

	return c.duplexSession.Done(), nil
}

// SessionError returns any error from the duplex session.
// Only available when the conversation was opened with OpenDuplex().
// Note: This is named SessionError to avoid conflict with the Error interface method.
func (c *Conversation) SessionError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.mode != DuplexMode {
		return fmt.Errorf("SessionError() only available in duplex mode")
	}
	if c.closed {
		return ErrConversationClosed
	}

	return c.duplexSession.Error()
}

// SetVar sets a single template variable.
//
// Variables are substituted into the system prompt template:
//
//	conv.SetVar("customer_name", "Alice")
//	// Template: "You are helping {{customer_name}}"
//	// Becomes: "You are helping Alice"
func (c *Conversation) SetVar(name, value string) {
	c.getBaseSession().SetVar(name, value)
}

// SetVars sets multiple template variables at once.
//
//	conv.SetVars(map[string]any{
//	    "customer_name": "Alice",
//	    "customer_tier": "premium",
//	    "max_discount": 20,
//	})
func (c *Conversation) SetVars(vars map[string]any) {
	sess := c.getBaseSession()
	for k, v := range vars {
		sess.SetVar(k, fmt.Sprintf("%v", v))
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
	const envKeyValueParts = 2 // key=value split parts
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", envKeyValueParts)
		if len(parts) != envKeyValueParts {
			continue
		}
		key, value := parts[0], parts[1]
		if strings.HasPrefix(key, prefix) {
			varName := strings.ToLower(strings.TrimPrefix(key, prefix))
			c.getBaseSession().SetVar(varName, value)
		}
	}
}

// GetVar returns the current value of a template variable.
// Returns empty string and false if the variable is not set.
func (c *Conversation) GetVar(name string) (string, bool) {
	return c.getBaseSession().GetVar(name)
}

// Messages returns the conversation history.
//
// The returned slice is a copy - modifying it does not affect the conversation.
func (c *Conversation) Messages(ctx context.Context) []types.Message {
	messages, err := c.getBaseSession().Messages(ctx)
	if err != nil {
		return nil
	}

	// Return a copy
	result := make([]types.Message, len(messages))
	copy(result, messages)
	return result
}

// Clear removes all messages from the conversation history.
//
// This keeps the system prompt and variables but removes all user/assistant
// messages. Useful for starting fresh within the same conversation session.
// In duplex mode, this will close the session first if actively streaming.
func (c *Conversation) Clear() error {
	ctx := context.Background()

	// For duplex mode, close the session first
	if c.mode == DuplexMode && c.duplexSession != nil {
		_ = c.duplexSession.Close()
	}

	return c.getBaseSession().Clear(ctx)
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
	// Need write lock because buildPipelineWithParams mutates shared tool registry
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlersMu.RLock()
	// Copy handlers
	handlers := make(map[string]ToolHandler, len(c.handlers))
	for k, v := range c.handlers {
		handlers[k] = v
	}
	c.handlersMu.RUnlock()

	c.asyncHandlersMu.RLock()
	// Copy async handlers
	asyncHandlers := make(map[string]sdktools.AsyncToolHandler, len(c.asyncHandlers))
	for k, v := range c.asyncHandlers {
		asyncHandlers[k] = v
	}
	c.asyncHandlersMu.RUnlock()

	// Create fork with new ID
	sess := c.getBaseSession()
	forkID := sess.ID() + "-fork"

	// Build a new pipeline for the fork
	var store statestore.Store
	if c.config != nil && c.config.stateStore != nil {
		store = c.config.stateStore
	} else {
		// Session is using internal memory store - it will be reused by ForkSession
		store = nil
	}

	ctx := context.Background()
	pipeline, err := c.buildPipelineWithParams(store, forkID, nil)
	if err != nil {
		return nil
	}

	// Create the forked conversation
	fork := &Conversation{
		pack:           c.pack,
		prompt:         c.prompt,
		promptName:     c.promptName,
		promptRegistry: c.promptRegistry,
		toolRegistry:   c.toolRegistry,
		config:         c.config,
		mode:           c.mode,
		handlers:       handlers,
		asyncHandlers:  asyncHandlers,
		pendingStore:   sdktools.NewPendingStore(),
		mcpRegistry:    c.mcpRegistry, // Share MCP registry
	}

	// Fork the session based on current mode
	switch c.mode {
	case UnaryMode:
		forkSession, err := c.unarySession.ForkSession(ctx, forkID, pipeline)
		if err != nil {
			return nil
		}
		fork.unarySession = forkSession

	case DuplexMode:
		// For duplex mode, create pipeline builder for the fork
		// Returns *stage.StreamPipeline directly for duplex sessions
		pipelineBuilder := func(
			ctx context.Context,
			provider providers.Provider,
			providerSession providers.StreamInputSession,
			convID string,
			stateStore statestore.Store,
		) (*stage.StreamPipeline, error) {
			return fork.buildStreamPipelineWithParams(stateStore, convID, providerSession)
		}

		forkSession, err := c.duplexSession.ForkSession(ctx, forkID, pipelineBuilder)
		if err != nil {
			return nil
		}
		fork.duplexSession = forkSession
	}

	return fork
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

	// Close duplex session if in duplex mode
	if c.mode == DuplexMode && c.duplexSession != nil {
		if err := c.duplexSession.Close(); err != nil {
			return fmt.Errorf("failed to close duplex session: %w", err)
		}
	}

	// Close MCP registry if present
	if c.mcpRegistry != nil {
		if err := c.mcpRegistry.Close(); err != nil {
			return fmt.Errorf("failed to close MCP registry: %w", err)
		}
	}

	// State is automatically persisted by the StateStore middleware in the pipeline
	// No explicit save needed here

	return nil
}

// ID returns the conversation's unique identifier.
func (c *Conversation) ID() string {
	return c.getBaseSession().ID()
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
	return c.config.eventBus
}
