//go:build ignore

// Command gen writes the generated authoring-agent artifacts to the
// test-a-codegen-agent example: the authoring pack (system prompt = the AGENTS.md
// brief) and the full promptarena-authoring skill (loaded via the example's
// `skills:` config so the agent gets a skill__activate tool). Both are kept in
// sync with agentkb by parity tests. Invoked by `go generate ./tools/arena/agentkb/...`.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
)

func main() {
	root := filepath.Join("..", "..", "..", "examples", "test-a-codegen-agent")

	pack := filepath.Join(root, "prompts", "authoring-agent.yaml")
	if err := os.MkdirAll(filepath.Dir(pack), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(pack, agentkb.AuthoringPackYAML(), 0o644); err != nil {
		log.Fatal(err)
	}

	skill, err := agentkb.Skill()
	if err != nil {
		log.Fatal(err)
	}
	skillPath := filepath.Join(root, "skills", "promptarena-authoring", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(skillPath, skill, 0o644); err != nil {
		log.Fatal(err)
	}
}
