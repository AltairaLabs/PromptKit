package agentkb

import (
	"embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed catalog.yaml
var catalogFS embed.FS

// CatalogEntry indexes one example/template the agent can discover.
type CatalogEntry struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Source      string   `yaml:"source"` // "builtin:<name>" or "remote:<ref>"
	Concepts    []string `yaml:"concepts"`
	Tags        []string `yaml:"tags"`
}

// Catalog is the embedded example index.
type Catalog struct {
	Entries []CatalogEntry `yaml:"entries"`
}

// LoadCatalog parses the embedded catalog.yaml.
func LoadCatalog() (Catalog, error) {
	raw, err := catalogFS.ReadFile("catalog.yaml")
	if err != nil {
		return Catalog{}, fmt.Errorf("read catalog: %w", err)
	}
	return parseCatalog(raw)
}

func parseCatalog(raw []byte) (Catalog, error) {
	var cat Catalog
	if err := yaml.Unmarshal(raw, &cat); err != nil {
		return Catalog{}, fmt.Errorf("parse catalog: %w", err)
	}
	return cat, nil
}
