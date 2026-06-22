package agentkb

import (
	"bytes"
	_ "embed"
	"strings"
)

const skillFrontmatter = `---
name: promptarena-authoring
description: Author valid PromptArena kit configs; use when building or editing scenarios, providers, prompts, tools.
---
`

//go:embed skill_spine.md
var spineMD []byte

// Skill assembles a SKILL.md from the authored lifecycle spine plus the embedded
// concepts (the idioms). Both are authored sources, so the skill can never drift
// from them.
func Skill() ([]byte, error) {
	cs, err := Concepts()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(skillFrontmatter)
	b.WriteByte('\n')
	b.Write(spineMD)
	if !bytes.HasSuffix(spineMD, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString("\n## Idioms\n\n")
	for _, c := range cs {
		b.WriteString("### ")
		b.WriteString(c.Title)
		b.WriteString("\n\n")
		b.WriteString(c.Body)
		if !strings.HasSuffix(c.Body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// AgentsBrief returns the cross-agent AGENTS.md shim written into a scaffolded
// project. The marker comment lets init detect a prior brief and avoid duplicating.
func AgentsBrief() []byte {
	return []byte(agentsBrief)
}

const agentsBrief = `<!-- promptarena-authoring -->
# PromptArena — notes for AI coding agents

You are in a PromptArena kit. Read the authoring skill first:
` + "`.claude/skills/promptarena-authoring/SKILL.md`" + `, and the bundled catalogs in
` + "`.claude/skills/promptarena-authoring/reference/`" + ` (evals-and-assertions,
config-fields, cli) — prefer them over repeatedly calling ` + "`promptarena explain`/`schema`" + `.

Workflow (full detail in SKILL.md): define measurable success → map to concepts → draw
the tool scope line → scaffold to the canonical layout → build against mocks → run
(TUI in a separate terminal; ` + "`serve`" + ` as a background task; or ` + "`--ci`" + `) →
real providers last → deploy.

Idiom traps:
- Mock response keys match the scenario's ` + "`metadata.name`" + `, not ` + "`spec.id`" + `.
- Thresholds live on the ` + "`type: assertion`" + ` wrapper, never on the inner eval.
- A pack ships tool **definitions** + bindings, never tool implementations.

Do not gold-plate: working configs and passing measures first; no fancy docs until the
kit runs green. Validate with ` + "`promptarena validate`" + ` before ` + "`promptarena run`" + `.
`
