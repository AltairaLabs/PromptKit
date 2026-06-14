package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// NewSchemaResolver returns a resolver that reads a schema file path relative to
// configDir (absolute paths are used as-is). An empty path resolves to (nil, nil)
// — "no schema" — matching CompositionExecutorDeps.SchemaResolver's contract.
func NewSchemaResolver(configDir string) func(path string) (json.RawMessage, error) {
	return func(path string) (json.RawMessage, error) {
		if path == "" {
			return nil, nil //nolint:nilnil // "no schema" is a valid, expected result
		}
		if !filepath.IsAbs(path) && configDir != "" {
			path = filepath.Join(configDir, path)
		}
		b, err := os.ReadFile(path) //nolint:gosec // path is pack-author-controlled config
		if err != nil {
			return nil, fmt.Errorf("reading schema %q: %w", path, err)
		}
		return b, nil
	}
}
