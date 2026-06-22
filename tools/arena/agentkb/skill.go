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

You are working in a PromptArena kit. Before authoring configs:

- Read the authoring skill at ` + "`.claude/skills/promptarena-authoring/SKILL.md`" + `.
- Discover idioms and examples on demand:
  ` + "`promptarena explain --list`" + `, ` + "`promptarena explain <id>`" + `,
  ` + "`promptarena examples list`" + `, ` + "`promptarena examples show <name>`" + `.
- Run ` + "`promptarena schema <type>`" + ` for the authoritative structure of a
  scenario, provider, prompt, tool, or arena config. The embedded schema is the
  version this binary's ` + "`promptarena validate`" + ` enforces — prefer it over
  any web copy.
- Run ` + "`promptarena validate`" + ` to check your work before ` + "`promptarena run`" + `.
- Mock providers simulate the LLM only; tools execute for real. Mock response keys
  match the scenario's ` + "`metadata.name`" + `, not ` + "`spec.id`" + `.
- Assertions apply thresholds; eval handlers emit raw scores. Keep thresholds on the
  ` + "`type: assertion`" + ` wrapper, never on the eval.
`
