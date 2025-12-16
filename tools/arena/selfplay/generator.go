package selfplay

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
)

const selfPlayUserRole = "self-play user"

// ContentGenerator generates user messages using an LLM
type ContentGenerator struct {
	provider providers.Provider
	persona  *config.UserPersonaPack
}

// NewContentGenerator creates a new content generator with a specific provider and persona
func NewContentGenerator(provider providers.Provider, persona *config.UserPersonaPack) *ContentGenerator {
	return &ContentGenerator{
		provider: provider,
		persona:  persona,
	}
}

// extractRegionFromPersonaID extracts the region suffix from a persona ID
// e.g., "challenger_uk" -> "uk", "social-engineer" -> "us" (default)
func extractRegionFromPersonaID(personaID string) string {
	if strings.HasSuffix(personaID, "_uk") {
		return "uk"
	}
	if strings.HasSuffix(personaID, "_au") {
		return "au"
	}
	if strings.HasSuffix(personaID, "_us") {
		return "us"
	}
	// Default to US region
	return "us"
}

// NextUserTurn generates a user message using the LLM through a stage pipeline
func (cg *ContentGenerator) NextUserTurn(
	ctx context.Context,
	history []types.Message,
	scenarioID string,
) (*pipeline.ExecutionResult, error) {
	if cg.provider == nil {
		return nil, fmt.Errorf("ContentGenerator provider is nil - check self-play provider configuration")
	}
	if cg.persona == nil {
		return nil, fmt.Errorf("ContentGenerator persona is nil")
	}

	// Extract region from persona ID (or default to "us")
	region := extractRegionFromPersonaID(cg.persona.ID)

	// Build base variables for persona assembly
	baseVariables := map[string]string{
		"context_slot": "", // Empty for self-play - conversation context comes through messages
		"domain_hint":  "general",
		"user_context": "user",
	}

	// Log the self-play user generation API call
	logger.LLMCall(
		cg.provider.ID(),
		selfPlayUserRole,
		len(history),
		float64(cg.persona.Defaults.Temperature),
		"persona",
		cg.persona.ID,
	)

	// Build stage pipeline:
	// 1. PersonaAssemblyStage - assembles persona prompt with fragments/vars
	// 2. HistoryInjectionStage - prepends conversation history
	// 3. SelfPlayUserTurnContextStage - injects scenario context for MockProvider
	// 4. TemplateStage - final variable substitution
	// 5. ProviderStage - calls LLM
	providerConfig := &stage.ProviderConfig{
		Temperature: cg.persona.Defaults.Temperature,
		MaxTokens:   200, // Short user messages
	}

	stages := []stage.Stage{
		arenastages.NewPersonaAssemblyStage(cg.persona, region, baseVariables),
		arenastages.NewHistoryInjectionStage(history),
		arenastages.NewSelfPlayUserTurnContextStage(&config.Scenario{ID: scenarioID}),
		stage.NewTemplateStage(),
		stage.NewProviderStage(cg.provider, nil, nil, providerConfig),
	}

	builder := stage.NewPipelineBuilder()
	pl, err := builder.Chain(stages...).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build stage pipeline: %w", err)
	}

	// Create empty input element (system prompt + history drives generation)
	inputElem := stage.StreamElement{
		Metadata: map[string]interface{}{
			"persona":     cg.persona.ID,
			"scenario_id": scenarioID,
		},
	}

	// Execute pipeline synchronously
	stageResult, err := pl.ExecuteSync(ctx, inputElem)
	if err != nil {
		logger.LLMError(cg.provider.ID(), selfPlayUserRole, err)
		return nil, fmt.Errorf("failed to generate user turn: %w", err)
	}

	// Convert stage.ExecutionResult to pipeline.ExecutionResult
	result := convertStageResult(stageResult)

	// Log response
	logger.LLMResponse(
		cg.provider.ID(),
		selfPlayUserRole,
		result.CostInfo.InputTokens,
		result.CostInfo.OutputTokens,
		result.CostInfo.TotalCost,
	)

	// Validate the response (treat failures as warnings, not hard errors)
	if result.Response != nil && result.Response.Content != "" {
		if err := cg.validateUserResponse(result.Response.Content); err != nil {
			logger.Warn(
				"User response validation warning",
				"provider", cg.provider.ID(),
				"role", selfPlayUserRole,
				"warning", err.Error(),
			)
			// Add warning to metadata
			if result.Metadata == nil {
				result.Metadata = make(map[string]interface{})
			}
			result.Metadata["validation_warning"] = err.Error()
			result.Metadata["warning_type"] = "user_response_validation"
		}
	}

	// Add persona metadata to result
	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["persona"] = cg.persona.ID
	result.Metadata["role"] = "self-play-user"
	result.Metadata["self_play_provider"] = cg.provider.ID()

	return result, nil
}

// convertStageResult converts stage.ExecutionResult to pipeline.ExecutionResult
func convertStageResult(stageResult *stage.ExecutionResult) *pipeline.ExecutionResult {
	result := &pipeline.ExecutionResult{
		Messages: stageResult.Messages,
		CostInfo: stageResult.CostInfo,
		Metadata: stageResult.Metadata,
	}

	// Convert Response if present
	if stageResult.Response != nil {
		result.Response = &pipeline.Response{
			Role:      stageResult.Response.Role,
			Content:   stageResult.Response.Content,
			ToolCalls: stageResult.Response.ToolCalls,
		}
	}

	return result
}

// validateUserResponse validates that the user response meets requirements
func (cg *ContentGenerator) validateUserResponse(content string) error {
	// Check length (≤2 sentences)
	sentences := strings.Split(strings.TrimSpace(content), ".")
	// Filter out empty strings
	var nonEmptySentences []string
	for _, s := range sentences {
		if strings.TrimSpace(s) != "" {
			nonEmptySentences = append(nonEmptySentences, s)
		}
	}

	if len(nonEmptySentences) > 2 {
		return fmt.Errorf("response too long: %d sentences (max 2)", len(nonEmptySentences))
	}

	// Check for at most one question (temporarily disabled for testing)
	questionCount := strings.Count(content, "?")
	if questionCount > 2 { // Increased from 1 to 2 for testing
		return fmt.Errorf("too many questions: %d (max 2)", questionCount)
	}

	// Check for role integrity (no plans or assistant-like language)
	content = strings.ToLower(content)
	problematicPhrases := []string{
		"here's your plan",
		"here's what you should do",
		"step 1:",
		"step 2:",
		"first, you should",
		"as an ai",
		"i recommend",
		"my suggestion is",
	}

	for _, phrase := range problematicPhrases {
		if strings.Contains(content, phrase) {
			return fmt.Errorf("role integrity violation: contains assistant-like language: %s", phrase)
		}
	}

	return nil
}
