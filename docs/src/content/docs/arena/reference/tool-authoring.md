---
title: Tool Authoring
sidebar:
  order: 8
---

How to write Tool YAMLs for PromptArena: which `mode` to use, and how to wire each one.

For the full `Tool` schema (every field), see [Configuration Schema → Tool](/arena/reference/config-schema/#tool) or `schemas/v1alpha1/tool.json` in the repo.

## Modes at a glance

| `mode` | Use when | Required fields |
|---|---|---|
| `mock` (static) | Response is the same regardless of args | `mock_result` |
| `mock` (template) | Response depends on args (e.g. branch on order_id) | `mock_template` or `mock_template_file` |
| `live` | Tool calls a real HTTP endpoint | `http:` |
| `mcp` | Tool is exposed by an MCP server already configured at the arena level | (none — auto-discovered) |
| `exec` | Tool shells out to a local subprocess | `exec:` |
| `client` | Tool is handled by client code (SDK consumer or external runtime) | `client:` |

`mock_result` and `mock_template` are mutually exclusive on a single tool.

## Mock — static (`mock_result`)

Returns the same response for every call. Use this when the value is a constant or when no test case in your suite needs to differentiate.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get-weather
spec:
  name: get_weather
  description: Get the current weather for a city.
  mode: mock
  timeout_ms: 1000
  input_schema:
    type: object
    properties:
      city: { type: string }
    required: [city]
  output_schema:
    type: object
    properties:
      temperature_c: { type: number }
      conditions: { type: string }
  mock_result:
    temperature_c: 18
    conditions: cloudy
```

## Mock — input-branching (`mock_template`)

Returns a different response based on tool-call args. Args are parsed as a JSON map and exposed as the template's data context. The rendered output is parsed back as JSON.

This is the right answer when:
- One scenario should look up a real order, another should fail to find it
- A "happy path" persona needs `in_warranty: true` while a "hostile" persona needs `false`
- You want to keep all branching logic in YAML rather than writing code

**Do not write a custom executor for this case** — the template executor already exists.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: lookup-order
spec:
  name: lookup_order
  description: Look up an order by ID.
  mode: mock
  timeout_ms: 1000
  input_schema:
    type: object
    properties:
      order_id: { type: string }
    required: [order_id]
  output_schema:
    type: object
    properties:
      order_id: { type: string }
      in_warranty: { type: boolean }
  mock_template: |
    {{- if eq .order_id "ORD-2024-9999" -}}
    {"order_id":"ORD-2024-9999","in_warranty":true}
    {{- else if eq .order_id "ORD-2023-7788" -}}
    {"order_id":"ORD-2023-7788","in_warranty":false}
    {{- else -}}
    {"error":"not_found"}
    {{- end -}}
```

### Template language

`mock_template` is rendered with Go's [`text/template`](https://pkg.go.dev/text/template) (`Option("missingkey=zero")`). The args map is the data context, so `.order_id` accesses the `order_id` field of the call.

Supported control flow includes:

- `{{ if eq .field "value" }}…{{ else if … }}…{{ else }}…{{ end }}`
- `{{ range .items }}…{{ end }}`
- Comparison helpers: `eq`, `ne`, `lt`, `gt`, `le`, `ge`
- `printf`, `index`, and the rest of the standard template functions

The `{{- … -}}` form trims surrounding whitespace, which is what you want when the rendered output must parse as JSON.

### Long templates: `mock_template_file`

For templates that don't fit comfortably inline, point at a file (path is relative to the tool YAML):

```yaml
spec:
  mode: mock
  mock_template_file: templates/lookup-order.tmpl
```

### Multimodal mocks: `mock_parts`

For tools that should return image/audio/video/document content alongside JSON, add `mock_parts` (works with both `mock_result` and `mock_template`). See [Configuration Schema → Tool](/arena/reference/config-schema/#tool) for the full structure.

## Live — HTTP (`mode: live`)

Calls a real HTTP endpoint. Args are sent as JSON; response is the parsed JSON body.

```yaml
spec:
  mode: live
  http:
    url: https://api.example.com/orders/lookup
    method: POST
    headers:
      Content-Type: "application/json"
    headers_from_env:
      - API_TOKEN          # → "Authorization: Bearer ${API_TOKEN}"
    timeout_ms: 5000
    redact:                # fields stripped from logs
      - api_key
```

## MCP — discovered tools (`mode: mcp`)

The tool is provided by an MCP server configured at the arena level. The arena auto-discovers tools from configured servers; the Tool YAML just declares the contract.

```yaml
spec:
  mode: mcp
  # No additional config — the MCP client provides the executor.
```

## Exec — subprocess (`mode: exec`)

Calls a local subprocess; args are sent on stdin, response is read from stdout.

```yaml
spec:
  mode: exec
  exec:
    command: ./bin/lookup-order
    args: ["--format=json"]
    timeout_ms: 5000
```

## Client — handled outside the runtime (`mode: client`)

The runtime hands the tool call back to the SDK consumer (or an external system) for execution. Used when the executor lives outside the test harness — e.g. a real backend you want the LLM to call, but where Arena should not own the implementation.

```yaml
spec:
  mode: client
  client:
    timeout_ms: 5000
    categories: [filesystem]
    consent:
      required: true
      message: "Allow the agent to read your filesystem?"
      decline_strategy: error
    validate_output: true
```

## Choosing a mode

```
Need a deterministic test fixture?
├─ Same response every call            → mock + mock_result
└─ Response should depend on args      → mock + mock_template

Want the LLM to hit a real system?
├─ HTTP API I control                  → live + http
├─ Tool provided by an MCP server      → mcp
├─ Local CLI / script                  → exec
└─ Caller (SDK / app) handles it       → client
```

## See also

- [Configuration Schema → Tool](/arena/reference/config-schema/#tool) — full field reference
- [`examples/voice-refund-demo`](https://github.com/AltairaLabs/PromptKit/tree/main/examples/voice-refund-demo) — `mock_template` branching across three personas
- [`examples/customer-support`](https://github.com/AltairaLabs/PromptKit/tree/main/examples/customer-support) — `mode: mock` with static results
