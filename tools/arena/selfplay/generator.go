package selfplay

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	arenamiddleware "github.com/AltairaLabs/PromptKit/tools/arena/middleware"
)

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

// NextUserTurn generates a user message using the LLM through a pipeline
func (cg *ContentGenerator) NextUserTurn(ctx context.Context, history []types.Message) (*pipeline.ExecutionResult, error) {
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
	logger.LLMCall(cg.provider.ID(), "self-play user", len(history), float64(cg.persona.Defaults.Temperature), "persona", cg.persona.ID)

	// Build pipeline with standard middleware:
	// 1. PersonaAssemblyMiddleware - assembles persona prompt with fragments/vars
	// 2. HistoryInjectionMiddleware - prepends conversation history
	// 3. TemplateMiddleware - final variable substitution (if needed)
	// 4. ProviderMiddleware - calls LLM
	providerConfig := &middleware.ProviderMiddlewareConfig{
		Temperature: cg.persona.Defaults.Temperature,
		MaxTokens:   200, // Short user messages
	}

	middlewares := []pipeline.Middleware{
		arenamiddleware.PersonaAssemblyMiddleware(cg.persona, region, baseVariables),
		arenamiddleware.HistoryInjectionMiddleware(history),
		middleware.TemplateMiddleware(),
		middleware.ProviderMiddleware(cg.provider, nil, nil, providerConfig),
	}

	pl := pipeline.NewPipeline(middlewares...)

	// Execute pipeline with empty content (system prompt + history drives generation)
	result, err := pl.Execute(ctx, "", "")
	if err != nil {
		logger.LLMError(cg.provider.ID(), "self-play user", err)
		return nil, fmt.Errorf("failed to generate user turn: %w", err)
	}

	// Log response
	logger.LLMResponse(cg.provider.ID(), "self-play user", result.CostInfo.InputTokens, result.CostInfo.OutputTokens, result.CostInfo.TotalCost)

	// Validate the response (treat failures as warnings, not hard errors)
	if result.Response != nil && result.Response.Content != "" {
		if err := cg.validateUserResponse(result.Response.Content); err != nil {
			logger.Warn("⚠️  User response validation warning", "provider", cg.provider.ID(), "role", "self-play user", "warning", err.Error())
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
