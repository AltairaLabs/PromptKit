package agentkb

import (
	"bytes"
	"embed"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default handlers
)

//go:embed evalmeta.yaml
var evalMetaFS embed.FS

type evalHandlerMeta struct {
	Level       string `yaml:"level"`
	Description string `yaml:"description"`
	Score       string `yaml:"score"`
}

type evalMeta struct {
	Handlers map[string]evalHandlerMeta `yaml:"handlers"`
}

func loadEvalMeta() (evalMeta, error) {
	b, err := evalMetaFS.ReadFile("evalmeta.yaml")
	if err != nil {
		return evalMeta{}, fmt.Errorf("read evalmeta.yaml: %w", err)
	}
	var m evalMeta
	if err := yaml.Unmarshal(b, &m); err != nil {
		return evalMeta{}, fmt.Errorf("parse evalmeta.yaml: %w", err)
	}
	return m, nil
}

// canonicalEvalTypes returns the registry's eval type names with alias names
// removed, sorted. These are the ids evalmeta.yaml must describe exactly.
func canonicalEvalTypes() []string {
	aliasNames := map[string]bool{}
	for _, pair := range evals.DefaultAliases() {
		aliasNames[pair[0]] = true
	}
	var out []string
	for _, t := range evals.NewEvalTypeRegistry().Types() {
		if !aliasNames[t] {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// GenerateEvalsReference renders the unified eval/assertion catalog markdown
// from the live handler registry + evalmeta.yaml + the alias table.
func GenerateEvalsReference() ([]byte, error) {
	meta, err := loadEvalMeta()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString("# Evals & Assertions\n\n")
	b.WriteString("Generated from the handler registry — do not edit by hand; run the reference generator.\n\n")
	b.WriteString("An **eval** emits a raw `Score` (0..1) or a boolean gate. A **`type: assertion`** ")
	b.WriteString("wrapper applies the pass/fail **threshold** (`min_score`/`max_score`). ")
	b.WriteString("Never put a threshold on the inner eval.\n\n")
	b.WriteString("| id | level | score | description |\n|----|-------|-------|-------------|\n")

	for _, id := range canonicalEvalTypes() {
		m := meta.Handlers[id]
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", id, dash(m.Level), dash(m.Score), dash(m.Description))
	}

	b.WriteString("\n## Aliases\n\nLegacy names that map to a canonical handler:\n\n")
	b.WriteString("| alias | canonical |\n|-------|-----------|\n")
	for _, pair := range evals.DefaultAliases() {
		fmt.Fprintf(&b, "| `%s` | `%s` |\n", pair[0], pair[1])
	}
	return b.Bytes(), nil
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
