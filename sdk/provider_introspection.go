package sdk

import (
	"slices"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/classify"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// RegisteredProviderTypes returns, per provider role, the types that have a
// registered factory in this binary — the types a With*Provider option (or a
// role in a *.provider.yaml) will successfully construct. Each list is sorted,
// and the result is a snapshot the caller may modify freely.
//
// Registration is the only gate on construction, so this answers "will this
// binding build?" exactly. It does not rank types by suitability: llm, image
// and video share one completion-provider registry, so all three report the
// same set even though a given type may only be useful for one of them.
//
// The registries are populated by package init(), which means the answer
// depends on which provider packages the binary imports. Callers that need a
// type not listed here should add its blank import.
//
// Intended for deploy-time and boot-time validation — a caller can check a
// configured type without constructing a provider and string-matching the
// error. See ValidateOptions to check a whole option set.
func RegisteredProviderTypes() map[string][]string {
	completion := providers.RegisteredProviderTypes()
	return map[string][]string{
		// llm/image/video all resolve through createProviderFromConfig, which
		// dispatches on the single completion-provider registry.
		pkgconfig.RoleLLM:       completion,
		pkgconfig.RoleImage:     slices.Clone(completion),
		pkgconfig.RoleVideo:     slices.Clone(completion),
		pkgconfig.RoleTTS:       tts.RegisteredTypes(),
		pkgconfig.RoleSTT:       stt.RegisteredTypes(),
		pkgconfig.RoleEmbedding: providers.RegisteredEmbeddingProviderTypes(),
		pkgconfig.RoleInference: classify.RegisteredTypes(),
	}
}

// ValidateOptions reports whether an option set would be accepted by Open,
// without loading a pack or starting a conversation. It runs exactly the
// option-application phase Open runs: every option is applied (so providers
// are constructed and credentials resolved) and the cross-option constraints
// are then checked.
//
// Call it once at startup with the same options the server will later pass to
// Open. Provider options are applied eagerly inside Open, and a server that
// opens a conversation per request would otherwise surface a bad binding as a
// failure on every request rather than as a startup error.
//
// Anything constructed during validation is discarded.
func ValidateOptions(opts ...Option) error {
	_, err := applyOptions("", opts)
	return err
}
