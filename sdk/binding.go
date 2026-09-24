package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// hostBinding answers a pack's LOGICAL provider names with what this host
// actually wired up.
//
// A pack names the providers it needs — `grader`, `screener`, whatever the
// author chose — and the host supplies one per name. The host is free to change
// what sits behind a name at any time, which is the entire reason packs name
// logical keys instead of models: nothing in the pack, and nothing in the
// runtime, gets to decide which concrete provider serves a role.
//
// A key resolves to whatever the host registered under that id: an LLM from the
// provider pool, an inference provider from the inference registry.
type hostBinding struct{ cfg *config }

var _ evals.ProviderBinding = (*hostBinding)(nil)

// LLM returns the completion provider bound to key.
//
// Binding the wrong KIND of thing is reported as exactly that. A host that
// wired a classifier to the name a judge-backed check uses has made a wiring
// mistake, and "not found" would send them looking for a missing provider
// instead of at the one they supplied.
func (b *hostBinding) LLM(key string) (providers.Provider, error) {
	if b == nil || b.cfg == nil {
		return nil, evals.ErrNoBinding
	}
	if b.cfg.providers != nil {
		if p, ok := b.cfg.providers.Get(key); ok {
			return p, nil
		}
	}
	if _, isInference := b.inference(key); isInference {
		return nil, fmt.Errorf(
			"%w: it is bound to an inference provider, and this check needs one that runs completions",
			evals.ErrWrongKind)
	}
	return nil, evals.ErrUnboundKey
}

// Inference returns the inference provider bound to key.
func (b *hostBinding) Inference(key string) (inference.Provider, error) {
	if b == nil || b.cfg == nil {
		return nil, evals.ErrNoBinding
	}
	if p, ok := b.inference(key); ok {
		return p, nil
	}
	if b.cfg.providers != nil {
		if _, isLLM := b.cfg.providers.Get(key); isLLM {
			return nil, fmt.Errorf(
				"%w: it is bound to an LLM provider, and this check needs an inference provider "+
					"(a providers: entry with role: inference)",
				evals.ErrWrongKind)
		}
	}
	return nil, evals.ErrUnboundKey
}

// newHostBinding returns the binding for this conversation, or nil when the
// host wired nothing a pack could name.
func newHostBinding(c *config) evals.ProviderBinding {
	if c == nil {
		return nil
	}
	if c.providers == nil && c.inferenceRegistry == nil {
		return nil
	}
	return &hostBinding{cfg: c}
}

// inference looks key up in the inference registry by exact id. A named key
// never falls back to the registry's default: the pack asked for that name.
func (b *hostBinding) inference(key string) (inference.Provider, bool) {
	if key == "" || b.cfg.inferenceRegistry == nil {
		return nil, false
	}
	p, err := b.cfg.inferenceRegistry.Get(key)
	return p, err == nil
}
