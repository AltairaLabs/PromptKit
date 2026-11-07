package handlers

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/template"
)

// Eval-context metadata a host sets to give the LLM-judge checks a default
// prompt. Arena sets both from its judge_defaults config block.
const (
	// MetadataJudgeDefaults holds a map with a "prompt" key: the name (task
	// type) of a prompt in the MetadataPromptRegistry registry whose rendered
	// system template the judge checks use as their system prompt when a check
	// sets no system_prompt of its own.
	MetadataJudgeDefaults = "judge_defaults"

	// MetadataPromptRegistry holds the *prompt.Registry the judge_defaults
	// prompt is loaded from.
	MetadataPromptRegistry = "prompt_registry"

	judgeDefaultsPromptKey = "prompt"
)

// judgeDefaultSystemPrompt resolves the system prompt named by
// judge_defaults.prompt, rendered with the check's criteria and rubric, the
// content under judgment as response, and the session transcript as
// conversation. It returns "" when no default prompt is named, and an error
// when one is named but cannot be loaded or rendered: a judge silently grading
// with the built-in prompt instead of the one configured would score against
// different instructions with nothing to show for it.
func judgeDefaultSystemPrompt(evalCtx *evals.EvalContext, opts *JudgeOpts) (string, error) {
	name := judgeDefaultsPromptName(evalCtx)
	if name == "" {
		return "", nil
	}
	reg, _ := evalCtx.Metadata[MetadataPromptRegistry].(*prompt.Registry)
	if reg == nil {
		return "", fmt.Errorf("judge_defaults.prompt %q: no prompt registry in eval metadata %q to load it from",
			name, MetadataPromptRegistry)
	}
	vars := map[string]string{
		pnCriteria:     opts.Criteria,
		pnRubric:       opts.Rubric,
		"response":     opts.Content,
		"conversation": renderSessionTranscript(evalCtx),
	}
	// Load once and render the loaded template here, the way
	// Registry.LoadWithVars does, so a render failure carries its reason. The
	// registry caches the prompt config and its fragments; the rendered text
	// depends on this call's vars, so it is not cached.
	tmpl, err := reg.LoadTemplate(name, vars, "")
	if err != nil {
		return "", fmt.Errorf("judge_defaults.prompt %q: %w", name, err)
	}
	rendered, err := template.NewRenderer().Render(tmpl.RawTemplate, tmpl.DefaultVars)
	if err != nil {
		return "", fmt.Errorf("judge_defaults.prompt %q: rendering the template: %w", name, err)
	}
	return rendered, nil
}

// judgeDefaultsPromptName reads judge_defaults.prompt from eval metadata.
func judgeDefaultsPromptName(evalCtx *evals.EvalContext) string {
	if evalCtx == nil || evalCtx.Metadata == nil {
		return ""
	}
	switch d := evalCtx.Metadata[MetadataJudgeDefaults].(type) {
	case map[string]any:
		s, _ := d[judgeDefaultsPromptKey].(string)
		return s
	case map[string]string:
		return d[judgeDefaultsPromptKey]
	}
	return ""
}
