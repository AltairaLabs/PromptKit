// Package testutil provides shared test helper utilities.
package testutil

// Ptr returns a pointer to v. It is a generic replacement for the various
// typed pointer helpers (ptrString, ptrFloat32, boolPtr, etc.) that are
// duplicated across test files.
func Ptr[T any](v T) *T { return &v }
