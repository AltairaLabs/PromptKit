package providers

// Registry manages available providers
type Registry struct {
	providers map[string]Provider
}

// ProviderFactory is a function that creates a provider from a spec
type ProviderFactory func(spec ProviderSpec) (Provider, error)

var providerFactories = make(map[string]ProviderFactory)

// RegisterProviderFactory registers a factory function for a provider type
func RegisterProviderFactory(providerType string, factory ProviderFactory) {
	providerFactories[providerType] = factory
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry
func (r *Registry) Register(provider Provider) {
	r.providers[provider.ID()] = provider
}

// Get retrieves a provider by ID
func (r *Registry) Get(id string) (Provider, bool) {
	provider, exists := r.providers[id]
	return provider, exists
}

// List returns all registered provider IDs
func (r *Registry) List() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// Close closes all registered providers and cleans up their resources
func (r *Registry) Close() error {
	for _, provider := range r.providers {
		if err := provider.Close(); err != nil {
			return err
		}
	}
	return nil
}

// ProviderSpec holds the configuration needed to create a provider instance
type ProviderSpec struct {
	ID               string
	Type             string
	Model            string
	BaseURL          string
	Defaults         ProviderDefaults
	IncludeRawOutput bool
	AdditionalConfig map[string]interface{} // Flexible key-value pairs for provider-specific configuration
}

// CreateProviderFromSpec creates a provider implementation from a spec.
// Returns an error if the provider type is unsupported.
func CreateProviderFromSpec(spec ProviderSpec) (Provider, error) {
	// Use default base URLs if not specified
	baseURL := spec.BaseURL
	if baseURL == "" {
		switch spec.Type {
		case "openai":
			baseURL = "https://api.openai.com/v1"
		case "gemini":
			baseURL = "https://generativelanguage.googleapis.com"
		case "claude":
			baseURL = "https://api.anthropic.com"
		case "mock":
			// No base URL needed for mock provider
		}
	}

	// Update spec with default baseURL
	spec.BaseURL = baseURL

	// Look up the factory for this provider type
	factory, exists := providerFactories[spec.Type]
	if !exists {
		return nil, &UnsupportedProviderError{ProviderType: spec.Type}
	}

	return factory(spec)
}

// UnsupportedProviderError is returned when a provider type is not recognized
type UnsupportedProviderError struct {
	ProviderType string
}

func (e *UnsupportedProviderError) Error() string {
	return "unsupported provider type: " + e.ProviderType
}
