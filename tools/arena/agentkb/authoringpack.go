package agentkb

import (
	"bytes"
	"strings"
)

//go:generate go run ./authoring_gen_interactive.go

// authoringPackHeader is the static PromptConfig wrapper. The brief is
// injected as the system_template block scalar so the agent's system prompt
// IS the real AGENTS.md brief (kept honest by TestAuthoringPack_CommittedFileInSync).
const authoringPackHeader = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: authoring-agent
spec:
  task_type: authoring-agent
  version: "v1.0.0"
  description: "PromptArena kit authoring agent (Arm C — brief-equipped)."
  system_template: |
`

const authoringPackFooter = `  allowed_tools:
    - Read
    - Edit
    - Write
    - Glob
    - Grep
    - Bash
`

// AuthoringPackYAML returns the full authoring-agent PromptConfig YAML with
// AgentsBrief() embedded as the system_template block scalar.
func AuthoringPackYAML() []byte {
	var b bytes.Buffer
	b.WriteString(authoringPackHeader)
	// Indent each brief line by 4 spaces to sit under `system_template: |`.
	for _, line := range strings.Split(string(AgentsBrief()), "\n") {
		if line == "" {
			b.WriteByte('\n')
			continue
		}
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(authoringPackFooter)
	return b.Bytes()
}
