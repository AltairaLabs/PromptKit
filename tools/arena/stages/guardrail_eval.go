package stages

import (
	"context"
	"time"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default eval handlers for guardrail factory
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// GuardrailEvalStage evaluates guardrail hooks against assistant messages
// and records pass/fail results in message.Validations. When a guardrail
// denies and FailOnViolation is true (the default), this stage also enforces
// the guardrail by modifying the message content (e.g., truncating for length
// validators, replacing content for content blockers).
//
// Validator configs are sourced from the per-Turn *TurnState* (populated by
// PromptAssemblyStage). When TurnState is not wired, the stage falls back to
// reading "validator_configs" from element metadata for backward compat.
type GuardrailEvalStage struct {
	stage.BaseStage
	turnState *stage.TurnState
}

// NewGuardrailEvalStage creates a new guardrail evaluation stage.
// Pipelines that have migrated to TurnState should use NewGuardrailEvalStageWithTurnState.
func NewGuardrailEvalStage() *GuardrailEvalStage {
	return &GuardrailEvalStage{
		BaseStage: stage.NewBaseStage("guardrail_eval", stage.StageTypeTransform),
	}
}

// NewGuardrailEvalStageWithTurnState creates a guardrail evaluation stage that
// reads validator configurations from the shared *TurnState.
func NewGuardrailEvalStageWithTurnState(turnState *stage.TurnState) *GuardrailEvalStage {
	return &GuardrailEvalStage{
		BaseStage: stage.NewBaseStage("guardrail_eval", stage.StageTypeTransform),
		turnState: turnState,
	}
}

// Process collects all elements, evaluates guardrail hooks against the last
// assistant message, and forwards all elements with updated Validations.
//
//nolint:gocognit,cyclop // Guardrail evaluation with config parsing and hook dispatch
func (s *GuardrailEvalStage) Process(
	ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement,
) error {
	defer close(output)

	// Collect all elements
	var elements []stage.StreamElement
	for elem := range input {
		elements = append(elements, elem)
	}

	// Extract validator configs from metadata
	configs := s.extractValidatorConfigs(elements)
	if len(configs) == 0 {
		// No validators — passthrough
		return s.forwardAll(ctx, elements, output)
	}

	// Find the last assistant message
	lastAssistantIdx := -1
	for i := len(elements) - 1; i >= 0; i-- {
		if elements[i].Message != nil && elements[i].Message.Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	if lastAssistantIdx < 0 {
		// No assistant message — passthrough
		return s.forwardAll(ctx, elements, output)
	}

	// Evaluate each guardrail against the assistant message
	msg := elements[lastAssistantIdx].Message
	var validations []types.ValidationResult

	for _, cfg := range configs {
		hook, err := guardrails.NewGuardrailHook(cfg.Type, cfg.Params)
		if err != nil {
			logger.Warn("Skipping unknown guardrail type", "type", cfg.Type, "error", err)
			continue
		}

		resp := &hooks.ProviderResponse{
			Message: *msg,
		}
		decision := hook.AfterCall(ctx, &hooks.ProviderRequest{}, resp)

		vr := types.ValidationResult{
			ValidatorType: cfg.Type,
			Passed:        decision.Allow,
			Timestamp:     time.Now(),
			Details:       map[string]interface{}{},
		}
		// Always include the validator config so reports can show what was tested
		if len(cfg.Params) > 0 {
			vr.Details["config"] = cfg.Params
		}
		if !decision.Allow {
			vr.Details["reason"] = decision.Reason
			if decision.Metadata != nil {
				for k, v := range decision.Metadata {
					vr.Details[k] = v
				}
			}
			// Enforce guardrail if FailOnViolation is not explicitly false
			if cfg.FailOnViolation == nil || *cfg.FailOnViolation {
				enforceGuardrail(msg, cfg, decision)
			}
		}
		validations = append(validations, vr)
	}

	// Append validations to the assistant message
	if len(validations) > 0 {
		elements[lastAssistantIdx].Message.Validations = append(
			elements[lastAssistantIdx].Message.Validations, validations...,
		)
	}

	return s.forwardAll(ctx, elements, output)
}

// extractValidatorConfigs sources validator configs from TurnState when wired,
// otherwise falls back to scanning element metadata for the deprecated
// "validator_configs" key.
func (s *GuardrailEvalStage) extractValidatorConfigs(elements []stage.StreamElement) []prompt.ValidatorConfig {
	if s.turnState != nil && len(s.turnState.Validators) > 0 {
		return s.turnState.Validators
	}
	for i := range elements {
		if elements[i].Metadata == nil {
			continue
		}
		raw, ok := elements[i].Metadata["validator_configs"]
		if !ok {
			continue
		}
		if configs, ok := raw.([]prompt.ValidatorConfig); ok {
			return configs
		}
	}
	return nil
}

// lengthValidatorTypes maps validator types that enforce max length truncation.
var lengthValidatorTypes = map[string]bool{
	"length":     true,
	"max_length": true,
}

// contentBlockerTypes maps validator types that block content entirely.
var contentBlockerTypes = map[string]bool{
	"banned_words":     true,
	"content_excludes": true,
}

// enforceGuardrail modifies the message content when a guardrail denies.
// For length validators, it truncates content to the configured maximum.
// For content blockers, it replaces content with a configurable blocked message.
func enforceGuardrail(msg *types.Message, cfg prompt.ValidatorConfig, decision hooks.Decision) {
	switch {
	case lengthValidatorTypes[cfg.Type]:
		enforceMaxLength(msg, cfg.Params)
	case contentBlockerTypes[cfg.Type]:
		enforceContentBlock(msg, cfg)
	default:
		logger.Debug("Guardrail denied but no enforcement action defined",
			"type", cfg.Type, "reason", decision.Reason)
	}
}

// enforceMaxLength truncates message content to the configured maximum character count.
func enforceMaxLength(msg *types.Message, params map[string]interface{}) {
	maxLen := extractIntParam(params, "max_characters")
	if maxLen == 0 {
		maxLen = extractIntParam(params, "max")
	}
	if maxLen == 0 {
		maxLen = extractIntParam(params, "max_chars")
	}
	if maxLen > 0 && len(msg.Content) > maxLen {
		logger.Info("Guardrail enforced: truncating content", "original_length", len(msg.Content), "max_length", maxLen)
		msg.Content = msg.Content[:maxLen]
	}
}

// enforceContentBlock replaces message content when forbidden content is detected.
// Uses the validator's configured Message, falling back to DefaultBlockedMessage.
func enforceContentBlock(msg *types.Message, cfg prompt.ValidatorConfig) {
	blockedMsg := cfg.Message
	if blockedMsg == "" {
		blockedMsg = prompt.DefaultBlockedMessage
	}
	logger.Info("Guardrail enforced: content blocked", "type", cfg.Type)
	msg.Content = blockedMsg
}

// extractIntParam extracts an integer parameter from a map, handling both int and float64 types.
func extractIntParam(params map[string]interface{}, key string) int {
	v, ok := params[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		return 0
	}
}

// forwardAll sends all elements to the output channel.
func (s *GuardrailEvalStage) forwardAll(
	ctx context.Context, elements []stage.StreamElement, output chan<- stage.StreamElement,
) error {
	for i := range elements {
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
