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
}

// NewEvalConversationExecutor creates a new eval conversation executor.
func NewEvalConversationExecutor(
	adapterRegistry *adapters.Registry,
	assertionRegistry *runtimeValidators.Registry,
	convAssertionReg *assertions.ConversationAssertionRegistry,
	promptRegistry *prompt.Registry,
	providerRegistry *providers.Registry,
) *EvalConversationExecutor {
	return &EvalConversationExecutor{
		adapterRegistry:   adapterRegistry,
		assertionRegistry: assertionRegistry,
		convAssertionReg:  convAssertionReg,
		promptRegistry:    promptRegistry,
		providerRegistry:  providerRegistry,
	}
}

// ExecuteConversation runs an evaluation on a saved conversation.
func (e *EvalConversationExecutor) ExecuteConversation(
	ctx context.Context,
	req ConversationRequest, //nolint:gocritic // Interface compliance requires value receiver
) *ConversationResult {
	// Validate eval configuration first
	if err := e.validateEvalConfig(req.Eval); err != nil {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("invalid eval configuration: %v", err),
		}
	}

	// Enrich context with eval information for structured logging
	ctx = logger.WithLoggingContext(ctx, &logger.LoggingFields{
		Scenario:  req.Eval.ID,
		SessionID: req.ConversationID,
		Stage:     "eval-execution",
	})

	logger.Info("executing eval mode",
		"eval_id", req.Eval.ID,
		"recording", req.Eval.Recording.Path)

	// Load recording using adapter registry
	if e.adapterRegistry == nil {
		return &ConversationResult{
			Failed: true,
			Error:  "adapter registry not configured for eval mode",
		}
	}

	adapter := e.adapterRegistry.FindAdapter(req.Eval.Recording.Path, req.Eval.Recording.Type)
	if adapter == nil {
		return &ConversationResult{
			Failed: true,
			Error: fmt.Sprintf("no adapter found for recording: %s (type: %s)",
				req.Eval.Recording.Path, req.Eval.Recording.Type),
		}
	}

	// Load messages and metadata from recording
	messages, metadata, err := adapter.Load(req.Eval.Recording.Path)
	if err != nil {
		return &ConversationResult{
			Failed: true,
			Error:  fmt.Sprintf("failed to load recording: %v", err),
		}
	}

	logger.Debug("loaded recording",
		"messages", len(messages),
		"session_id", metadata.SessionID)

	// Build conversation context from recording
	convCtx := e.buildConversationContext(req.Eval, messages, metadata)

	// Apply turn-level assertions to assistant messages
	for i := range messages {
		msg := &messages[i]
		if msg.Role == roleAssistant {
			e.applyTurnAssertions(req.Eval.Assertions, msg, convCtx)
		}
	}

	// Run conversation-level assertions if configured
	var convResults []assertions.ConversationValidationResult
	if e.convAssertionReg != nil && len(req.Eval.Assertions) > 0 {
		convResults = e.applyConversationAssertions(ctx, req.Eval.Assertions, convCtx)
	}

	// Calculate costs from metadata
	totalCost := e.calculateCost(messages)

	// Determine if eval failed based on assertions
	failed := e.hasFailedAssertions(messages, convResults)

	// Return result with same schema as scenario execution
	return &ConversationResult{
		Messages:                     messages,
		Cost:                         totalCost,
		ConversationAssertionResults: convResults,
		Failed:                       failed,
	}
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
// Merges metadata from the recording with eval configuration.
func (e *EvalConversationExecutor) buildConversationContext(
	eval *config.Eval,
	messages []types.Message,
	metadata *adapters.RecordingMetadata,
) *assertions.ConversationContext {
	// Build judge targets map, prioritizing eval config over recording metadata
	judgeTargets := e.buildJudgeTargets(eval, metadata)

	// Build extras map from metadata
	extras := e.buildExtrasMap(eval, metadata, judgeTargets)

	return &assertions.ConversationContext{
		AllTurns: messages,
		Metadata: assertions.ConversationMetadata{
			Extras: extras,
		},
	}
}

// buildJudgeTargets builds the judge targets map from recording metadata and eval config.
func (e *EvalConversationExecutor) buildJudgeTargets(
	eval *config.Eval,
	metadata *adapters.RecordingMetadata,
) map[string]interface{} {
	judgeTargets := make(map[string]interface{})

	// Add judge targets from recording metadata if available
	if metadata != nil && metadata.JudgeTargets != nil {
		for name, spec := range metadata.JudgeTargets {
			judgeTargets[name] = e.createJudgeTarget(spec.ID, spec.Type, spec.Model)
		}
	}

	// Override with eval config judge targets (takes precedence)
	for name, spec := range eval.JudgeTargets {
		judgeTargets[name] = e.createJudgeTarget(spec.ID, spec.Type, spec.Model)
	}

	return judgeTargets
}

// createJudgeTarget creates a judge target map from provider spec.
func (e *EvalConversationExecutor) createJudgeTarget(id, providerType, model string) map[string]interface{} {
	providerID := id
	if providerID == "" {
		providerID = providerType
	}
	return map[string]interface{}{
		"provider_id": providerID,
		"model":       model,
	}
}

// buildExtrasMap builds the extras metadata map.
func (e *EvalConversationExecutor) buildExtrasMap(
	eval *config.Eval,
	metadata *adapters.RecordingMetadata,
	judgeTargets map[string]interface{},
) map[string]interface{} {
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
	extras["eval_id"] = eval.ID
	extras["tags"] = e.mergeTags(eval.Tags, metadata)
	extras["judge_targets"] = judgeTargets

	return extras
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
