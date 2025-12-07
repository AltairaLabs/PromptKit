package sdk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/template"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
)

// Default parameter values for LLM calls.
const (
	defaultMaxTokens   = 4096
	defaultTemperature = 0.7
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
	pack       *pack.Pack
	prompt     *pack.Prompt
	promptName string

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
	// Get current variables for template substitution
	vars := c.getVariables()

	// Render system prompt with variables
	renderer := template.NewRenderer()
	systemPrompt, err := renderer.Render(c.prompt.SystemTemplate, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to render system prompt: %w", err)
	}

	// Build tool registry from handlers
	toolRegistry, toolDescriptors := c.buildToolRegistry()

	// Build pipeline
	pipelineCfg := &intpipeline.Config{
		Provider:     c.provider,
		ToolRegistry: toolRegistry,
		SystemPrompt: systemPrompt,
		Tools:        toolDescriptors,
		MaxTokens:    defaultMaxTokens,
		Temperature:  defaultTemperature,
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
func (c *Conversation) getVariables() map[string]string {
	c.varMu.RLock()
	defer c.varMu.RUnlock()
	vars := make(map[string]string, len(c.variables))
	for k, v := range c.variables {
		vars[k] = v
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

// buildToolRegistry creates a tool registry from registered handlers.
func (c *Conversation) buildToolRegistry() (*tools.Registry, []*tools.ToolDescriptor) {
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()

	if len(c.handlers) == 0 {
		return nil, nil
	}

	registry := tools.NewRegistry()
	var descriptors []*tools.ToolDescriptor

	// Get tool definitions from pack
	for name, handler := range c.handlers {
		packTool := c.pack.GetTool(name)
		if packTool == nil {
			// Tool not in pack - skip (or we could error)
			continue
		}

		// Convert pack tool parameters to JSON
		paramsJSON, err := json.Marshal(packTool.Parameters)
		if err != nil {
			continue
		}

		// Create descriptor
		desc := &tools.ToolDescriptor{
			Name:        packTool.Name,
			Description: packTool.Description,
			InputSchema: paramsJSON,
			Mode:        "local", // SDK handlers are always local
		}
		descriptors = append(descriptors, desc)

		// Register executor adapter
		adapter := &handlerAdapter{
			name:    name,
			handler: handler,
		}
		registry.RegisterExecutor(adapter)
	}

	return registry, descriptors
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
	ch := make(chan StreamChunk)

	go func() {
		defer close(ch)

		// For now, fall back to Send and emit as single chunk
		resp, err := c.Send(ctx, message, opts...)
		if err != nil {
			ch <- StreamChunk{Error: err}
			return
		}

		ch <- StreamChunk{
			Type:    ChunkDone,
			Message: resp,
		}
	}()

	return ch
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

// ToolRegistry returns the underlying tool registry.
//
// This is a power-user method for direct registry access. Tool descriptors
// are loaded from the pack; this allows inspecting them or registering
// custom executors.
//
//	registry := conv.ToolRegistry()
//	for _, desc := range registry.Descriptors() {
//	    fmt.Printf("Tool: %s\n", desc.Name)
//	}
func (c *Conversation) ToolRegistry() interface{} {
	// TODO: Return the actual registry when pipeline is built
	return nil
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
		pack:       c.pack,
		prompt:     c.prompt,
		promptName: c.promptName,
		provider:   c.provider,
		config:     c.config,
		variables:  vars,
		handlers:   handlers,
		state:      stateCopy,
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
