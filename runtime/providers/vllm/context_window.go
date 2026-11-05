package vllm

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

// MaxContextTokens returns additional_config.context_window, or 0 (unknown),
// which makes the compactor use its default budget. vLLM serves arbitrary
// models whose usable window is set by the server's max-model-len rather than
// by the model name, so there is no built-in table to fall back to.
func (p *Provider) MaxContextTokens() int {
	return base.ResolveContextWindow(p.ID(), p.model,
		base.ContextWindowFromConfig(p.additionalConfig), nil)
}
