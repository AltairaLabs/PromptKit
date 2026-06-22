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
   tool-boundary idiom below.) When the user names an **entity** that lives in or comes
   from a backend or another agent, that is a tool — **ask for its source of truth** (an
   MCP server, an API/OpenAPI spec, or sample payloads) and derive the schema from it
   instead of guessing. See the "Derive a tool from a reference" idiom.
4. **Lay it out, scaffold, and give it a personality.** Canonical layout and naming:
   `config.arena.yaml`, `prompts/*.prompt.yaml`, `providers/*.provider.yaml`,
   `scenarios/*.scenario.yaml`, `tools/*.tool.yaml`, `mock-responses.yaml`. Start from the
   skeletons below; each carries a `$schema` modeline for editor autocomplete. When you
   author the system prompt, **capture the agent's personality from the user** (identity,
   tone, guidelines) — the user will have an opinion. See the "Capture the agent's
   personality" idiom.
5. **Build against mocks, then stress with self-play.** Use a `type: mock` provider; mock
   response keys match the scenario's `metadata.name`, not `spec.id`. Tools still execute
   for real. Because agents are non-deterministic, add **self-play personas** (a
   cooperative, a confused, an impatient, and an adversarial user) to confirm your
   guardrails fire and your assertions are sensible — the adversarial persona should trip
   them. See the "Self-play personas" idiom.
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
