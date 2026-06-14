package composition

import "regexp"

// refRe matches a ${root.path...} template reference and captures the leading
// root token (the part before the first dot or the closing brace).
var refRe = regexp.MustCompile(`\$\{\s*([a-zA-Z_][a-zA-Z0-9_]*)[^}]*\}`)

// collectRefRoots walks an arbitrary JSON-ish value (string, map, slice) and
// returns the deduplicated set of root tokens of every ${...} reference found.
// Roots are "input" or a step id; resolution against those is done by Validate.
func collectRefRoots(v any) []string {
	seen := map[string]struct{}{}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			for _, m := range refRe.FindAllStringSubmatch(t, -1) {
				seen[m[1]] = struct{}{}
			}
		case map[string]any:
			for _, val := range t {
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		}
	}
	walk(v)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
