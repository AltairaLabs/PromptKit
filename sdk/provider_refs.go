package sdk

import (
	"errors"
	"fmt"
	"maps"
	"slices"

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
	return newCallProviderCheck(p).run(cfg)
}

// callProviderCheck is the pack-only half of checkCallProviders: which call
// sites name which keys, and what the pack declares. It depends on nothing a
// host supplies, so a PackTemplate builds it once and runs it per Open.
type callProviderCheck struct {
	refs     []callSiteRef
	declared map[string]rtprompt.ResolvedRequirement
	err      error
}

func newCallProviderCheck(p *pack.Pack) *callProviderCheck {
	c := &callProviderCheck{refs: collectCallSiteRefs(p)}
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

func checkCallSiteRef(ref callSiteRef, declared map[string]rtprompt.ResolvedRequirement, cfg *config) string {
	req, ok := declared[ref.key]
	if !ok {
		return fmt.Sprintf("%s names provider %q, which the pack does not declare in requires "+
			"(declared: %s)", ref.site, ref.key, describeKeys(declared))
	}
	if req.Role != rtprompt.RequirementRoleLLM {
		return fmt.Sprintf("%s names provider %q, which the pack declares with role %q; "+
			"a prompt or step runs on an llm provider", ref.site, ref.key, req.Role)
	}
	prov, err := resolveCallProvider(cfg, ref.key)
	switch {
	case errors.Is(err, evals.ErrNoBinding):
		return fmt.Sprintf("%s names provider %q and this conversation has no providers wired at all",
			ref.site, ref.key)
	case errors.Is(err, evals.ErrWrongKind):
		return fmt.Sprintf("%s names provider %q, and the host bound an inference provider to it; "+
			"a prompt or step needs an LLM provider (WithNamedProvider)", ref.site, ref.key)
	case err != nil:
		return fmt.Sprintf("%s names provider %q, which the pack declares and the host bound nothing to; "+
			"bind one with WithNamedProvider under that id", ref.site, ref.key)
	}
	if ref.needTools {
		if _, ok := prov.(providers.ToolSupport); !ok {
			return fmt.Sprintf("%s uses tools and names provider %q, which the host bound to %T, "+
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
	stateTasks := workflowStateTasks(p)
	for _, name := range sortedKeys(p.Prompts) {
		if pr := p.Prompts[name]; pr != nil && isNamedKey(pr.Provider) {
			// A workflow state's prompt needs tools even when it declares
			// none: the transition tool is offered to it, and a mid-turn
			// handoff reaches it with the transition call in the history.
			refs = append(refs, callSiteRef{
				site: fmt.Sprintf("prompt %q", name), key: pr.Provider,
				needTools: len(pr.Tools) > 0 || stateTasks[name],
			})
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

// packNeedsAgent reports whether any call in the pack can run on the agent
// provider: a prompt that names no key, or a composition prompt/agent step
// whose step and prompt name none. When none can, a host that bound every key
// it names need not supply an agent, and Open does not go looking for one.
func packNeedsAgent(p *pack.Pack) bool {
	if p == nil {
		return true
	}
	for _, pr := range p.Prompts {
		if pr == nil || !isNamedKey(pr.Provider) {
			return true
		}
	}
	for _, comp := range p.Compositions {
		if comp != nil && stepsNeedAgent(p, comp.Steps) {
			return true
		}
	}
	return false
}

func stepsNeedAgent(p *pack.Pack, steps []*composition.Step) bool {
	for _, step := range steps {
		if step == nil {
			continue
		}
		if stepsNeedAgent(p, step.Branches) {
			return true
		}
		isCall := step.Kind == composition.KindAgent || step.Kind == composition.KindPrompt
		if isCall && !isNamedKey(callProviderKey(p, step.PromptTask, step.Provider)) {
			return true
		}
	}
	return false
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
	if !packNeedsAgent(p) && cfg.apiKey == "" && cfg.model == "" && cfg.platform == nil && cfg.credential == nil {
		return nil
	}
	_, err := resolveProvider(cfg)
	return err
}

// workflowStateTasks returns the prompt tasks the pack's workflow states run.
func workflowStateTasks(p *pack.Pack) map[string]bool {
	tasks := map[string]bool{}
	if p.Workflow == nil {
		return tasks
	}
	for _, st := range p.Workflow.States {
		if st != nil && st.PromptTask != "" {
			tasks[st.PromptTask] = true
		}
	}
	return tasks
}

func isNamedKey(key string) bool {
	return key != "" && key != rtprompt.RequirementKeyDefault
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
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

// resolvePromptProvider resolves the provider for an opened prompt and records
// it on the conversation. checkCallProviders has already validated the key.
func (c *Conversation) resolvePromptProvider() (providers.Provider, error) {
	prov, err := resolvePromptCallProvider(c.pack, c.config, c.promptName)
	if err != nil {
		return nil, err
	}
	c.provider = prov
	return prov, nil
}

// resolvePromptCallProvider returns the provider a call to task's prompt runs
// on, outside any composition step: the one place Open and a workflow handoff
// both pick it.
func resolvePromptCallProvider(p *pack.Pack, cfg *config, task string) (providers.Provider, error) {
	key := callProviderKey(p, task, "")
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
