package gemini

import (
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
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
//     -1 lets the model decide. Omit to use the model default. GEMINI 2.5.
//   - thinking_level    (string): low | medium | high. GEMINI 3's replacement
//     for thinking_budget. Required there to get thought summaries reliably —
//     Gemini 3 accepts a budget but largely ignores it for summaries, and
//     Gemini 2.5 rejects a level with HTTP 400, so pick the one matching the
//     model generation. When both are set, the level wins.
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
	if lvl, ok := normalizeThinkingLevel(spec.AdditionalConfig["thinking_level"]); ok {
		p.thinkingLevel = &lvl
	}
}

// thinkingLevels are the values Gemini 3 accepts. "minimal" is rejected by the
// API with HTTP 400, so an unrecognized value is dropped rather than forwarded
// — sending it would fail the whole request instead of just the thinking hint.
var thinkingLevels = map[string]bool{"low": true, "medium": true, "high": true}

// normalizeThinkingLevel lowercases and validates a thinking_level value.
func normalizeThinkingLevel(v any) (string, bool) {
	raw, ok := v.(string)
	if !ok {
		return "", false
	}
	lvl := strings.ToLower(strings.TrimSpace(raw))
	if !thinkingLevels[lvl] {
		if raw != "" {
			logger.Warn("gemini: ignoring unrecognized thinking_level",
				"value", raw, "accepted", "low|medium|high")
		}
		return "", false
	}
	return lvl, true
}

// geminiThinkingConfigFor returns the thinkingConfig to attach to a request, or
// nil when nothing is configured — in which case the model applies its own
// default thinking. We deliberately do NOT auto-disable thinking based on model
// name: that's a maintenance trap (every new model/tier would need a rule) and
// unnecessary, since at the default maxOutputTokens (4096) thinking has ample
// room and doesn't truncate. Callers who consider thinking unnecessary opt out
// per provider with additional_config.thinking_budget: 0 (valid on flash;
// pro/thinking-only models reject 0 with a clear API error).
//
// maxTokens is the resolved maxOutputTokens; when a positive budget can't leave
// room for an answer, a warning is logged (that combination returns MAX_TOKENS
// with no usable answer).
func (p *Provider) geminiThinkingConfigFor(maxTokens int) *geminiThinkingConfig {
	if p.thinkingBudget == nil && p.thinkingLevel == nil && !p.includeThoughts {
		return nil
	}

	// Gemini 3 takes a level, Gemini 2.5 takes a budget, and neither accepts
	// the other's field — 2.5 rejects a level outright with HTTP 400. When a
	// level is configured it wins and the budget is dropped, so exactly one
	// control reaches the wire.
	if p.thinkingLevel != nil {
		return &geminiThinkingConfig{
			ThinkingLevel:   p.thinkingLevel,
			IncludeThoughts: p.includeThoughts,
		}
	}
	if p.thinkingBudget != nil && *p.thinkingBudget > 0 && maxTokens > 0 && maxTokens <= *p.thinkingBudget {
		logger.Warn("Gemini thinking_budget leaves no room for the answer: "+
			"maxOutputTokens must exceed thinking_budget (reasoning tokens count toward the output cap)",
			"provider", p.ID(), "max_output_tokens", maxTokens, "thinking_budget", *p.thinkingBudget)
	}
	return &geminiThinkingConfig{ThinkingBudget: p.thinkingBudget, IncludeThoughts: p.includeThoughts}
}
