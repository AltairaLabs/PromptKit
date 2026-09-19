package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
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
// provider pool, a classify backend from the classify registry.
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
	if _, isClassifier := b.cfg.classifyBackends[key]; isClassifier {
		return nil, fmt.Errorf(
			"%w: it is bound to a classify provider, and this check needs one that runs completions",
			evals.ErrWrongKind)
	}
	return nil, evals.ErrUnboundKey
}

// Classifier returns the classify backend bound to key. The caller asserts the
// task interface it needs; this only answers whether the host bound a
// classifier at all.
func (b *hostBinding) Classifier(key string) (classify.Backend, error) {
	if b == nil || b.cfg == nil {
		return nil, evals.ErrNoBinding
	}
	if backend, ok := b.cfg.classifyBackends[key]; ok {
		return backend, nil
	}
	if b.cfg.providers != nil {
		if _, isLLM := b.cfg.providers.Get(key); isLLM {
			return nil, fmt.Errorf(
				"%w: it is bound to an LLM provider, and this check needs a classify provider "+
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
	if c.providers == nil && len(c.classifyBackends) == 0 {
		return nil
	}
	return &hostBinding{cfg: c}
}
