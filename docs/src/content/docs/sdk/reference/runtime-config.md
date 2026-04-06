---
title: RuntimeConfig
description: YAML schema reference for declarative SDK configuration
sidebar:
  order: 8
---

RuntimeConfig is a Kubernetes-style YAML manifest that declaratively configures the SDK runtime environment. It separates _what_ an agent does (defined in a pack) from _how_ to run it (providers, tool bindings, state store, logging). This makes packs portable across environments while RuntimeConfig adapts them to each deployment target.

The Go types backing this schema live in `pkg/config/runtime_config.go` and `pkg/config/types.go`.

---

## Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiVersion` | string | yes | Must be `promptkit.altairalabs.ai/v1alpha1`. |
| `kind` | string | yes | Must be `RuntimeConfig`. |
| `metadata` | object | no | Standard resource metadata. |
| `spec` | object | yes | Runtime configuration specification. |

---

## metadata

Standard Kubernetes-style metadata. All fields are optional.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Human-readable name for this configuration. |
| `namespace` | string | Namespace for the resource. |
| `labels` | map[string]string | Key-value pairs for organizing resources. |
| `annotations` | map[string]string | Arbitrary metadata annotations. |

---

## spec

The `spec` object contains all runtime configuration. Every field in `spec` is optional — include only the sections you need.

| Field | Type | Description |
|-------|------|-------------|
| `providers` | Provider[] | LLM provider configurations. |
| `tools` | map[string]ToolSpec | Tool implementation bindings keyed by pack tool name. |
| `mcp_servers` | MCPServerConfig[] | MCP tool server configurations. |
| `state_store` | StateStoreConfig | Conversation state persistence. |
| `logging` | LoggingConfigSpec | Log levels, format, and per-module settings. |
| `evals` | map[string]ExecBinding | External eval process bindings keyed by eval type name. |
| `hooks` | map[string]ExecHook | External hook process configurations. |

---

### spec.providers[]

Array of LLM provider configurations. Each entry configures credentials, model selection, rate limits, and default generation parameters for one provider.

Validation requires `type` and `model` on every entry.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | no | Unique provider identifier. Used to reference this provider elsewhere. |
| `type` | string | yes | Provider type. One of: `claude`, `openai`, `gemini`, `ollama`, `vllm`, `voyageai`, `mock`, `replay`. |
| `model` | string | yes | Model name (e.g., `claude-sonnet-4-20250514`, `gpt-4o`). |
| `base_url` | string | no | Custom API base URL. Overrides the default endpoint for the provider type. |
| `credential` | object | no | API key configuration. See [credential](#credential). |
| `defaults` | object | no | Default generation parameters. See [defaults](#defaults). |
| `rate_limit` | object | no | Rate limiting. See [rate_limit](#rate_limit). |
| `pricing` | object | no | Token cost tracking. See [pricing](#pricing). |
| `platform` | object | no | Cloud platform config for hyperscaler hosting. See [platform](#platform). |
| `capabilities` | string[] | no | Declared provider capabilities: `text`, `streaming`, `vision`, `tools`, `json`, `audio`, `video`, `documents`. |
| `include_raw_output` | bool | no | Include raw API request/response in output for debugging. |
| `additional_config` | map[string]any | no | Provider-specific configuration not covered by other fields. |
| `request_timeout` | string | no | Wall-clock timeout for non-streaming HTTP calls (Predict, embeddings). Go duration string, e.g. `"60s"`, `"2m"`. Does not apply to streaming. |
| `stream_idle_timeout` | string | no | Max silence between bytes on a streaming body before the stream is aborted. Timer resets on every byte received. Default: `"30s"`. |
| `stream_retry` | object | no | Streaming retry configuration. See [stream_retry](#stream_retry). |
| `stream_max_concurrent` | int | no | Max concurrent streaming requests in flight. Requests beyond the limit block on the caller's context. `0` = unlimited (default). |
| `http_transport` | object | no | HTTP connection pool tuning. See [http_transport](#http_transport). |

#### credential

Credentials are resolved in order of precedence: `api_key` > `credential_file` > `credential_env`.

| Field | Type | Description |
|-------|------|-------------|
| `api_key` | string | Explicit API key value. Not recommended for production — prefer `credential_env`. |
| `credential_file` | string | Path to a file containing the API key. |
| `credential_env` | string | Name of an environment variable containing the API key. |

#### defaults

Default generation parameters applied to every request unless overridden per-call.

| Field | Type | Description |
|-------|------|-------------|
| `temperature` | float | Sampling temperature (e.g., `0.7`). |
| `top_p` | float | Top-p (nucleus) sampling parameter. |
| `max_tokens` | int | Maximum number of output tokens. |

#### rate_limit

| Field | Type | Description |
|-------|------|-------------|
| `rps` | int | Maximum requests per second. |
| `burst` | int | Maximum burst size above the steady-state rate. |

#### pricing

Used for cost tracking and reporting.

| Field | Type | Description |
|-------|------|-------------|
| `input_cost_per_1k` | float | Cost per 1,000 input tokens. |
| `output_cost_per_1k` | float | Cost per 1,000 output tokens. |

#### platform

Configures hyperscaler hosting platforms (Bedrock, Vertex, Azure) that provide managed access to LLM providers. The `platform.type` determines authentication and endpoint resolution, while the parent `type` field determines message/response handling.

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Platform type: `bedrock`, `vertex`, or `azure`. |
| `region` | string | Cloud region (e.g., `us-west-2`, `us-central1`). |
| `project` | string | Cloud project ID. Required for Vertex. |
| `endpoint` | string | Custom endpoint URL override. |
| `additional_config` | map[string]any | Platform-specific settings. |

#### stream_retry

Bounded retry for streaming requests. By default, retry only fires in the pre-first-chunk window (before any content has been forwarded to the caller), which is safe and invisible. Setting `retry_window: always` enables mid-stream retry: on failure after content has been forwarded, a `Reset` signal is emitted so consumers discard accumulated state, then the full request is retried from scratch. Mid-stream retry costs additional tokens because the provider generates a new response.

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Turn the retry loop on. Default: `false`. |
| `max_attempts` | int | Total attempts including the initial request. `2` = one retry. Default: `2`. |
| `initial_delay` | string | Base backoff before the first retry. Go duration string. Default: `"250ms"`. |
| `max_delay` | string | Maximum per-attempt backoff. Go duration string. Default: `"2s"`. |
| `retry_window` | string | `"pre_first_chunk"` (default, safe) or `"always"` (mid-stream reset retry, costs tokens). |
| `budget` | object | Token bucket that gates retry attempts to prevent thundering-herd reconnects. See below. |

**stream_retry.budget:**

| Field | Type | Description |
|-------|------|-------------|
| `rate_per_sec` | float | Sustained token refill rate. |
| `burst` | int | Maximum tokens that can accumulate. |

#### http_transport

Per-provider HTTP connection pool tuning. Controls how many TCP connections the provider opens to its upstream and how long idle connections linger. The effective concurrent-stream ceiling per upstream is `max_conns_per_host` multiplied by the upstream's HTTP/2 `SETTINGS_MAX_CONCURRENT_STREAMS` (typically 100-256).

| Field | Type | Description |
|-------|------|-------------|
| `max_conns_per_host` | int | Max TCP connections to any single host (in-use + idle). Default: `100`. |
| `max_idle_conns_per_host` | int | Max idle keep-alive connections retained per host. Default: `100`. |
| `idle_conn_timeout` | string | How long idle connections linger before being closed. Go duration string. Default: `"90s"`. |

---

### spec.tools

Map of tool bindings. Keys are tool names that must match names declared in the pack. Values configure how the tool is implemented at runtime.

Each tool spec can use one of several modes: `mock` (canned responses), `live` (HTTP), `exec` (subprocess), or `client` (client-side execution).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | no | Tool name (usually inferred from the map key). |
| `description` | string | yes | Human-readable description of the tool. |
| `input_schema` | object | yes | JSON Schema (Draft-07) defining the tool's input. |
| `output_schema` | object | yes | JSON Schema (Draft-07) defining the tool's output. |
| `mode` | string | yes | Execution mode: `mock`, `live`, `exec`, or `client`. |
| `timeout_ms` | int | no | Per-invocation timeout in milliseconds. |
| `mock_result` | any | no | Static mock response (mode: `mock`). |
| `mock_template` | string | no | Go template for dynamic mock responses (mode: `mock`). |
| `mock_parts` | MockPartSpec[] | no | Multimodal mock response parts (mode: `mock`). |
| `http` | object | no | HTTP binding configuration (mode: `live`). |
| `exec` | object | no | Subprocess binding configuration (mode: `exec`). See [exec](#exec). |
| `client` | object | no | Client-side execution configuration (mode: `client`). |

#### exec

Subprocess binding for tools. The command is resolved relative to the config file.

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | Path to the executable. |
| `args` | string[] | Additional command arguments. |
| `runtime` | string | Execution mode: `exec` (one-shot, default) or `server` (long-running JSON-RPC). |
| `env` | string[] | Environment variable names to pass through from the host. |
| `timeout_ms` | int | Per-invocation timeout in milliseconds. |

---

### spec.evals

Map of external eval process bindings. Keys are eval type names matching those used in the pack. Eval types not bound here resolve to built-in Go handlers.

Each value is an `ExecBinding`:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | yes | Path to the executable. |
| `args` | string[] | no | Additional command arguments. |
| `runtime` | string | no | Execution mode: `exec` (default) or `server`. |
| `env` | string[] | no | Environment variable names to pass through from the host. |
| `timeout_ms` | int | no | Per-invocation timeout in milliseconds. |

---

### spec.hooks

Map of external hook bindings. Keys are hook names (arbitrary identifiers). Each hook binds an external process to pipeline lifecycle events.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | yes | Path to the executable. |
| `args` | string[] | no | Additional command arguments. |
| `hook` | string | yes | Hook interface type: `provider`, `tool`, or `session`. |
| `phases` | string[] | no | Lifecycle phases to intercept. See below. |
| `mode` | string | no | Execution mode: `filter` (synchronous, can modify/deny; default) or `observe` (async, fire-and-forget). |
| `runtime` | string | no | Process mode: `exec` (default) or `server`. |
| `env` | string[] | no | Environment variable names to pass through from the host. |
| `timeout_ms` | int | no | Per-invocation timeout in milliseconds. |

#### Valid phases by hook type

| Hook type | Valid phases |
|-----------|-------------|
| `provider` | `before_call`, `after_call` |
| `tool` | `before_execution`, `after_execution` |
| `session` | `session_start`, `session_update`, `session_end` |

---

### spec.mcp_servers[]

Array of MCP (Model Context Protocol) server configurations. Each entry starts and manages a stdio-based MCP server process.

Validation requires `name` and `command` on every entry.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique server name. |
| `command` | string | yes | Command to start the server process. |
| `args` | string[] | no | Command arguments. |
| `env` | map[string]string | no | Environment variables passed to the server process. |
| `working_dir` | string | no | Working directory for the server process. |
| `timeout_ms` | int | no | Per-request timeout in milliseconds. |
| `tool_filter` | object | no | Tool filtering configuration. See [tool_filter](#tool_filter). |

#### tool_filter

Controls which tools from the MCP server are exposed to the LLM. If both lists are set, `allowlist` is applied first, then `blocklist` removes from the result.

| Field | Type | Description |
|-------|------|-------------|
| `allowlist` | string[] | Only expose tools with these names. |
| `blocklist` | string[] | Hide tools with these names. |

---

### spec.state_store

Configures conversation state persistence. Defaults to in-memory storage when omitted.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | no | Store implementation: `memory` (default) or `redis`. |
| `redis` | object | conditional | Redis configuration. Required when `type` is `redis`. See [redis](#redis). |

#### redis

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `address` | string | yes | Redis server address (`host:port`). |
| `password` | string | no | Redis authentication password. |
| `database` | int | no | Redis database number (0-15, default: 0). |
| `ttl` | string | no | Key TTL as a Go duration string (e.g., `24h`, `168h`). Default: `24h`. |
| `prefix` | string | no | Key prefix for all state keys. Default: `promptkit`. |

---

### spec.logging

Configures structured logging output.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `defaultLevel` | string | no | Default log level: `trace`, `debug`, `info` (default), `warn`, `error`. |
| `format` | string | no | Output format: `text` (default) or `json`. |
| `commonFields` | map[string]string | no | Key-value pairs added to every log entry. Useful for environment, service name, etc. |
| `modules` | ModuleLoggingConfig[] | no | Per-module log level overrides. See [modules](#modules). |

#### modules

Override log levels for specific subsystems. More specific module names take precedence over less specific ones.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Module name using dot notation (e.g., `runtime`, `runtime.pipeline`, `providers.openai`). |
| `level` | string | Log level for this module: `trace`, `debug`, `info`, `warn`, `error`. |
| `fields` | map[string]string | Additional fields added to logs from this module. |

---

## Complete Example

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: production
  labels:
    env: prod
spec:
  providers:
    - id: main-llm
      type: claude
      model: claude-sonnet-4-20250514
      credential:
        credential_env: ANTHROPIC_API_KEY
      defaults:
        temperature: 0.7
        max_tokens: 4096
      rate_limit:
        rps: 10
        burst: 20
      pricing:
        input_cost_per_1k: 0.003
        output_cost_per_1k: 0.015
      request_timeout: "60s"
      stream_idle_timeout: "30s"
      stream_retry:
        enabled: true
        max_attempts: 2
        retry_window: pre_first_chunk  # or "always" for mid-stream reset retry
        budget:
          rate_per_sec: 5
          burst: 10
      stream_max_concurrent: 100
      http_transport:
        max_conns_per_host: 200
        max_idle_conns_per_host: 200

    - id: embeddings
      type: voyageai
      model: voyage-3
      credential:
        credential_env: VOYAGE_API_KEY

  tools:
    search_knowledge_base:
      description: Search the knowledge base
      input_schema:
        type: object
        properties:
          query:
            type: string
      output_schema:
        type: object
        properties:
          results:
            type: array
      mode: exec
      exec:
        command: ./tools/search
        args: ["--format", "json"]
        runtime: server
        env: [DATABASE_URL]
        timeout_ms: 5000

    get_weather:
      description: Get current weather
      input_schema:
        type: object
        properties:
          location:
            type: string
      output_schema:
        type: object
      mode: live
      http:
        url: https://api.weather.com/v1/current
        method: GET

  evals:
    custom_accuracy:
      command: ./evals/accuracy
      args: ["--strict"]
      env: [EVAL_API_KEY]
      timeout_ms: 30000

  hooks:
    audit_log:
      command: ./hooks/audit
      hook: provider
      phases: [before_call, after_call]
      mode: observe
      env: [AUDIT_ENDPOINT]
      timeout_ms: 2000

    pii_filter:
      command: ./hooks/pii-filter
      hook: tool
      phases: [after_execution]
      mode: filter
      timeout_ms: 1000

  mcp_servers:
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
      timeout_ms: 10000
      tool_filter:
        blocklist: [delete_file]

    - name: database
      command: ./mcp-servers/db-server
      env:
        DATABASE_URL: postgres://localhost:5432/mydb
      working_dir: /opt/mcp
      tool_filter:
        allowlist: [query, list_tables]

  state_store:
    type: redis
    redis:
      address: localhost:6379
      password: secret
      database: 1
      ttl: 168h
      prefix: myapp

  logging:
    defaultLevel: info
    format: json
    commonFields:
      service: my-agent
      env: production
    modules:
      - name: runtime.pipeline
        level: debug
      - name: providers
        level: warn
```

---

## See Also

- [Use RuntimeConfig](/sdk/how-to/use-runtime-config/) — how-to guide for loading and applying RuntimeConfig
- [Exec Tools](/sdk/how-to/exec-tools/) — how-to guide for subprocess tool bindings
- [Exec Hooks](/sdk/how-to/exec-hooks/) — how-to guide for external process hooks
