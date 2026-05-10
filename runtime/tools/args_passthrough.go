package tools

import (
	"encoding/json"
	"fmt"
)

// DecodeArgsExtras decodes raw tool args twice: once into the executor's typed
// struct, and once into a generic map so any keys NOT named in knownKeys can be
// returned to the caller for routing into the output's Metadata bag.
//
// Hosts use sdk.WithToolDescriptorOverride to extend a capability tool's
// InputSchema with deployment-specific top-level fields. Without this helper,
// the executor's typed Unmarshal would drop those fields silently. With it,
// the executor can keep its strict typing AND let unknown args flow through
// as metadata extras.
//
// knownKeys must list every top-level JSON key the executor's typed struct
// owns; passing the wrong list causes typed fields to be duplicated into
// extras (annoying but harmless — typed values still win when merged).
//
// Returns an empty map (not nil) when args contains no extras, so callers can
// merge unconditionally without nil checks. Returns nil + nil when args is
// empty or "null" — both mean "no args at all".
func DecodeArgsExtras(args json.RawMessage, typed any, knownKeys ...string) (map[string]any, error) {
	if len(args) == 0 || string(args) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(args, typed); err != nil {
		return nil, fmt.Errorf("decode typed args: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(args, &raw); err != nil {
		// Args parsed as the typed struct but not as an object — e.g. a JSON
		// array or scalar. There are no extras to extract.
		return map[string]any{}, nil //nolint:nilerr // typed decode succeeded; non-object args have no extras
	}
	for _, k := range knownKeys {
		delete(raw, k)
	}
	return raw, nil
}

// MergeExtrasIntoMetadata copies extras into target, leaving any keys already
// present in target untouched. This implements the "typed-fields-win" conflict
// rule: if both the typed `metadata` arg and a passthrough extra have the same
// key, the typed value (already in target) wins.
//
// If target is nil and extras is non-empty, returns a fresh map; otherwise
// mutates target in place and returns it. Returns nil when both are empty.
func MergeExtrasIntoMetadata(target, extras map[string]any) map[string]any {
	if len(extras) == 0 {
		return target
	}
	if target == nil {
		target = make(map[string]any, len(extras))
	}
	for k, v := range extras {
		if _, ok := target[k]; ok {
			continue
		}
		target[k] = v
	}
	return target
}
