package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// typeNull is the JSON-Schema type used to mark a scalar nullable.
const typeNull = "null"

// Node is a JSON-Schema node, parsed loosely enough to inspect every keyword
// present — including ones this generator does not understand, which is what
// lets Coverage report them rather than silently ignore them.
type Node struct {
	Raw map[string]any
}

func (n Node) Has(k string) bool { _, ok := n.Raw[k]; return ok }

func (n Node) Str(k string) string {
	if v, ok := n.Raw[k].(string); ok {
		return v
	}
	return ""
}

func (n Node) Child(k string) (Node, bool) {
	m, ok := n.Raw[k].(map[string]any)
	return Node{Raw: m}, ok
}

// Types returns the declared type(s). The spec uses a list in exactly one place
// (Parameters.top_k is ["integer","null"]), which is how a nullable scalar is
// expressed and must become a pointer in Go.
func (n Node) Types() []string {
	switch v := n.Raw["type"].(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, t := range v {
			if s, ok := t.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func (n Node) Nullable() bool {
	for _, t := range n.Types() {
		if t == typeNull {
			return true
		}
	}
	return false
}

// PrimaryType is the non-null type.
func (n Node) PrimaryType() string {
	for _, t := range n.Types() {
		if t != typeNull {
			return t
		}
	}
	return ""
}

func (n Node) Required() map[string]bool {
	out := map[string]bool{}
	if list, ok := n.Raw["required"].([]any); ok {
		for _, r := range list {
			if s, ok := r.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// PropertyNames returns property keys in a stable order so generated output is
// deterministic — a generator whose output reorders between runs cannot be
// gated with `git diff --exit-code`.
func (n Node) PropertyNames() []string {
	props, ok := n.Raw["properties"].(map[string]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(props))
	for k := range props {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (n Node) Property(name string) Node {
	props, _ := n.Raw["properties"].(map[string]any)
	m, _ := props[name].(map[string]any)
	return Node{Raw: m}
}

type Schema struct {
	Root Node
	Defs map[string]Node
	// DefNames is sorted, for deterministic emission.
	DefNames []string
}

func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a build-time argument
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	s := &Schema{Root: Node{Raw: root}, Defs: map[string]Node{}}
	defs, _ := root[kwDefs].(map[string]any)
	for name, raw := range defs {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$defs/%s is not an object", name)
		}
		s.Defs[name] = Node{Raw: m}
		s.DefNames = append(s.DefNames, name)
	}
	sort.Strings(s.DefNames)
	return s, nil
}

// WalkKeywords records every keyword in the document against the coverage
// ledger. It walks the raw map rather than a typed model on purpose: a typed
// model can only see the keywords it was written to hold, which is exactly the
// blindness this is meant to remove.
func (s *Schema) WalkKeywords(c *Coverage) {
	s.walkNode(c, s.Root.Raw, "")
}

func (s *Schema) walkNode(c *Coverage, v any, path string) {
	switch t := v.(type) {
	case map[string]any:
		s.walkObject(c, t, path)
	case []any:
		for i, e := range t {
			s.walkNode(c, e, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

func (s *Schema) walkObject(c *Coverage, m map[string]any, path string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		c.SeeKeyword(k, path+"/"+k)
		switch {
		case k == kwProperties || k == kwDefs:
			// Keys under these are NAMES, not keywords.
			s.walkNamed(c, m[k], path+"/"+k)
		case dataBearing[k]:
			// Values here are arbitrary pack data, not schema. Walking them
			// would report every field name in every example as an
			// unrecognized keyword.
		default:
			s.walkNode(c, m[k], path+"/"+k)
		}
	}
}

func (s *Schema) walkNamed(c *Coverage, v any, path string) {
	child, ok := v.(map[string]any)
	if !ok {
		return
	}
	names := make([]string, 0, len(child))
	for n := range child {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s.walkNode(c, child[n], path+"/"+n)
	}
}

// dataBearing keywords hold example/default pack fragments rather than nested
// schema. Their contents are arbitrary user data, so the keyword walker records
// the keyword itself and stops.
var dataBearing = map[string]bool{
	kwExamples: true, kwDefault: true, kwConst: true, kwEnum: true,
}
