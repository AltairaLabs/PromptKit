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
    Environment  string `json:"environment"`
    PriorState   string `json:"prior_state,omitempty"`
}

type PlanResponse struct {
    Changes []ResourceChange `json:"changes"`
    Summary string           `json:"summary"`
}
```

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
