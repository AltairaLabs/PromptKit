---
id: tools-from-a-reference
title: Derive a tool from a reference — don't guess its shape
summary: When the user names an entity that lives in or comes from a backend or another agent, that's a tool. Ask for an MCP server, API/OpenAPI spec, or sample payloads and derive the schema from it instead of inventing fields.
tags: [tools, scope, schema]
---

When the user talks about **entities** — records, accounts, orders, anything that lives in
or comes from a backend system or another agent — that is the signal for a **tool**. Do
not invent its `input_schema`/`output_schema`. Ask for the source of truth, in priority
order:

1. **An MCP server** → bind `mode: mcp` and point at the server. The real tool name and
   schema are discovered from the server at runtime — you author no schema at all. This is
   the most faithful binding when it exists.
2. **An API / OpenAPI spec or endpoint docs** → read it and derive `input_schema`,
   `output_schema`, and the `http` binding (url, method, request/response mapping) by hand.
   There is no auto-importer; you transcribe the contract from the doc.
3. **Sample request/response payloads** (a curl command, real JSON) → derive the schemas
   from the actual shapes you were given.
4. **Nothing available** → author a `mode: mock` tool as your best understanding, and tell
   the user it is a **guess to confirm**. Never present an invented contract as fact.

A live HTTP binding looks like:

```yaml
spec:
  mode: live
  input_schema:
    type: object
    properties:
      latitude: { type: number }
      longitude: { type: number }
  output_schema:
    type: object
    properties:
      temperature: { type: number }
  http:
    url: "https://api.example.com/v1/forecast"
    method: GET
    timeout_ms: 10000
    response:
      body_mapping: "{temperature: current.temperature_2m}"
```

Ask before you author: "Do you have an API spec, an MCP server, or a sample
request/response for this?" The answer determines the binding and the schema.
