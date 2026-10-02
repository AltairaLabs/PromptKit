---
title: RuntimeConfig
description: YAML schema reference for declarative SDK configuration
sidebar:
  order: 8
verified:
  commit: fd966db8c393d5338bb3556904f0d2e806f0d43a
  sources:
    - pkg/config/logging.go
    - pkg/config/provider_tuning.go
    - pkg/config/role.go
    - pkg/config/runtime_config.go
    - pkg/config/types.go
    - runtime/credentials/resolver.go
    - runtime/credentials/resolver_platform.go
    - runtime/credentials/types.go
    - runtime/hooks/exec_build.go
    - runtime/hooks/exec_hooks.go
    - runtime/hooks/execconfig/execconfig.go
    - runtime/inference/all/all.go
    - runtime/inference/huggingface/register.go
    - runtime/inference/openai/register.go
    - runtime/inference/systemone/register.go
    - runtime/logger/config.go
    - runtime/mcp/types.go
    - runtime/providers/all/all.go
    - runtime/providers/base/http_tuning.go
    - runtime/providers/base_provider.go
    - runtime/providers/bedrock/embedding_register.go
    - runtime/providers/claude/claude_multimodal.go
    - runtime/providers/claude/pricing_table.go
    - runtime/providers/cohere/rerank_register.go
    - runtime/providers/gemini/embedding_register.go
    - runtime/providers/huggingface/embedding_register.go
    - runtime/providers/imagen/factory.go
    - runtime/providers/mock/mock.go
    - runtime/providers/multimodal.go
    - runtime/providers/ollama/embedding_register.go
    - runtime/providers/openai/embedding_register.go
    - runtime/providers/registry.go
    - runtime/providers/replay/factory.go
    - runtime/providers/rerank_factory.go
    - runtime/providers/rerank_mock.go
    - runtime/providers/stream_retry.go
    - runtime/providers/stream_retry_driver.go
    - runtime/providers/streaming.go
    - runtime/providers/vertex/embedding_register.go
    - runtime/providers/vllm/factory.go
    - runtime/providers/voyageai/embedding_register.go
    - runtime/providers/voyageai/rerank_register.go
    - runtime/statestore/file/store.go
    - runtime/statestore/redis.go
    - runtime/statestore/types.go
    - runtime/tools/types.go
    - schemas/v1alpha1/runtime-config.json
    - sdk/conversation_tools.go
    - sdk/exec_tools.go
    - sdk/provider_file.go
    - sdk/runtime_config.go
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
| `providers` | Provider[] | Provider configurations, routed by each entry's `role`. |
| `tools` | map[string]ToolSpec | Tool implementation bindings keyed by pack tool name. |
| `mcp_servers` | MCPServerConfig[] | MCP tool server configurations. |
| `state_store` | StateStoreConfig | Conversation state persistence. |
| `logging` | LoggingConfigSpec | Log level, format, and common fields. |
| `evals` | map[string]ExecBinding | External eval process bindings keyed by eval type name. |
| `hooks` | map[string]ExecHook | External hook process configurations. |
| `sandboxes` | map[string]SandboxConfig | Named sandbox backends that hooks and selectors launch their processes in. |
| `selectors` | map[string]SelectorConfig | External selector processes keyed by selector name. |
| `skills` | SkillsConfig | Runtime skill wiring. |
| `tool_selector` | string | Name of a selector under `selectors` that narrows the tool set offered to the LLM each turn. |

---

### spec.providers[]

Array of provider configurations. Each entry configures credentials, model selection, and default generation parameters for one provider.

**Every entry is routed by its `role`.** Providers with a completion role
(`llm`, `image`, `video`) go into the agent pool — the first one declared becomes
the conversation's agent and the rest stay available by ID. Providers with a
capability role (`tts`, `stt`, `embedding`, `inference`, `rerank`) fill that
capability's slot, first declared winning.

```yaml
spec:
  providers:
    - id: main-llm
      role: llm                    # omitted role defaults to llm
      type: claude
      model: claude-sonnet-4-20250514
      credential:
        credential_env: ANTHROPIC_API_KEY

    - id: judge                    # stays in the pool, reachable by ID
      role: llm
      type: openai
      model: gpt-4o

    - id: voice
      role: tts                    # fills the TTS slot, not the agent slot
      type: elevenlabs
      credential:
        credential_env: ELEVENLABS_API_KEY

    - id: retrieval
      role: embedding              # becomes the default RAG provider
      type: openai
      model: text-embedding-3-small
      credential:
        credential_env: OPENAI_API_KEY
```

Validation requires `type` on every entry, and `model` on completion roles only
— for capability roles, `model` is an optional override of the provider's own
default. Two entries may not share an ID *within a role*; the same ID under two
different roles is fine, since the slots are separate.

A provider supplied programmatically (`WithProvider`, `WithTTS`, …) always wins
over one declared here; the config entry is kept in the pool rather than
discarded.

:::note[Superseded blocks]
`embedding_providers`, `tts_providers`, `stt_providers` and `inference_providers`
are the older, per-capability spelling of the same thing. They still work, but
`role:` is the preferred form and the four blocks are removed in v3. Declaring
the same ID in both spellings is rejected.
:::

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | no | Unique provider identifier. Other config refers to this provider by this value. Defaults to `type`. |
| `role` | string | no | What this provider is for: `llm` (default), `image`, `video`, `tts`, `stt`, `embedding`, `inference`, `rerank`. |
| `type` | string | yes | Provider type. Not an enum — it names a registered factory, and which factories exist depends on which provider packages are linked in. See [provider types](#provider-types). |
| `model` | string | for completion roles | Model name (e.g., `claude-sonnet-4-20250514`, `gpt-4o`). Optional for capability roles, where it overrides the provider default. |
| `base_url` | string | no | Custom API base URL. Overrides the default endpoint for the provider type. |
| `credential` | object | no | API key configuration. See [credential](#credential). |
| `defaults` | object | no | Default generation parameters. See [defaults](#defaults). |
| `pricing` | object | no | Token cost tracking. See [pricing](#pricing). |
| `platform` | object | no | Cloud platform config for hyperscaler hosting. See [platform](#platform). |
| `capabilities` | string[] | no | Declared provider capabilities: `text`, `streaming`, `vision`, `tools`, `json`, `audio`, `video`, `documents`. |
| `include_raw_output` | bool | no | Include raw API request/response in output for debugging. |
| `additional_config` | map[string]any | no | Provider-specific configuration not covered by other fields. |
| `headers` | map[string]string | no | Extra HTTP headers sent on every request, e.g. for a gateway. A header the provider sets itself, such as its credential header, is not replaced: the request fails with an error naming the header. |
| `request_timeout` | string | no | Wall-clock timeout for request/response calls (Predict, embeddings). Go duration string, e.g. `"2m"`. Does not apply to streaming. Default: `"60s"` for completion roles; capability roles keep the provider's own default. |
| `stream_idle_timeout` | string | no | Longest silence allowed on a streaming body before the stream is aborted. The timer resets on every byte. Default: `"30s"`. |
| `stream_retry` | object | no | Bounded retry for streaming requests. See [stream_retry](#stream_retry). |
| `stream_max_concurrent` | int | no | Maximum concurrent streaming requests in flight. Requests beyond the limit wait on the caller's context. `0` means unlimited (the default). |
| `http_transport` | object | no | HTTP connection pool tuning. See [http_transport](#http_transport). |

Which tuning fields take effect depends on the role:

| Roles | Honored | Rejected |
|-------|---------|----------|
| `llm`, `image`, `video` | `headers`, `request_timeout`, `stream_idle_timeout`, `stream_retry`, `stream_max_concurrent`, `http_transport`, `defaults.prompt_caching` | none |
| `tts`, `stt`, `embedding`, `inference`, `rerank` | `headers`, `request_timeout`, `http_transport` | `stream_idle_timeout`, `stream_retry`, `stream_max_concurrent`, `defaults.prompt_caching` |

The same rules apply whether the provider is declared under `spec.providers` or in its own provider file. A capability provider that sets a rejected field fails to load, with an error naming the field and the role. On a capability role, `http_transport` replaces the provider's transport with a pooled, instrumented one; without it, the provider keeps its own. A provider type that cannot take `headers`, `request_timeout` or `http_transport` for its role, such as the `mock` reranker, fails to load when any of them is set. The `openai` and `nvidia-topic-control` inference types also reject `request_timeout` alongside `additional_config.timeout_seconds`.

A duration string that does not parse, or is not positive, is logged and ignored, and the provider uses the default for that field.

#### provider types

`type` is a registry key, not a fixed enum. Each role has its own registry, so
the same name can mean different things under different roles, and a name is
valid only when the package that registers it is linked into the binary.

Importing the SDK registers these:

| Role | Types |
|------|-------|
| `llm` | `claude`, `openai`, `gemini`, `ollama` |
| `image` | `imagen` |
| `embedding` | `openai`, `gemini`, `ollama`, `voyageai`, `bedrock`, `vertex`, `huggingface` |
| `rerank` | `voyageai`, `cohere`, `mock` |
| `inference` | `huggingface`, `openai`, `systemone`, `nvidia-topic-control` (alias: `openai` + NemoGuard topic control) |

Each type calls exactly one vendor API; `base_url`, `credential` and
`additional_config` only adjust calls to that API. A gateway that serves the
same API (the Vercel AI Gateway for `systemone`, LiteLLM or vLLM for `openai`)
is only a `base_url` and a credential, not a new type.

Others need a blank import of their package: `vllm`, `replay` and `mock` (all `llm`)
are registered by `runtime/providers/vllm`, `runtime/providers/replay` and `runtime/providers/mock`. `runtime/providers/all` registers every provider package.
The `vllm` import:

```go
import _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
```

An unregistered type fails at construction rather than degrading — the
embedding and rerank paths name the types that *are* registered, which is the
quickest way to tell a typo from a missing import.

Bedrock, Vertex and Azure hosting of a chat model is not a separate `type` —
it is the [`platform`](#platform) block on `claude` or `openai`.

#### credential

Credentials are resolved in order of precedence: `api_key` > `credential_file` > `credential_env`.

| Field | Type | Description |
|-------|------|-------------|
| `api_key` | string | Explicit API key value. |
| `credential_file` | string | Path to a file containing the API key. |
| `credential_env` | string | Name of an environment variable containing the API key. |

#### defaults

Default generation parameters, applied to a request that does not set its own. A
prompt's `parameters` override them.

| Field | Type | Description |
|-------|------|-------------|
| `temperature` | float | Sampling temperature (e.g., `0.7`). |
| `top_p` | float | Top-p (nucleus) sampling parameter. |
| `max_tokens` | int | Maximum number of output tokens. `0` or `-1` sends no limit, so the model's own maximum applies. Claude requires a limit: `0` sends 4096 and `-1` is rejected. |
| `prompt_caching` | bool | Anthropic prompt caching on Claude providers. Default: `true`; `false` disables it. |

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

Retries a streaming request that fails before any content reaches the caller (the default `pre_first_chunk` window). With `retry_window: always`, a failure after content was forwarded emits a `Reset` signal so consumers discard what they received, then retries the whole request; the retry is billed as a new response.

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | bool | Turns retry on. Default: `false`. |
| `max_attempts` | int | Total attempts including the first. `2` means one retry. Default: `2`. |
| `initial_delay` | string | Base backoff before the first retry. Go duration string. Default: `"250ms"`. |
| `max_delay` | string | Maximum backoff per attempt. Go duration string. Default: `"2s"`. A `429` or `503` with `Retry-After` waits the longer of the two; a `Retry-After` over 60 seconds is not retried. |
| `retry_window` | string | `pre_first_chunk` (default) or `always`. |
| `budget` | object | Token bucket shared by all in-flight requests on this provider that limits retry attempts: `rate_per_sec` (float, refill rate) and `burst` (int, maximum tokens). Without it, only `max_attempts` limits retries. |

#### http_transport

Per-provider HTTP connection pool. The concurrent-stream ceiling per upstream is `max_conns_per_host` multiplied by the upstream's HTTP/2 `SETTINGS_MAX_CONCURRENT_STREAMS`.

| Field | Type | Description |
|-------|------|-------------|
| `max_conns_per_host` | int | Maximum TCP connections to one host (in use and idle). Default: `0` (unlimited). |
| `max_idle_conns_per_host` | int | Maximum idle keep-alive connections kept per host. Default: `100`. |
| `idle_conn_timeout` | string | How long an idle connection is kept. Go duration string. Default: `"90s"`. |

---

### spec.tools

Map of tool bindings. Keys are tool names that must match names declared in the pack.

| Field | Type | Description |
|-------|------|-------------|
| `exec` | object | Subprocess binding configuration. See [exec](#exec). |

#### exec

Subprocess binding for tools. A relative `command` is resolved against the process working directory.

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | Path to the executable. |
| `args` | string[] | Additional command arguments. |
| `runtime` | string | Execution mode: `exec` (one-shot, default) or `server` (long-running JSON-RPC). |
| `env` | string[] | Environment variable names to pass through from the host. |
| `timeout_ms` | int | Timeout for each call in milliseconds. Overrides the tool descriptor's timeout (30 seconds when the pack sets none). |

---

### spec.evals

Map of external eval process bindings. Keys are eval type names matching those used in the pack. Eval types not bound here resolve to built-in Go handlers. Eval bindings run one-shot, once per invocation; `runtime: server` on an eval binding fails validation.

Each value is an `ExecBinding`:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | yes | Path to the executable. |
| `args` | string[] | no | Additional command arguments. |
| `env` | string[] | no | Environment variable names to pass through from the host. |
| `timeout_ms` | int | no | Per-invocation timeout in milliseconds. |

---

### spec.hooks

Map of external hook bindings. Keys are hook names (arbitrary identifiers). Each hook binds an external process to pipeline lifecycle events. Hook processes run one-shot, once per invocation; `runtime: server` on a hook fails validation.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | yes | Path to the executable. |
| `args` | string[] | no | Additional command arguments. |
| `hook` | string | yes | Hook interface type: `provider`, `tool`, `session`, or `eval`. |
| `phases` | string[] | no | Lifecycle phases to intercept. See below. |
| `mode` | string | no | Execution mode: `filter` (synchronous, can modify/deny; default) or `observe` (async, fire-and-forget). |
| `env` | string[] | no | Environment variable names to pass through from the host. |
| `timeout_ms` | int | no | Per-invocation timeout in milliseconds. |
| `sandbox` | string | no | Name of a sandbox declared under [`spec.sandboxes`](#specsandboxes). Without it, the hook process runs directly on the host. |

#### Valid phases by hook type

| Hook type | Valid phases |
|-----------|-------------|
| `provider` | `before_call`, `after_call` |
| `tool` | `before_execution`, `after_execution` |
| `session` | `session_start`, `session_update`, `session_end` |
| `eval` | none; `eval` hooks ignore `phases` and `mode` and run once per eval result |

---

### spec.sandboxes

Map of named sandbox backends. Hooks and selectors refer to an entry by name in their `sandbox` field. A `sandbox` that names an undeclared entry fails validation.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | yes | Name of a registered sandbox factory. `direct` is always registered; any other mode needs its factory registered with `sandbox.RegisterFactory` before the config is loaded. |
| *(other keys)* | any | no | Passed to the factory as its configuration. Each factory defines its own keys. |

See [Sandbox Hooks](/sdk/how-to/hooks/sandbox-hooks/) for the available modes.

---

### spec.selectors

Map of external selector processes. Keys are selector names, which `skills.selector` and `tool_selector` refer to. A selector registered with `WithSelector` before `WithRuntimeConfig` keeps its name, and the entry of the same name here is skipped.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | yes | Path to the selector executable. |
| `args` | string[] | no | Additional command arguments. |
| `env` | string[] | no | Environment variable names to pass through from the host. |
| `timeout_ms` | int | no | Per-invocation timeout in milliseconds. |
| `sandbox` | string | no | Name of a sandbox declared under [`spec.sandboxes`](#specsandboxes). |

---

### spec.skills

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `selector` | string | no | Name of a selector under [`spec.selectors`](#specselectors) that narrows the skills offered to the LLM. Without it, every eligible skill is offered. |

---

### spec.tool_selector

Name of a selector under [`spec.selectors`](#specselectors). Each turn, the selector receives the last user message and the pack-declared tools, and the LLM is offered only the tools it returns. When the selector returns nothing or fails, the full tool list is offered. A name that is not declared under `spec.selectors` fails validation, as it does for `skills.selector`.

---

### spec.mcp_servers[]

Array of MCP (Model Context Protocol) server configurations. Each entry configures one MCP server, reached over stdio, `sse` or `streamable_http`.

Every entry needs `name` and one transport: `command` (stdio) or `url` (HTTP). An entry with `source` (a PromptArena MCPSource) fails to load in the SDK.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Unique server name. |
| `command` | string | conditional | Command to start the server process (stdio transport). |
| `args` | string[] | no | Command arguments. |
| `env` | map[string]string | no | Environment variables passed to the server process. |
| `working_dir` | string | no | Working directory for the server process. |
| `url` | string | conditional | URL of an HTTP MCP server. |
| `transport` | string | no | Transport adapter: `stdio`, `sse` or `streamable_http`. Defaults to `stdio` for `command`. For `url`, Streamable HTTP, falling back to `sse` when the server does not host a Streamable HTTP endpoint. |
| `headers` | map[string]string | no | HTTP headers sent with every request to the server. |
| `timeout_ms` | int | no | Per-request timeout in milliseconds. |
| `tool_filter` | object | no | Tool filtering configuration. See [tool_filter](#tool_filter). |

#### tool_filter

Controls which tools from the MCP server are exposed to the LLM. If `allowlist` is non-empty, only allowlisted tools are exposed and `blocklist` is not consulted. `blocklist` applies only when `allowlist` is empty. Entries ending in `*` are prefix matches.

| Field | Type | Description |
|-------|------|-------------|
| `allowlist` | string[] | Only expose tools with these names. |
| `blocklist` | string[] | Hide tools with these names. |

---

### spec.state_store

Configures conversation state persistence. Defaults to in-memory storage when omitted.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | no | Store implementation: `memory` (default), `redis`, or `file`. |
| `redis` | object | conditional | Redis configuration. Required when `type` is `redis`. See [redis](#redis). |
| `file` | object | conditional | Filesystem store configuration. Required when `type` is `file`. See [file](#file). |

#### redis

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `address` | string | yes | Redis server address (`host:port`). |
| `password` | string | no | Redis authentication password. |
| `database` | int | no | Redis database number, passed to the server as configured. Default: `0`. |
| `ttl` | string | no | Key TTL as a Go duration string (e.g., `24h`, `168h`). Default: `24h`. |
| `prefix` | string | no | Key prefix for all state keys. Default: `promptkit`. |

#### file

Single-machine, filesystem-backed persistence (survives process restarts; no cross-process locking).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `root` | string | yes | Directory under which per-conversation state is stored. Created if absent. |
| `fsync` | string | no | Durability policy: `off`, `on-save` (default), `on-append`. |
| `ttl_days` | int | no | At startup, remove conversation state older than this many days. Default: `0` (disabled). |

---

### spec.logging

Configures structured logging output.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `defaultLevel` | string | no | Default log level: `trace`, `debug`, `info` (default), `warn`, `error`. |
| `format` | string | no | Output format: `text` (default) or `json`. |
| `commonFields` | map[string]string | no | Key-value pairs added to every log entry. Useful for environment, service name, etc. |

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
      pricing:
        input_cost_per_1k: 0.003
        output_cost_per_1k: 0.015

    - id: embeddings
      role: embedding
      type: voyageai
      model: voyage-3
      credential:
        credential_env: VOYAGE_API_KEY

  tools:
    search_knowledge_base:
      exec:
        command: ./tools/search
        args: ["--format", "json"]
        runtime: server
        env: [DATABASE_URL]
        timeout_ms: 10000

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
```

---

## See Also

- [Use RuntimeConfig](/sdk/how-to/conversations/use-runtime-config/): how-to guide for loading and applying RuntimeConfig
- [Exec Tools](/sdk/how-to/tools/exec-tools/): how-to guide for subprocess tool bindings
- [Exec Hooks](/sdk/how-to/hooks/exec-hooks/): how-to guide for external process hooks
