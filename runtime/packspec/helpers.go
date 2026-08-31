// Package packspec holds the Go types generated from the PromptPack schema.
//
// types.go is generated and must not be edited; this file is hand-written and
// holds the small helpers callers need to work with the generated shapes.
package packspec

import "encoding/json"

// Deref returns the value a pointer field holds, or fallback when it is nil.
//
// Optional numeric and boolean properties are pointers in the generated types
// because their zero value is a legitimate setting: `temperature: 0` and
// `max_tokens: 0` mean something, and "unset" means something else. That
// distinction is the point, but most call sites only want "the effective
// value", and writing the nil check at each one is where the distinction gets
// quietly dropped.
//
// Pass the spec's default as fallback where the schema declares one.
func Deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// Ptr returns a pointer to v, for constructing the optional fields above.
//
// Mainly for tests and for callers building a pack in Go rather than loading
// one: `Parameters{MaxTokens: packspec.Ptr(512)}`.
func Ptr[T any](v T) *T { return &v }

// DecodeYAMLViaJSON and EncodeYAMLViaJSON route YAML through a type's JSON
// codec.
//
// The generated types carry custom JSON marshaling for unions, scalar
// shorthands and open-object extensions. A YAML library does not consult
// MarshalJSON/UnmarshalJSON, and this repo decodes YAML directly into these
// structs — prompt.ParseConfig runs yaml.Unmarshal into Config, whose Evals
// carry MetricDef. Without this bridge, generating the types silently dropped
// metric.labels from YAML configs and made `requires` and composition steps
// fail to parse at all.
//
// Routing through JSON rather than reimplementing means the union, shorthand
// and extension rules have exactly one definition. A second implementation is
// how two formats drift apart, which is the whole failure this package exists
// to prevent.
//
// These live here, hand-written, rather than being emitted into every generated
// codec: 54 copies of the same six lines is 54 places for an error branch to go
// untested, and one place to fix if the bridge is ever wrong.
func DecodeYAMLViaJSON(unmarshal func(any) error, into json.Unmarshaler) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return into.UnmarshalJSON(data)
}

// EncodeYAMLViaJSON is the encode half of DecodeYAMLViaJSON.
func EncodeYAMLViaJSON(from json.Marshaler) (any, error) {
	data, err := from.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
