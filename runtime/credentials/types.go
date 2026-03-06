package credentials

// CredentialConfig defines how to obtain credentials for a provider.
// Resolution order: api_key → credential_file → credential_env → default env vars.
type CredentialConfig struct {
	// APIKey is an explicit API key value (not recommended for production).
	// Excluded from JSON serialization to prevent accidental credential leakage.
	APIKey string `json:"-" yaml:"api_key,omitempty"`
	// CredentialFile is a path to a file containing the API key.
	CredentialFile string `json:"credential_file,omitempty" yaml:"credential_file,omitempty"`
	// CredentialEnv is the name of an environment variable containing the API key.
	CredentialEnv string `json:"credential_env,omitempty" yaml:"credential_env,omitempty"`
}

// PlatformConfig defines platform-specific settings for hyperscaler hosting.
// Platforms are hosting layers (bedrock, vertex, azure) that determine auth and endpoints,
// while provider type determines message/response handling.
type PlatformConfig struct {
	// Type is the platform type: "bedrock", "vertex", or "azure".
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Region is the cloud region (e.g., "us-west-2", "us-central1").
	Region string `json:"region,omitempty" yaml:"region,omitempty"`
	// Project is the cloud project ID (required for Vertex).
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
	// Endpoint is an optional custom endpoint URL.
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	// AdditionalConfig holds platform-specific settings.
	AdditionalConfig map[string]interface{} `json:"additional_config,omitempty" yaml:"additional_config,omitempty"`
}
