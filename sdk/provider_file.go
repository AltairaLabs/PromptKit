package sdk

import (
	"fmt"
	"path/filepath"
	"sort"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
)

// providerSpecFromConfig maps a loaded *pkgconfig.Provider onto the SDK's
// uniform ProviderSpec. Platform is intentionally not carried — ProviderSpec
// has no platform field; platform-auth providers are handled separately.
func providerSpecFromConfig(p *pkgconfig.Provider) ProviderSpec {
	return ProviderSpec{
		ID:               p.ID,
		Type:             p.Type,
		Model:            p.Model,
		BaseURL:          p.BaseURL,
		Credential:       p.Credential,
		AdditionalConfig: p.AdditionalConfig,
	}
}

// applyProviderConfig routes a loaded provider to the SDK slot/pool matching
// its role. Completion providers (llm/image) are pooled by ID; the first one
// becomes the agent (first-wins). tts/stt/embedding/inference delegate to the
// matching public option (each first-wins on its default slot).
func (c *config) applyProviderConfig(p *pkgconfig.Provider) error {
	if err := p.ValidateRole(); err != nil {
		return err
	}
	id := p.ID
	if id == "" {
		id = p.Type
	}
	switch p.GetRole() {
	case pkgconfig.RoleLLM, pkgconfig.RoleImage:
		prov, err := createProviderFromConfig(p)
		if err != nil {
			return fmt.Errorf("provider %q: %w", id, err)
		}
		if c.agentSet {
			ensureProviderPool(c)
			c.providers.Register(prov) // keep in pool; first-declared stays the agent
			return nil
		}
		registerAgentProvider(c, prov)
		return nil
	case pkgconfig.RoleTTS:
		return WithTTSProvider(providerSpecFromConfig(p))(c)
	case pkgconfig.RoleSTT:
		return WithSTTProvider(providerSpecFromConfig(p))(c)
	case pkgconfig.RoleEmbedding:
		if p.Platform != nil {
			return fmt.Errorf(
				"provider %q: platform-auth embedding providers are not yet supported via "+
					"file loading; use WithBedrock/WithVertex/WithAzure", id)
		}
		return WithEmbeddingProvider(providerSpecFromConfig(p))(c)
	case pkgconfig.RoleInference:
		return WithInferenceProvider(providerSpecFromConfig(p))(c)
	default:
		// Unreachable in practice: ValidateRole() above rejects any role not in
		// the known set, and every known role is handled. Kept as a defensive
		// guard against a future role being added to pkg/config without a route.
		return fmt.Errorf("provider %q: unsupported role %q", id, p.GetRole())
	}
}

// WithProviderFile loads a single Arena-format *.provider.yaml and routes it
// into the SDK by its role. Path is resolved relative to the working directory.
func WithProviderFile(path string) Option {
	return func(c *config) error {
		p, err := pkgconfig.LoadProvider(path)
		if err != nil {
			return fmt.Errorf("WithProviderFile %q: %w", path, err)
		}
		if err := c.applyProviderConfig(p); err != nil {
			return fmt.Errorf("WithProviderFile %q: %w", path, err)
		}
		return nil
	}
}

// WithProvidersDir loads every *.provider.yaml in dir (lexically sorted) and
// routes each by role. Among multiple llm/image providers the first sorted
// file becomes the agent; the rest stay in the pool.
func WithProvidersDir(dir string) Option {
	return func(c *config) error {
		matches, err := filepath.Glob(filepath.Join(dir, "*.provider.yaml"))
		if err != nil {
			return fmt.Errorf("WithProvidersDir %q: %w", dir, err)
		}
		sort.Strings(matches)
		for _, f := range matches {
			p, err := pkgconfig.LoadProvider(f)
			if err != nil {
				return fmt.Errorf("WithProvidersDir %q: loading %s: %w", dir, f, err)
			}
			if err := c.applyProviderConfig(p); err != nil {
				return fmt.Errorf("WithProvidersDir %q: %s: %w", dir, f, err)
			}
		}
		return nil
	}
}
