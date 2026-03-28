package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultMaxRounds            = 10
	defaultMaxParallelToolCalls = 10
	toolChoiceAuto              = "auto"
)

// ProviderStage implementation notes:
// - ✅ Multi-round tool execution with automatic tool result handling
// - ✅ Synchronous tool execution via toolRegistry.ExecuteAsync()
// - ⚠️ Limited async/pending tool support (no ExecutionContext for tracking)
// - TODO: Implement full async tool support with approval workflows

// ProviderStage executes LLM calls and handles tool execution.
// This is the request/response mode implementation.
type ProviderStage struct {
	BaseStage
	provider     providers.Provider
	toolRegistry *tools.Registry
	toolPolicy   *pipeline.ToolPolicy
	config       *ProviderConfig
	emitter      *events.Emitter // Optional event emitter for provider call events
	hookRegistry *hooks.Registry // Optional hook registry for policy enforcement
}

// ProviderConfig contains configuration for the provider stage.
type ProviderConfig struct {
	MaxTokens      int
	Temperature    float32
	Seed           *int
	ResponseFormat *providers.ResponseFormat // Optional response format (JSON mode)
	Labels         map[string]string         // Optional labels propagated to events, metrics, and traces
	Source         string                    // Origin of the call: "agent" (default), "judge", "selfplay"

	// MessageLog enables per-round write-through persistence during tool loops.
	// When set, messages are appended to the log after each tool-loop round
	// completes, so they survive process crashes. Best-effort: failures are
	// logged but don't abort the loop.
	MessageLog statestore.MessageLog

	// MessageLogConvID is the conversation ID for message log operations.
	MessageLogConvID string
}

// streamingRoundParams holds parameters for a streaming round execution.
type streamingRoundParams struct {
	messages      []types.Message
	systemPrompt  string
	providerTools interface{}
	toolChoice    string
	round         int
	metadata      map[string]interface{}
}

// NewProviderStage creates a new provider stage for request/response mode.
func NewProviderStage(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
) *ProviderStage {
	return NewProviderStageWithEmitter(provider, toolRegistry, toolPolicy, config, nil)
}

// NewProviderStageWithEmitter creates a new provider stage with event emission support.
// The emitter is used to emit provider.call.started, provider.call.completed, and
// provider.call.failed events for observability and session recording.
func NewProviderStageWithEmitter(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
	emitter *events.Emitter,
) *ProviderStage {
	return NewProviderStageWithHooks(provider, toolRegistry, toolPolicy, config, emitter, nil)
}

// NewProviderStageWithHooks creates a provider stage with event emission and hook support.
// The hookRegistry enables synchronous interception of provider calls, streaming chunks,
// and tool execution. Pass nil for no hooks (zero overhead).
func NewProviderStageWithHooks(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
	emitter *events.Emitter,
	hookRegistry *hooks.Registry,
) *ProviderStage {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &ProviderStage{
		BaseStage:    NewBaseStage("provider", StageTypeGenerate),
		provider:     provider,
		toolRegistry: toolRegistry,
		toolPolicy:   toolPolicy,
		config:       config,
		emitter:      emitter,
		hookRegistry: hookRegistry,
	}
}

// toolLabels returns the Labels from the ToolDescriptor for the given tool name,
// or nil if the tool is not found or has no labels.
func (s *ProviderStage) toolLabels(name string) map[string]string {
	if s.toolRegistry == nil {
		return nil
	}
	desc := s.toolRegistry.Get(name)
	if desc == nil {
		return nil
	}
	return desc.Labels
}

// providerInput holds accumulated input data for provider execution.
type providerInput struct {
	messages     []types.Message
	systemPrompt string
	allowedTools []string
	metadata     map[string]interface{}
}

// Process executes the LLM provider call and handles tool execution.
func (s *ProviderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.provider == nil {
		return errors.New("provider stage: no provider configured")
	}

	accumulated := s.accumulateInput(input)

	logger.Debug("ProviderStage accumulated input",
		"messages", len(accumulated.messages),
		"allowed_tools", accumulated.allowedTools,
		"mock_scenario_id", accumulated.metadata["mock_scenario_id"],
		"mock_turn_number", accumulated.metadata["mock_turn_number"])

	return s.executeAndEmit(ctx, accumulated, output)
}

// accumulateInput collects messages and metadata from input channel.
func (s *ProviderStage) accumulateInput(input <-chan StreamElement) *providerInput {
	acc := &providerInput{
		metadata: make(map[string]interface{}),
	}

	for elem := range input {
		if elem.Message != nil {
			acc.messages = append(acc.messages, *elem.Message)
		}
		s.extractMetadata(&elem, acc)
	}

	return acc
}

// extractMetadata extracts prompt data and merges metadata from element.
func (s *ProviderStage) extractMetadata(elem *StreamElement, acc *providerInput) {
	if elem.Metadata == nil {
		return
	}
	if sp, ok := elem.Metadata["system_prompt"].(string); ok {
		acc.systemPrompt = sp
	}
	if toolsList, ok := elem.Metadata["allowed_tools"].([]string); ok {
		acc.allowedTools = toolsList
		logger.Debug("ProviderStage received allowed_tools", "tools", toolsList, "count", len(toolsList))
	}
	for k, v := range elem.Metadata {
		acc.metadata[k] = v
	}
}

// executeAndEmit runs provider execution and emits results.
func (s *ProviderStage) executeAndEmit(
	ctx context.Context,
	acc *providerInput,
	output chan<- StreamElement,
) error {
	var responseMessages []types.Message
	var err error

	if s.provider.SupportsStreaming() {
		responseMessages, err = s.executeStreamingMultiRound(ctx, acc, output)
	} else {
		responseMessages, err = s.executeMultiRound(ctx, acc)
	}

	if err != nil {
		// If tools are pending, emit collected messages and propagate pending
		// info as metadata so the SDK can surface them to the caller.
		if ep, ok := tools.IsErrToolsPending(err); ok {
			if emitErr := s.emitResponseMessages(ctx, responseMessages, acc.metadata, output); emitErr != nil {
				return emitErr
			}
			// Send a marker element with pending tool info in metadata
			pendingElem := StreamElement{
				Metadata: map[string]interface{}{
					"pending_tools": ep.Pending,
				},
			}
			select {
			case output <- pendingElem:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
		output <- NewErrorElement(err)
		return err
	}

	return s.emitResponseMessages(ctx, responseMessages, acc.metadata, output)
}

// emitResponseMessages sends response messages to output channel.
func (s *ProviderStage) emitResponseMessages(
	ctx context.Context,
	messages []types.Message,
	metadata map[string]interface{},
	output chan<- StreamElement,
) error {
	for i := range messages {
		elem := NewMessageElement(&messages[i])
		elem.Metadata = metadata

		logger.Debug("ProviderStage emitting response message",
			"role", messages[i].Role,
			"has_validator_configs", metadata["validator_configs"] != nil)

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *ProviderStage) executeMultiRound(
	ctx context.Context,
	acc *providerInput,
) ([]types.Message, error) {
	loop, err := s.newToolLoop(acc)
	if err != nil {
		return nil, err
	}
	for round := 1; round <= loop.maxRounds; round++ {
		response, hasToolCalls, err := s.executeRound(
			ctx, loop.messages, acc.systemPrompt, loop.providerTools, loop.toolChoice, round, acc.metadata)
		if err != nil {
			return loop.messages, err
		}
		if done, msgs, err := loop.afterRound(ctx, acc.allowedTools, &response, hasToolCalls, round); done {
			return msgs, err
		}
	}
	return loop.messages, nil
}

// getMaxRounds returns the maximum number of tool call rounds.
func (s *ProviderStage) getMaxRounds() int {
	if s.toolPolicy != nil && s.toolPolicy.MaxRounds > 0 {
		return s.toolPolicy.MaxRounds
	}
	return defaultMaxRounds
}

func (s *ProviderStage) executeStreamingMultiRound(
	ctx context.Context,
	acc *providerInput,
	output chan<- StreamElement,
) ([]types.Message, error) {
	loop, err := s.newToolLoop(acc)
	if err != nil {
		return nil, err
	}
	for round := 1; round <= loop.maxRounds; round++ {
		params := &streamingRoundParams{
			messages:      loop.messages,
			systemPrompt:  acc.systemPrompt,
			providerTools: loop.providerTools,
			toolChoice:    loop.toolChoice,
			round:         round,
			metadata:      acc.metadata,
		}
		response, hasToolCalls, err := s.executeStreamingRound(ctx, params, output)
		if err != nil {
			return loop.messages, err
		}
		if done, msgs, err := loop.afterRound(ctx, acc.allowedTools, &response, hasToolCalls, round); done {
			return msgs, err
		}
	}
	return loop.messages, nil
}

// toolLoop holds shared state for multi-round tool execution.
type toolLoop struct {
	stage            *ProviderStage
	messages         []types.Message
	providerTools    interface{}
	toolChoice       string
	maxRounds        int
	excluded         map[string]bool
	rejectionCounts  map[string]int
	lastPersistedSeq int // messages persisted so far via MessageLog
}

func (s *ProviderStage) newToolLoop(acc *providerInput) (*toolLoop, error) {
	excluded := map[string]bool{}
	providerTools, toolChoice, err := s.buildProviderTools(acc.allowedTools, excluded)
	if err != nil {
		return nil, fmt.Errorf("provider stage: %w", err)
	}
	return &toolLoop{
		stage:            s,
		messages:         acc.messages,
		providerTools:    providerTools,
		toolChoice:       toolChoice,
		maxRounds:        s.getMaxRounds(),
		excluded:         excluded,
		rejectionCounts:  map[string]int{},
		lastPersistedSeq: len(acc.messages), // history already in store
	}, nil
}

// afterRound handles tool execution, rejection tracking, and loop control after
// a provider round completes. Returns (done, messages, error). When done is true,
// the caller should return immediately with the provided messages and error.
func (tl *toolLoop) afterRound(
	ctx context.Context,
	allowedTools []string,
	response *types.Message,
	hasToolCalls bool,
	round int,
) (bool, []types.Message, error) {
	tl.messages = append(tl.messages, *response)

	if !hasToolCalls {
		tl.persistMessages(ctx, round)
		return true, tl.messages, nil
	}

	toolResults, err := tl.stage.executeToolCalls(ctx, response.ToolCalls)
	if err != nil {
		if _, ok := tools.IsErrToolsPending(err); ok {
			tl.messages = append(tl.messages, toolResults...)
			tl.persistMessages(ctx, round)
			return true, tl.messages, err
		}
		return true, tl.messages, fmt.Errorf("provider stage: tool execution failed: %w", err)
	}

	tl.messages = append(tl.messages, toolResults...)
	ResetIdleFromContext(ctx)
	tl.persistMessages(ctx, round)

	if tl.stage.updateExcludedTools(toolResults, tl.rejectionCounts, tl.excluded) {
		rebuilt, _, rebuildErr := tl.stage.buildProviderTools(allowedTools, tl.excluded)
		if rebuildErr != nil {
			return true, tl.messages, fmt.Errorf("provider stage: rebuild tools: %w", rebuildErr)
		}
		tl.providerTools = rebuilt
	}

	tl.toolChoice = toolChoiceAuto

	if round == tl.maxRounds {
		return true, tl.messages, fmt.Errorf("provider stage: max rounds (%d) exceeded", tl.maxRounds)
	}

	return false, nil, nil
}

// persistMessages writes new messages to the MessageLog if configured.
// Best-effort: failures are logged but don't affect the tool loop.
func (tl *toolLoop) persistMessages(ctx context.Context, round int) {
	cfg := tl.stage.config
	if cfg == nil || cfg.MessageLog == nil {
		return
	}
	newMsgs := tl.messages[tl.lastPersistedSeq:]
	if len(newMsgs) == 0 {
		return
	}
	newTotal, err := cfg.MessageLog.LogAppend(ctx, cfg.MessageLogConvID, tl.lastPersistedSeq, newMsgs)
	if err != nil {
		logger.Warn("message log append failed", "round", round, "error", err)
		return
	}
	tl.lastPersistedSeq = newTotal
}

func (s *ProviderStage) executeRound(
	ctx context.Context,
	messages []types.Message,
	systemPrompt string,
	providerTools interface{},
	toolChoice string,
	round int,
	metadata map[string]interface{},
) (types.Message, bool, error) {
	ResetIdleFromContext(ctx)

	// Build provider request
	req := providers.PredictionRequest{
		System:         systemPrompt,
		Messages:       messages,
		MaxTokens:      s.config.MaxTokens,
		Temperature:    s.config.Temperature,
		Seed:           s.config.Seed,
		ResponseFormat: s.config.ResponseFormat,
		Metadata:       metadata,
	}

	// Count tools for event emission
	toolCount := 0
	if providerTools != nil {
		if toolDescs, ok := providerTools.([]*providers.ToolDescriptor); ok {
			toolCount = len(toolDescs)
		}
	}

	logger.Debug("Provider round starting",
		"round", round,
		"messages", len(messages),
		"tools", providerTools != nil)

	// Emit provider call started event
	if s.emitter != nil {
		s.emitter.ProviderCallStarted(s.provider.ID(), s.provider.Model(), len(messages), toolCount, s.config.Labels)
	}

	// Run BeforeCall hooks
	if s.hookRegistry != nil {
		hookReq := &hooks.ProviderRequest{
			ProviderID:   s.provider.ID(),
			Model:        s.provider.Model(),
			Messages:     messages,
			SystemPrompt: systemPrompt,
			Round:        round,
			Metadata:     metadata,
		}
		if d := s.hookRegistry.RunBeforeProviderCall(ctx, hookReq); !d.Allow {
			return types.Message{}, false, &hooks.HookDeniedError{
				HookName: "provider_hook",
				HookType: "provider_before",
				Reason:   d.Reason,
				Metadata: d.Metadata,
			}
		}
	}

	// Call provider (with or without tools)
	startTime := time.Now()
	var resp providers.PredictionResponse
	var toolCalls []types.MessageToolCall
	var err error

	if providerTools != nil {
		// Use tool-aware provider interface
		toolProvider, ok := s.provider.(providers.ToolSupport)
		if !ok {
			return types.Message{}, false, errors.New("provider does not support tools")
		}
		resp, toolCalls, err = toolProvider.PredictWithTools(ctx, req, providerTools, toolChoice)
	} else {
		// Regular prediction
		resp, err = s.provider.Predict(ctx, req)
		toolCalls = resp.ToolCalls
	}

	duration := time.Since(startTime)

	if err != nil {
		logger.Error("Provider call failed", "error", err, "duration", duration)
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: s.provider.ID(),
				Model:    s.provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   s.config.Source,
				Labels:   s.config.Labels,
			})
		}
		return types.Message{}, false, fmt.Errorf("provider call failed: %w", err)
	}

	// Emit provider call completed event
	if s.emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider:      s.provider.ID(),
			Model:         s.provider.Model(),
			Duration:      duration,
			ToolCallCount: len(toolCalls),
			Source:        s.config.Source,
			Labels:        s.config.Labels,
		}
		if resp.CostInfo != nil {
			completedData.InputTokens = resp.CostInfo.InputTokens
			completedData.OutputTokens = resp.CostInfo.OutputTokens
			completedData.CachedTokens = resp.CostInfo.CachedTokens
			completedData.Cost = resp.CostInfo.TotalCost
		}
		s.emitter.ProviderCallCompletedCtx(ctx, completedData)
	}

	// Build response message with latency and cost info
	responseMsg := types.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Parts:     resp.Parts,
		ToolCalls: toolCalls,
		Timestamp: timeNow(),
		LatencyMs: duration.Milliseconds(),
		CostInfo:  resp.CostInfo,
	}

	// Run AfterCall hooks
	if s.hookRegistry != nil {
		hookReq := &hooks.ProviderRequest{
			ProviderID:   s.provider.ID(),
			Model:        s.provider.Model(),
			Messages:     messages,
			SystemPrompt: systemPrompt,
			Round:        round,
			Metadata:     metadata,
		}
		hookResp := &hooks.ProviderResponse{
			ProviderID: s.provider.ID(),
			Model:      s.provider.Model(),
			Message:    responseMsg,
			Round:      round,
			LatencyMs:  duration.Milliseconds(),
		}
		hookStart := time.Now()
		if d := s.hookRegistry.RunAfterProviderCall(ctx, hookReq, hookResp); !d.Allow {
			s.emitGuardrailEvent(d, time.Since(hookStart))
			// Populate msg.Validations for guardrail_triggered compat
			if vType, ok := d.Metadata["validator_type"].(string); ok {
				responseMsg.Validations = append(responseMsg.Validations, types.ValidationResult{
					ValidatorType: vType,
					Passed:        false,
					Details:       d.Metadata,
				})
			}
			if d.Enforced {
				// Hook already enforced (truncated/replaced content) — pick up
				// the modified message and continue the pipeline.
				responseMsg.Content = hookResp.Message.Content
				responseMsg.Validations = append(responseMsg.Validations, hookResp.Message.Validations...)
			} else {
				return responseMsg, false, &hooks.HookDeniedError{
					HookName: "provider_hook",
					HookType: "provider_after",
					Reason:   d.Reason,
					Metadata: d.Metadata,
				}
			}
		}
	}

	logger.Debug("Provider round completed",
		"round", round,
		"duration", duration,
		"latencyMs", responseMsg.LatencyMs,
		"tool_calls", len(toolCalls))

	// Check for tool calls
	hasToolCalls := len(toolCalls) > 0

	return responseMsg, hasToolCalls, nil
}

func (s *ProviderStage) executeStreamingRound(
	ctx context.Context,
	params *streamingRoundParams,
	output chan<- StreamElement,
) (types.Message, bool, error) {
	ResetIdleFromContext(ctx)

	// Build provider request
	req := providers.PredictionRequest{
		System:         params.systemPrompt,
		Messages:       params.messages,
		MaxTokens:      s.config.MaxTokens,
		Temperature:    s.config.Temperature,
		Seed:           s.config.Seed,
		Metadata:       params.metadata,
		ResponseFormat: s.config.ResponseFormat,
	}

	// Count tools for event emission
	toolCount := 0
	if params.providerTools != nil {
		if toolDescs, ok := params.providerTools.([]*providers.ToolDescriptor); ok {
			toolCount = len(toolDescs)
		}
	}

	logger.Debug("Provider streaming round starting",
		"round", params.round,
		"messages", len(params.messages),
		"tools", params.providerTools != nil)

	// Run BeforeCall hooks
	if s.hookRegistry != nil {
		hookReq := &hooks.ProviderRequest{
			ProviderID:   s.provider.ID(),
			Model:        s.provider.Model(),
			Messages:     params.messages,
			SystemPrompt: params.systemPrompt,
			Round:        params.round,
			Metadata:     params.metadata,
		}
		if d := s.hookRegistry.RunBeforeProviderCall(ctx, hookReq); !d.Allow {
			return types.Message{}, false, &hooks.HookDeniedError{
				HookName: "provider_hook",
				HookType: "provider_before",
				Reason:   d.Reason,
				Metadata: d.Metadata,
			}
		}
	}

	// Emit provider call started event
	if s.emitter != nil {
		s.emitter.ProviderCallStarted(s.provider.ID(), s.provider.Model(), len(params.messages), toolCount, s.config.Labels)
	}

	startTime := time.Now()

	// Start the streaming request
	streamChan, err := s.startStreamingRequest(ctx, req, params.providerTools, params.toolChoice)
	if err != nil {
		duration := time.Since(startTime)
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: s.provider.ID(),
				Model:    s.provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   s.config.Source,
				Labels:   s.config.Labels,
			})
		}
		return types.Message{}, false, err
	}

	// Process all chunks and collect response
	content, toolCalls, costInfo, err := s.processStreamChunks(ctx, streamChan, params.metadata, output)
	duration := time.Since(startTime)

	if err != nil {
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: s.provider.ID(),
				Model:    s.provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   s.config.Source,
				Labels:   s.config.Labels,
			})
		}
		return types.Message{}, false, err
	}

	// Emit provider call completed event with cost info from streaming response
	if s.emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider:      s.provider.ID(),
			Model:         s.provider.Model(),
			Duration:      duration,
			ToolCallCount: len(toolCalls),
			Source:        s.config.Source,
			Labels:        s.config.Labels,
		}
		// Populate token counts from cost info if available (present in final chunk)
		if costInfo != nil {
			completedData.InputTokens = costInfo.InputTokens
			completedData.OutputTokens = costInfo.OutputTokens
			completedData.CachedTokens = costInfo.CachedTokens
			completedData.Cost = costInfo.TotalCost
		}
		s.emitter.ProviderCallCompletedCtx(ctx, completedData)
	}

	// Build final response message with latency and cost info
	responseMsg := types.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
		Timestamp: timeNow(),
		LatencyMs: duration.Milliseconds(),
		CostInfo:  costInfo,
	}

	// Run AfterCall hooks
	if s.hookRegistry != nil {
		hookReq := &hooks.ProviderRequest{
			ProviderID:   s.provider.ID(),
			Model:        s.provider.Model(),
			Messages:     params.messages,
			SystemPrompt: params.systemPrompt,
			Round:        params.round,
			Metadata:     params.metadata,
		}
		hookResp := &hooks.ProviderResponse{
			ProviderID: s.provider.ID(),
			Model:      s.provider.Model(),
			Message:    responseMsg,
			Round:      params.round,
			LatencyMs:  duration.Milliseconds(),
		}
		hookStart := time.Now()
		if d := s.hookRegistry.RunAfterProviderCall(ctx, hookReq, hookResp); !d.Allow {
			s.emitGuardrailEvent(d, time.Since(hookStart))
			if vType, ok := d.Metadata["validator_type"].(string); ok {
				responseMsg.Validations = append(
					responseMsg.Validations,
					types.ValidationResult{
						ValidatorType: vType,
						Passed:        false,
						Details:       d.Metadata,
					},
				)
			}
			if d.Enforced {
				// Hook already enforced — pick up modified content and continue.
				responseMsg.Content = hookResp.Message.Content
				responseMsg.Validations = append(responseMsg.Validations, hookResp.Message.Validations...)
			} else {
				return responseMsg, false, &hooks.HookDeniedError{
					HookName: "provider_hook",
					HookType: "provider_after",
					Reason:   d.Reason,
					Metadata: d.Metadata,
				}
			}
		}
	}

	logger.Debug("Provider streaming round completed",
		"round", params.round,
		"duration", duration,
		"latencyMs", responseMsg.LatencyMs,
		"tool_calls", len(toolCalls))

	return responseMsg, len(toolCalls) > 0, nil
}

// startStreamingRequest initiates a streaming request with or without tools.
func (s *ProviderStage) startStreamingRequest(
	ctx context.Context,
	req providers.PredictionRequest,
	providerTools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	if providerTools != nil {
		toolProvider, ok := s.provider.(providers.ToolSupport)
		if !ok {
			return nil, errors.New("provider does not support tools")
		}
		streamChan, err := toolProvider.PredictStreamWithTools(ctx, req, providerTools, toolChoice)
		if err != nil {
			logger.Error("Provider stream failed", "error", err)
			return nil, fmt.Errorf("provider stream failed: %w", err)
		}
		return streamChan, nil
	}

	streamChan, err := s.provider.PredictStream(ctx, req)
	if err != nil {
		logger.Error("Provider stream failed", "error", err)
		return nil, fmt.Errorf("provider stream failed: %w", err)
	}
	return streamChan, nil
}

// processStreamChunks processes streaming chunks and emits elements to output.
// Returns accumulated content, tool calls, cost info (from final chunk), and any error.
func (s *ProviderStage) processStreamChunks(
	ctx context.Context,
	streamChan <-chan providers.StreamChunk,
	metadata map[string]interface{},
	output chan<- StreamElement,
) (string, []types.MessageToolCall, *types.CostInfo, error) {
	var content string
	var toolCalls []types.MessageToolCall
	var costInfo *types.CostInfo

	for chunk := range streamChan {
		ResetIdleFromContext(ctx)

		if chunk.Error != nil {
			logger.Error("Stream chunk error", "error", chunk.Error)
			return "", nil, nil, fmt.Errorf("stream chunk error: %w", chunk.Error)
		}

		content = chunk.Content
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		// Capture cost info from final chunk (only present when FinishReason != nil)
		if chunk.CostInfo != nil {
			costInfo = chunk.CostInfo
		}

		if err := s.emitChunkElement(ctx, &chunk, metadata, output); err != nil {
			return "", nil, nil, err
		}

		// Run chunk interceptor hooks
		if s.hookRegistry != nil && s.hookRegistry.HasChunkInterceptors() {
			if d := s.hookRegistry.RunOnChunk(ctx, &chunk); !d.Allow {
				s.emitGuardrailEvent(d, 0)
				if d.Enforced {
					// Hook enforced (e.g., truncated content) — use the
					// modified chunk content and stop reading the stream.
					content = chunk.Content
					break
				}
				return "", nil, nil, &providers.ValidationAbortError{
					Reason: d.Reason,
					Chunk:  chunk,
				}
			}
		}
	}

	return content, toolCalls, costInfo, nil
}

// emitChunkElement creates and emits a streaming element for a chunk.
func (s *ProviderStage) emitChunkElement(
	ctx context.Context,
	chunk *providers.StreamChunk,
	metadata map[string]interface{},
	output chan<- StreamElement,
) error {
	if chunk.Delta == "" {
		return nil
	}

	elem := NewTextElement(chunk.Delta)
	elem.Timestamp = timeNow()
	elem.Priority = PriorityNormal

	for k, v := range metadata {
		elem.Metadata[k] = v
	}

	elem.Metadata["token_count"] = chunk.TokenCount
	if chunk.FinishReason != nil {
		elem.Metadata["finish_reason"] = *chunk.FinishReason
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// toolCallResult holds the outcome of a single tool call execution,
// including its original index for preserving result ordering.
type toolCallResult struct {
	index   int
	message types.Message
	pending *tools.PendingToolExecution // non-nil if tool returned pending status
}

// getMaxParallelToolCalls returns the max concurrency for parallel tool execution.
func (s *ProviderStage) getMaxParallelToolCalls() int {
	if s.toolPolicy != nil && s.toolPolicy.MaxParallelToolCalls > 0 {
		return s.toolPolicy.MaxParallelToolCalls
	}
	return defaultMaxParallelToolCalls
}

func (s *ProviderStage) executeToolCalls(
	ctx context.Context, toolCalls []types.MessageToolCall,
) ([]types.Message, error) {
	if s.toolRegistry == nil {
		return nil, errors.New("tool registry not configured but tool calls present")
	}

	resultSlots := make([]toolCallResult, len(toolCalls))
	var mu sync.Mutex
	var pendingTools []tools.PendingToolExecution

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.getMaxParallelToolCalls())

	for i, tc := range toolCalls {
		idx := i
		toolCall := tc

		g.Go(func() error {
			result := s.executeSingleToolCall(gctx, toolCall)
			mu.Lock()
			defer mu.Unlock()
			result.index = idx
			resultSlots[idx] = result
			if result.pending != nil {
				pendingTools = append(pendingTools, *result.pending)
			}
			return nil
		})
	}

	// errgroup goroutines always return nil, so this only errors on ctx cancel.
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Collect results in original order, excluding pending entries.
	results := make([]types.Message, 0, len(toolCalls))
	for i := range resultSlots {
		if resultSlots[i].pending != nil {
			continue
		}
		results = append(results, resultSlots[i].message)
	}

	if len(pendingTools) > 0 {
		return results, &tools.ErrToolsPending{Pending: pendingTools}
	}

	return results, nil
}

// preExecCheck runs policy and hook checks before tool execution.
// Returns (hookDecision, blocked result, shouldSkip).
// When shouldSkip is true the caller should return the blocked result directly.
func (s *ProviderStage) preExecCheck(
	ctx context.Context, toolCall types.MessageToolCall,
) (hooks.Decision, toolCallResult, bool) {
	if s.toolPolicy != nil && isToolBlocked(toolCall.Name, s.toolPolicy.Blocklist) {
		errMsg := fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name)
		result := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
		result.Error = errMsg
		return hooks.Decision{}, toolCallResult{
			message: types.NewToolResultMessage(result),
		}, true
	}

	var hookDecision hooks.Decision
	if s.hookRegistry != nil {
		toolReq := hooks.ToolRequest{
			Name: toolCall.Name, Args: toolCall.Args, CallID: toolCall.ID,
		}
		hookDecision = s.hookRegistry.RunBeforeToolExecution(ctx, toolReq)
		if !hookDecision.Allow {
			errMsg := fmt.Sprintf(
				"Tool %s blocked by hook: %s", toolCall.Name, hookDecision.Reason,
			)
			hookResult := types.NewTextToolResult(toolCall.ID, toolCall.Name, errMsg)
			hookResult.Error = errMsg
			msg := types.NewToolResultMessage(hookResult)
			if hookDecision.Metadata != nil {
				msg.Meta = hookDecision.Metadata
			}
			return hookDecision, toolCallResult{message: msg}, true
		}
	}
	return hookDecision, toolCallResult{}, false
}

// executeSingleToolCall handles policy checks, hooks, execution, and event
// emission for a single tool call. It never returns an error — failures are
// captured as error results in the returned message, matching the previous
// sequential behavior where one tool failure does not cancel others.
func (s *ProviderStage) executeSingleToolCall(
	ctx context.Context,
	toolCall types.MessageToolCall,
) toolCallResult {
	hookDecision, blocked, skip := s.preExecCheck(ctx, toolCall)
	if skip {
		return blocked
	}

	labels := s.toolLabels(toolCall.Name)
	s.emitToolStarted(toolCall, labels)

	startTime := time.Now()
	ctx = tools.WithCallID(ctx, toolCall.ID)
	asyncResult, err := s.toolRegistry.ExecuteAsync(ctx, toolCall.Name, toolCall.Args)
	ResetIdleFromContext(ctx)
	if err != nil {
		if s.emitter != nil {
			s.emitter.ToolCallFailedCtx(
				ctx, toolCall.Name, toolCall.ID, err, time.Since(startTime), labels,
			)
		}
		errResult := types.NewTextToolResult(toolCall.ID, toolCall.Name, fmt.Sprintf("Error: %v", err))
		errResult.Error = err.Error()
		return toolCallResult{
			message: types.NewToolResultMessage(errResult),
		}
	}

	if asyncResult.Status == tools.ToolStatusPending {
		return s.buildPendingResult(ctx, toolCall, asyncResult)
	}

	result := s.handleToolResult(toolCall, asyncResult)
	if s.emitter != nil {
		status := string(asyncResult.Status)
		s.emitter.ToolCallCompletedCtx(
			ctx, toolCall.Name, toolCall.ID, time.Since(startTime), status, result.Parts, labels,
		)
	}
	resultMsg := types.NewToolResultMessage(result)
	if hookDecision.Metadata != nil {
		resultMsg.Meta = hookDecision.Metadata
	}

	s.runAfterToolHooks(ctx, toolCall, result, startTime)

	return toolCallResult{message: resultMsg}
}

// emitToolStarted emits the tool call started event if an emitter is configured.
func (s *ProviderStage) emitToolStarted(toolCall types.MessageToolCall, labels map[string]string) {
	if s.emitter == nil {
		return
	}
	var argsMap map[string]interface{}
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}
	s.emitter.ToolCallStarted(toolCall.Name, toolCall.ID, argsMap, labels)
}

// emitGuardrailEvent emits a validation event from a hook decision.
func (s *ProviderStage) emitGuardrailEvent(d hooks.Decision, duration time.Duration) {
	if s.emitter == nil {
		return
	}
	vType, _ := d.Metadata["validator_type"].(string)
	score, _ := d.Metadata["score"].(float64)
	monitorOnly, _ := d.Metadata["monitor_only"].(bool)
	data := &events.ValidationEventData{
		ValidatorName: vType,
		ValidatorType: vType,
		Duration:      duration,
		Enforced:      d.Enforced && !monitorOnly,
		MonitorOnly:   monitorOnly,
		Score:         score,
	}
	if !d.Allow {
		data.Violations = []string{d.Reason}
	}
	s.emitter.GuardrailResult(data)
}

// buildPendingResult creates a toolCallResult for a pending tool execution.
// It emits a tool.client.request event so observers know a client tool is
// awaiting fulfillment, and a tool.call.completed with status "pending" so
// every tool.call.started has a matching completion.
func (s *ProviderStage) buildPendingResult(
	ctx context.Context, toolCall types.MessageToolCall, asyncResult *tools.ToolExecutionResult,
) toolCallResult {
	var argsMap map[string]any
	if toolCall.Args != nil {
		_ = json.Unmarshal(toolCall.Args, &argsMap)
	}

	// Emit client tool request event with consent/category metadata
	if s.emitter != nil {
		reqData := &events.ClientToolRequestData{
			CallID:   toolCall.ID,
			ToolName: toolCall.Name,
			Args:     argsMap,
		}
		if asyncResult.PendingInfo != nil {
			reqData.ConsentMsg = asyncResult.PendingInfo.Message
			if cats, ok := asyncResult.PendingInfo.Metadata["categories"].([]string); ok {
				reqData.Categories = cats
			}
		}
		s.emitter.ClientToolRequest(reqData)
	}

	// Emit tool.call.completed with status "pending" so the started event is paired
	if s.emitter != nil {
		labels := s.toolLabels(toolCall.Name)
		s.emitter.ToolCallCompletedCtx(ctx, toolCall.Name, toolCall.ID, 0, "pending", nil, labels)
	}

	toolResult := s.handleToolResult(toolCall, asyncResult)
	return toolCallResult{
		pending: &tools.PendingToolExecution{
			CallID:      toolCall.ID,
			ToolName:    toolCall.Name,
			Args:        argsMap,
			PendingInfo: asyncResult.PendingInfo,
			ToolResult:  toolResult,
		},
	}
}

// runAfterToolHooks runs AfterExecution hooks if a hook registry is configured.
func (s *ProviderStage) runAfterToolHooks(
	ctx context.Context,
	toolCall types.MessageToolCall,
	result types.MessageToolResult,
	startTime time.Time,
) {
	if s.hookRegistry == nil {
		return
	}
	toolReq := hooks.ToolRequest{
		Name: toolCall.Name, Args: toolCall.Args, CallID: toolCall.ID,
	}
	toolResp := hooks.ToolResponse{
		Name:      toolCall.Name,
		CallID:    toolCall.ID,
		Content:   result.GetTextContent(),
		Error:     result.Error,
		LatencyMs: time.Since(startTime).Milliseconds(),
	}
	s.hookRegistry.RunAfterToolExecution(ctx, toolReq, toolResp)
}

// handleToolResult converts tool execution result to MessageToolResult
func (s *ProviderStage) handleToolResult(
	call types.MessageToolCall,
	asyncResult *tools.ToolExecutionResult,
) types.MessageToolResult {
	switch asyncResult.Status {
	case tools.ToolStatusPending:
		// Tool requires approval - for stages we don't have ExecutionContext for tracking pending tools
		// Return a message indicating approval is needed
		pendingMsg := asyncResult.PendingInfo.Message
		if pendingMsg == "" {
			pendingMsg = fmt.Sprintf("Tool %s requires approval", call.Name)
		}
		logger.Warn("Tool requires approval in ProviderStage - pending tool support not yet implemented",
			"tool", call.Name, "call_id", call.ID)
		return types.NewTextToolResult(
			call.ID, call.Name,
			pendingMsg+" (Note: Async tool approval workflows not yet implemented in stages)",
		)

	case tools.ToolStatusFailed:
		failResult := types.NewTextToolResult(
			call.ID, call.Name,
			fmt.Sprintf("Tool execution failed: %s", asyncResult.Error),
		)
		failResult.Error = asyncResult.Error
		return failResult

	case tools.ToolStatusComplete:
		// Tool completed successfully
		content := string(asyncResult.Content)

		// Try to format nicely if it's JSON
		var resultValue interface{}
		if json.Unmarshal(asyncResult.Content, &resultValue) == nil {
			content = formatToolResult(resultValue)
		}

		// Enforce tool result size limit
		content = s.enforceResultSizeLimit(call.Name, content)

		// If the executor returned multimodal parts, propagate them directly;
		// otherwise wrap the text content as a single text ContentPart (legacy path).
		if len(asyncResult.Parts) > 0 {
			return types.MessageToolResult{
				ID:    call.ID,
				Name:  call.Name,
				Parts: asyncResult.Parts,
			}
		}

		return types.NewTextToolResult(call.ID, call.Name, content)

	default:
		unknownMsg := fmt.Sprintf("Unknown tool status: %v", asyncResult.Status)
		unknownResult := types.NewTextToolResult(call.ID, call.Name, unknownMsg)
		unknownResult.Error = unknownMsg
		return unknownResult
	}
}

// enforceResultSizeLimit truncates the tool result content if it exceeds the
// configured maximum size from the tool registry.
func (s *ProviderStage) enforceResultSizeLimit(toolName, content string) string {
	if s.toolRegistry == nil {
		return content
	}
	maxSize := s.toolRegistry.MaxToolResultSize()
	if maxSize <= 0 {
		return content
	}
	size := len(content)
	if size <= maxSize {
		return content
	}
	logger.Warn("Tool result truncated",
		"tool", toolName,
		"size", size,
		"limit", maxSize,
	)
	truncated := content[:maxSize]
	return fmt.Sprintf(
		"%s\n... [truncated, %d bytes exceeded limit of %d bytes]",
		truncated, size, maxSize,
	)
}

// isToolBlocked checks if a tool is in the blocklist
func isToolBlocked(toolName string, blocklist []string) bool {
	for _, blocked := range blocklist {
		if blocked == toolName {
			return true
		}
	}
	return false
}

// formatToolResult formats tool result for display
func formatToolResult(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]interface{}:
		// Pretty print JSON objects
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	case []interface{}:
		// Pretty print JSON arrays
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// updateExcludedTools increments rejection counts for tools whose results have
// errors and marks them for exclusion after the second rejection. Returns true
// if the excluded set changed (caller should rebuild provider tools).
func (s *ProviderStage) updateExcludedTools(
	results []types.Message,
	rejectionCounts map[string]int,
	excluded map[string]bool,
) bool {
	changed := false
	for i := range results {
		tr := results[i].ToolResult
		if tr == nil || tr.Error == "" {
			continue
		}
		rejectionCounts[tr.Name]++
		if rejectionCounts[tr.Name] > 1 && !excluded[tr.Name] {
			excluded[tr.Name] = true
			changed = true
			logger.Warn("Tool excluded after repeated rejection",
				"tool", tr.Name, "rejections", rejectionCounts[tr.Name])
		}
	}
	return changed
}

// buildProviderTools constructs the tool descriptors sent to the provider.
// Tools in the excluded set are omitted from the result.
func (s *ProviderStage) buildProviderTools(
	allowedTools []string, excluded map[string]bool,
) (providerTools interface{}, toolChoice string, err error) {
	if s.toolRegistry == nil {
		return nil, "", nil
	}

	// Check if provider supports tools
	toolProvider, ok := s.provider.(providers.ToolSupport)
	if !ok {
		return nil, "", nil
	}

	// Build tool descriptors: pack-declared tools (allowedTools) + capability tools (system-namespaced)
	seen := make(map[string]bool)
	var descriptors []*providers.ToolDescriptor

	// 1. Add pack-declared tools from the prompt's allowed list
	for _, toolName := range allowedTools {
		if excluded[toolName] {
			continue
		}
		tool, err := s.toolRegistry.GetTool(toolName)
		if err != nil {
			logger.Warn("Tool not found in registry", "tool", toolName, "error", err)
			continue
		}
		seen[tool.Name] = true
		descriptors = append(descriptors, &providers.ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}

	// 2. Add capability tools (system-namespaced: skill__, a2a__, workflow__, mcp__, memory__)
	//    These are registered by capabilities and are always available to the LLM.
	//    Uses IterateTools to avoid a full map copy from GetTools.
	s.toolRegistry.IterateTools(func(name string, tool *tools.ToolDescriptor) {
		if seen[name] || excluded[name] || !tools.IsSystemTool(name) {
			return
		}
		descriptors = append(descriptors, &providers.ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	})

	if len(descriptors) == 0 {
		return nil, "", nil
	}

	// Build provider-specific tools
	providerTools, err = toolProvider.BuildTooling(descriptors)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build tools: %w", err)
	}

	// Determine tool choice from policy
	toolChoice = toolChoiceAuto // default
	if s.toolPolicy != nil && s.toolPolicy.ToolChoice != "" {
		toolChoice = s.toolPolicy.ToolChoice
	}

	return providerTools, toolChoice, nil
}
