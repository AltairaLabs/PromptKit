---
name: arena-example
description: Generate valid PromptArena example configurations. Use when creating new Arena examples, scenarios, provider configs, prompt configs, or tool configs.
argument-hint: "<description of the example to create>"
allowed-tools: Read,Write,Glob,Grep,Bash
---

# Create PromptArena Example

Generate a complete, schema-valid PromptArena example based on: $ARGUMENTS

## Process

1. **Read existing examples** — before creating any config file, read at least one working example of the same `kind` from `examples/`. Use Glob to find them:
   - Arena: `examples/*/config.arena.yaml`
   - Scenario: `examples/*/scenarios/*.scenario.yaml`
   - Provider: `examples/*/providers/*.provider.yaml`
   - Prompt: `examples/*/prompts/*.yaml`
   - Tool: `examples/*/tools/*.tool.yaml`
   - Persona: `examples/*/personas/*.persona.yaml`

2. **Read the JSON schema** — read the relevant schema from `schemas/v1alpha1/` to verify required fields:
   - `schemas/v1alpha1/arena.json`
   - `schemas/v1alpha1/scenario.json`
   - `schemas/v1alpha1/promptconfig.json`
   - `schemas/v1alpha1/provider.json`
   - `schemas/v1alpha1/tool.json`
   - `schemas/v1alpha1/persona.json`

3. **Generate configs** matching the schemas exactly, using the existing examples as templates.

4. **Validate** by running: `env PROMPTKIT_SCHEMA_SOURCE=local ./bin/promptarena run --config <arena-config> --mock-provider --ci --formats json`
   - Build first if needed: `make build-arena`
   - If validation fails, read the error, fix the config, and retry.

## Required Config Patterns

### Arena Config (`config.arena.yaml`)
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: <name>
spec:
  prompt_configs:
    - id: <prompt-task-type>
      file: prompts/<file>.yaml
  providers:
    - file: providers/<file>.provider.yaml
  scenarios:
    - file: scenarios/<file>.scenario.yaml
  defaults:
    temperature: 0.7
    max_tokens: 1500
    seed: 42
    concurrency: 3
    output:
      dir: out
      formats: ["json", "html"]
```

### Provider (`providers/<name>.provider.yaml`)
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: <provider-name>
spec:
  id: <provider-id>
  type: openai|claude|gemini|mock
  model: <model-name>
  # base_url: http://custom-endpoint/v1  # optional
  defaults:
    temperature: 0.7
    max_tokens: 1500
    top_p: 1.0          # REQUIRED — always include top_p
  pricing:               # optional
    input_cost_per_1k: 0.00015
    output_cost_per_1k: 0.0006
```

**CRITICAL**: `defaults.top_p` is required by the schema. Always include it.

### Prompt Config (`prompts/<name>.yaml`)
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: <name>
spec:
  task_type: <task-type>        # REQUIRED — must match scenario task_type
  version: "1.0.0"              # REQUIRED
  description: "<description>"  # REQUIRED
  system_template: |            # REQUIRED
    Your system prompt here.
```

**CRITICAL**: `kind` is `PromptConfig` (not `Prompt`). Fields `version` and `description` are required. There is NO `id` field in spec — use `task_type` instead. The `prompt_configs[].id` in the arena config must match the `task_type` here.

### Judge Prompt Config (for `llm_judge` assertions)
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: <judge-name>
spec:
  task_type: <judge-task-type>
  version: "1.0.0"
  description: "<description>"
  system_template: |
    You are an impartial judge. Respond with JSON {"passed":bool,"score":number,"reasoning":string}.

    CRITERIA:
    {{criteria}}

    CONVERSATION:
    {{conversation}}

    RESPONSE:
    {{response}}
  variables:
    - name: criteria
      required: true
      description: Evaluation criteria
    - name: conversation
      required: false
      description: Full conversation transcript
    - name: response
      required: true
      description: Assistant response being evaluated
```

**CRITICAL**: Judge prompts MUST include `{{criteria}}`, `{{conversation}}`, and `{{response}}` template variables.

### Scenario (`scenarios/<name>.scenario.yaml`)
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: <name>
spec:
  id: "<scenario-id>"
  task_type: <task-type>          # must match a prompt_configs[].id
  description: "<description>"
  turns:
    - role: user
      content: "<user message>"
      assertions:
        - type: <assertion-type>
          params: { ... }
          message: "<failure message>"
```

### Self-Play Turns
```yaml
turns:
  - role: user
    content: "Initial message to seed the conversation"
  - role: gemini-user            # self-play provider role
    persona: curious-learner     # optional persona name
    turns: 4                     # number of self-play turns
    assertions:
      - type: content_matches
        params:
          pattern: "(?i)relevant-topic"
```

Self-play requires `self_play` config in the arena config:
```yaml
spec:
  self_play:
    enabled: true
    roles:
      - id: gemini-user
        provider: <provider-id>
```

### LLM Judge Assertions

Arena config needs judges:
```yaml
spec:
  judges:
    - name: <judge-name>
      provider: <judge-provider-id>
  judge_defaults:
    prompt: <judge-prompt-task-type>
```

Judge provider needs its own provider config in `group: judge`:
```yaml
providers:
  - file: providers/assistant.provider.yaml
    group: default
  - file: providers/judge.provider.yaml
    group: judge
```

Scenario usage:
```yaml
assertions:
  - type: llm_judge
    params:
      criteria: |
        Evaluation criteria here.
      judge: <judge-name>
      min_score: 0.7
    message: "Quality check"

conversation_assertions:
  - type: llm_judge_conversation
    params:
      judge: <judge-name>
      criteria: |
        Overall conversation quality criteria.
      min_score: 0.7
    message: "Overall quality"
```

### Trials (statistical repetition)
```yaml
spec:
  trials: 5
  turns:
    - role: user
      content: "..."
      assertions:
        - type: min_length
          params:
            min: 50
          pass_threshold: 0.8    # pass in 80% of trials
```

### Tool Config (`tools/<name>.tool.yaml`)
```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: <tool-name>
spec:
  name: <tool-name>
  description: "<description>"
  mode: mock
  timeout_ms: 2000
  input_schema:
    type: object
    properties:
      param1:
        type: string
        description: "<description>"
    required: ["param1"]
  output_schema:
    type: object
    properties:
      result:
        type: string
  mock_result:
    result: "static response"
```

## Common Assertion Types

| Type | Params | Use for |
|------|--------|---------|
| `content_matches` | `pattern`, `message` | Regex on response |
| `content_includes` | `patterns` (list) | Response contains strings |
| `content_excludes` | `patterns` (list) | Response must NOT contain |
| `min_length` | `min` | Minimum response length |
| `llm_judge` | `criteria`, `judge`, `min_score` | Quality scoring per turn |
| `llm_judge_conversation` | `criteria`, `judge`, `min_score` | Quality scoring for full conversation |
| `tools_called` | `tool_names` (list) | Verify tool usage |
| `cost_budget` | `max_cost_usd` or `max_total_tokens` | Cost/token limits |
| `is_valid_json` | `allow_wrapped`, `extract_json` | JSON response validation |

## Directory Structure

```
examples/<name>/
  config.arena.yaml
  providers/
    <name>.provider.yaml
  prompts/
    <name>.yaml
  scenarios/
    <name>.scenario.yaml
  tools/                    # optional
    <name>.tool.yaml
  personas/                 # optional
    <name>.persona.yaml
  mock-responses.yaml       # optional, for mock providers
  README.md
```

## Validation

After generating all files, ALWAYS validate:

```bash
make build-arena
env PROMPTKIT_SCHEMA_SOURCE=local ./bin/promptarena run \
  --config examples/<name>/config.arena.yaml \
  --mock-provider --ci --formats json
```

Fix any schema validation errors and retry until it passes config loading. Note that `--mock-provider` only replaces the assistant provider — judge assertions will fail without API keys, which is expected.
