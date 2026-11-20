package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// compileStepValidators compiles the validators of every prompt the active
// composition's prompt and agent steps run, as an open compiles the opened
// prompt's, so a step's guardrails run and one that cannot be built fails the
// open. A composition opens no prompt of its own, so the conversation's hooks
// are the host's alone and a step's registry adds its prompt's validators to
// them once.
func compileStepValidators(p *pack.Pack, cfg *config) error {
	cfg.stepValidatorHooks = nil
	if cfg.activeComposition == nil {
		return nil
	}
	compiled := map[string][]hooks.ProviderHook{}
	if err := compileStepsValidators(p, cfg, cfg.activeComposition.Steps, compiled); err != nil {
		return err
	}
	cfg.stepValidatorHooks = compiled
	return nil
}

// compileStepsValidators adds to compiled the validators of each prompt steps
// (and their parallel branches) run that it does not hold yet.
func compileStepsValidators(
	p *pack.Pack, cfg *config, steps []*composition.Step, compiled map[string][]hooks.ProviderHook,
) error {
	for _, step := range steps {
		if step == nil {
			continue
		}
		if err := compileStepsValidators(p, cfg, step.Branches, compiled); err != nil {
			return err
		}
		if _, done := compiled[step.PromptTask]; done {
			continue
		}
		prompt := p.Prompts[step.PromptTask]
		if step.PromptTask == "" || prompt == nil {
			continue
		}
		built, err := compilePackValidators(prompt, cfg)
		if err != nil {
			return fmt.Errorf("composition step %q: prompt %q: %w", step.ID, step.PromptTask, err)
		}
		compiled[step.PromptTask] = built
	}
	return nil
}

// stepHookRegistries returns, for a step's prompt task, the registry its
// provider stage runs: the step prompt's validators ahead of the host's hooks,
// as an open puts the opened prompt's. Nil for a task with no validators, so
// the step runs the conversation's registry.
func (c *config) stepHookRegistries() func(promptTask string) *hooks.Registry {
	registries := map[string]*hooks.Registry{}
	for task, validators := range c.stepValidatorHooks {
		if len(validators) == 0 {
			continue
		}
		registries[task] = c.buildHookRegistryWith(
			append(append([]hooks.ProviderHook(nil), validators...), c.providerHooks...))
	}
	if len(registries) == 0 {
		return nil
	}
	return func(promptTask string) *hooks.Registry { return registries[promptTask] }
}
