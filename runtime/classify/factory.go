package classify

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// ProviderSpec is the runtime form of an inference-provider declaration.
// Aliased to base.CapabilitySpec so the field shape is shared with the
// TTS, STT, embedding, and image factories (id/type/model/base_url/
// credential/additional_config).
type ProviderSpec = base.CapabilitySpec

// Backend is whatever a factory returns: a value that implements one or
// more of the task interfaces (AudioClassifier, TextClassifier, …).
// RegisterBackend type-asserts it against each. It is intentionally an
// alias for any — there is no method common to all task interfaces.
type Backend = any

// Factory builds a Backend from a spec. Per-backend packages register
// one via init() (see runtime/classify/backends/hf/register.go) so this
// package never imports them.
type Factory = base.Factory[Backend]

var classifyRegistry = base.NewFactoryRegistry[Backend]()

// RegisterFactory registers a factory for the given provider type.
// Typically called from a per-backend package init().
func RegisterFactory(providerType string, f Factory) {
	classifyRegistry.Register(providerType, f)
}

// CreateFromSpec builds a Backend for the spec's Type.
//
//nolint:gocritic // spec is a value-semantics builder; callers assemble inline.
func CreateFromSpec(spec ProviderSpec) (Backend, error) {
	return classifyRegistry.Create(spec)
}

// ResolveCredential is a thin wrapper around base.ResolveCredential,
// matching tts.ResolveCredential / stt.ResolveCredential.
func ResolveCredential(
	ctx context.Context,
	providerType string,
	cfgDir string,
	cred *credentials.CredentialConfig,
) (credentials.Credential, error) {
	return base.ResolveCredential(ctx, providerType, cfgDir, cred)
}

// RegistryDefaults pins which provider id serves each task when a
// handler doesn't name one. Mirrors config.InferenceDefaults but lives
// in runtime so both Arena and the SDK map onto it.
type RegistryDefaults struct {
	AudioClassifier string
	TextClassifier  string
	ImageClassifier string
	VideoClassifier string
	Embedder        string
}

// RegisterBackend registers b under id against every task interface it
// implements, returning the task labels registered (e.g. ["audio",
// "text"]). A backend that satisfies no task interface registers nothing
// and returns an empty slice. Shared by BuildRegistry and the SDK's
// programmatic options so the type-assert logic lives in one place.
func RegisterBackend(reg *Registry, id string, b Backend) []string {
	var tasks []string
	if c, ok := b.(AudioClassifier); ok {
		reg.RegisterAudio(id, c)
		tasks = append(tasks, "audio")
	}
	if c, ok := b.(TextClassifier); ok {
		reg.RegisterText(id, c)
		tasks = append(tasks, "text")
	}
	if c, ok := b.(ImageClassifier); ok {
		reg.RegisterImage(id, c)
		tasks = append(tasks, "image")
	}
	if c, ok := b.(VideoClassifier); ok {
		reg.RegisterVideo(id, c)
		tasks = append(tasks, "video")
	}
	if c, ok := b.(Embedder); ok {
		reg.RegisterEmbedder(id, c)
		tasks = append(tasks, "embedder")
	}
	return tasks
}

// BuildRegistry constructs a Registry from specs, registering each
// backend against every task interface it implements, then applies
// defaults. For any task with no explicit default, the first-declared
// provider implementing it becomes the default (first-wins, matching
// STT/TTS service defaults). Returns (nil, nil) when specs is empty —
// classification is optional; callers that don't use it must not require
// a token.
//
//nolint:gocritic // RegistryDefaults is a flat value type; pointer indirection adds no benefit here.
func BuildRegistry(specs []ProviderSpec, defaults RegistryDefaults) (*Registry, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	reg := NewRegistry()
	first := make(map[string]string)
	for i := range specs {
		spec := specs[i]
		if spec.ID == "" {
			return nil, fmt.Errorf("classify: inference provider at index %d has empty id", i)
		}
		b, err := CreateFromSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("classify: inference provider %q: %w", spec.ID, err)
		}
		for _, task := range RegisterBackend(reg, spec.ID, b) {
			if _, ok := first[task]; !ok {
				first[task] = spec.ID
			}
		}
	}
	if err := applyDefaults(reg, defaults, first); err != nil {
		return nil, err
	}
	return reg, nil
}

//nolint:gocritic // RegistryDefaults is a flat value type; pointer indirection adds no benefit here.
func applyDefaults(reg *Registry, d RegistryDefaults, first map[string]string) error {
	pick := func(explicit, task string) string {
		if explicit != "" {
			return explicit
		}
		return first[task]
	}
	type defPair struct {
		id  string
		set func(string) error
	}
	for _, p := range []defPair{
		{pick(d.AudioClassifier, "audio"), reg.SetDefaultAudio},
		{pick(d.TextClassifier, "text"), reg.SetDefaultText},
		{pick(d.ImageClassifier, "image"), reg.SetDefaultImage},
		{pick(d.VideoClassifier, "video"), reg.SetDefaultVideo},
		{pick(d.Embedder, "embedder"), reg.SetDefaultEmbedder},
	} {
		if p.id == "" {
			continue
		}
		if err := p.set(p.id); err != nil {
			return err
		}
	}
	return nil
}
