package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
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
	mediaStorage storage.MediaStorageService
	eventBus     *events.EventBus

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

	// Media externalization settings
	EnableMediaExternalization bool   // Enable automatic media externalization
	MediaSizeThresholdKB       int64  // Size threshold in KB (media larger than this is externalized)
	MediaDefaultPolicy         string // Default retention policy for externalized media
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
	eventBus      *events.EventBus
	baseVariables map[string]string // Template variables for prompt assembly

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

// WithMediaStorage sets the media storage service for automatic media externalization.
// When enabled, large media content in provider responses is automatically moved from inline
// base64 data to file storage, significantly reducing memory usage and improving performance.
//
// Default behavior when storage is provided:
//   - Media externalization is enabled
//   - Size threshold is set to 100KB (media larger than this is externalized)
//   - Retention policy defaults to "retain" (keep media indefinitely)
//
// To customize these defaults, use WithConfig after WithMediaStorage.
//
// Example:
//
//	storage, _ := local.NewFileStore(local.FileStoreConfig{BaseDir: "./media"})
//	manager, _ := sdk.NewConversationManager(
//	    sdk.WithProvider(provider),
//	    sdk.WithMediaStorage(storage),
//	)
func WithMediaStorage(storageService storage.MediaStorageService) ManagerOption {
	return func(cm *ConversationManager) error {
		cm.mediaStorage = storageService
		// Enable media externalization by default when storage is provided
		if cm.config.MediaSizeThresholdKB == 0 {
			cm.config.MediaSizeThresholdKB = 100 // Default 100KB threshold
		}
		if cm.config.MediaDefaultPolicy == "" {
			cm.config.MediaDefaultPolicy = "retain" // Default retention policy
		}
		cm.config.EnableMediaExternalization = true
		return nil
	}
}

// WithEventBus sets a shared event bus for conversation event listeners.
func WithEventBus(bus *events.EventBus) ManagerOption {
	return func(cm *ConversationManager) error {
		cm.eventBus = bus
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

	// Register pack tool schemas into tool registry
	// This ensures tools have proper InputSchema for LLM function calling
	// Users only need to provide executors - tool definitions come from pack
	if cm.toolRegistry != nil && len(pack.Tools) > 0 {
		for _, tool := range pack.Tools {
			// Convert Parameters map to JSON for InputSchema
			var inputSchema json.RawMessage
			if tool.Parameters != nil {
				schemaBytes, schemaErr := json.Marshal(tool.Parameters)
				if schemaErr != nil {
					return nil, fmt.Errorf("failed to marshal tool schema for %s: %w", tool.Name, schemaErr)
				}
				inputSchema = schemaBytes
			}

			// Check if tool already exists in registry
			if existing, _ := cm.toolRegistry.GetTool(tool.Name); existing != nil {
				// Update existing tool with schema from pack
				existing.Description = tool.Description
				existing.InputSchema = inputSchema
			} else {
				// Register new tool from pack
				if regErr := cm.toolRegistry.Register(&tools.ToolDescriptor{
					Name:        tool.Name,
					Description: tool.Description,
					InputSchema: inputSchema,
				}); regErr != nil {
					return nil, fmt.Errorf("failed to register tool %s: %w", tool.Name, regErr)
				}
			}
		}
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
	state.Metadata["base_variables"] = baseVariables

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
		eventBus:      cm.eventBus,
		baseVariables: baseVariables,
	}

	// Register conversation
	cm.mu.Lock()
	cm.conversations[conversationID] = conv
	cm.mu.Unlock()

	return conv, nil
}

// GetConversation loads an existing conversation from state store
func (cm *ConversationManager) GetConversation(
	ctx context.Context, conversationID string, pack *Pack,
) (*Conversation, error) {
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

	// Restore baseVariables from metadata
	baseVariables := make(map[string]string)
	switch vars := state.Metadata["base_variables"].(type) {
	case map[string]string:
		baseVariables = vars
	case map[string]interface{}:
		// Handle JSON unmarshaling which produces map[string]interface{}
		for k, v := range vars {
			if s, ok := v.(string); ok {
				baseVariables[k] = s
			}
		}
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
		eventBus:      cm.eventBus,
		baseVariables: baseVariables,
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
	emitter := c.getEmitter()

	// Execute pipeline with user message
	// The pipeline will automatically handle loading state, adding the message, executing LLM, and saving state
	start := time.Now()
	result, err := p.ExecuteWithOptions(&pipeline.ExecutionOptions{
		Context:        ctx,
		RunID:          c.id,
		SessionID:      c.userID,
		ConversationID: c.id,
		EventEmitter:   emitter,
	}, "user", userMessage)
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

	// Extract validations and pending tools
	validations := c.extractValidationsFromResult(result)
	pendingTools := c.extractPendingToolsFromMetadata(result.Metadata)

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

	// Execute pipeline in streaming mode
	streamChan, err := c.executeStreamingPipeline(ctx, userMessage)
	if err != nil {
		return nil, err
	}

	// Create and start stream processor
	eventChan := make(chan StreamEvent, 10)
	go c.processStreamEvents(streamChan, eventChan)

	return eventChan, nil
}

// executeStreamingPipeline executes the pipeline in streaming mode
func (c *Conversation) executeStreamingPipeline(ctx context.Context, userMessage string) (<-chan providers.StreamChunk, error) {
	pipelineMiddleware := c.buildMiddlewarePipeline()
	p := pipeline.NewPipeline(pipelineMiddleware...)

	emitter := c.getEmitter()
	streamChan, err := p.ExecuteStreamWithEvents(ctx, "user", userMessage, emitter)
	if err != nil {
		return nil, fmt.Errorf("failed to start streaming: %w", err)
	}

	return streamChan, nil
}

// processStreamEvents processes stream chunks and converts them to SDK events
func (c *Conversation) processStreamEvents(streamChan <-chan providers.StreamChunk, eventChan chan<- StreamEvent) {
	defer close(eventChan)

	start := time.Now()
	var lastContent string
	var finalResult *pipeline.ExecutionResult

	for chunk := range streamChan {
		if c.handleStreamError(&chunk, eventChan) {
			return
		}

		c.handleContentDelta(&chunk, eventChan, &lastContent)
		c.handleToolCalls(&chunk, eventChan)

		if c.handleStreamCompletion(&chunk, eventChan, &finalResult, lastContent, start) {
			return
		}
	}
}

// handleStreamError handles error events in the stream
func (c *Conversation) handleStreamError(chunk *providers.StreamChunk, eventChan chan<- StreamEvent) bool {
	if chunk.Error != nil {
		eventChan <- StreamEvent{
			Type:  "error",
			Error: chunk.Error,
		}
		return true // Signal to stop processing
	}
	return false
}

// handleContentDelta handles content delta events
func (c *Conversation) handleContentDelta(chunk *providers.StreamChunk, eventChan chan<- StreamEvent, lastContent *string) {
	if chunk.Delta != "" {
		eventChan <- StreamEvent{
			Type:    "content",
			Content: chunk.Delta,
		}
		*lastContent = chunk.Content // Accumulated content
	}
}

// handleToolCalls handles tool call events
func (c *Conversation) handleToolCalls(chunk *providers.StreamChunk, eventChan chan<- StreamEvent) {
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

// handleStreamCompletion handles stream completion and final response
func (c *Conversation) handleStreamCompletion(
	chunk *providers.StreamChunk,
	eventChan chan<- StreamEvent,
	finalResult **pipeline.ExecutionResult,
	lastContent string,
	start time.Time,
) bool {
	if chunk.FinishReason == nil {
		return false
	}

	// Extract final result if present
	c.extractFinalResult(chunk, finalResult)

	// Send completion event
	if *finalResult != nil {
		c.updateStateFromStreamResult(*finalResult)
		response := c.buildStreamResponse(*finalResult, lastContent, start)
		eventChan <- StreamEvent{Type: "done", Final: response}
	} else {
		eventChan <- StreamEvent{Type: "done"}
	}

	return true // Signal to stop processing
}

// extractFinalResult extracts the final result from stream chunk
func (c *Conversation) extractFinalResult(chunk *providers.StreamChunk, finalResult **pipeline.ExecutionResult) {
	if chunk.FinalResult != nil {
		if result, ok := chunk.FinalResult.(*pipeline.ExecutionResult); ok {
			*finalResult = result
		}
	}
}

// updateStateFromStreamResult updates conversation state from streaming result
func (c *Conversation) updateStateFromStreamResult(result *pipeline.ExecutionResult) {
	c.state.Messages = result.Messages
	c.state.LastAccessedAt = time.Now()
}

// buildStreamResponse builds the final response for streaming
func (c *Conversation) buildStreamResponse(result *pipeline.ExecutionResult, content string, start time.Time) *Response {
	return &Response{
		Content:     content,
		ToolCalls:   result.Response.ToolCalls,
		TokensUsed:  result.CostInfo.InputTokens + result.CostInfo.OutputTokens,
		Cost:        result.CostInfo.TotalCost,
		LatencyMs:   time.Since(start).Milliseconds(),
		Validations: c.extractValidationsFromResult(result), // Reuse existing function
	}
}

// HasPendingTools checks if the conversation has any pending tool calls awaiting approval
func (c *Conversation) HasPendingTools() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.state.Messages) == 0 {
		return false
	}

	// Find the last assistant message with tool calls
	msgIndex, msg := c.findLastAssistantMessageWithToolCalls()
	if msgIndex == -1 || msg == nil {
		return false
	}

	// Check if any tool call is missing a result
	return c.hasAnyPendingToolCall(msg.ToolCalls, msgIndex)
}

// hasAnyPendingToolCall checks if any tool calls are missing results
func (c *Conversation) hasAnyPendingToolCall(toolCalls []types.MessageToolCall, startIndex int) bool {
	for _, toolCall := range toolCalls {
		if !c.hasToolResult(toolCall.ID, startIndex) {
			return true
		}
	}
	return false
}

// GetPendingTools returns information about pending tool calls that require approval.
// This extracts PendingToolInfo from the conversation state metadata.
func (c *Conversation) GetPendingTools() []tools.PendingToolInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.state.Metadata == nil {
		return nil
	}

	return c.extractPendingToolsFromMetadata(c.state.Metadata)
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
	if !c.findToolCallInHistory(toolCallID) {
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

	// Note: We keep pending_tools in metadata until Continue() is called
	// This allows multiple AddToolResult calls before continuing

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

	// Validate preconditions for continuing
	lastMsg, err := c.validateContinuePreconditions()
	if err != nil {
		return nil, err
	}

	// Save current state before continuing
	if saveErr := c.manager.stateStore.Save(ctx, c.state); saveErr != nil {
		return nil, fmt.Errorf("failed to save state before continuing: %w", saveErr)
	}

	// Execute pipeline and get result
	result, latency, err := c.executeContinuePipeline(ctx, lastMsg)
	if err != nil {
		return nil, err
	}

	// Update conversation state and clean up metadata
	c.updateStateAfterContinue(result)

	// Build and return response
	return c.buildContinueResponse(result, latency), nil
}

// validateContinuePreconditions validates that we can continue the conversation
func (c *Conversation) validateContinuePreconditions() (types.Message, error) {
	if len(c.state.Messages) == 0 {
		return types.Message{}, fmt.Errorf("no messages in conversation")
	}

	lastMsg := c.state.Messages[len(c.state.Messages)-1]
	if lastMsg.Role != RoleTool {
		return types.Message{}, fmt.Errorf("last message must be a tool result to continue")
	}

	return lastMsg, nil
}

// executeContinuePipeline executes the pipeline for continuing conversation
func (c *Conversation) executeContinuePipeline(ctx context.Context, lastMsg types.Message) (*pipeline.ExecutionResult, time.Duration, error) {
	// Build middleware pipeline using the existing helper
	pipelineMiddleware := c.buildMiddlewarePipeline()

	// Create and execute pipeline
	p := pipeline.NewPipeline(pipelineMiddleware...)
	start := time.Now()

	result, err := p.ExecuteWithMessageOptions(&pipeline.ExecutionOptions{
		Context:        ctx,
		RunID:          c.id,
		SessionID:      c.userID,
		ConversationID: c.id,
		EventEmitter:   c.getEmitter(),
	}, lastMsg)
	if err != nil {
		return nil, 0, fmt.Errorf("pipeline execution error: %w", err)
	}

	return result, time.Since(start), nil
}

// updateStateAfterContinue updates conversation state after successful continue
func (c *Conversation) updateStateAfterContinue(result *pipeline.ExecutionResult) {
	c.state.Messages = result.Messages
	c.state.LastAccessedAt = time.Now()

	// Clear pending tools from metadata
	if c.state.Metadata != nil {
		delete(c.state.Metadata, "pending_tools")
	}
}

// buildContinueResponse builds the response object for Continue operation
func (c *Conversation) buildContinueResponse(result *pipeline.ExecutionResult, latency time.Duration) *Response {
	return &Response{
		Content:      result.Response.Content,
		ToolCalls:    result.Response.ToolCalls,
		TokensUsed:   result.CostInfo.InputTokens + result.CostInfo.OutputTokens,
		Cost:         result.CostInfo.TotalCost,
		LatencyMs:    latency.Milliseconds(),
		Validations:  c.extractValidationsFromResult(result),
		PendingTools: c.extractPendingToolsFromMetadata(result.Metadata),
	}
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

// AddEventListener subscribes to runtime events emitted during conversation execution.
func (c *Conversation) AddEventListener(listener events.Listener) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.eventBus == nil {
		c.eventBus = events.NewEventBus()
		if c.manager != nil && c.manager.eventBus == nil {
			c.manager.eventBus = c.eventBus
		}
	}
	c.eventBus.SubscribeAll(listener)
}

func (c *Conversation) getEmitter() *events.Emitter {
	if c.eventBus == nil {
		return nil
	}
	return events.NewEmitter(c.eventBus, c.id, c.userID, c.id)
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
	// 3. Template middleware - substitutes variables in system prompt
	pipelineMiddleware = append(pipelineMiddleware,
		middleware.PromptAssemblyMiddleware(c.registry, c.promptName, c.baseVariables),
		middleware.TemplateMiddleware(),
	)

	// 4. Context builder middleware - manages token budget and truncation (if policy configured)
	if c.contextPolicy != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.ContextBuilderMiddleware(c.contextPolicy))
	}

	// 5. Provider middleware - executes LLM
	providerConfig := &middleware.ProviderMiddlewareConfig{}
	if c.prompt.Parameters != nil {
		providerConfig.MaxTokens = c.prompt.Parameters.MaxTokens
		providerConfig.Temperature = float32(c.prompt.Parameters.Temperature)
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

	// 6. Media externalizer middleware - externalizes large media to storage (if enabled)
	if c.manager.config.EnableMediaExternalization && c.manager.mediaStorage != nil {
		// Extract RunID and SessionID from metadata if available
		var runID, sessionID string
		if c.state.Metadata != nil {
			if rid, ok := c.state.Metadata["run_id"].(string); ok {
				runID = rid
			}
			if sid, ok := c.state.Metadata["session_id"].(string); ok {
				sessionID = sid
			}
		}

		mediaConfig := &middleware.MediaExternalizerConfig{
			Enabled:         true,
			StorageService:  c.manager.mediaStorage,
			SizeThresholdKB: c.manager.config.MediaSizeThresholdKB,
			DefaultPolicy:   c.manager.config.MediaDefaultPolicy,
			RunID:           runID,
			SessionID:       sessionID,
			ConversationID:  c.id,
		}
		pipelineMiddleware = append(pipelineMiddleware, middleware.MediaExternalizerMiddleware(mediaConfig))
	}

	// 7. Dynamic validator middleware - validates response
	pipelineMiddleware = append(pipelineMiddleware, middleware.DynamicValidatorMiddleware(validators.DefaultRegistry))

	// 8. StateStore Save middleware - saves conversation state
	pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreSaveMiddleware(storeConfig))

	return pipelineMiddleware
}

// extractValidationsFromResult extracts validation results from the last assistant message
func (c *Conversation) extractValidationsFromResult(result *pipeline.ExecutionResult) []types.ValidationResult {
	if len(result.Messages) == 0 {
		return nil
	}

	lastMsg := result.Messages[len(result.Messages)-1]
	if lastMsg.Role == RoleAssistant {
		return lastMsg.Validations
	}

	return nil
}

// extractPendingToolsFromMetadata extracts pending tools from result metadata with type conversion
func (c *Conversation) extractPendingToolsFromMetadata(metadata map[string]interface{}) []tools.PendingToolInfo {
	if metadata == nil {
		return nil
	}

	pendingData, ok := metadata["pending_tools"]
	if !ok {
		return nil
	}

	return c.convertToPendingToolsSlice(pendingData)
}

// convertToPendingToolsSlice converts various types to []tools.PendingToolInfo
func (c *Conversation) convertToPendingToolsSlice(data interface{}) []tools.PendingToolInfo {
	switch v := data.(type) {
	case []tools.PendingToolInfo:
		return v
	case []*tools.PendingToolInfo:
		return c.convertPointerSliceToValueSlice(v)
	case []interface{}:
		return c.convertInterfaceSliceToTools(v)
	default:
		return nil
	}
}

// convertPointerSliceToValueSlice converts []*tools.PendingToolInfo to []tools.PendingToolInfo
func (c *Conversation) convertPointerSliceToValueSlice(pointers []*tools.PendingToolInfo) []tools.PendingToolInfo {
	var result []tools.PendingToolInfo
	for _, p := range pointers {
		if p != nil {
			result = append(result, *p)
		}
	}
	return result
}

// convertInterfaceSliceToTools converts []interface{} to []tools.PendingToolInfo
func (c *Conversation) convertInterfaceSliceToTools(items []interface{}) []tools.PendingToolInfo {
	var result []tools.PendingToolInfo
	for _, item := range items {
		if info, ok := item.(*tools.PendingToolInfo); ok {
			result = append(result, *info)
		} else if info, ok := item.(tools.PendingToolInfo); ok {
			result = append(result, info)
		}
	}
	return result
}

// findLastAssistantMessageWithToolCalls finds the most recent assistant message with tool calls
func (c *Conversation) findLastAssistantMessageWithToolCalls() (int, *types.Message) {
	for i := len(c.state.Messages) - 1; i >= 0; i-- {
		msg := &c.state.Messages[i]
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			return i, msg
		}
	}
	return -1, nil
}

// hasToolResult checks if a tool call has a corresponding result message
func (c *Conversation) hasToolResult(toolCallID string, startIndex int) bool {
	for j := startIndex + 1; j < len(c.state.Messages); j++ {
		msg := &c.state.Messages[j]
		if msg.Role == RoleTool &&
			msg.ToolResult != nil &&
			msg.ToolResult.ID == toolCallID {
			return true
		}
	}
	return false
}

// findToolCallInHistory finds a tool call by ID in conversation history
func (c *Conversation) findToolCallInHistory(toolCallID string) bool {
	for i := len(c.state.Messages) - 1; i >= 0; i-- {
		msg := c.state.Messages[i]
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			for _, toolCall := range msg.ToolCalls {
				if toolCall.ID == toolCallID {
					return true
				}
			}
		}
	}
	return false
}

func generateConversationID() string {
	// Simple timestamp-based ID for now
	// In production, use UUID or similar
	return fmt.Sprintf("conv_%d", time.Now().UnixNano())
}
