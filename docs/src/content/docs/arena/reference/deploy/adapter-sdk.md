---
title: 'Deploy: Adapter SDK'
---

## Overview

The Adapter SDK (`runtime/deploy/adaptersdk`) provides Go functions for building deploy adapter plugins. It handles JSON-RPC communication so you only need to implement the `Provider` interface.

## Installation

```bash
go get github.com/AltairaLabs/PromptKit/runtime/deploy
```

Import the packages:

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/deploy"
    "github.com/AltairaLabs/PromptKit/runtime/deploy/adaptersdk"
)
```

## Quick Start

A minimal adapter:

```go
package main

import (
    "context"
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/deploy"
    "github.com/AltairaLabs/PromptKit/runtime/deploy/adaptersdk"
)

type myProvider struct{}

func (p *myProvider) GetProviderInfo(ctx context.Context) (*deploy.ProviderInfo, error) {
    return &deploy.ProviderInfo{
        Name:    "myprovider",
        Version: "0.1.0",
    }, nil
}

func (p *myProvider) ValidateConfig(ctx context.Context, req *deploy.ValidateRequest) (*deploy.ValidateResponse, error) {
    return &deploy.ValidateResponse{Valid: true}, nil
}

func (p *myProvider) Plan(ctx context.Context, req *deploy.PlanRequest) (*deploy.PlanResponse, error) {
    return &deploy.PlanResponse{
        Changes: []deploy.ResourceChange{
            {Type: "runtime", Name: "main", Action: deploy.ActionCreate, Detail: "Create runtime"},
        },
        Summary: "1 resource to create",
    }, nil
}

func (p *myProvider) Apply(ctx context.Context, req *deploy.PlanRequest, callback deploy.ApplyCallback) (string, error) {
    // Create resources...
    return `{"resource_id": "abc123"}`, nil
}

func (p *myProvider) Destroy(ctx context.Context, req *deploy.DestroyRequest, callback deploy.DestroyCallback) error {
    // Delete resources...
    return nil
}

func (p *myProvider) Status(ctx context.Context, req *deploy.StatusRequest) (*deploy.StatusResponse, error) {
    return &deploy.StatusResponse{
        Status: "deployed",
        Resources: []deploy.ResourceStatus{
            {Type: "runtime", Name: "main", Status: "healthy"},
        },
    }, nil
}

func (p *myProvider) Import(ctx context.Context, req *deploy.ImportRequest) (*deploy.ImportResponse, error) {
    // Look up existing resource by req.Identifier...
    return &deploy.ImportResponse{
        Resource: deploy.ResourceStatus{
            Type: req.ResourceType, Name: req.ResourceName, Status: "healthy",
        },
        State: `{"resource_id": "` + req.Identifier + `"}`,
    }, nil
}

func main() {
    if err := adaptersdk.Serve(&myProvider{}); err != nil {
        log.Fatal(err)
    }
}
```

Build the binary with the correct name:

```bash
go build -o promptarena-deploy-myprovider .
```

## API Reference

### Serve

```go
func Serve(provider deploy.Provider) error
```

Starts a JSON-RPC server on stdin/stdout. This is the main entry point for adapter binaries. It reads JSON-RPC requests from stdin, dispatches them to the appropriate `Provider` method, and writes responses to stdout.

`Serve` blocks until stdin is closed (the CLI exits).

### ServeIO

```go
func ServeIO(provider deploy.Provider, r io.Reader, w io.Writer) error
```

Same as `Serve` but with custom I/O streams. Useful for testing.

### ParsePack

```go
func ParsePack(packJSON []byte) (*prompt.Pack, error)
```

Deserializes the pack JSON from a `PlanRequest.PackJSON` field into a `prompt.Pack` struct. Use this to inspect pack contents during Plan or Apply:

```go
func (p *myProvider) Plan(ctx context.Context, req *deploy.PlanRequest) (*deploy.PlanResponse, error) {
    pack, err := adaptersdk.ParsePack([]byte(req.PackJSON))
    if err != nil {
        return nil, fmt.Errorf("invalid pack: %w", err)
    }
    // Use pack.Prompts, pack.Name, etc.
}
```

### ProgressReporter

```go
type ProgressReporter struct { ... }

func NewProgressReporter(callback deploy.ApplyCallback) *ProgressReporter
```

Helper for emitting structured events during `Apply`:

```go
func (p *myProvider) Apply(ctx context.Context, req *deploy.PlanRequest, callback deploy.ApplyCallback) (string, error) {
    reporter := adaptersdk.NewProgressReporter(callback)

    reporter.Progress("Creating runtime...", 25)
    // ... create runtime ...

    reporter.Resource(&deploy.ResourceResult{
        Type:   "runtime",
        Name:   "main",
        Action: deploy.ActionCreate,
        Status: "created",
    })

    reporter.Progress("Creating endpoint...", 75)
    // ... create endpoint ...

    reporter.Resource(&deploy.ResourceResult{
        Type:   "runtime",
        Name:   "endpoint",
        Action: deploy.ActionCreate,
        Status: "created",
    })

    return `{"runtime_id": "abc"}`, nil
}
```

**Methods:**

| Method | Description |
|--------|-------------|
| `Progress(message string, pct float64)` | Emit a progress message with percentage (0-100) |
| `Resource(result *deploy.ResourceResult)` | Report a completed resource operation |
| `Error(err error)` | Report a non-fatal error |

Progress messages with valid percentages (0-100) are formatted as `"message (XX%)"`.

## Planning Helpers

The SDK provides helper functions for building resource plans from `PlanRequest` fields. You can use the high-level `GenerateResourcePlan` for a complete plan, or compose the lower-level functions for custom logic.

### GenerateResourcePlan

```go
func GenerateResourcePlan(packJSON, arenaConfigJSON, deployConfigJSON string) (*ResourcePlan, error)
```

Builds a combined resource plan by orchestrating all lower-level helpers. Call this from your `Plan()` method to get a complete plan in one step:

```go
func (p *myProvider) Plan(ctx context.Context, req *deploy.PlanRequest) (*deploy.PlanResponse, error) {
    plan, err := adaptersdk.GenerateResourcePlan(req.PackJSON, req.ArenaConfig, req.DeployConfig)
    if err != nil {
        return nil, err
    }
    return &deploy.PlanResponse{
        Changes: plan.Changes,
        Summary: adaptersdk.SummarizeChanges(plan.Changes),
    }, nil
}
```

**What it does internally:**

1. Parses the pack JSON (required — returns error if invalid)
2. Generates agent resources for multi-agent packs (`agent_runtime`, `a2a_endpoint`, `gateway`)
3. Extracts tool info and policies from the arena config
4. Filters out blocklisted tools
5. Parses tool targets from the deploy config
6. Generates `tool_gateway` resources for tools with targets
7. Combines agent + tool changes into a single plan

Empty arena config or deploy config are handled gracefully (no tool resources).

### ResourcePlan

```go
type ResourcePlan struct {
    Changes []deploy.ResourceChange // Combined agent + tool resource changes
    Tools   []ToolInfo              // Filtered tools (blocklisted tools removed)
    Targets ToolTargetMap           // Tool target mappings from deploy config
    Policy  *ToolPolicyInfo         // Merged tool policies from scenarios
}
```

Returned by `GenerateResourcePlan`. The `Tools`, `Targets`, and `Policy` fields are exposed for adapter-specific logic beyond the standard plan.

### SummarizeChanges

```go
func SummarizeChanges(changes []deploy.ResourceChange) string
```

Generates a human-readable summary of resource changes:

```go
adaptersdk.SummarizeChanges(changes)
// "3 to create, 1 to update, 1 to delete"
// "5 to create, 2 unchanged"
// "No changes"
```

### FilterBlocklistedTools

```go
func FilterBlocklistedTools(tools []ToolInfo, policy *ToolPolicyInfo) []ToolInfo
```

Removes tools whose names appear in the policy blocklist. Returns tools unchanged if policy is nil or the blocklist is empty. Called automatically by `GenerateResourcePlan`, but available for custom plan logic.

### Lower-Level Functions

These are used internally by `GenerateResourcePlan` and are available for adapters that need finer control.

#### Agent Helpers

```go
// Returns true if the pack has an agents section with members.
func IsMultiAgent(pack *prompt.Pack) bool

// Returns agent metadata for all members, sorted by name.
// Defaults: text/plain for missing input/output modes.
func ExtractAgents(pack *prompt.Pack) []AgentInfo

// Creates resource changes for multi-agent deployment.
// Per member: agent_runtime + a2a_endpoint. Entry agent also gets a gateway.
// Returns nil for single-agent packs.
func GenerateAgentResourcePlan(pack *prompt.Pack) []deploy.ResourceChange

// Generates A2A agent cards from the pack.
func GenerateAgentCards(pack *prompt.Pack) map[string]*a2a.AgentCard
```

#### Tool Helpers

```go
// Extracts tool metadata from ArenaConfig JSON (inline specs + loaded tool files).
// Returns sorted tools with deduplication (inline specs take precedence).
func ExtractToolInfo(arenaConfigJSON string) ([]ToolInfo, error)

// Extracts and merges tool blocklists from all scenarios in the ArenaConfig.
// Returns nil if no policies are found.
func ExtractToolPolicies(arenaConfigJSON string) (*ToolPolicyInfo, error)

// Extracts the tool_targets map from deploy config JSON.
// Each value is raw JSON for adapter-specific unmarshaling.
func ParseDeployToolTargets(deployConfigJSON string) (ToolTargetMap, error)

// Creates tool_gateway resource changes for tools that have target mappings.
func GenerateToolGatewayPlan(tools []ToolInfo, targets ToolTargetMap) []deploy.ResourceChange
```

### Planning Types

#### AgentInfo

```go
type AgentInfo struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    IsEntry     bool     `json:"is_entry"`
    Tags        []string `json:"tags,omitempty"`
    InputModes  []string `json:"input_modes"`
    OutputModes []string `json:"output_modes"`
}
```

#### ToolInfo

```go
type ToolInfo struct {
    Name        string      `json:"name"`
    Description string      `json:"description"`
    Mode        string      `json:"mode"`          // "mock", "live", "mcp", "a2a"
    HasSchema   bool        `json:"has_schema"`
    InputSchema interface{} `json:"input_schema,omitempty"`
    HTTPURL     string      `json:"http_url,omitempty"`
    HTTPMethod  string      `json:"http_method,omitempty"`
}
```

#### ToolTargetMap

```go
// Opaque map of tool name → adapter-specific target config as raw JSON.
type ToolTargetMap map[string]json.RawMessage
```

Each adapter defines its own target schema. For example, a deploy config might specify:

```json
{
  "tool_targets": {
    "get_weather": { "lambda_arn": "arn:aws:lambda:us-east-1:123:function:weather" }
  }
}
```

The adapter unmarshals each `json.RawMessage` into its own target type.

#### ToolPolicyInfo

```go
type ToolPolicyInfo struct {
    Blocklist []string `json:"blocklist,omitempty"`
}
```

## Provider Interface

```go
type Provider interface {
    GetProviderInfo(ctx context.Context) (*ProviderInfo, error)
    ValidateConfig(ctx context.Context, req *ValidateRequest) (*ValidateResponse, error)
    Plan(ctx context.Context, req *PlanRequest) (*PlanResponse, error)
    Apply(ctx context.Context, req *PlanRequest, callback ApplyCallback) (adapterState string, err error)
    Destroy(ctx context.Context, req *DestroyRequest, callback DestroyCallback) error
    Status(ctx context.Context, req *StatusRequest) (*StatusResponse, error)
    Import(ctx context.Context, req *ImportRequest) (*ImportResponse, error)
}
```

## Types

### ProviderInfo

```go
type ProviderInfo struct {
    Name         string   `json:"name"`
    Version      string   `json:"version"`
    Capabilities []string `json:"capabilities,omitempty"`
    ConfigSchema string   `json:"config_schema,omitempty"`
}
```

### ValidateRequest / ValidateResponse

```go
type ValidateRequest struct {
    Config string `json:"config"`
}

type ValidateResponse struct {
    Valid  bool     `json:"valid"`
    Errors []string `json:"errors,omitempty"`
}
```

### PlanRequest / PlanResponse

```go
type PlanRequest struct {
    PackJSON     string `json:"pack_json"`
    DeployConfig string `json:"deploy_config"`
    ArenaConfig  string `json:"arena_config,omitempty"`
    Environment  string `json:"environment"`
    PriorState   string `json:"prior_state,omitempty"`
}

type PlanResponse struct {
    Changes []ResourceChange `json:"changes"`
    Summary string           `json:"summary"`
}
```

`ArenaConfig` contains the JSON-serialized arena config with loaded resources (tools, scenarios). Planning helpers use this to extract tool info and policies.

### ResourceChange

```go
type ResourceChange struct {
    Type   string `json:"type"`
    Name   string `json:"name"`
    Action Action `json:"action"`
    Detail string `json:"detail,omitempty"`
}
```

### Action Constants

```go
const (
    ActionCreate   Action = "CREATE"
    ActionUpdate   Action = "UPDATE"
    ActionDelete   Action = "DELETE"
    ActionNoChange Action = "NO_CHANGE"
    ActionDrift    Action = "DRIFT"
)
```

### ApplyEvent / ApplyCallback

```go
type ApplyEvent struct {
    Type     string          `json:"type"`
    Message  string          `json:"message,omitempty"`
    Resource *ResourceResult `json:"resource,omitempty"`
}

type ApplyCallback func(event *ApplyEvent) error
```

Event types: `"progress"`, `"resource"`, `"error"`, `"complete"`.

### ResourceResult

```go
type ResourceResult struct {
    Type   string `json:"type"`
    Name   string `json:"name"`
    Action Action `json:"action"`
    Status string `json:"status"`
    Detail string `json:"detail,omitempty"`
}
```

Status values: `"created"`, `"updated"`, `"deleted"`, `"failed"`.

### DestroyRequest / DestroyEvent / DestroyCallback

```go
type DestroyRequest struct {
    DeployConfig string `json:"deploy_config"`
    Environment  string `json:"environment"`
    PriorState   string `json:"prior_state"`
}

type DestroyEvent struct {
    Type    string `json:"type"`
    Message string `json:"message,omitempty"`
}

type DestroyCallback func(event *DestroyEvent) error
```

### StatusRequest / StatusResponse

```go
type StatusRequest struct {
    DeployConfig string `json:"deploy_config"`
    Environment  string `json:"environment"`
    PriorState   string `json:"prior_state"`
}

type StatusResponse struct {
    Status    string           `json:"status"`
    Resources []ResourceStatus `json:"resources,omitempty"`
    State     string           `json:"state,omitempty"`
}
```

### ResourceStatus

```go
type ResourceStatus struct {
    Type   string `json:"type"`
    Name   string `json:"name"`
    Status string `json:"status"`
    Detail string `json:"detail,omitempty"`
}
```

### ImportRequest / ImportResponse

```go
type ImportRequest struct {
    ResourceType string `json:"resource_type"`
    ResourceName string `json:"resource_name"`
    Identifier   string `json:"identifier"`
    DeployConfig string `json:"deploy_config"`
    Environment  string `json:"environment,omitempty"`
    PriorState   string `json:"prior_state,omitempty"`
}

type ImportResponse struct {
    Resource ResourceStatus `json:"resource"`
    State    string         `json:"state"`
}
```

## Building and Installing

Build your adapter binary:

```bash
go build -o promptarena-deploy-myprovider .
```

Install locally for testing:

```bash
mkdir -p ~/.promptarena/adapters
cp promptarena-deploy-myprovider ~/.promptarena/adapters/
```

Test with the CLI:

```bash
promptarena deploy adapter list
promptarena deploy plan
```

## See Also

- [Protocol](protocol) — JSON-RPC wire protocol details
- [Adapter Architecture](../../explanation/deploy/adapter-architecture) — Design concepts
- [CLI Commands](cli-commands) — CLI usage
