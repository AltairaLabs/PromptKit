package schema

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxSuggestionDistance = 2
	shortTargetLen        = 3
)

// levenshtein computes the edit distance between a and b using the classic
// dynamic-programming algorithm.
func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// nearestMatches returns candidates within Levenshtein distance of target,
// sorted by distance ascending then lexicographically. Targets of length
// <= shortTargetLen require distance <= 1.
func nearestMatches(target string, candidates []string) []string {
	if target == "" || len(candidates) == 0 {
		return nil
	}
	limit := maxSuggestionDistance
	if len(target) <= shortTargetLen {
		limit = 1
	}
	type scored struct {
		name string
		dist int
	}
	matches := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		d := levenshtein(target, c)
		if d > 0 && d <= limit {
			matches = append(matches, scored{name: c, dist: d})
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].dist != matches[j].dist {
			return matches[i].dist < matches[j].dist
		}
		return matches[i].name < matches[j].name
	})
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.name
	}
	return out
}

// lookupProperties returns the names of properties valid at fieldPath in the
// given parsed schema document. fieldPath uses the gojsonschema convention:
// "(root)" for the document root, dot-separated for nested paths. Resolves
// single-layer $ref pointers to #/definitions/X and #/$defs/X. Returns nil
// when the path cannot be resolved (oneOf/anyOf/allOf, unknown $ref, or
// missing intermediate node).
func lookupProperties(root map[string]any, fieldPath string) []string {
	node, ok := resolveSchemaAt(root, fieldPath)
	if !ok {
		return nil
	}
	props, ok := node["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	return names
}

// lookupEnumValues returns the enum allowed-set at fieldPath as strings.
// Numeric and other JSON values are coerced via fmt.Sprint. Returns nil if
// the path cannot be resolved or the node lacks an `enum` array.
func lookupEnumValues(root map[string]any, fieldPath string) []string {
	node, ok := resolveSchemaAt(root, fieldPath)
	if !ok {
		return nil
	}
	raw, ok := node["enum"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

// resolveSchemaAt navigates root by dot-separated fieldPath and returns the
// schema node at that location, with $ref resolved. Reports false when the
// path cannot be resolved.
func resolveSchemaAt(root map[string]any, fieldPath string) (map[string]any, bool) {
	current := root
	if fieldPath != "(root)" && fieldPath != "" {
		for _, seg := range strings.Split(fieldPath, ".") {
			next, ok := descendInto(root, current, seg)
			if !ok {
				return nil, false
			}
			current = next
		}
	}
	return resolveRef(root, current)
}

// descendInto returns the schema node for property `name` within `node`.
// Resolves $ref on `node` first, then rejects conditional schemas
// (oneOf/anyOf/allOf) because we can't pick a branch unambiguously.
func descendInto(root, node map[string]any, name string) (map[string]any, bool) {
	resolved, ok := resolveRef(root, node)
	if !ok {
		return nil, false
	}
	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		if _, has := resolved[k]; has {
			return nil, false
		}
	}
	props, ok := resolved["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	child, ok := props[name].(map[string]any)
	if !ok {
		return nil, false
	}
	return child, true
}

// resolveRef follows a single layer of $ref pointing at #/definitions/X or
// #/$defs/X. Returns (node, true) unchanged when there is no $ref. Returns
// (nil, false) when the ref is present but unresolvable.
func resolveRef(root, node map[string]any) (map[string]any, bool) {
	ref, ok := node["$ref"].(string)
	if !ok {
		return node, true
	}
	const (
		defsPrefix       = "#/definitions/"
		dollarDefsPrefix = "#/$defs/"
	)
	var name, bucket string
	switch {
	case strings.HasPrefix(ref, defsPrefix):
		name = strings.TrimPrefix(ref, defsPrefix)
		bucket = "definitions"
	case strings.HasPrefix(ref, dollarDefsPrefix):
		name = strings.TrimPrefix(ref, dollarDefsPrefix)
		bucket = "$defs"
	default:
		return nil, false
	}
	bucketMap, ok := root[bucket].(map[string]any)
	if !ok {
		return nil, false
	}
	target, ok := bucketMap[name].(map[string]any)
	if !ok {
		return nil, false
	}
	return target, true
}

// LookupProperties is the exported wrapper for cross-package callers.
func LookupProperties(root map[string]any, fieldPath string) []string {
	return lookupProperties(root, fieldPath)
}

// LookupEnumValues is the exported wrapper for cross-package callers.
func LookupEnumValues(root map[string]any, fieldPath string) []string {
	return lookupEnumValues(root, fieldPath)
}

// NearestMatches is the exported wrapper for cross-package callers.
func NearestMatches(target string, candidates []string) []string {
	return nearestMatches(target, candidates)
}
