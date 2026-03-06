package engine

import (
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// PerturbationVariant represents a single set of variable substitutions.
type PerturbationVariant struct {
	// Substitutions maps placeholder names to their values for this variant.
	Substitutions map[string]string
}

// ExpandPerturbations computes all perturbation variants for a scenario.
// It collects all perturbation maps across turns and computes the Cartesian product.
// Returns nil if no perturbations are defined.
func ExpandPerturbations(scenario *config.Scenario) []PerturbationVariant {
	allPerturbations := collectPerturbations(scenario.Turns)
	if len(allPerturbations) == 0 {
		return nil
	}
	return cartesianProduct(allPerturbations)
}

// collectPerturbations merges perturbation maps from all turns into a single map.
// If the same key appears in multiple turns, the values are merged (union).
func collectPerturbations(turns []config.TurnDefinition) map[string][]string {
	merged := make(map[string][]string)
	for i := range turns {
		for key, values := range turns[i].Perturbations {
			if len(values) == 0 {
				continue
			}
			merged[key] = mergeUnique(merged[key], values)
		}
	}
	return merged
}

// mergeUnique appends values from src to dst, skipping duplicates already in dst.
func mergeUnique(dst, src []string) []string {
	if len(dst) == 0 {
		return append([]string{}, src...)
	}
	seen := make(map[string]bool, len(dst))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range src {
		if !seen[v] {
			dst = append(dst, v)
		}
	}
	return dst
}

// cartesianProduct computes all combinations of perturbation values.
// For example, {city: [NYC, LA], tone: [formal, casual]} produces 4 variants.
func cartesianProduct(perturbations map[string][]string) []PerturbationVariant {
	keys := sortedKeys(perturbations)
	if len(keys) == 0 {
		return nil
	}

	variants := []PerturbationVariant{{Substitutions: make(map[string]string)}}

	for _, key := range keys {
		values := perturbations[key]
		var expanded []PerturbationVariant
		for _, variant := range variants {
			for _, val := range values {
				newSubs := make(map[string]string, len(variant.Substitutions)+1)
				for k, v := range variant.Substitutions {
					newSubs[k] = v
				}
				newSubs[key] = val
				expanded = append(expanded, PerturbationVariant{Substitutions: newSubs})
			}
		}
		variants = expanded
	}

	return variants
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ApplyPerturbation substitutes perturbation variables in turn content.
// Placeholders use {key} syntax (e.g., "Book a flight from {city}" with city=NYC
// becomes "Book a flight from NYC").
func ApplyPerturbation(turns []config.TurnDefinition, variant PerturbationVariant) []config.TurnDefinition {
	if len(variant.Substitutions) == 0 {
		return turns
	}

	result := make([]config.TurnDefinition, len(turns))
	for i := range turns {
		result[i] = turns[i]
		result[i].Content = substituteVars(turns[i].Content, variant.Substitutions)
		if len(turns[i].Parts) > 0 {
			result[i].Parts = make([]config.TurnContentPart, len(turns[i].Parts))
			for j := range turns[i].Parts {
				result[i].Parts[j] = turns[i].Parts[j]
				if turns[i].Parts[j].Type == "text" {
					result[i].Parts[j].Text = substituteVars(turns[i].Parts[j].Text, variant.Substitutions)
				}
			}
		}
	}
	return result
}

// perturbationIndexForTrial returns the perturbation variant index for a given trial.
// When perturbations are combined with trials, the trial index cycles through perturbation variants.
// Returns -1 if perturbationCount <= 1 (no perturbations).
func perturbationIndexForTrial(trialIndex, perturbationCount int) int {
	if perturbationCount <= 1 {
		return -1
	}
	return trialIndex % perturbationCount
}

// substituteVars replaces {key} placeholders with their values.
func substituteVars(content string, subs map[string]string) string {
	for key, val := range subs {
		content = strings.ReplaceAll(content, "{"+key+"}", val)
	}
	return content
}
