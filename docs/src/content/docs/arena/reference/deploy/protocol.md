---
title: 'Deploy: Protocol'
---

## Overview

Deploy adapters communicate with the CLI using **JSON-RPC 2.0** over **stdio** (stdin/stdout). This page documents the wire protocol, including all methods, request/response formats, and error codes.

## Transport

- **Input**: Line-delimited JSON on stdin
- **Output**: Line-delimited JSON on stdout
- **Encoding**: UTF-8
- **Max line size**: 10 MB
- **Initial buffer**: 64 KB

Each JSON-RPC message is a single line terminated by `\n`. The CLI sends requests and reads responses sequentially.

## JSON-RPC 2.0

All messages follow the [JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification).

### Request Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "plan",
  "params": { ... }
}
```

### Response Format (Success)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": { ... }
}
```

### Response Format (Error)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32603,
    "message": "provider error: resource creation failed"
  }
}
```

## Methods

### get_provider_info

Returns adapter metadata.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "get_provider_info",
  "params": {}
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "name": "agentcore",
    "version": "0.2.0",
    "capabilities": ["a2a", "bedrock"],
    "config_schema": "{ ... JSON Schema ... }"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Provider name |
| `version` | string | Yes | Adapter version |
| `capabilities` | string[] | No | List of capabilities |
| `config_schema` | string | No | JSON Schema for config validation |

---

### validate_config

Validates provider configuration before planning.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "validate_config",
  "params": {
    "config": "{\"region\":\"us-west-2\"}"
  }
}
```

**Response (valid):**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "valid": true
  }
}
```

**Response (invalid):**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "valid": false,
    "errors": [
      "region 'invalid' is not supported",
      "account_id is required"
    ]
  }
}
```

| Param | Type | Description |
|-------|------|-------------|
| `config` | string | JSON-encoded provider config |

| Result Field | Type | Description |
|--------------|------|-------------|
| `valid` | bool | Whether config is valid |
| `errors` | string[] | Validation error messages |

---

### plan

Analyzes the pack and config to determine what resources need to change.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "plan",
  "params": {
    "pack_json": "{ ... serialized pack ... }",
    "deploy_config": "{\"region\":\"us-west-2\"}",
    "environment": "production",
    "prior_state": "eyJyZXNvdXJjZV9pZCI6ImFiYzEyMyJ9"
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "changes": [
      {
        "type": "agent_runtime",
        "name": "greeting",
        "action": "CREATE",
        "detail": "Create agent runtime for greeting prompt"
      },
      {
        "type": "a2a_endpoint",
        "name": "greeting",
        "action": "UPDATE",
        "detail": "Update endpoint configuration"
      }
    ],
    "summary": "1 to create, 1 to update"
  }
}
```

| Param | Type | Description |
|-------|------|-------------|
| `pack_json` | string | Serialized `.pack.json` contents |
| `deploy_config` | string | JSON-encoded merged provider config |
| `environment` | string | Target environment name |
| `prior_state` | string | Opaque adapter state from prior deploy (empty if first deploy) |

| Result Field | Type | Description |
|--------------|------|-------------|
| `changes` | ResourceChange[] | List of planned resource changes |
| `summary` | string | Human-readable summary |

**ResourceChange:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Resource type (e.g., `"agent_runtime"`) |
| `name` | string | Resource name |
| `action` | string | `"CREATE"`, `"UPDATE"`, `"DELETE"`, `"DRIFT"`, or `"NO_CHANGE"` |
| `detail` | string | Human-readable description |

---

### apply

Executes the deployment plan, creating/updating/deleting resources.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "apply",
  "params": {
    "pack_json": "{ ... serialized pack ... }",
    "deploy_config": "{\"region\":\"us-west-2\"}",
    "environment": "production",
    "prior_state": ""
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "state": "eyJydW50aW1lX2lkIjoiYWJjMTIzIn0=",
    "events": [
      {
        "type": "progress",
        "message": "Creating runtime... (25%)"
      },
      {
        "type": "resource",
        "resource": {
          "type": "agent_runtime",
          "name": "greeting",
          "action": "CREATE",
          "status": "created"
        }
      },
      {
        "type": "complete",
        "message": "Deployment complete"
      }
    ]
  }
}
```

| Param | Type | Description |
|-------|------|-------------|
| `pack_json` | string | Serialized `.pack.json` contents |
| `deploy_config` | string | JSON-encoded merged provider config |
| `environment` | string | Target environment name |
| `prior_state` | string | Opaque adapter state (empty if first deploy) |

| Result Field | Type | Description |
|--------------|------|-------------|
| `state` | string | Opaque adapter state to persist |
| `events` | ApplyEvent[] | Progress events from the apply |

**ApplyEvent:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `"progress"`, `"resource"`, `"error"`, or `"complete"` |
| `message` | string | Human-readable message |
| `resource` | ResourceResult | Resource operation result (for `"resource"` type) |

**ResourceResult:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Resource type |
| `name` | string | Resource name |
| `action` | string | Applied action |
| `status` | string | `"created"`, `"updated"`, `"deleted"`, or `"failed"` |
| `detail` | string | Additional info |

---

### destroy

Tears down all managed resources.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "destroy",
  "params": {
    "deploy_config": "{\"region\":\"us-west-2\"}",
    "environment": "production",
    "prior_state": "eyJydW50aW1lX2lkIjoiYWJjMTIzIn0="
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "events": [
      {
        "type": "progress",
        "message": "Deleting resources..."
      },
      {
        "type": "complete",
        "message": "All resources destroyed"
      }
    ]
  }
}
```

| Param | Type | Description |
|-------|------|-------------|
| `deploy_config` | string | JSON-encoded provider config |
| `environment` | string | Target environment |
| `prior_state` | string | Opaque adapter state |

---

### status

Queries current deployment status from the cloud provider.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "status",
  "params": {
    "deploy_config": "{\"region\":\"us-west-2\"}",
    "environment": "production",
    "prior_state": "eyJydW50aW1lX2lkIjoiYWJjMTIzIn0="
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "status": "deployed",
    "resources": [
      {
        "type": "agent_runtime",
        "name": "greeting",
        "status": "healthy",
        "detail": "Running on instance i-abc123"
      },
      {
        "type": "a2a_endpoint",
        "name": "greeting",
        "status": "healthy",
        "detail": "Serving at https://example.com/a2a"
      }
    ],
    "state": "eyJydW50aW1lX2lkIjoiYWJjMTIzIn0="
  }
}
```

| Param | Type | Description |
|-------|------|-------------|
| `deploy_config` | string | JSON-encoded provider config |
| `environment` | string | Target environment |
| `prior_state` | string | Opaque adapter state |

| Result Field | Type | Description |
|--------------|------|-------------|
| `status` | string | `"deployed"`, `"not_deployed"`, `"degraded"`, or `"unknown"` |
| `resources` | ResourceStatus[] | Per-resource health |
| `state` | string | Updated adapter state (optional) |

**ResourceStatus:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Resource type |
| `name` | string | Resource name |
| `status` | string | `"healthy"`, `"unhealthy"`, or `"missing"` |
| `detail` | string | Additional info |

---

### import

Imports a pre-existing resource into deployment state. This lets the CLI manage resources that were created outside of the deploy workflow.

**Request:**

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "import",
  "params": {
    "resource_type": "agent_runtime",
    "resource_name": "my-agent",
    "identifier": "container-abc123",
    "deploy_config": "{\"region\":\"us-west-2\"}",
    "environment": "production",
    "prior_state": "eyJydW50aW1lX2lkIjoiYWJjMTIzIn0="
  }
}
```

**Response:**

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": {
    "resource": {
      "type": "agent_runtime",
      "name": "my-agent",
      "status": "healthy",
      "detail": "Imported from container-abc123"
    },
    "state": "eyJ1cGRhdGVkX3N0YXRlIjoiLi4uIn0="
  }
}
```

| Param | Type | Description |
|-------|------|-------------|
| `resource_type` | string | Resource type (e.g., `"agent_runtime"`) |
| `resource_name` | string | Name to assign in local state |
| `identifier` | string | Provider-specific resource identifier |
| `deploy_config` | string | JSON-encoded provider config |
| `environment` | string | Target environment name |
| `prior_state` | string | Opaque adapter state (optional) |

| Result Field | Type | Description |
|--------------|------|-------------|
| `resource` | ResourceStatus | Imported resource details |
| `state` | string | Updated opaque adapter state |

## Error Codes

Standard JSON-RPC 2.0 error codes:

| Code | Name | Description |
|------|------|-------------|
| `-32700` | Parse Error | Invalid JSON received |
| `-32600` | Invalid Request | Request object is not valid JSON-RPC |
| `-32601` | Method Not Found | Unknown method name |
| `-32603` | Internal Error | Provider method returned an error |

Adapters may also return custom error codes with descriptive messages:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32603,
    "message": "AWS API error: AccessDeniedException: User is not authorized"
  }
}
```

## Request ID Sequencing

The CLI uses sequential integer IDs starting from 1. The adapter must include the matching `id` in each response. IDs are not reused within a single session.

## See Also

- [Adapter SDK](adapter-sdk) — Go SDK for implementing this protocol
- [Adapter Architecture](../../explanation/deploy/adapter-architecture) — Design overview
- [CLI Commands](cli-commands) — CLI that drives the protocol
