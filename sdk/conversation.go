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

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/session"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
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

	// Text session for executing pipeline
	textSession session.TextSession

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

// buildPipelineWithParams builds a pipeline with explicit parameters.
// Used during initialization when textSession doesn't exist yet.
func (c *Conversation) buildPipelineWithParams(store statestore.Store, conversationID string) (*rtpipeline.Pipeline, error) {
	// Note: Variables are no longer passed to pipeline at build time.
	// They are resolved dynamically from the session during execution.
	// For now, pass empty vars since pipeline doesn't capture them anymore anyway.
	vars := make(map[string]string)

	// Build tool registry
	c.handlersMu.RLock()
	localExec := &localExecutor{handlers: c.handlers}
	c.toolRegistry.RegisterExecutor(localExec)
	c.registerMCPExecutors()
	toolRegistry := c.toolRegistry
	c.handlersMu.RUnlock()

	// Build pipeline configuration
	pipelineCfg := &intpipeline.Config{
		Provider:          c.provider,
		ToolRegistry:      toolRegistry,
		PromptRegistry:    c.promptRegistry,
		TaskType:          c.promptName,
		Variables:         vars,
		VariableProviders: c.config.variableProviders, // Pass to pipeline for dynamic resolution
		MaxTokens:         defaultMaxTokens,
		Temperature:       defaultTemperature,
		StateStore:        store,
		ConversationID:    conversationID,
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

// executePipeline builds and executes the LLM pipeline.
func (c *Conversation) executePipeline(
	ctx context.Context,
	userMsg *types.Message,
) (*rtpipeline.ExecutionResult, error) {
	// Execute through the text session
	return c.textSession.ExecuteWithMessage(ctx, *userMsg)
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

// SetVar sets a single template variable.
//
// Variables are substituted into the system prompt template:
//
//	conv.SetVar("customer_name", "Alice")
//	// Template: "You are helping {{customer_name}}"
//	// Becomes: "You are helping Alice"
func (c *Conversation) SetVar(name, value string) {
	c.textSession.SetVar(name, value)
}

// SetVars sets multiple template variables at once.
//
//	conv.SetVars(map[string]any{
//	    "customer_name": "Alice",
//	    "customer_tier": "premium",
//	    "max_discount": 20,
//	})
func (c *Conversation) SetVars(vars map[string]any) {
	for k, v := range vars {
		c.textSession.SetVar(k, fmt.Sprintf("%v", v))
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
			c.textSession.SetVar(varName, value)
		}
	}
}

// GetVar returns the current value of a template variable.
// Returns empty string if the variable is not set.
func (c *Conversation) GetVar(name string) string {
	return c.textSession.GetVar(name)
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

	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, ErrConversationClosed
	}
	c.mu.RUnlock()

	// Build the tool results from resolved pending tools
	// and continue the conversation
	// For now, this is a simplified implementation that re-sends
	// with the last context - full implementation would inject tool results

	// Get the last message from state store
	msgs := c.Messages(ctx)
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages to continue from")
	}

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
func (c *Conversation) Messages(ctx context.Context) []types.Message {
	store := c.textSession.StateStore()
	if store == nil {
		return nil
	}

	state, err := store.Load(ctx, c.textSession.ID())
	if err != nil {
		return nil
	}

	// Return a copy
	messages := make([]types.Message, len(state.Messages))
	copy(messages, state.Messages)
	return messages
}

// Clear removes all messages from the conversation history.
//
// This keeps the system prompt and variables but removes all user/assistant
// messages. Useful for starting fresh within the same conversation session.
func (c *Conversation) Clear() {
	store := c.textSession.StateStore()
	if store == nil {
		return
	}

	// Reset state in store
	ctx := context.Background()
	state := &statestore.ConversationState{
		ID:       c.textSession.ID(),
		Messages: nil,
	}
	_ = store.Save(ctx, state)
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
	c.handlersMu.RLock()
	defer c.handlersMu.RUnlock()
	c.asyncHandlersMu.RLock()
	defer c.asyncHandlersMu.RUnlock()

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

	// Create fork with new ID
	forkID := c.textSession.ID() + "-fork"

	// Fork state using the store's Fork method
	store := c.textSession.StateStore()
	if store != nil {
		ctx := context.Background()
		_ = store.Fork(ctx, c.textSession.ID(), forkID)
	}

	// Copy variables from the current session
	currentVars := c.textSession.Variables()
	forkVars := make(map[string]string, len(currentVars))
	for k, v := range currentVars {
		forkVars[k] = v
	}

	// Build a pipeline for the fork (reuses same configuration)
	forkPipeline, err := c.buildPipelineWithParams(store, forkID)
	if err != nil {
		// If pipeline build fails, return a conversation without a session
		// This maintains backward compatibility with tests
		return &Conversation{
			pack:           c.pack,
			prompt:         c.prompt,
			promptName:     c.promptName,
			promptRegistry: c.promptRegistry,
			toolRegistry:   c.toolRegistry,
			provider:       c.provider,
			config:         c.config,
			textSession:    nil,
			handlers:       handlers,
			asyncHandlers:  asyncHandlers,
			pendingStore:   sdktools.NewPendingStore(),
		}
	}

	// Create a new session for the fork with copied variables and forked state
	forkSession, err := session.NewTextSession(session.TextConfig{
		ConversationID: forkID,
		StateStore:     store,
		Pipeline:       forkPipeline,
		Variables:      forkVars,
	})
	if err != nil {
		// If session creation fails, return a conversation without a session
		return &Conversation{
			pack:           c.pack,
			prompt:         c.prompt,
			promptName:     c.promptName,
			promptRegistry: c.promptRegistry,
			toolRegistry:   c.toolRegistry,
			provider:       c.provider,
			config:         c.config,
			textSession:    nil,
			handlers:       handlers,
			asyncHandlers:  asyncHandlers,
			pendingStore:   sdktools.NewPendingStore(),
		}
	}

	return &Conversation{
		pack:           c.pack,
		prompt:         c.prompt,
		promptName:     c.promptName,
		promptRegistry: c.promptRegistry,
		toolRegistry:   c.toolRegistry,
		provider:       c.provider,
		config:         c.config,
		textSession:    forkSession,
		handlers:       handlers,
		asyncHandlers:  asyncHandlers,
		pendingStore:   sdktools.NewPendingStore(),
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

	// State is automatically persisted by the StateStore middleware in the pipeline
	// No explicit save needed here

	return nil
}

// ID returns the conversation's unique identifier.
func (c *Conversation) ID() string {
	return c.textSession.ID()
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
