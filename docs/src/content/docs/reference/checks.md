---
title: Checks Reference
sidebar:
  order: 1
---

PromptKit has a unified check system: one set of check types usable across three surfaces.

- **Assertions** -- validate LLM behavior in Arena test scenarios (`assertions:` field in scenario YAML).
- **Guardrails** -- enforce runtime policy in production (`validators:` field in pack YAML).
- **Evals** -- monitor quality in production (`evals:` field in pack YAML).

All check types are implemented as `EvalTypeHandler` instances registered in the `runtime/evals/` package. See the [Unified Check Model](/concepts/validation/) for the conceptual overview.

**Surface legend used in the tables below:**

| Symbol | Surface |
|--------|---------|
| **A** | Assertion |
| **G** | Guardrail |
| **E** | Eval |

"Streaming" indicates whether the check supports incremental evaluation during streaming responses (relevant for guardrails).

---

## Content Checks

| Type | Aliases | Params | Surfaces | Streaming |
|------|---------|--------|----------|-----------|
| `contains` | `content_includes` | `patterns` (string[]) | A G E | No |
| `regex` | `content_matches` | `pattern` (string) | A G E | No |
| `content_excludes` | `banned_words`, `content_not_includes` | `patterns` (string[]) | A G E | Yes |
| `contains_any` | `content_includes_any` | `patterns` (string[]) | A E | No |
| `min_length` | -- | `min` or `min_characters` (int) | A E | No |
| `max_length` | `length` | `max` or `max_characters` (int), `max_tokens` (int) | A G E | Yes |
| `sentence_count` | `max_sentences` | `max` or `max_sentences` (int) | A G E | No |
| `field_presence` | `required_fields` | `fields` or `required_fields` (string[]) | A G E | No |
| `cosine_similarity` | -- | `reference` (string), `min_similarity` (float) | A E | No |

When `content_excludes` is invoked via the `banned_words` alias, `match_mode` defaults to `word_boundary`.

**Example -- assertion (scenario YAML):**

```yaml
assertions:
  - type: contains
    params:
      patterns: ["thank you", "welcome"]
```

**Example -- guardrail (pack YAML):**

```yaml
validators:
  - type: banned_words
    params:
      patterns: ["competitor-name", "internal-only"]
```

**Example -- eval (pack YAML):**

```yaml
evals:
  - id: response_length
    type: max_length
    trigger: every_turn
    params:
      max: 500
```

---

## JSON & Structure Checks

| Type | Aliases | Params | Surfaces |
|------|---------|--------|----------|
| `json_valid` | `is_valid_json`, `valid_json` | -- | A E |
| `json_schema` | -- | `schema` (object) | A E |
| `json_path` | -- | `expression` (string), `expected`, `contains`, `min_results`, `max_results` | A E |

**Example:**

```yaml
assertions:
  - type: json_path
    params:
      expression: "$.order.status"
      expected: "confirmed"
```

---

## Tool Checks (Turn-Level)

These checks evaluate tool usage within a single assistant turn.

| Type | Aliases | Params | Surfaces |
|------|---------|--------|----------|
| `tools_called` | `tool_called` | `tool_names` (string[]) | A G E |
| `tools_not_called` | -- | `tool_names` (string[]) | A G E |
| `tool_args` | -- | `tool_name` (string), `expected_args` (object) | A E |
| `tool_calls_with_args` | -- | `tool_name`, `expected_args`, `result_includes` | A E |
| `tool_call_count` | -- | `tool` (string), `min` (int), `max` (int) | A E |
| `tool_call_sequence` | -- | `sequence` (string[]) | A E |
| `tool_call_chain` | -- | `chain` (string[]) | A E |
| `tool_anti_pattern` | -- | `patterns` (array of `{sequence, message}`) | A E |
| `tool_no_repeat` | -- | `tools` (string[]), `max_repeats` (int) | E |
| `tool_efficiency` | -- | `max_calls`, `max_errors`, `max_error_rate` | E |
| `no_tool_errors` | -- | -- | A E |
| `tool_result_includes` | -- | `tool_name`, `patterns` (string[]) | A E |
| `tool_result_matches` | -- | `tool_name`, `pattern` (string) | A E |
| `tool_result_has_media` | -- | `tool_name` | E |
| `tool_result_media_type` | -- | `tool_name`, `media_type` | E |

**Example:**

```yaml
assertions:
  - type: tool_call_sequence
    params:
      sequence: ["lookup_customer", "create_ticket"]
```

---

## Tool Checks (Session-Level)

These checks evaluate tool usage across the entire session.

| Type | Aliases | Params | Surfaces |
|------|---------|--------|----------|
| `tools_called` (session) | `tool_called` | `tool_names` (string[]) | A G E |
| `tools_not_called` (session) | -- | `tool_names` (string[]) | A G E |
| `tool_args` (session) | -- | `tool_name`, `expected_args` | A E |
| `tool_args_excluded_session` | `tools_not_called_with_args` | `tool_name`, `excluded_args` | A E |

Session-level tool checks use the `on_session_complete` or `on_conversation_complete` trigger.

---

## Agent & Skill Checks

| Type | Params | Surfaces |
|------|--------|----------|
| `agent_invoked` | `agent_names` (string[]) | A E |
| `agent_not_invoked` | `agent_names` (string[]) | A E |
| `agent_response_contains` | `agent_name`, `patterns` | E |
| `skill_activated` | `skill_names` (string[]) | A E |
| `skill_not_activated` | `skill_names` (string[]) | A E |
| `skill_activation_order` | `sequence` (string[]) | A E |

**Example:**

```yaml
assertions:
  - type: agent_invoked
    params:
      agent_names: ["billing-agent"]
```

---

## Workflow Checks

| Type | Params | Surfaces |
|------|--------|----------|
| `workflow_complete` | -- | A E |
| `workflow_state_is` | `state` (string) | A E |
| `workflow_transitioned_to` | `state` (string) | A E |
| `workflow_transition_order` | `sequence` (string[]) | A E |
| `workflow_tool_access` | `rules` (array of `{state, allowed}`) | A E |

**Example:**

```yaml
assertions:
  - type: workflow_transition_order
    params:
      sequence: ["triage", "investigation", "resolution"]
```

---

## Media Checks

| Type | Params | Surfaces |
|------|--------|----------|
| `image_format` | `formats` (string[]) | A E |
| `image_dimensions` | `min_width`, `max_width`, `min_height`, `max_height` | A E |
| `audio_format` | `formats` (string[]) | A E |
| `audio_duration` | `min_seconds`, `max_seconds` | A E |
| `video_duration` | `min_seconds`, `max_seconds` | A E |
| `video_resolution` | `min_width`, `max_width`, `presets` (string[]) | A E |

---

## LLM Judge Checks

LLM judge checks send the assistant output (or full session) to a language model for evaluation. The judge returns a score (0.0--1.0) and reasoning.

### `llm_judge`

Turn-level LLM evaluation. The judge sees the current assistant response and evaluates it against the provided criteria.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `criteria` | string | Yes | What the judge should evaluate |
| `rubric` | string | No | Detailed scoring guidance |
| `model` | string | No | Model to use for judging |
| `system_prompt` | string | No | Override the default judge system prompt |
| `min_score` | float | No | Minimum score threshold for passing |
| `extra` | object | No | Additional provider-specific parameters |

**Surfaces:** A E

### `llm_judge_session`

Session-level LLM evaluation. The judge sees the full conversation. Alias: `llm_judge_conversation`.

Same params as `llm_judge`. **Surfaces:** A E

### `llm_judge_tool_calls`

Evaluates tool usage patterns via an LLM judge.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `criteria` | string | Yes | What the judge should evaluate about tool usage |
| `tools` | string[] | No | Filter to specific tools |

Plus all standard judge params (`rubric`, `model`, `system_prompt`, `min_score`, `extra`). **Surfaces:** A E

**Example:**

```yaml
assertions:
  - type: llm_judge
    params:
      criteria: "Response is empathetic and addresses the customer's concern"
      min_score: 0.7
```

---

## External Checks

External checks delegate evaluation to HTTP endpoints or A2A agents. These are the no-code extensibility points for teams that want custom evaluation logic without writing Go.

### `rest_eval`

POSTs turn data to an HTTP endpoint. The endpoint must return `{"score": float, "reasoning": string}`. The `passed` field is accepted for backward compatibility but ignored — pass/fail is determined by the assertion or guardrail wrapper based on score thresholds.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | Endpoint URL |
| `method` | string | No | HTTP method (default: POST) |
| `headers` | object | No | Request headers; values support `${ENV_VAR}` expansion |
| `timeout` | string | No | Request timeout |
| `include_messages` | bool | No | Include conversation messages in payload |
| `include_tool_calls` | bool | No | Include tool call records in payload |
| `criteria` | string | No | Evaluation criteria passed to the endpoint |
| `min_score` | float | No | Minimum score threshold |
| `extra` | object | No | Additional fields merged into the request body |

**Surfaces:** A E

### `rest_eval_session`

POSTs full session data to an HTTP endpoint. Same params as `rest_eval`. **Surfaces:** A E

### `a2a_eval`

Sends evaluation data to an A2A-protocol eval agent.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_url` | string | Yes | URL of the A2A eval agent |
| `auth_token` | string | No | Auth token; supports `${ENV_VAR}` expansion |
| `criteria` | string | No | Evaluation criteria |
| `min_score` | float | No | Minimum score threshold |

**Surfaces:** A E

### `a2a_eval_session`

Session-level A2A evaluation. Same params as `a2a_eval`. **Surfaces:** A E

**Example:**

```yaml
evals:
  - id: safety_check
    type: rest_eval
    trigger: every_turn
    params:
      url: "https://safety.internal/evaluate"
      headers:
        Authorization: "Bearer ${SAFETY_API_KEY}"
      criteria: "Content is safe for all audiences"
      min_score: 0.9
```

---

## Budget & Performance Checks

| Type | Params | Surfaces |
|------|--------|----------|
| `latency_budget` | `max_ms` (int) | A E |
| `cost_budget` | `max_cost_usd`, `max_total_tokens` | E |

`cost_budget` is session-level and fires on `on_session_complete`.

---

## Meta Checks

| Type | Params | Surfaces |
|------|--------|----------|
| `guardrail_triggered` | `guardrail` (string), `should_trigger` (bool) | A E |
| `invariant_fields_preserved` | `tool` (string), `fields` (string[]) | E |

`guardrail_triggered` inspects prior eval results in the same batch, verifying that a specific guardrail did (or did not) fire.

---

## Behavioral Testing Checks

These checks compare behavior across prompt variants or input perturbations.

| Type | Params | Surfaces |
|------|--------|----------|
| `outcome_equivalent` | `metric` (`"tool_calls"` \| `"final_state"` \| `"content_hash"`) | E |
| `directional` | `check` (`"same_tool_calls"` \| `"same_outcome"` \| `"similar_content"`) | E |

---

## Param Aliases

For backward compatibility, some parameter names are aliased. When you use an aliased param name, it is automatically mapped to the canonical name before the handler runs.

| Check Type(s) | Alias Param | Canonical Param |
|---------------|-------------|-----------------|
| `content_excludes`, `banned_words` | `words` | `patterns` |
| `max_length`, `length` | `max_characters`, `max_chars` | `max` |
| `min_length` | `min_characters`, `min_chars` | `min` |
| `sentence_count`, `max_sentences` | `max_sentences` | `max` |
| `field_presence`, `required_fields` | `required_fields` | `fields` |

---

## Extending the Check System

PromptKit provides several extensibility points for adding custom check logic.

### Custom EvalTypeHandler (Go)

Implement the `EvalTypeHandler` interface and register it:

```go
type EvalTypeHandler interface {
    Type() string
    Eval(ctx context.Context, evalCtx *EvalContext, params map[string]any) (*EvalResult, error)
}
```

Register globally (available to all registries):

```go
evals.RegisterDefault(handler)
```

Or register on a specific registry instance:

```go
registry.Register(handler)
```

### StreamableEvalHandler (Go)

For checks that need streaming support in guardrails, implement `StreamableEvalHandler`. This enables incremental evaluation on each streaming chunk, allowing early abort.

```go
type StreamableEvalHandler interface {
    EvalTypeHandler
    EvalPartial(ctx context.Context, content string, params map[string]any) (*EvalResult, error)
}
```

### Exec Eval Handlers (Any Language)

Define eval handlers as external subprocesses in RuntimeConfig YAML. The subprocess receives JSON on stdin and writes JSON to stdout, so you can use any language.

```yaml
spec:
  evals:
    my_python_eval:
      command: python3
      args: ["./evaluators/my_eval.py"]
      env: ["EVAL_TYPE=my_python_eval"]
      timeoutMs: 5000
```

**Stdin** receives:

```json
{"type": "my_python_eval", "params": {...}, "content": "...", "context": {...}}
```

**Stdout** must return:

```json
{"score": 0.85, "detail": "Explanation text", "data": {}}
```

The `score` value (0.0--1.0) is the eval's output. Pass/fail is not determined by the handler — assertion and guardrail wrappers apply score thresholds to determine pass/fail.

### Custom JudgeProvider (Go)

Customize how LLM judge checks call language models:

```go
type JudgeProvider interface {
    Judge(ctx context.Context, opts JudgeOpts) (*JudgeResult, error)
}
```

Register via `sdk.WithJudgeProvider(provider)` when opening a conversation.

### Custom ProviderHook (Go, for guardrails)

For custom runtime guardrails beyond the built-in check types, implement `ProviderHook` to intercept LLM calls:

```go
type ProviderHook interface {
    Name() string
    BeforeCall(ctx context.Context, req *ProviderRequest) Decision
    AfterCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision
}
```

Optionally implement `ChunkInterceptor` for streaming interception:

```go
type ChunkInterceptor interface {
    OnChunk(ctx context.Context, chunk *providers.StreamChunk) Decision
}
```

Register via `sdk.WithProviderHook(hook)`.

### REST and A2A External Checks

For no-code extensibility, use the [`rest_eval`](#rest_eval) and [`a2a_eval`](#a2a_eval) check types. These let you delegate evaluation to any HTTP endpoint or A2A-compatible agent without writing Go code.

---

## See Also

- [Unified Check Model](/concepts/validation/) -- How checks, assertions, guardrails, and evals relate
- [Write Assertions](/arena/reference/assertions/) -- Using checks in Arena test scenarios
- [Add Guardrails](/arena/reference/validators/) -- Using checks as runtime policy enforcers
- [Eval Framework](/arena/explanation/eval-framework/) -- Production eval architecture
- [Run Evals](/sdk/how-to/run-evals/) -- Programmatic eval execution
