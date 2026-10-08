package sdk

import (
	"errors"
	"fmt"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// RFC 0017 lets a prompt, or a composition prompt/agent step, name the
// requires.providers key that runs it. One rule picks the provider for every
// call site, and nothing else picks one:
//
//  1. the composition step's `provider`, if set;
//  2. otherwise the prompt's `provider`, if set;
//  3. otherwise `default`, which is the agent provider.
//
// A key other than `default` resolves through the host binding, exactly as a
// check's `provider` param does: the pack names a logical key and the host
// decides what serves it.

// callProviderKey returns the requires key that runs a call to promptTask,
// with stepProvider the composition step's own `provider` ("" outside a
// composition or when the step sets none).
func callProviderKey(p *pack.Pack, promptTask, stepProvider string) string {
	if stepProvider != "" {
		return stepProvider
	}
	if p != nil {
		if pr, ok := p.Prompts[promptTask]; ok && pr != nil && pr.Provider != "" {
			return pr.Provider
		}
	}
	return rtprompt.RequirementKeyDefault
}

// resolveCallProvider returns the provider bound to key, the agent provider for
// `default`.
func resolveCallProvider(cfg *config, key string) (providers.Provider, error) {
	if key == "" || key == rtprompt.RequirementKeyDefault {
		return cfg.getAgentProvider(), nil
	}
	binding := newHostBinding(cfg)
	if binding == nil {
		return nil, evals.ErrNoBinding
	}
	return binding.LLM(key)
}

// stepProviderResolver returns the per-step resolver a composition runs with.
func stepProviderResolver(p *pack.Pack, cfg *config) func(*composition.Step) (providers.Provider, error) {
	return func(step *composition.Step) (providers.Provider, error) {
		key := callProviderKey(p, step.PromptTask, step.Provider)
		prov, err := resolveCallProvider(cfg, key)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", key, err)
		}
		return prov, nil
	}
}

// callSiteRef is one call site's reference to a requires key.
type callSiteRef struct {
	site      string // "prompt \"drafter\"", "composition \"c\" step \"s\""
	key       string
	needTools bool // an agent step runs a tool loop
}

// checkCallProviders validates, at Open, every provider key a call site in the
// pack names: every prompt and every composition step, not only the prompt
// being opened, so a workflow fails at its entry state rather than at the
// transition that reaches a bad one.
//
// Each fault belongs to someone different, so each is reported as such:
//
//   - the key is not declared in requires.providers — the pack author's;
//   - the key is declared and the host bound nothing to it — the host's. This
//     holds for an optional requirement too: a call site that names it cannot
//     run without it;
//   - the host bound something that cannot serve the call — also the host's:
//     an inference provider anywhere, or a provider without tool support on an
//     agent step.
func checkCallProviders(p *pack.Pack, cfg *config) error {
	refs := collectCallSiteRefs(p)
	if len(refs) == 0 {
		return nil
	}
	declared, err := declaredRequirementKeys(p)
	if err != nil {
		return err
	}

	var problems []string
	for _, ref := range refs {
		if msg := checkCallSiteRef(ref, declared, cfg); msg != "" {
			problems = append(problems, msg)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w — pack call sites cannot resolve their providers:\n  - %s",
			errProviderKeys, joinProblems(problems))
	}
	return nil
}

func checkCallSiteRef(ref callSiteRef, declared map[string]rtprompt.ResolvedRequirement, cfg *config) string {
	if _, ok := declared[ref.key]; !ok {
		return fmt.Sprintf("%s names provider %q, which the pack does not declare in requires "+
			"(declared: %s)", ref.site, ref.key, describeKeys(declared))
	}
	prov, err := resolveCallProvider(cfg, ref.key)
	switch {
	case errors.Is(err, evals.ErrWrongKind):
		return fmt.Sprintf("%s names provider %q, and the host bound an inference provider to it; "+
			"a prompt or step needs an LLM provider (WithNamedProvider)", ref.site, ref.key)
	case err != nil:
		return fmt.Sprintf("%s names provider %q, which the pack declares and the host bound nothing to; "+
			"bind one with WithNamedProvider under that id", ref.site, ref.key)
	}
	if ref.needTools {
		if _, ok := prov.(providers.ToolSupport); !ok {
			return fmt.Sprintf("%s is an agent step and names provider %q, which the host bound to %T, "+
				"a provider without tool support", ref.site, ref.key, prov)
		}
	}
	return ""
}

// collectCallSiteRefs lists every call site that names a non-default key, in a
// stable order. An agent step that names no key of its own is listed when its
// prompt names one, because it is the step that needs tool support.
func collectCallSiteRefs(p *pack.Pack) []callSiteRef {
	if p == nil {
		return nil
	}
	var refs []callSiteRef
	for _, name := range sortedKeys(p.Prompts) {
		if pr := p.Prompts[name]; pr != nil && isNamedKey(pr.Provider) {
			refs = append(refs, callSiteRef{site: fmt.Sprintf("prompt %q", name), key: pr.Provider})
		}
	}
	for _, name := range sortedKeys(p.Compositions) {
		if comp := p.Compositions[name]; comp != nil {
			refs = appendStepRefs(refs, p, name, comp.Steps)
		}
	}
	return refs
}

// appendStepRefs adds the call-site refs of steps, and of the steps nested in
// their branches, to refs.
func appendStepRefs(refs []callSiteRef, p *pack.Pack, compName string, steps []*composition.Step) []callSiteRef {
	for _, step := range steps {
		if step == nil {
			continue
		}
		refs = appendStepRefs(refs, p, compName, step.Branches)
		isAgent := step.Kind == composition.KindAgent
		if !isAgent && step.Kind != composition.KindPrompt {
			continue
		}
		key := callProviderKey(p, step.PromptTask, step.Provider)
		// A prompt step that inherits its prompt's key is already checked as
		// that prompt.
		if !isNamedKey(key) || (step.Provider == "" && !isAgent) {
			continue
		}
		refs = append(refs, callSiteRef{
			site:      fmt.Sprintf("composition %q step %q", compName, step.ID),
			key:       key,
			needTools: isAgent,
		})
	}
	return refs
}

func isNamedKey(key string) bool {
	return key != "" && key != rtprompt.RequirementKeyDefault
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// callProvider returns the provider that runs this conversation's prompt.
func (c *Conversation) callProvider() providers.Provider {
	if c.provider != nil {
		return c.provider
	}
	return c.config.getAgentProvider()
}

// resolvePromptProvider resolves the provider for an opened prompt and records
// it on the conversation. checkCallProviders has already validated the key.
func (c *Conversation) resolvePromptProvider() (providers.Provider, error) {
	key := callProviderKey(c.pack, c.promptName, "")
	prov, err := resolveCallProvider(c.config, key)
	if err != nil {
		return nil, fmt.Errorf("prompt %q: provider %q: %w", c.promptName, key, err)
	}
	c.provider = prov
	return prov, nil
}
