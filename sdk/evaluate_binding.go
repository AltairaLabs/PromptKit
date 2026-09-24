package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// The offline Evaluate() path needs the same answer a live conversation does:
// when a check names a provider, something has to say what that name means.
// There is no conversation here and no provider pool, so the caller supplies
// the mapping — or, when they passed judge targets, those serve as one, because
// they are already names pointing at provider specs.

// evaluateBinding resolves which binding Evaluate should use, or nil when the
// caller supplied nothing a name could resolve against.
func evaluateBinding(opts *EvaluateOpts) evals.ProviderBinding {
	if opts.ProviderBinding != nil {
		return opts.ProviderBinding
	}
	if len(opts.JudgeTargets) > 0 {
		return judgeTargetBinding(opts.JudgeTargets)
	}
	return nil
}

// judgeTargetBinding treats Arena-style judge targets as a binding: the map
// keys are names and the values are provider specs, which is the same shape a
// pack's logical names resolve through.
type judgeTargetBinding map[string]any

var _ evals.ProviderBinding = (judgeTargetBinding)(nil)

func (b judgeTargetBinding) LLM(key string) (providers.Provider, error) {
	raw, ok := b[key]
	if !ok {
		return nil, evals.ErrUnboundKey
	}
	spec, ok := raw.(providers.ProviderSpec)
	if !ok {
		if specPtr, isPtr := raw.(*providers.ProviderSpec); isPtr && specPtr != nil {
			spec = *specPtr
		} else {
			return nil, fmt.Errorf("%w: judge target %q is a %T, not a provider spec",
				evals.ErrWrongKind, key, raw)
		}
	}
	p, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("judge target %q: %w", key, err)
	}
	return p, nil
}

// Inference reports that judge targets hold no inference providers, rather
// than pretending a name is unbound: the caller supplied something for it, just
// not a thing that classifies.
func (b judgeTargetBinding) Inference(key string) (inference.Provider, error) {
	if _, ok := b[key]; !ok {
		return nil, evals.ErrUnboundKey
	}
	return nil, fmt.Errorf(
		"%w: %q is a judge target, and this check needs an inference provider. "+
			"Pass EvaluateOpts.ProviderBinding to supply one",
		evals.ErrWrongKind, key)
}
