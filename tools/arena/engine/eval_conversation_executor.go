package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// EvalConversationExecutor handles evaluation mode: replaying saved conversations with assertions.
// Unlike scenario execution, eval mode:
// - Loads turns from recordings (no prompt building)
// - Applies assertions to pre-recorded assistant messages
// - Skips tool execution (tool calls are metadata only)
// - Returns results in the same schema as scenario execution for output parity
type EvalConversationExecutor struct {
	adapterRegistry   *adapters.Registry
	assertionRegistry *runtimeValidators.Registry
	convAssertionReg  *assertions.ConversationAssertionRegistry
	promptRegistry    *prompt.Registry
	providerRegistry  *providers.Registry
	packEvalHook      *PackEvalHook
}

// NewEvalConversationExecutor creates a new eval conversation executor.
func NewEvalConversationExecutor(
	adapterRegistry *adapters.Registry,
	assertionRegistry *runtimeValidators.Registry,
	convAssertionReg *assertions.ConversationAssertionRegistry,
	promptRegistry *prompt.Registry,
	providerRegistry *providers.Registry,
	packEvalHook *PackEvalHook,
) *EvalConversationExecutor {
	return &EvalConversationExecutor{
		adapterRegistry:   adapterRegistry,
		assertionRegistry: assertionRegistry,
		convAssertionReg:  convAssertionReg,
		promptRegistry:    promptRegistry,
		providerRegistry:  providerRegistry,
		packEvalHook:      packEvalHook,
	}
}

// ExecuteConversation runs an evaluation on a saved conversation.
func (e *EvalConversationExecutor) ExecuteConversation(
	ctx context.Context,
	req ConversationRequest, //nolint:gocritic // Interface compliance requires value receiver
) *ConversationResult {
	if err := e.validateEvalConfig(req.Eval); err != nil {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("invalid eval configuration: %v", err),
		}
	}

	ctx = e.enrichLoggingContext(ctx, &req)
	messages, metadata, err := e.loadRecording(&req)
	if err != nil {
		return &ConversationResult{Failed: true, Error: err.Error()}
	}

	convCtx := e.buildConversationContext(&req, messages, metadata)
	e.applyAllTurnAssertions(req.Eval.Turns, messages, convCtx)
	convResults := e.evaluateConversationAssertions(ctx, req.Eval.ConversationAssertions, convCtx)

	// Run pack eval session-level evals if configured
	if e.packEvalHook != nil && e.packEvalHook.HasEvals() {
		packResults := e.packEvalHook.RunSessionEvals(ctx, messages, req.ConversationID)
		convResults = append(convResults, packResults...)
	}

	return &ConversationResult{
		Messages:                     messages,
		Cost:                         e.calculateCost(messages),
		ConversationAssertionResults: convResults,
		Failed:                       e.hasFailedAssertions(messages, convResults),
	}
}

// enrichLoggingContext adds eval metadata to the logging context.
func (e *EvalConversationExecutor) enrichLoggingContext(ctx context.Context, req *ConversationRequest) context.Context {
	logger.Info("executing eval mode",
		"eval_id", req.Eval.ID,
		"recording", req.Eval.Recording.Path)

	return logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Scenario:  req.Eval.ID,
		SessionID: req.ConversationID,
		Stage:     "eval-execution",
	})
}

// loadRecording loads messages from the recording using the adapter registry.
func (e *EvalConversationExecutor) loadRecording(
	req *ConversationRequest,
) ([]types.Message, *adapters.RecordingMetadata, error) {
	if e.adapterRegistry == nil {
		return nil, nil, fmt.Errorf("adapter registry not configured for eval mode")
	}

	// Create a recording reference from the eval config
	ref := adapters.RecordingReference{
		ID:       req.Eval.Recording.Path,
		Source:   req.Eval.Recording.Path,
		TypeHint: req.Eval.Recording.Type,
	}

	messages, metadata, err := e.adapterRegistry.Load(ref)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load recording: %w", err)
	}

	logger.Debug("loaded recording",
		"messages", len(messages),
		"session_id", metadata.SessionID)

	return messages, metadata, nil
}

// applyAllTurnAssertions extracts and applies all turn-level assertions to assistant messages.
func (e *EvalConversationExecutor) applyAllTurnAssertions(
	turns []config.EvalTurnConfig,
	messages []types.Message,
	convCtx *assertions.ConversationContext,
) {
	turnAssertions := e.extractTurnAssertions(turns)
	for i := range messages {
		if messages[i].Role == roleAssistant {
			e.applyTurnAssertions(turnAssertions, &messages[i], convCtx)
		}
	}
}

// extractTurnAssertions collects all turn-level assertions from eval config.
func (e *EvalConversationExecutor) extractTurnAssertions(turns []config.EvalTurnConfig) []assertions.AssertionConfig {
	var assertionConfigs []assertions.AssertionConfig
	for _, turnCfg := range turns {
		if turnCfg.AllTurns != nil && len(turnCfg.AllTurns.Assertions) > 0 {
			assertionConfigs = append(assertionConfigs, turnCfg.AllTurns.Assertions...)
		}
	}
	return assertionConfigs
}

// evaluateConversationAssertions runs conversation-level assertions if configured.
func (e *EvalConversationExecutor) evaluateConversationAssertions(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	convCtx *assertions.ConversationContext,
) []assertions.ConversationValidationResult {
	if e.convAssertionReg == nil || len(assertionConfigs) == 0 {
		return nil
	}
	return e.applyConversationAssertions(ctx, assertionConfigs, convCtx)
}

// ExecuteConversationStream runs evaluation with streaming output.
// For eval mode, we don't have true streaming since we're replaying,
// but we implement this to satisfy the interface.
func (e *EvalConversationExecutor) ExecuteConversationStream(
	ctx context.Context,
	req ConversationRequest, //nolint:gocritic // Interface compliance requires value receiver
) (<-chan ConversationStreamChunk, error) {
	outChan := make(chan ConversationStreamChunk, 1)

	go func() {
		defer close(outChan)

		// Execute non-streaming and send final result
		result := e.ExecuteConversation(ctx, req)
		outChan <- ConversationStreamChunk{
			Result: result,
		}
	}()

	return outChan, nil
}

// validateEvalConfig validates the eval configuration.
func (e *EvalConversationExecutor) validateEvalConfig(eval *config.Eval) error {
	if eval == nil {
		return fmt.Errorf("eval configuration is required")
	}

	if eval.Recording.Path == "" {
		return fmt.Errorf("recording path is required")
	}

	return nil
}

// buildConversationContext creates a conversation context for eval mode.
// Uses the same metadata attachment as scenarios for consistency.
func (e *EvalConversationExecutor) buildConversationContext(
	req *ConversationRequest,
	messages []types.Message,
	metadata *adapters.RecordingMetadata,
) *assertions.ConversationContext {
	// Build extras map from metadata
	extras := make(map[string]interface{})

	// Add recording metadata
	if metadata != nil {
		if metadata.ProviderInfo != nil {
			extras["provider_info"] = metadata.ProviderInfo
		}
		if metadata.SessionID != "" {
			extras["session_id"] = metadata.SessionID
		}
		if metadata.Extras != nil {
			for k, v := range metadata.Extras {
				extras[k] = v
			}
		}
	}

	// Add eval-specific metadata
	extras["eval_id"] = req.Eval.ID
	extras["tags"] = e.mergeTags(req.Eval.Tags, metadata)

	// Attach judge metadata using the same function as scenarios
	attachJudgeMetadata(extras, req.Config)

	meta := &assertions.ConversationMetadata{
		Extras: extras,
	}
	return assertions.BuildConversationContextFromMessages(messages, meta)
}

// applyTurnAssertions applies turn-level assertions to a single message.
func (e *EvalConversationExecutor) applyTurnAssertions(
	assertionConfigs []assertions.AssertionConfig,
	msg *types.Message,
	convCtx *assertions.ConversationContext,
) {
	if e.assertionRegistry == nil || len(assertionConfigs) == 0 {
		return
	}

	results := make([]assertions.AssertionResult, 0, len(assertionConfigs))

	for _, cfg := range assertionConfigs {
		// Build validator params
		params := map[string]interface{}{
			"assistant_response": msg.Content,
			"_metadata": map[string]interface{}{
				"judge_targets":     convCtx.Metadata.Extras["judge_targets"],
				"provider_registry": e.providerRegistry,
				"prompt_registry":   e.promptRegistry,
			},
		}

		// Merge assertion params
		for k, v := range cfg.Params {
			params[k] = v
		}

		// Get and execute validator
		factory, ok := e.assertionRegistry.Get(cfg.Type)
		if !ok {
			results = append(results, assertions.AssertionResult{
				Passed:  false,
				Details: map[string]interface{}{"error": "unknown assertion type: " + cfg.Type},
				Message: cfg.Message,
			})
			continue
		}

		validator := factory(params)
		vr := validator.Validate(msg.Content, params)
		results = append(results, assertions.FromValidationResult(vr, cfg.Message))
	}

	// Store in message metadata
	if msg.Meta == nil {
		msg.Meta = make(map[string]interface{})
	}
	msg.Meta["assertions"] = results
}

// applyConversationAssertions runs conversation-level assertions on the full context.
func (e *EvalConversationExecutor) applyConversationAssertions(
	ctx context.Context,
	assertionConfigs []assertions.AssertionConfig,
	convCtx *assertions.ConversationContext,
) []assertions.ConversationValidationResult {
	if e.convAssertionReg == nil || len(assertionConfigs) == 0 {
		return nil
	}

	results := make([]assertions.ConversationValidationResult, 0)

	for _, cfg := range assertionConfigs {
		// Only process conversation-level assertions
		// Check if this assertion type is in the conversation registry
		if !e.convAssertionReg.Has(cfg.Type) {
			continue
		}

		validator, err := e.convAssertionReg.Get(cfg.Type)
		if err != nil {
			results = append(results, assertions.ConversationValidationResult{
				Type:    cfg.Type,
				Passed:  false,
				Details: map[string]interface{}{"error": err.Error()},
				Message: cfg.Message,
			})
			continue
		}

		result := validator.ValidateConversation(ctx, convCtx, cfg.Params)
		result.Type = cfg.Type
		result.Message = cfg.Message
		results = append(results, result)
	}

	return results
}

// calculateCost estimates or extracts cost information from the messages.
func (e *EvalConversationExecutor) calculateCost(messages []types.Message) types.CostInfo {
	totalCost := types.CostInfo{}

	for i := range messages {
		msg := &messages[i]
		if msg.Role == roleAssistant && msg.Meta != nil {
			// Try to extract cost info from metadata if available
			if costData, ok := msg.Meta["cost"]; ok {
				if cost, ok := costData.(types.CostInfo); ok {
					totalCost.TotalCost += cost.TotalCost
					totalCost.InputTokens += cost.InputTokens
					totalCost.OutputTokens += cost.OutputTokens
					totalCost.CachedTokens += cost.CachedTokens
				}
			}
		}
	}

	return totalCost
}

// hasFailedAssertions checks if any assertions failed.
func (e *EvalConversationExecutor) hasFailedAssertions(
	messages []types.Message,
	convResults []assertions.ConversationValidationResult,
) bool {
	if e.hasTurnAssertionFailures(messages) {
		return true
	}
	return e.hasConversationAssertionFailures(convResults)
}

// hasTurnAssertionFailures checks if any turn-level assertions failed.
func (e *EvalConversationExecutor) hasTurnAssertionFailures(messages []types.Message) bool {
	for i := range messages {
		if e.messageHasFailedAssertions(&messages[i]) {
			return true
		}
	}
	return false
}

// messageHasFailedAssertions checks if a message has any failed assertions.
func (e *EvalConversationExecutor) messageHasFailedAssertions(msg *types.Message) bool {
	if msg.Meta == nil {
		return false
	}

	results, ok := msg.Meta["assertions"].([]assertions.AssertionResult)
	if !ok {
		return false
	}

	for j := range results {
		if !results[j].Passed {
			return true
		}
	}
	return false
}

// hasConversationAssertionFailures checks if any conversation-level assertions failed.
func (e *EvalConversationExecutor) hasConversationAssertionFailures(
	convResults []assertions.ConversationValidationResult,
) bool {
	for i := range convResults {
		if !convResults[i].Passed {
			return true
		}
	}
	return false
}

// mergeTags merges tags from eval config and recording metadata.
func (e *EvalConversationExecutor) mergeTags(
	evalTags []string,
	metadata *adapters.RecordingMetadata,
) []string {
	tagSet := make(map[string]bool)
	merged := make([]string, 0)

	// Add eval tags
	for _, tag := range evalTags {
		if !tagSet[tag] {
			tagSet[tag] = true
			merged = append(merged, tag)
		}
	}

	// Add recording tags
	if metadata != nil {
		for _, tag := range metadata.Tags {
			if !tagSet[tag] {
				tagSet[tag] = true
				merged = append(merged, tag)
			}
		}
	}

	return merged
}
