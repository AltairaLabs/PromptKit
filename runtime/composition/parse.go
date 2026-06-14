package composition

import (
	"encoding/json"
	"fmt"
)

// ParseConfig parses an untyped compositions map (typically config.Compositions,
// stored as interface{}) into typed Compositions keyed by name. Returns nil, nil
// when raw is nil. Mirrors workflow.ParseConfig.
func ParseConfig(raw interface{}) (map[string]*Composition, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshaling compositions config: %w", err)
	}
	var comps map[string]*Composition
	if err := json.Unmarshal(data, &comps); err != nil {
		return nil, fmt.Errorf("parsing compositions config: %w", err)
	}
	return comps, nil
}
