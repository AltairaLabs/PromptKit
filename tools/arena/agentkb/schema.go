package agentkb

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed schemas/*.json schemas/common/*.json
var schemasFS embed.FS

// SchemaNames lists the top-level config schema type names (sorted), excluding the
// common/ $ref directory.
func SchemaNames() ([]string, error) {
	entries, err := schemasFS.ReadDir("schemas")
	if err != nil {
		return nil, fmt.Errorf("read schemas dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names, nil
}

// Schema returns the raw JSON schema bytes for a config type (e.g. "scenario").
func Schema(name string) ([]byte, error) {
	b, err := schemasFS.ReadFile("schemas/" + name + ".json")
	if err != nil {
		return nil, fmt.Errorf("unknown schema %q: %w", name, err)
	}
	return b, nil
}
