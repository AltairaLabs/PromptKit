package sdk

import (
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// RFC 0017 lets a prompt, or a composition prompt/agent step, name the
// requires.providers key that runs it. rtprompt.CallProviderKey is the one rule
// that picks it, shared with every other host (Arena).
//
// A key other than `default` resolves through the host binding, exactly as a
// check's `provider` param does: the pack names a logical key and the host
// decides what serves it.

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
		key := rtprompt.CallProviderKey(p, step.PromptTask, step.Provider)
		prov, err := resolveCallProvider(cfg, key)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", key, err)
		}
		return prov, nil
	}
}

// checkLoadGates runs every load-time provider gate, in one order for both
// sdk.Open and PackTemplate.Open so the two paths cannot drift:
//
//  1. checks' provider keys (checkProviderKeys) — first, because when a gate
//     below fails on the same missing provider, this one says more: which
//     check wanted it, and whether what the host bound is missing or merely
//     unsuitable;
//  2. RFC 0017 call sites, across the whole pack — before the requirements
//     gate, which only warns about an unbound optional requirement that a
//     call site cannot run without. A workflow transition re-opens the pack
//     with the bindings its first Open checked, and skips it;
//  3. RFC 0012 requirements (checkProviderRequirements) — what nothing above
//     references.
//
// calls is the pack's prebuilt call-site check, or nil to build it here only
// when it runs.
func checkLoadGates(p *pack.Pack, prompt *pack.Prompt, cfg *config, calls *callProviderCheck) error {
	if err := checkProviderKeys(p, prompt, cfg); err != nil {
		return err
	}
	if !cfg.callProvidersChecked {
		if calls == nil {
			calls = newCallProviderCheck(p)
		}
		if err := calls.run(cfg); err != nil {
			return err
		}
	}
	return checkProviderRequirements(p, cfg)
}

// The RFC 0017 call-site check validates, at Open, every provider key a call site in the
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
//
// callProviderCheck is its pack-only half: which call sites name which keys,
// and what the pack declares. It depends on nothing a host supplies, so a
// PackTemplate builds it once and runs it per Open.
type callProviderCheck struct {
	refs     []rtprompt.CallSite
	declared map[string]rtprompt.ResolvedRequirement
	err      error
}

func newCallProviderCheck(p *pack.Pack) *callProviderCheck {
	c := &callProviderCheck{refs: rtprompt.CallSites(p)}
	if len(c.refs) > 0 {
		c.declared, c.err = declaredRequirementKeys(p)
	}
	return c
}

// run checks the call sites against the providers cfg binds.
func (c *callProviderCheck) run(cfg *config) error {
	if c.err != nil {
		return c.err
	}
	var problems []string
	for _, ref := range c.refs {
		if msg := checkCallSiteRef(ref, c.declared, cfg); msg != "" {
			problems = append(problems, msg)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w — pack call sites cannot resolve their providers:\n  - %s",
			errProviderKeys, joinProblems(problems))
	}
	return nil
}

func checkCallSiteRef(ref rtprompt.CallSite, declared map[string]rtprompt.ResolvedRequirement, cfg *config) string {
	req, ok := declared[ref.Key]
	if !ok {
		return fmt.Sprintf("%s names provider %q, which the pack does not declare in requires "+
			"(declared: %s)", ref.Site, ref.Key, describeKeys(declared))
	}
	if req.Role != rtprompt.RequirementRoleLLM {
		return fmt.Sprintf("%s names provider %q, which the pack declares with role %q; "+
			"a prompt or step runs on an llm provider", ref.Site, ref.Key, req.Role)
	}
	prov, err := resolveCallProvider(cfg, ref.Key)
	switch {
	case errors.Is(err, evals.ErrNoBinding):
		return fmt.Sprintf("%s names provider %q and this conversation has no providers wired at all",
			ref.Site, ref.Key)
	case errors.Is(err, evals.ErrWrongKind):
		return fmt.Sprintf("%s names provider %q, and the host bound an inference provider to it; "+
			"a prompt or step needs an LLM provider (WithNamedProvider)", ref.Site, ref.Key)
	case err != nil:
		return fmt.Sprintf("%s names provider %q, which the pack declares and the host bound nothing to; "+
			"bind one with WithNamedProvider under that id", ref.Site, ref.Key)
	}
	if ref.NeedsTools {
		if _, ok := prov.(providers.ToolSupport); !ok {
			return fmt.Sprintf("%s uses tools and names provider %q, which the host bound to %T, "+
				"a provider without tool support", ref.Site, ref.Key, prov)
		}
	}
	return ""
}

// resolveAgentProvider resolves the agent provider as resolveProvider does,
// except that it does not auto-detect one when no call in the pack can use it
// and the host asked for nothing an agent would be built from (an API key, a
// model, a platform or a credential). Detecting then would only pick up
// whatever key happens to be in the environment, or fail Open for a provider
// no call needs.
func resolveAgentProvider(cfg *config, p *pack.Pack) error {
	if cfg.getAgentProvider() != nil {
		return nil
	}
	askedForAgent := cfg.apiKey != "" || cfg.model != "" || cfg.platform != nil || cfg.credential != nil
	if !rtprompt.NeedsDefaultProvider(p) && !askedForAgent {
		return nil
	}
	_, err := resolveProvider(cfg)
	return err
}

// callProvider returns the provider that runs this conversation's prompt.
func (c *Conversation) callProvider() providers.Provider {
	if c.provider != nil {
		return c.provider
	}
	return c.config.getAgentProvider()
}

// callModel returns the model of the provider that runs this conversation's
// prompt, which selects its model_overrides entry; "" when there is none.
func (c *Conversation) callModel() string {
	if prov := c.callProvider(); prov != nil {
		return prov.Model()
	}
	return ""
}

// resolvePromptCallProvider returns the provider a call to task's prompt runs
// on, outside any composition step: the one place Open and a workflow handoff
// both pick it.
func resolvePromptCallProvider(p *pack.Pack, cfg *config, task string) (providers.Provider, error) {
	key := rtprompt.CallProviderKey(p, task, "")
	prov, err := resolveCallProvider(cfg, key)
	if err != nil {
		return nil, fmt.Errorf("prompt %q: provider %q: %w", task, key, err)
	}
	return prov, nil
}

// withCallProvidersChecked marks the call-site provider check as already done
// for these bindings. Internal: only a workflow transition, re-opening the
// pack its first Open checked, sets it.
func withCallProvidersChecked() Option {
	return func(c *config) error {
		c.callProvidersChecked = true
		return nil
	}
}
