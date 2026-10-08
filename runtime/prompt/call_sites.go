package prompt

import (
	"fmt"
	"maps"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
)

// RFC 0017: a prompt, or a composition prompt/agent step, names the
// requires.providers key that runs it. The rule for which key runs a call is
// the same for every host (the SDK, Arena), so it lives here rather than in
// each of them:
//
//  1. the composition step's `provider`, if set;
//  2. otherwise the prompt's `provider`, if set;
//  3. otherwise RequirementKeyDefault, the host's agent provider.

// CallProviderKey returns the requires key that runs a call to promptTask,
// with stepProvider the composition step's own `provider` ("" outside a
// composition or when the step sets none).
func CallProviderKey(p *Pack, promptTask, stepProvider string) string {
	if stepProvider != "" {
		return stepProvider
	}
	if p != nil {
		if pr, ok := p.Prompts[promptTask]; ok && pr != nil && pr.Provider != "" {
			return pr.Provider
		}
	}
	return RequirementKeyDefault
}

// IsNamedProviderKey reports whether key names a requirement rather than the
// default provider.
func IsNamedProviderKey(key string) bool {
	return key != "" && key != RequirementKeyDefault
}

// CallSite is one call site's reference to a requires key.
type CallSite struct {
	// Site names the call site for an error message: `prompt "drafter"`,
	// `composition "c" step "s"`.
	Site string
	// Key is the requires key it runs on.
	Key string
	// NeedsTools is set when the call runs a tool loop (an agent step, a
	// prompt with tools, or a workflow state's prompt), so its provider must
	// support tools.
	NeedsTools bool
}

// CallSites lists, in a stable order, every call site in p that names a
// non-default key. A host checks each against its bindings before running, so
// a pack fails at load rather than at the call that reaches a bad key. An
// agent step that names no key of its own is listed when its prompt names one,
// because it is the step that needs tool support; a prompt step that inherits
// its prompt's key is already covered by that prompt.
func CallSites(p *Pack) []CallSite {
	if p == nil {
		return nil
	}
	var sites []CallSite
	stateTasks := workflowStateTasks(p)
	for _, name := range slices.Sorted(maps.Keys(p.Prompts)) {
		if pr := p.Prompts[name]; pr != nil && IsNamedProviderKey(pr.Provider) {
			// A workflow state's prompt needs tools even when it declares
			// none: the transition tool is offered to it, and a mid-turn
			// handoff reaches it with the transition call in the history.
			sites = append(sites, CallSite{
				Site: fmt.Sprintf("prompt %q", name), Key: pr.Provider,
				NeedsTools: len(pr.Tools) > 0 || stateTasks[name],
			})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Compositions)) {
		if comp := p.Compositions[name]; comp != nil {
			sites = appendStepCallSites(sites, p, name, comp.Steps)
		}
	}
	return sites
}

// appendStepCallSites adds the call sites of steps, and of the steps nested
// in their branches, to sites.
func appendStepCallSites(sites []CallSite, p *Pack, compName string, steps []*composition.Step) []CallSite {
	for _, step := range steps {
		if step == nil {
			continue
		}
		sites = appendStepCallSites(sites, p, compName, step.Branches)
		isAgent := step.Kind == composition.KindAgent
		if !isAgent && step.Kind != composition.KindPrompt {
			continue
		}
		key := CallProviderKey(p, step.PromptTask, step.Provider)
		if !IsNamedProviderKey(key) || (step.Provider == "" && !isAgent) {
			continue
		}
		sites = append(sites, CallSite{
			Site:       fmt.Sprintf("composition %q step %q", compName, step.ID),
			Key:        key,
			NeedsTools: isAgent,
		})
	}
	return sites
}

// NeedsDefaultProvider reports whether any call in p can run on the default
// provider: a prompt that names no key, or a composition prompt/agent step
// whose step and prompt name none. When none can, a host that bound every key
// p names need not supply an agent provider.
func NeedsDefaultProvider(p *Pack) bool {
	if p == nil {
		return true
	}
	for _, pr := range p.Prompts {
		if pr == nil || !IsNamedProviderKey(pr.Provider) {
			return true
		}
	}
	for _, comp := range p.Compositions {
		if comp != nil && stepsNeedDefault(p, comp.Steps) {
			return true
		}
	}
	return false
}

func stepsNeedDefault(p *Pack, steps []*composition.Step) bool {
	for _, step := range steps {
		if step == nil {
			continue
		}
		if stepsNeedDefault(p, step.Branches) {
			return true
		}
		isCall := step.Kind == composition.KindAgent || step.Kind == composition.KindPrompt
		if isCall && !IsNamedProviderKey(CallProviderKey(p, step.PromptTask, step.Provider)) {
			return true
		}
	}
	return false
}

// workflowStateTasks returns the prompt tasks p's workflow states run.
func workflowStateTasks(p *Pack) map[string]bool {
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
