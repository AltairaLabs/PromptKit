//go:build ignore

// Command gen writes the generated authoring-agent pack to the benchmark dir.
// Invoked by `go generate ./tools/arena/agentkb/...`.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
)

func main() {
	out := filepath.Join("..", "..", "..", "examples", "test-a-codegen-agent", "packs", "authoring-agent.yaml")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(out, agentkb.AuthoringPackYAML(), 0o644); err != nil {
		log.Fatal(err)
	}
}
