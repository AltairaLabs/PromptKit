---
name: promptarena-authoring
description: Author valid PromptArena kit configs; use when building or editing scenarios, providers, prompts, tools.
---

# Building a PromptArena Kit

A PromptArena kit tests an agent. **There is no point building an agent without clear,
measurable success criteria** — so this workflow is success-first. Read the bundled
catalogs in `reference/` instead of calling `promptarena explain`/`schema` repeatedly:

- `reference/evals-and-assertions.md` — every assertion/eval type, level, and score.
- `reference/config-fields.md` — fields per config kind.
- `reference/cli.md` — the command surface.

## Workflow

1. **Define success first.** If the use case is fuzzy, pin it down — grounded in what
   PromptArena can express. Turn each success criterion into a concrete assertion/eval
   early; those are the spec. Don't write a prompt before you know how you'll measure it.
2. **Map use case → platform concepts.** Decide which you need: workflow states, tools,
   compositions, evals/assertions. See `reference/evals-and-assertions.md`.
3. **Draw the scope line — especially tools.** The pack defines tools; it does not
   implement them. Per tool decide: `mode: mock` (test-only), bind an existing API
   (`http`/`mcp`/`exec`), or have the agent build a separate external service. (See the
   tool-boundary idiom below.)
4. **Lay it out and scaffold.** Canonical layout and naming:
   `config.arena.yaml`, `prompts/*.prompt.yaml`, `providers/*.provider.yaml`,
   `scenarios/*.scenario.yaml`, `tools/*.tool.yaml`, `mock-responses.yaml`. Start from the
   skeletons below; each carries a `$schema` modeline for editor autocomplete.
5. **Build against mocks.** Use a `type: mock` provider; mock response keys match the
   scenario's `metadata.name`, not `spec.id`. Tools still execute for real.
6. **Run it — pick a surface.**
   - **TUI** — does NOT run inside a Claude/Codex/Gemini session. Tell the user to run it
     in a separate terminal: `promptarena run` (no `--ci`).
   - **Web** — `promptarena serve` works as a background task from the session.
   - **Offline** — `promptarena run --ci --formats html,json`.
7. **Try real providers last.** Swap mock→real only once the kit is green against mocks.
8. **Let the user play** with `promptarena chat`.
9. **Deploy.** The pack carries tool definitions/bindings, not backends — deploy any
   externally-bound tools separately and configure their URLs/credentials. Then
   `promptarena deploy` + an adapter, or commit a CI workflow that builds, tests, deploys.
10. **Do not gold-plate.** Working configs and passing measures first. No fancy READMEs
    until the kit runs green. The deliverable is a *measured* kit, not documentation.

Validate every file with `promptarena validate` before `promptarena run`. Use only
assertion types listed in `reference/evals-and-assertions.md` — e.g. `contains_any`,
`min_length`, `max_length`, `llm_judge` — never invent type names.

## Skeletons

Minimal valid configs. Copy, then fill in. The `$schema` modeline gives editor
autocomplete and documents which schema applies.

### config.arena.yaml

```yaml
# yaml-language-server: $schema=https://promptkit.altairalabs.ai/schemas/v1alpha1/arena.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: my-kit
spec:
  prompt_configs:
    - id: assistant
      file: prompts/assistant.prompt.yaml
  providers:
    - file: providers/mock.provider.yaml
  scenarios:
    - file: scenarios/basic.scenario.yaml
  defaults:
    temperature: 0.7
    max_tokens: 1024
    output:
      dir: out
      formats: ["json", "html"]
```

### prompts/assistant.prompt.yaml

```yaml
# yaml-language-server: $schema=https://promptkit.altairalabs.ai/schemas/v1alpha1/promptconfig.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: assistant
spec:
  task_type: general
  version: "1.0.0"
  description: A helpful assistant.
  system_template: |
    You are a helpful assistant. Be concise and accurate.
```

### providers/mock.provider.yaml

```yaml
# yaml-language-server: $schema=https://promptkit.altairalabs.ai/schemas/v1alpha1/provider.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: mock
spec:
  id: mock-provider
  type: mock
  model: mock-model
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1024
  additional_config:
    response: "This is a mock response for testing."
```

### scenarios/basic.scenario.yaml

```yaml
# yaml-language-server: $schema=https://promptkit.altairalabs.ai/schemas/v1alpha1/scenario.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic
spec:
  task_type: general
  description: Basic greeting and Q&A.
  turns:
    - role: user
      content: "What's the capital of France?"
      assertions:
        - type: contains_any
          params:
            patterns: ["Paris", "paris"]
          message: "Response should mention Paris"
        - type: max_length
          params:
            max: 400
          message: "Response should be concise"
```

### tools/example.tool.yaml

```yaml
# yaml-language-server: $schema=https://promptkit.altairalabs.ai/schemas/v1alpha1/tool.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: lookup
spec:
  name: lookup
  description: Look up a record by id.
  input_schema:
    type: object
    properties:
      id:
        type: string
    required: ["id"]
  output_schema:
    type: object
    properties:
      value:
        type: string
  # mode: mock returns canned data for tests. To use a real backend, swap to a
  # binding — mode: live with an `http:` block, mode: mcp, or mode: exec — and
  # deploy that service separately. The pack ships this definition, not the impl.
  mode: mock
  mock_result:
    value: "example"
```

## Idioms

### Assertions judge; evals measure

PromptArena is an **assertion** framework. Eval handlers are *inputs* to assertions:
an eval handler emits `Score` as a raw signal (0..1) and nothing else. The pass/fail
threshold lives on a `type: assertion` wrapper:

```yaml
assertions:
  - type: assertion
    eval:
      type: toxicity        # eval handler — emits a raw score
    max_score: 0.2          # threshold lives HERE, on the assertion
```

Putting `min_score`/`max_score` on the inner eval is a common trap — the eval must
stay a pure primitive. Guardrails reuse the same eval primitives but enforce in
production; assertions are test-only and may observe guardrail firings.

### Mock providers simulate the LLM, not the tools

A provider with `type: mock` simulates **only the LLM's decisions** — the text it
returns and which tools it calls. The tools themselves run for real (InMemoryStore,
workflow state machine, memory). Point the provider at a response file:

```yaml
spec:
  type: mock
  additional_config:
    mock_config: mock-responses.yaml   # relative to the arena config directory
```

Response keys MUST match the scenario's `metadata.name`, NOT `spec.id`:

```yaml
scenarios:
  my-scenario-name:
    turns:
      1:
        response: "I'll look that up"
        tool_calls:
          - name: memory__recall
            arguments: { query: "user preferences" }
      2: "Based on what I found..."
```

The `--mock-provider` CLI flag is different: it replaces ALL providers with a generic
mock that ignores `mock-responses.yaml`. If your example ships a `providers/mock-provider.yaml`,
run it WITHOUT `--mock-provider`.

### Validate against the binary's own schemas

Every config type (scenario, provider, prompt, tool, arena) has a JSON schema. The
schema embedded in your installed `promptarena` binary is the source of truth — it is
the exact version `promptarena validate` enforces. Prefer it over the public web copy,
which may be a different release.

- `promptarena schema <type>` — print the authoritative schema for a type.
- `promptarena validate` — check your configs before running.

Author configs to the schema first; don't guess field names.

### A pack defines tools; it does not implement them

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

