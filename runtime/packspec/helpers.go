// Package packspec holds the Go types generated from the PromptPack schema.
//
// types.go is generated and must not be edited; this file is hand-written and
// holds the small helpers callers need to work with the generated shapes.
package packspec

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
