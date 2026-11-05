package ollama

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

// MaxContextTokens returns additional_config.context_window, or 0 (unknown),
// which makes the compactor use its default budget. Ollama serves arbitrary
// local models whose usable window depends on the server's num_ctx rather than
// on the model name, so there is no built-in table to fall back to.
func (p *Provider) MaxContextTokens() int {
	return base.ResolveContextWindow(p.ID(), p.model, p.contextWindow, nil)
}
