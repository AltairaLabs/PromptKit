// Package testutil re-exports the runtime/testutil package for backward
// compatibility. New code should import runtime/testutil directly.
package testutil

import rttestutil "github.com/AltairaLabs/PromptKit/runtime/testutil"

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return rttestutil.Ptr(v) }
