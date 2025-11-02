package sdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// Role constants for message types
const (
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleTool      = "tool"
)

// ConversationManager provides high-level API for managing LLM conversations.
// It automatically constructs the pipeline with appropriate middleware based on
// the PromptPack configuration.
//
// Key Features:
//   - Load PromptPacks and create conversations for specific prompts
//   - Automatic pipeline construction with middleware stack
//   - State persistence via StateStore
//   - Support for streaming and tool execution
//   - Multi-turn conversation management
type ConversationManager struct {
	packManager  *PackManager
	provider     providers.Provider
	stateStore   statestore.Store
	toolRegistry *tools.Registry

	// Configuration
	config ManagerConfig

	// Active conversations
	conversations map[string]*Conversation
	mu            sync.RWMutex
}

// ManagerConfig configures the ConversationManager
type ManagerConfig struct {
	// MaxConcurrentExecutions limits parallel pipeline executions
	MaxConcurrentExecutions int

	// DefaultTimeout for LLM requests
	DefaultTimeout time.Duration

	// EnableMetrics enables built-in metrics collection
	EnableMetrics bool
}

// ConversationConfig configures a new conversation
type ConversationConfig struct {
	// Required fields
	UserID     string // User who owns this conversation
	PromptName string // Task type from the pack (e.g., "support", "sales")

	// Optional fields
	ConversationID string                 // If empty, auto-generated
	Variables      map[string]interface{} // Template variables
	SystemPrompt   string                 // Override system prompt
	Metadata       map[string]interface{} // Custom metadata

	// Context policy (token budget management)
	ContextPolicy *middleware.ContextBuilderPolicy
}

// Conversation represents an active conversation
type Conversation struct {
	id            string
	userID        string
	promptName    string
	pack          *Pack
	prompt        *Prompt
	manager       *ConversationManager
	registry      *prompt.Registry                 // Prompt registry for middleware pipeline
	contextPolicy *middleware.ContextBuilderPolicy // Token budget policy

	// State
	state *statestore.ConversationState
	mu    sync.RWMutex
}

// SendOptions configures message sending behavior
type SendOptions struct {
	Stream       bool                   // Enable streaming
	MaxToolCalls int                    // Max tool calls per turn (0 = use prompt default)
	Metadata     map[string]interface{} // Turn-specific metadata
}

// Response represents a conversation turn response
type Response struct {
	Content      string
	ToolCalls    []types.MessageToolCall
	TokensUsed   int
	Cost         float64
	LatencyMs    int64
	Validations  []types.ValidationResult
	Truncated    bool                    // True if context was truncated
	PendingTools []tools.PendingToolInfo // Tools awaiting external approval/input
}

// StreamEvent represents a streaming response event
type StreamEvent struct {
	Type     string // "content", "tool_call", "done", "error"
	Content  string
	ToolCall *types.MessageToolCall
	Error    error
	Final    *Response // Set when Type="done"
}

// NewConversationManager creates a new ConversationManager
func NewConversationManager(opts ...ManagerOption) (*ConversationManager, error) {
	cm := &ConversationManager{
		packManager:   NewPackManager(),
		conversations: make(map[string]*Conversation),
		config: ManagerConfig{
			MaxConcurrentExecutions: 100,
			DefaultTimeout:          30 * time.Second,
		},
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(cm); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Validate required fields
	if cm.provider == nil {
		return nil, fmt.Errorf("provider is required (use WithProvider option)")
	}

	// Default to in-memory state store if not provided
	if cm.stateStore == nil {
		cm.stateStore = statestore.NewMemoryStore()
	}

	return cm, nil
}

// ManagerOption configures ConversationManager
type ManagerOption func(*ConversationManager) error

// WithProvider sets the LLM provider
func WithProvider(provider providers.Provider) ManagerOption {
	return func(cm *ConversationManager) error {
		cm.provider = provider
		return nil
	}
}

// WithStateStore sets the state persistence backend
func WithStateStore(store statestore.Store) ManagerOption {
	return func(cm *ConversationManager) error {
		cm.stateStore = store
		return nil
	}
}

// WithToolRegistry sets the tool registry for tool execution
func WithToolRegistry(registry *tools.Registry) ManagerOption {
	return func(cm *ConversationManager) error {
		cm.toolRegistry = registry
		return nil
	}
}

// WithConfig sets the manager configuration
func WithConfig(config ManagerConfig) ManagerOption {
	return func(cm *ConversationManager) error {
		cm.config = config
		return nil
	}
}

// LoadPack loads a PromptPack from a file
func (cm *ConversationManager) LoadPack(packPath string) (*Pack, error) {
	return cm.packManager.LoadPack(packPath)
}

// CreateConversation creates a new conversation for a specific prompt in the pack
func (cm *ConversationManager) CreateConversation(ctx context.Context, pack *Pack, config ConversationConfig) (*Conversation, error) {
	// Validate config
	if config.UserID == "" {
		return nil, WrapPackError(ErrInvalidConfig, "user_id is required")
	}
	if config.PromptName == "" {
		return nil, WrapPackError(ErrInvalidConfig, "prompt_name is required")
	}

	// Get prompt from pack
	prompt, err := pack.GetPrompt(config.PromptName)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt: %w", err)
	}

	// Generate conversation ID if not provided
	conversationID := config.ConversationID
	if conversationID == "" {
		conversationID = generateConversationID()
	}

	// Create prompt registry from pack
	registry, err := pack.CreateRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to create registry from pack: %w", err)
	}

	// Convert variables to string map for middleware
	baseVariables := make(map[string]string)
	for k, v := range config.Variables {
		baseVariables[k] = fmt.Sprintf("%v", v)
	}

	// Use middleware pipeline to assemble system prompt
	systemPrompt := config.SystemPrompt
	if systemPrompt == "" {
		// Build minimal pipeline to assemble prompt
		execCtx := &pipeline.ExecutionContext{
			Context:   ctx,
			Metadata:  make(map[string]interface{}),
			Variables: baseVariables,
		}

		// Create prompt assembly middleware
		promptMiddleware := middleware.PromptAssemblyMiddleware(registry, config.PromptName, baseVariables)
		templateMiddleware := middleware.TemplateMiddleware()

		// Execute middleware chain to populate SystemPrompt
		err = promptMiddleware.Process(execCtx, func() error {
			return templateMiddleware.Process(execCtx, func() error {
				return nil
			})
		})
		if err != nil {
			return nil, fmt.Errorf("failed to assemble prompt: %w", err)
		}

		systemPrompt = execCtx.SystemPrompt
	}

	// Create conversation state
	state := &statestore.ConversationState{
		ID:             conversationID,
		UserID:         config.UserID,
		Messages:       []types.Message{},
		SystemPrompt:   systemPrompt,
		Summaries:      []statestore.Summary{},
		TokenCount:     0,
		LastAccessedAt: time.Now(),
		Metadata:       config.Metadata,
	}

	// Store prompt_name in metadata for later loading
	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}
	state.Metadata["prompt_name"] = config.PromptName

	// Save initial state
	if err := cm.stateStore.Save(ctx, state); err != nil {
		return nil, fmt.Errorf("failed to save conversation state: %w", err)
	}

	// Create conversation
	conv := &Conversation{
		id:            conversationID,
		userID:        config.UserID,
		promptName:    config.PromptName,
		pack:          pack,
		prompt:        prompt,
		manager:       cm,
		registry:      registry,
		contextPolicy: config.ContextPolicy,
		state:         state,
	}

	// Register conversation
	cm.mu.Lock()
	cm.conversations[conversationID] = conv
	cm.mu.Unlock()

	return conv, nil
}

// GetConversation loads an existing conversation from state store
func (cm *ConversationManager) GetConversation(ctx context.Context, conversationID string, pack *Pack) (*Conversation, error) {
	// Check if already loaded
	cm.mu.RLock()
	if conv, exists := cm.conversations[conversationID]; exists {
		cm.mu.RUnlock()
		return conv, nil
	}
	cm.mu.RUnlock()

	// Load from state store
	state, err := cm.stateStore.Load(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load conversation: %w", err)
	}

	// Get prompt from pack (we need to know which prompt this conversation uses)
	// For now, we'll store this in metadata during NewConversation
	promptName, ok := state.Metadata["prompt_name"].(string)
	if !ok {
		return nil, fmt.Errorf("conversation metadata missing prompt_name")
	}

	prompt, err := pack.GetPrompt(promptName)
	if err != nil {
		return nil, fmt.Errorf("failed to get prompt: %w", err)
	}

	// Create prompt registry from pack
	registry, err := pack.CreateRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to create registry from pack: %w", err)
	}

	// Create conversation
	conv := &Conversation{
		id:            conversationID,
		userID:        state.UserID,
		promptName:    promptName,
		pack:          pack,
		prompt:        prompt,
		manager:       cm,
		registry:      registry,
		contextPolicy: nil, // Context policy can be loaded from metadata when persistence layer supports it
		state:         state,
	}

	// Register conversation
	cm.mu.Lock()
	cm.conversations[conversationID] = conv
	cm.mu.Unlock()

	return conv, nil
}

// Send sends a user message and gets an assistant response
func (c *Conversation) Send(ctx context.Context, userMessage string, opts ...SendOptions) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build middleware pipeline using helper
	pipelineMiddleware := c.buildMiddlewarePipeline()

	// Create pipeline
	p := pipeline.NewPipeline(pipelineMiddleware...)

	// Execute pipeline with user message
	// The pipeline will automatically handle loading state, adding the message, executing LLM, and saving state
	start := time.Now()
	result, err := p.Execute(ctx, "user", userMessage)
	if err != nil {
		return nil, fmt.Errorf("pipeline execution error: %w", err)
	}
	latency := time.Since(start)

	// Extract response
	if result.Response == nil {
		return nil, fmt.Errorf("no response from pipeline")
	}

	// Update conversation state with the new messages from pipeline
	c.state.Messages = result.Messages
	c.state.LastAccessedAt = time.Now()
	// Note: SystemPrompt is already set in state from NewConversation

	// Extract validations from the assistant message
	var validations []types.ValidationResult
	if len(result.Messages) > 0 {
		lastMsg := result.Messages[len(result.Messages)-1]
		if lastMsg.Role == RoleAssistant {
			validations = lastMsg.Validations
		}
	}

	// Extract pending tools from result metadata
	var pendingTools []tools.PendingToolInfo
	if result.Metadata != nil {
		if pendingData, ok := result.Metadata["pending_tools"]; ok {
			switch v := pendingData.(type) {
			case []tools.PendingToolInfo:
				pendingTools = v
			case []*tools.PendingToolInfo:
				for _, p := range v {
					if p != nil {
						pendingTools = append(pendingTools, *p)
					}
				}
			case []interface{}:
				for _, item := range v {
					if info, ok := item.(*tools.PendingToolInfo); ok {
						pendingTools = append(pendingTools, *info)
					} else if info, ok := item.(tools.PendingToolInfo); ok {
						pendingTools = append(pendingTools, info)
					}
				}
			}
		}
	}

	// Build response
	response := &Response{
		Content:      result.Response.Content,
		ToolCalls:    result.Response.ToolCalls,
		TokensUsed:   result.CostInfo.InputTokens + result.CostInfo.OutputTokens,
		Cost:         result.CostInfo.TotalCost,
		LatencyMs:    latency.Milliseconds(),
		Validations:  validations,
		PendingTools: pendingTools,
	}

	return response, nil
}

// SendStream sends a user message and returns a streaming response
func (c *Conversation) SendStream(ctx context.Context, userMessage string, opts ...SendOptions) (<-chan StreamEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build middleware pipeline using helper
	pipelineMiddleware := c.buildMiddlewarePipeline()

	// Create pipeline
	p := pipeline.NewPipeline(pipelineMiddleware...)

	// Execute pipeline in streaming mode
	streamChan, err := p.ExecuteStream(ctx, "user", userMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to start streaming: %w", err)
	}

	// Create output channel for SDK stream events
	eventChan := make(chan StreamEvent, 10)

	// Start goroutine to convert provider stream chunks to SDK stream events
	go func() {
		defer close(eventChan)

		start := time.Now()
		var lastContent string
		var finalResult *pipeline.ExecutionResult

		for chunk := range streamChan {
			// Handle errors
			if chunk.Error != nil {
				eventChan <- StreamEvent{
					Type:  "error",
					Error: chunk.Error,
				}
				return
			}

			// Handle content deltas
			if chunk.Delta != "" {
				eventChan <- StreamEvent{
					Type:    "content",
					Content: chunk.Delta,
				}
				lastContent = chunk.Content // Accumulated content
			}

			// Handle tool calls
			if len(chunk.ToolCalls) > 0 {
				for _, tc := range chunk.ToolCalls {
					eventChan <- StreamEvent{
						Type: "tool_call",
						ToolCall: &types.MessageToolCall{
							ID:   tc.ID,
							Name: tc.Name,
							Args: tc.Args,
						},
					}
				}
			}

			// Handle stream completion
			if chunk.FinishReason != nil {
				// Extract final result if present
				if chunk.FinalResult != nil {
					if result, ok := chunk.FinalResult.(*pipeline.ExecutionResult); ok {
						finalResult = result
					}
				}

				// Update conversation state
				if finalResult != nil {
					c.state.Messages = finalResult.Messages
					c.state.LastAccessedAt = time.Now()

					// Extract validations from the assistant message
					var validations []types.ValidationResult
					if len(finalResult.Messages) > 0 {
						lastMsg := finalResult.Messages[len(finalResult.Messages)-1]
						if lastMsg.Role == RoleAssistant {
							validations = lastMsg.Validations
						}
					}

					// Build final response
					response := &Response{
						Content:     lastContent,
						ToolCalls:   finalResult.Response.ToolCalls,
						TokensUsed:  finalResult.CostInfo.InputTokens + finalResult.CostInfo.OutputTokens,
						Cost:        finalResult.CostInfo.TotalCost,
						LatencyMs:   time.Since(start).Milliseconds(),
						Validations: validations,
					}

					// Send done event with final response
					eventChan <- StreamEvent{
						Type:  "done",
						Final: response,
					}
				} else {
					// No final result, just send done
					eventChan <- StreamEvent{
						Type: "done",
					}
				}

				return
			}
		}
	}()

	return eventChan, nil
}

// HasPendingTools checks if the conversation has any pending tool calls awaiting approval
func (c *Conversation) HasPendingTools() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check if the last message is an assistant message with tool calls
	// and there's no corresponding tool result message
	if len(c.state.Messages) == 0 {
		return false
	}

	// Find the last assistant message with tool calls
	for i := len(c.state.Messages) - 1; i >= 0; i-- {
		msg := c.state.Messages[i]

		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			// Check if any tool call is missing a result
			for _, toolCall := range msg.ToolCalls {
				hasResult := false

				// Look for corresponding tool result message
				for j := i + 1; j < len(c.state.Messages); j++ {
					if c.state.Messages[j].Role == RoleTool &&
						c.state.Messages[j].ToolResult != nil &&
						c.state.Messages[j].ToolResult.ID == toolCall.ID {
						hasResult = true
						break
					}
				}

				if !hasResult {
					return true
				}
			}
		}
	}

	return false
}

// GetPendingTools returns information about pending tool calls that require approval.
// This extracts PendingToolInfo from the conversation state metadata.
func (c *Conversation) GetPendingTools() []tools.PendingToolInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var pending []tools.PendingToolInfo

	// Check metadata for pending_tools (set by provider middleware)
	if c.state.Metadata != nil {
		if pendingData, ok := c.state.Metadata["pending_tools"]; ok {
			// Handle different possible types
			switch v := pendingData.(type) {
			case []tools.PendingToolInfo:
				pending = v
			case []*tools.PendingToolInfo:
				// Convert pointers to values
				for _, p := range v {
					if p != nil {
						pending = append(pending, *p)
					}
				}
			case []interface{}:
				// Try to convert each item
				for _, item := range v {
					if info, ok := item.(*tools.PendingToolInfo); ok {
						pending = append(pending, *info)
					} else if info, ok := item.(tools.PendingToolInfo); ok {
						pending = append(pending, info)
					}
				}
			}
		}
	}

	return pending
}

// AddToolResult adds a tool execution result to the conversation.
// This is used to provide the result of a tool call that was pending approval.
//
// Parameters:
//   - toolCallID: The ID of the tool call (from MessageToolCall.ID)
//   - result: The JSON string result from the tool execution
func (c *Conversation) AddToolResult(toolCallID, result string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate that there's a pending tool call with this ID
	found := false
	for i := len(c.state.Messages) - 1; i >= 0; i-- {
		msg := c.state.Messages[i]
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				if toolCall.ID == toolCallID {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
	}

	if !found {
		return fmt.Errorf("tool call ID %s not found in conversation history", toolCallID)
	}

	// Add tool result message
	toolResultMsg := types.Message{
		Role:    RoleTool,
		Content: result,
		ToolResult: &types.MessageToolResult{
			ID:      toolCallID,
			Content: result,
		},
	}

	c.state.Messages = append(c.state.Messages, toolResultMsg)
	c.state.LastAccessedAt = time.Now()

	// Clear pending_tools from metadata since we're adding a result
	if c.state.Metadata != nil {
		// Note: We only clear if ALL pending tools have results
		// For simplicity, we'll keep the metadata until Continue() is called
	}

	return nil
}

// Continue resumes execution after tool results have been added.
// This should be called after one or more AddToolResult() calls to continue
// the conversation with the LLM using the tool results.
//
// Returns the assistant's response after processing the tool results.
func (c *Conversation) Continue(ctx context.Context) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Verify we have tool result messages to continue with
	if len(c.state.Messages) == 0 {
		return nil, fmt.Errorf("no messages in conversation")
	}

	lastMsg := c.state.Messages[len(c.state.Messages)-1]
	if lastMsg.Role != RoleTool {
		return nil, fmt.Errorf("last message must be a tool result to continue")
	}

	// Save the current state before continuing
	if err := c.manager.stateStore.Save(ctx, c.state); err != nil {
		return nil, fmt.Errorf("failed to save state before continuing: %w", err)
	}

	// Build middleware pipeline (similar to Send but without adding a new user message)
	var pipelineMiddleware []pipeline.Middleware

	// 1. StateStore Load middleware
	storeConfig := &pipeline.StateStoreConfig{
		Store:          c.manager.stateStore,
		ConversationID: c.id,
		UserID:         c.userID,
		Metadata:       c.state.Metadata,
	}
	pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreLoadMiddleware(storeConfig))

	// 2. Prompt assembly
	baseVariables := make(map[string]string)
	pipelineMiddleware = append(pipelineMiddleware, middleware.PromptAssemblyMiddleware(c.registry, c.promptName, baseVariables))

	// 3. Template middleware
	pipelineMiddleware = append(pipelineMiddleware, middleware.TemplateMiddleware())

	// 4. Context builder (if configured)
	if c.contextPolicy != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.ContextBuilderMiddleware(c.contextPolicy))
	}

	// 5. Provider middleware
	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   c.prompt.Parameters.MaxTokens,
		Temperature: float32(c.prompt.Parameters.Temperature),
	}

	var toolPolicy *pipeline.ToolPolicy
	if c.prompt.ToolPolicy != nil {
		toolPolicy = &pipeline.ToolPolicy{
			ToolChoice:          c.prompt.ToolPolicy.ToolChoice,
			MaxToolCallsPerTurn: c.prompt.ToolPolicy.MaxToolCallsPerTurn,
			Blocklist:           c.prompt.ToolPolicy.Blocklist,
		}
	}

	pipelineMiddleware = append(pipelineMiddleware, middleware.ProviderMiddleware(
		c.manager.provider,
		c.manager.toolRegistry,
		toolPolicy,
		providerConfig,
	))

	// 6. Validator middleware
	pipelineMiddleware = append(pipelineMiddleware, middleware.DynamicValidatorMiddleware(validators.DefaultRegistry))

	// 7. StateStore Save middleware
	pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreSaveMiddleware(storeConfig))

	// Create pipeline
	p := pipeline.NewPipeline(pipelineMiddleware...)

	// Execute pipeline using ExecuteWithMessage to continue from current state
	// We need to use a special approach since we're not adding a new user message
	// Instead, we'll execute with the existing message history

	// The pipeline will load the state (including our new tool result message)
	// and the provider middleware will see the tool result and continue execution

	// Create a synthetic "continue" message that the pipeline can process
	// Actually, we can use Execute with an empty continuation
	start := time.Now()

	// Use ExecuteWithMessage with the last tool result message
	result, err := p.ExecuteWithMessage(ctx, lastMsg)
	if err != nil {
		return nil, fmt.Errorf("pipeline execution error: %w", err)
	}
	latency := time.Since(start)

	// Update conversation state
	c.state.Messages = result.Messages
	c.state.LastAccessedAt = time.Now()

	// Clear pending tools from metadata
	if c.state.Metadata != nil {
		delete(c.state.Metadata, "pending_tools")
	}

	// Extract validations
	var validations []types.ValidationResult
	if len(result.Messages) > 0 {
		lastMsg := result.Messages[len(result.Messages)-1]
		if lastMsg.Role == RoleAssistant {
			validations = lastMsg.Validations
		}
	}

	// Extract pending tools from result metadata (in case there are more pending)
	var pendingTools []tools.PendingToolInfo
	if result.Metadata != nil {
		if pendingData, ok := result.Metadata["pending_tools"]; ok {
			switch v := pendingData.(type) {
			case []tools.PendingToolInfo:
				pendingTools = v
			case []*tools.PendingToolInfo:
				for _, p := range v {
					if p != nil {
						pendingTools = append(pendingTools, *p)
					}
				}
			case []interface{}:
				for _, item := range v {
					if info, ok := item.(*tools.PendingToolInfo); ok {
						pendingTools = append(pendingTools, *info)
					} else if info, ok := item.(tools.PendingToolInfo); ok {
						pendingTools = append(pendingTools, info)
					}
				}
			}
		}
	}

	// Build response
	response := &Response{
		Content:      result.Response.Content,
		ToolCalls:    result.Response.ToolCalls,
		TokensUsed:   result.CostInfo.InputTokens + result.CostInfo.OutputTokens,
		Cost:         result.CostInfo.TotalCost,
		LatencyMs:    latency.Milliseconds(),
		Validations:  validations,
		PendingTools: pendingTools,
	}

	return response, nil
}

// GetHistory returns the conversation message history
func (c *Conversation) GetHistory() []types.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	history := make([]types.Message, len(c.state.Messages))
	copy(history, c.state.Messages)
	return history
}

// GetID returns the conversation ID
func (c *Conversation) GetID() string {
	return c.id
}

// GetUserID returns the user ID
func (c *Conversation) GetUserID() string {
	return c.userID
}

// Helper functions

// buildMiddlewarePipeline constructs the middleware pipeline for conversation execution
func (c *Conversation) buildMiddlewarePipeline() []pipeline.Middleware {
	var pipelineMiddleware []pipeline.Middleware

	// 1. StateStore Load middleware - loads conversation state
	storeConfig := &pipeline.StateStoreConfig{
		Store:          c.manager.stateStore,
		ConversationID: c.id,
		UserID:         c.userID,
		Metadata:       c.state.Metadata,
	}
	pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreLoadMiddleware(storeConfig))

	// 2. Prompt assembly - loads prompt config and populates SystemPrompt + AllowedTools
	baseVariables := make(map[string]string)
	pipelineMiddleware = append(pipelineMiddleware, middleware.PromptAssemblyMiddleware(c.registry, c.promptName, baseVariables))

	// 3. Template middleware - substitutes variables in system prompt
	pipelineMiddleware = append(pipelineMiddleware, middleware.TemplateMiddleware())

	// 4. Context builder middleware - manages token budget and truncation (if policy configured)
	if c.contextPolicy != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.ContextBuilderMiddleware(c.contextPolicy))
	}

	// 5. Provider middleware - executes LLM
	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   c.prompt.Parameters.MaxTokens,
		Temperature: float32(c.prompt.Parameters.Temperature),
	}

	// Tool policy from prompt if configured
	var toolPolicy *pipeline.ToolPolicy
	if c.prompt.ToolPolicy != nil {
		toolPolicy = &pipeline.ToolPolicy{
			ToolChoice:          c.prompt.ToolPolicy.ToolChoice,
			MaxToolCallsPerTurn: c.prompt.ToolPolicy.MaxToolCallsPerTurn,
			Blocklist:           c.prompt.ToolPolicy.Blocklist,
		}
	}

	pipelineMiddleware = append(pipelineMiddleware, middleware.ProviderMiddleware(
		c.manager.provider,
		c.manager.toolRegistry,
		toolPolicy,
		providerConfig,
	))

	// 6. Dynamic validator middleware - validates response
	pipelineMiddleware = append(pipelineMiddleware, middleware.DynamicValidatorMiddleware(validators.DefaultRegistry))

	// 7. StateStore Save middleware - saves conversation state
	pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreSaveMiddleware(storeConfig))

	return pipelineMiddleware
}

func convertMessagesToProvider(messages []types.Message) []types.Message {
	// Messages are already in the correct type, just return a copy
	providerMsgs := make([]types.Message, len(messages))
	copy(providerMsgs, messages)
	return providerMsgs
}

func generateConversationID() string {
	// Simple timestamp-based ID for now
	// In production, use UUID or similar
	return fmt.Sprintf("conv_%d", time.Now().UnixNano())
}
