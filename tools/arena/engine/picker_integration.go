package engine

import (
	"sort"
	"strings"
)

// ProviderInfo is a lightweight view of a loaded provider for picker UIs.
type ProviderInfo struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Model string `json:"model,omitempty"`
}

// ScenarioInfo is a lightweight view of a loaded scenario for picker UIs.
type ScenarioInfo struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

// ListProviders returns IDs of all loaded providers, sorted to put mock-style
// providers first (so a picker UI can default to a no-cost option) and the rest
// alphabetically afterwards.
func (e *Engine) ListProviders() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(e.providers))
	for _, p := range e.providers {
		out = append(out, ProviderInfo{ID: p.ID, Type: p.Type, Model: p.Model})
	}
	isMock := func(t string) bool { return strings.Contains(strings.ToLower(t), "mock") }
	sort.SliceStable(out, func(i, j int) bool {
		mi, mj := isMock(out[i].Type), isMock(out[j].Type)
		if mi != mj {
			return mi
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ListScenarios returns IDs and descriptions of all loaded scenarios, sorted by ID.
func (e *Engine) ListScenarios() []ScenarioInfo {
	out := make([]ScenarioInfo, 0, len(e.scenarios))
	for _, s := range e.scenarios {
		out = append(out, ScenarioInfo{ID: s.ID, Description: s.Description})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
