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
}

// PlanRequest is the input to Plan.
type PlanRequest struct {
	PackJSON     string `json:"pack_json"`
	DeployConfig string `json:"deploy_config"` // JSON provider config
	Environment  string `json:"environment,omitempty"`
	PriorState   string `json:"prior_state,omitempty"` // Opaque adapter state from last deploy
}

// PlanResponse is the output of Plan.
type PlanResponse struct {
	Changes []ResourceChange `json:"changes"`
	Summary string           `json:"summary"`
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
