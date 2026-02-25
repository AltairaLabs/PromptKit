package stages

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// GuardrailEvalStage evaluates guardrail hooks against assistant messages
// and records pass/fail results in message.Validations. Unlike ProviderStage
// hook enforcement, this stage never terminates the pipeline on denial —
// it records the result for downstream assertion stages to evaluate.
//
// This stage reads "validator_configs" from element metadata (set by
// PromptAssemblyStage) and uses guardrails.NewGuardrailHook to instantiate
// each hook, then calls AfterCall to evaluate the response.
type GuardrailEvalStage struct {
	stage.BaseStage
}

// NewGuardrailEvalStage creates a new guardrail evaluation stage.
func NewGuardrailEvalStage() *GuardrailEvalStage {
	return &GuardrailEvalStage{
		BaseStage: stage.NewBaseStage("guardrail_eval", stage.StageTypeTransform),
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
		}
		if !decision.Allow {
			vr.Details = map[string]interface{}{
				"reason": decision.Reason,
			}
			if decision.Metadata != nil {
				for k, v := range decision.Metadata {
					vr.Details[k] = v
				}
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

// extractValidatorConfigs finds validator_configs in any element's metadata.
func (s *GuardrailEvalStage) extractValidatorConfigs(elements []stage.StreamElement) []prompt.ValidatorConfig {
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
