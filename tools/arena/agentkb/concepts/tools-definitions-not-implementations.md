---
id: tools-definitions-not-implementations
title: A pack defines tools; it does not implement them
summary: A kind:Tool config is a contract plus a binding — never backend code. Decide mock vs existing API vs new external service up front.
tags: [tools, scope, deploy]
---

A PromptPack ships tool **definitions**, not tool **implementations**. A `kind: Tool`
config declares the contract — `name`, `description`, `input_schema`, `output_schema` —
plus agent-facing usage instructions and a binding `mode`. It never contains the backend
code. Decide, per tool:

- **Mock** (`mode: mock`) — a canned/templated result that travels with the pack. For
  tests and demos only; not a real backend.
- **Bind an existing API** (`mode: live`/`http`, `mcp`, or `exec`) — point the definition
  at a service you already run. The pack carries only the binding (URL/command/server ref
  + how the agent should use it); the service stays external.
- **Build a new tool** — if no backend exists, a coding agent can write one, but it is a
  **separate deployable**, outside the pack and the `deploy` mechanism. The pack just
  references it.

At deploy time the pack carries definitions and bindings, **not** tool backends. Ensure
any externally-bound tools are deployed independently and their URLs/credentials are
configured in the target environment.
