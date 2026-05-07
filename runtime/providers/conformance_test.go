// Package providers compile-time interface conformance assertions.
// Runtime conformance tests (verifying each impl satisfies base.Provider)
// live in runtime/providers/conformance/ to avoid the import cycle that
// arises when an external test package in this directory imports sub-packages
// (openai, claude, etc.) that themselves import providers.
package providers

import "github.com/AltairaLabs/PromptKit/runtime/providers/base"

// Compile-time assertion: BaseProvider satisfies base.Provider via the
// embedded *base.Implementation. If this fails, the embedding is broken.
var _ base.Provider = (*BaseProvider)(nil)
