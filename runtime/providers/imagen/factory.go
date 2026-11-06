// Package imagen provides Google Imagen image generation provider integration.
package imagen

import (
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

//nolint:gochecknoinits // provider self-registration via init is the established pattern
func init() {
	providers.RegisterProviderFactory("imagen", func(spec providers.ProviderSpec) (providers.Provider, error) {
		// Resolve API key. Construction must not fail when the key is
		// absent — the provider is registered in the engine's provider
		// registry at startup regardless of whether anyone will use it.
		// Tooling that swaps in a mock (e.g. `promptarena run --mock-provider`)
		// or scenarios that don't target imagen would otherwise be blocked
		// by a key check for a provider they'll never call. Predict()
		// surfaces the missing-key error when imagen is actually invoked.
		//
		// Key precedence: a resolved credential (spec.Credential) wins, then
		// GOOGLE_API_KEY, then GEMINI_API_KEY. An api_key credential is sent
		// in x-goog-api-key, as the env keys are; any other credential is
		// applied as it is.
		apiKey := providers.ExtractAPIKey(spec.Credential)
		if apiKey == "" && !spec.HasCredential() {
			apiKey = os.Getenv("GOOGLE_API_KEY")
			if apiKey == "" {
				apiKey = os.Getenv("GEMINI_API_KEY")
			}
		}

		// Project ID and location are no longer required when using Gemini API endpoint
		// Keep them for backwards compatibility but they're not used
		// Type assertion failure returns empty string, handled below
		projectID, _ := spec.AdditionalConfig["project_id"].(string) // NOSONAR
		if projectID == "" {
			projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		}

		// Type assertion failure returns empty string, handled below
		location, _ := spec.AdditionalConfig["location"].(string) // NOSONAR
		if location == "" {
			location = defaultLocation
		}

		provider := NewProvider(Config{
			ID:               spec.ID,
			Model:            spec.Model,
			BaseURL:          spec.BaseURL,
			ApiKey:           apiKey,
			ProjectID:        projectID,
			Location:         location,
			IncludeRawOutput: spec.IncludeRawOutput,
			Defaults:         spec.Defaults,
		})
		if apiKey == "" && spec.HasCredential() {
			provider.SetCredential(spec.Credential)
		}
		return provider, nil
	})
}
