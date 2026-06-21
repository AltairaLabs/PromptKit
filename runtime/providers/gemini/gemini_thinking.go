package gemini

import (
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Gemini 2.5 "thinking" model support (#1404 follow-on).
//
// On 2.5 models the model spends output-token budget on internal reasoning
// before any visible text, and those reasoning tokens count toward
// maxOutputTokens. So a tight maxOutputTokens can be exhausted by thinking
// alone, returning finishReason=MAX_TOKENS with no content. thinkingConfig lets
// callers bound (or disable) that reasoning so maxOutputTokens is predictable:
//
//	maxOutputTokens >= thinkingBudget + expected_answer_tokens
//
// Verified live: thinkingBudget is a TOKEN ceiling (budget 128 -> 83 thinking
// tokens used), and maxOutputTokens caps the SUM of thinking + answer (92 + 54
// hit a 150 cap). Configured via additional_config; off by default (model's own
// dynamic thinking applies).

// applyThinkingConfig reads thinking-model settings from additional_config:
//   - thinking_budget   (int): token ceiling on reasoning. 0 disables (flash),
//     -1 lets the model decide. Omit to use the model default.
//   - include_thoughts (bool): return thought summaries.
//
//nolint:gocritic // hugeParam: providers.ProviderSpec is passed by value across the factory
func applyThinkingConfig(p *Provider, spec providers.ProviderSpec) {
	if spec.AdditionalConfig == nil {
		return
	}
	if v, ok := spec.AdditionalConfig["thinking_budget"]; ok {
		if f, ok := toFloat(v); ok {
			budget := int(f)
			p.thinkingBudget = &budget
		}
	}
	if v, ok := spec.AdditionalConfig["include_thoughts"].(bool); ok {
		p.includeThoughts = v
	}
}

// geminiThinkingConfigFor returns the thinkingConfig to attach to a request, or
// nil when nothing is configured (the model applies its own default thinking).
// maxTokens is the resolved maxOutputTokens for the request; when it cannot
// cover the thinking budget plus any answer, a warning is logged — that
// combination returns MAX_TOKENS with no usable answer.
func (p *Provider) geminiThinkingConfigFor(maxTokens int) *geminiThinkingConfig {
	if p.thinkingBudget == nil && !p.includeThoughts {
		return nil
	}
	if p.thinkingBudget != nil && *p.thinkingBudget > 0 && maxTokens > 0 && maxTokens <= *p.thinkingBudget {
		logger.Warn("Gemini thinking_budget leaves no room for the answer: "+
			"maxOutputTokens must exceed thinking_budget (reasoning tokens count toward the output cap)",
			"provider", p.ID(), "max_output_tokens", maxTokens, "thinking_budget", *p.thinkingBudget)
	}
	return &geminiThinkingConfig{
		ThinkingBudget:  p.thinkingBudget,
		IncludeThoughts: p.includeThoughts,
	}
}
