// Package imagen provides Google Imagen image generation provider integration.
package imagen

import (
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func init() {
	providers.RegisterProviderFactory("imagen", func(spec providers.ProviderSpec) (providers.Provider, error) {
		// Get API key from environment
		apiKey := os.Getenv("GOOGLE_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GOOGLE_API_KEY or GEMINI_API_KEY environment variable not set")
		}

		// Project ID and location are no longer required when using Gemini API endpoint
		// Keep them for backwards compatibility but they're not used
		projectID, _ := spec.AdditionalConfig["project_id"].(string) // NOSONAR: Type assertion failure returns empty string, handled below
		if projectID == "" {
			projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		}

		location, _ := spec.AdditionalConfig["location"].(string) // NOSONAR: Type assertion failure returns empty string, handled below
		if location == "" {
			location = "us-central1"
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
		return provider, nil
	})
}
