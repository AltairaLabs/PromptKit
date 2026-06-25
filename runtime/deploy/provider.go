package deploy

import "context"

// Action represents the type of change to a resource.
type Action string

// Possible resource actions.
const (
	ActionCreate   Action = "CREATE"
	ActionUpdate   Action = "UPDATE"
	ActionDelete   Action = "DELETE"
	ActionNoChange Action = "NO_CHANGE"
	ActionDrift    Action = "DRIFT"
)

// ProviderInfo describes a deploy adapter's capabilities.
type ProviderInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities,omitempty"`
	ConfigSchema string   `json:"config_schema,omitempty"` // JSON Schema for provider config
}

// ValidateRequest is the input to ValidateConfig.
type ValidateRequest struct {
	Config string `json:"config"` // JSON provider config
}

// ValidateResponse is the output of ValidateConfig.
type ValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
	// Warnings are non-blocking advisories surfaced to the user. Unlike
	// Errors, they do not make Valid false; the deployment proceeds.
	Warnings []string `json:"warnings,omitempty"`
}

// PlanRequest is the input to Plan.
type PlanRequest struct {
	PackJSON     string `json:"pack_json"`
	DeployConfig string `json:"deploy_config"`          // JSON provider config
	ArenaConfig  string `json:"arena_config,omitempty"` // JSON-serialized config.Config with loaded resources
	Environment  string `json:"environment,omitempty"`
	PriorState   string `json:"prior_state,omitempty"` // Opaque adapter state from last deploy
}

// PlanResponse is the output of Plan.
type PlanResponse struct {
	Changes []ResourceChange `json:"changes"`
	Summary string           `json:"summary"`
	// Warnings are non-blocking advisories surfaced to the user before the
	// plan changes (e.g. a config that deploys but behaves surprisingly).
	Warnings []string `json:"warnings,omitempty"`
}

// ResourceChange describes a single resource modification.
type ResourceChange struct {
	Type   string `json:"type"`             // Resource type (e.g., "agent_runtime", "a2a_endpoint")
	Name   string `json:"name"`             // Resource name
	Action Action `json:"action"`           // CREATE, UPDATE, DELETE, NO_CHANGE
	Detail string `json:"detail,omitempty"` // Human-readable description
}

// ApplyEvent is a streaming event during Apply.
type ApplyEvent struct {
	Type     string          `json:"type"` // "progress", "resource", "error", "complete"
	Message  string          `json:"message,omitempty"`
	Resource *ResourceResult `json:"resource,omitempty"`
}

// ResourceResult describes the outcome of a resource operation.
type ResourceResult struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Action Action `json:"action"`
	Status string `json:"status"` // "created", "updated", "deleted", "failed"
	Detail string `json:"detail,omitempty"`
}

// DestroyEvent is a streaming event during Destroy.
type DestroyEvent struct {
	Type     string          `json:"type"` // "progress", "resource", "error", "complete"
	Message  string          `json:"message,omitempty"`
	Resource *ResourceResult `json:"resource,omitempty"`
}

// StatusResponse describes the current deployment status.
type StatusResponse struct {
	Status    string           `json:"status"` // "deployed", "not_deployed", "degraded", "unknown"
	Resources []ResourceStatus `json:"resources,omitempty"`
	State     string           `json:"state,omitempty"` // Opaque adapter state
}

// ResourceStatus describes the current state of a resource.
type ResourceStatus struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Status string `json:"status"` // "healthy", "unhealthy", "missing"
	Detail string `json:"detail,omitempty"`
}

// DestroyRequest is the input to Destroy.
type DestroyRequest struct {
	DeployConfig string `json:"deploy_config"`
	Environment  string `json:"environment,omitempty"`
	PriorState   string `json:"prior_state,omitempty"`
}

// StatusRequest is the input to Status.
type StatusRequest struct {
	DeployConfig string `json:"deploy_config"`
	Environment  string `json:"environment,omitempty"`
	PriorState   string `json:"prior_state,omitempty"`
}

// ApplyCallback is called for each ApplyEvent during Apply.
type ApplyCallback func(event *ApplyEvent) error

// DestroyCallback is called for each DestroyEvent during Destroy.
type DestroyCallback func(event *DestroyEvent) error

// ImportRequest is the input to Import.
type ImportRequest struct {
	ResourceType string `json:"resource_type"`
	ResourceName string `json:"resource_name"`
	Identifier   string `json:"identifier"`
	DeployConfig string `json:"deploy_config"`
	Environment  string `json:"environment,omitempty"`
	PriorState   string `json:"prior_state,omitempty"`
}

// ImportResponse is the output of Import.
type ImportResponse struct {
	Resource ResourceStatus `json:"resource"`
	State    string         `json:"state"`
}

// Provider defines the interface that deploy adapters must implement.
type Provider interface {
	GetProviderInfo(ctx context.Context) (*ProviderInfo, error)
	ValidateConfig(ctx context.Context, req *ValidateRequest) (*ValidateResponse, error)
	Plan(ctx context.Context, req *PlanRequest) (*PlanResponse, error)
	Apply(ctx context.Context, req *PlanRequest, callback ApplyCallback) (adapterState string, err error)
	Destroy(ctx context.Context, req *DestroyRequest, callback DestroyCallback) error
	Status(ctx context.Context, req *StatusRequest) (*StatusResponse, error)
	Import(ctx context.Context, req *ImportRequest) (*ImportResponse, error)
}

// LoginCapability is the ProviderInfo capability string an adapter advertises
// when it implements LoginProvider (browser-based autoconfigure).
const LoginCapability = "login"

// LoginURLRequest asks the adapter for the provider's authorize URL to open in
// the browser. The CLI supplies the loopback callback it is listening on and a
// CSRF state nonce.
type LoginURLRequest struct {
	CallbackURL string `json:"callback_url"`
	State       string `json:"state"`
	// Config is the current deploy config as JSON (may be partial or empty). The
	// adapter reads provider-specific coordinates from it — e.g. the Omnia
	// adapter needs api_endpoint to build the authorize URL.
	Config string `json:"config,omitempty"`
}

// LoginURLResponse carries the provider-specific authorize URL the CLI opens.
type LoginURLResponse struct {
	AuthorizeURL string `json:"authorize_url"`
}

// CompleteLoginRequest hands the adapter the opaque query parameters captured
// from the browser's loopback callback (e.g. code, state). The adapter knows
// which it needs and how to exchange them.
type CompleteLoginRequest struct {
	Params map[string]string `json:"params"`
	// Config is the current deploy config as JSON (may be partial or empty),
	// carrying provider-specific coordinates the adapter needs to exchange the
	// callback (e.g. the Omnia api_endpoint for the back-channel call).
	Config string `json:"config,omitempty"`
}

// CompleteLoginResponse is the result of a completed login: the deploy profile
// to merge into the arena config (same shape as a dashboard-exported profile)
// and the scoped secret token to store outside the config file.
type CompleteLoginResponse struct {
	Profile map[string]interface{} `json:"profile"`
	Token   string                 `json:"token,omitempty"`
}

// LoginProvider is the OPTIONAL deploy capability for browser-based
// autoconfigure. Adapters that implement it advertise LoginCapability in
// ProviderInfo.Capabilities. The adaptersdk exposes get_login_url /
// complete_login only for providers that satisfy this interface, and the CLI
// degrades gracefully (method not found) for those that don't.
type LoginProvider interface {
	GetLoginURL(ctx context.Context, req *LoginURLRequest) (*LoginURLResponse, error)
	CompleteLogin(ctx context.Context, req *CompleteLoginRequest) (*CompleteLoginResponse, error)
}
