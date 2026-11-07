---
title: Checks Reference
sidebar:
  order: 1
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - runtime/evals/handlers/a2a_eval.go
    - runtime/evals/handlers/agent_invoked.go
    - runtime/evals/handlers/agent_not_invoked.go
    - runtime/evals/handlers/agent_response_contains.go
    - runtime/evals/handlers/audio_emotion.go
    - runtime/evals/handlers/classify_handler_base.go
    - runtime/evals/handlers/composition.go
    - runtime/evals/handlers/content_excludes.go
    - runtime/evals/handlers/cosine_similarity.go
    - runtime/evals/handlers/cost_budget.go
    - runtime/evals/handlers/directional.go
    - runtime/evals/handlers/exec_handler.go
    - runtime/evals/handlers/external_eval.go
    - runtime/evals/handlers/guardrail_triggered.go
    - runtime/evals/handlers/image_dimensions.go
    - runtime/evals/handlers/image_moderation.go
    - runtime/evals/handlers/json_path.go
    - runtime/evals/handlers/latency_budget.go
    - runtime/evals/handlers/length.go
    - runtime/evals/handlers/llm_judge.go
    - runtime/evals/handlers/no_tool_errors.go
    - runtime/evals/handlers/pii_leakage.go
    - runtime/evals/handlers/provider_binding.go
    - runtime/evals/handlers/rag_helpers.go
    - runtime/evals/handlers/regex.go
    - runtime/evals/handlers/register.go
    - runtime/evals/handlers/rest_eval.go
    - runtime/evals/handlers/role_violation.go
    - runtime/evals/handlers/safety_helpers.go
    - runtime/evals/handlers/sentence_count.go
    - runtime/evals/handlers/skill_activated.go
    - runtime/evals/handlers/tool_call_chain.go
    - runtime/evals/handlers/tool_call_views.go
    - runtime/evals/handlers/tool_calls_with_args.go
    - runtime/evals/handlers/tool_exec.go
    - runtime/evals/handlers/tool_exec_score.go
    - runtime/evals/handlers/tool_no_repeat.go
    - runtime/evals/handlers/tool_result_includes.go
    - runtime/evals/handlers/tool_result_matches.go
    - runtime/evals/handlers/tool_result_media.go
    - runtime/evals/handlers/tools_called.go
    - runtime/evals/handlers/tools_called_session.go
    - runtime/evals/handlers/tools_offered.go
    - runtime/evals/handlers/topic_policy.go
    - runtime/evals/handlers/topic_policy_params.go
    - runtime/evals/handlers/topic_prompt.go
    - runtime/evals/handlers/video_resolution.go
    - runtime/evals/handlers/workflow_spoke_in_state.go
    - runtime/evals/handlers/workflow_state_is.go
    - runtime/evals/handlers/workflow_transitioned_to.go
    - runtime/evals/normalize.go
    - runtime/evals/types.go
    - runtime/evals/validate.go
    - runtime/evals/wrappers.go
    - runtime/hooks/guardrails/adapter.go
    - runtime/hooks/guardrails/factory.go
    - runtime/inference/openai/register.go
    - runtime/inference/systemone/register.go
    - sdk/sdk.go
---

Every check type registered in `runtime/evals/handlers`, with its aliases, params and defaults. One check type runs in three roles:

| Role | Declared in | Verdict |
|------|-------------|---------|
| Eval | `evals:` entry in a pack (`id`, `type`, `trigger`, `params`) | None. The result carries a `Score`; `Passed` is never set. |
| Assertion | `assertions:` / `conversation_assertions:` entry in an Arena scenario, or the `assertion` wrapper | The wrapper sets `Passed` from the score and its thresholds. |
| Guardrail | `validators:` entry in a pack, `sdk.WithGuardrail`, or the `guardrail` wrapper | The check triggers when the score is below the floor (or outside its thresholds), and the pipeline blocks or rewrites. |

Check types are `EvalTypeHandler` implementations in the `runtime/evals` registry. See the [Unified Check Model](/concepts/validation/) for the conceptual overview.

## Roles

### Scores and verdicts

Handlers return a `Score` (0.0 to 1.0 for the built-in checks, except `cosine_similarity` and a scored `tool_exec`, which return the raw measurement). Count-based checks return a ratio, so partial matches score between 0 and 1.

| Role | Rule |
|------|------|
| Assertion wrapper | Passes when `Score >= min_score` and `Score <= max_score`. With neither set, `min_score` is `1.0`. A result with an `Error` fails whatever the thresholds. A result with no score passes. |
| Guardrail | Triggers when `Score < min_score` (default `1.0`) or `Score > max_score`. A nil result or nil score triggers. |
| Eval | No verdict. |

Handlers in the classifier, judge, RAG, safety, composition, REST, A2A, `tool_exec` and `topic_policy` families, and `cosine_similarity`, reject `min_score` and `max_score` in their own params with an error result (`tool_exec` and `topic_policy` reject them when the pack loads). Put thresholds on the [`assertion`](#assertion-wrapper) wrapper. The other handlers ignore the keys.

### Guardrail scope

Any registered type, alias or custom handler works as a guardrail. A guardrail evaluates one message: `CurrentOutput` is the user's message (`direction: input`) or the assistant response (`direction: output`). `contains_any` and `content_excludes` scan only that message in a guardrail, and the whole assistant transcript when used as an eval or assertion. Handlers that read `ToolCalls`, `Extras`, `Metadata` or `PriorResults` receive them derived from the message history; `SessionID`, `PromptID` and `Variables` are not set.

### Triggers

An eval `trigger` is one of `every_turn`, `on_session_complete`, `sample_turns`, `sample_sessions`, `on_conversation_complete` or `on_workflow_step`. `sample_percentage` defaults to `5`. A conversation opened with the SDK runs `on_session_complete` and then `on_conversation_complete` evals on every `Close()`, over the transcript it holds. A conversation closed with an empty transcript runs no conversation evals.

### Evals that cannot run

`Open()` skips an eval it cannot run and logs a `Skipping unusable pack eval` warning naming it. An eval is a measurement, so the conversation opens without it. The pack's evals and each prompt's evals are checked separately, so a prompt eval that shares a pack eval's `id` overrides it and is not a duplicate.

| Problem | Behavior |
|---------|----------|
| Missing `id` or `type`, or an `id` used twice in the same list | Skipped (for a duplicate, the later one) |
| Missing `trigger` | Skipped |
| A `trigger` this runtime does not know | Skipped, because it can never run. The schema allows any trigger, so the pack is still valid |
| `sample_percentage` outside 0–100 | Skipped |
| A `metric` with no `name` or `type`, a `type` other than `gauge`, `counter`, `histogram` or `boolean`, a name or label Prometheus cannot register, or `range.min` above `range.max` | Skipped |
| Unknown `type`, params the handler rejects, or an unusable `when` | Skipped |

`sdk.ValidatePack` reports each of these as an `eval` issue: an unknown trigger with severity `warning`, the others with `error`. `prompt.Pack.ValidateDetailed` reports them as errors and the unknown trigger as a warning. `prompt.Pack.Validate` includes them in its findings, except the unknown trigger.

## Registry

### Type aliases

An alias resolves to its target handler. Param aliases and defaults are keyed by the name the check is declared under; the tables below list each name they apply to.

| Alias | Target type |
|-------|-------------|
| `content_includes` | `contains` |
| `content_includes_any` | `contains_any` |
| `content_matches` | `regex` |
| `content_not_includes` | `content_excludes` |
| `banned_words` | `content_excludes` |
| `is_valid_json`, `valid_json` | `json_valid` |
| `tool_called` | `tools_called` |
| `tools_not_called_with_args` | `tool_args_excluded_session` |
| `llm_judge_conversation` | `llm_judge_session` |
| `length` | `max_length` |
| `max_sentences` | `sentence_count` |
| `required_fields` | `field_presence` |

### Param aliases

Before the handler runs, a legacy param name is rewritten to the canonical name unless the canonical key is already present.

| Check type(s) | Legacy param | Canonical param |
|---------------|--------------|-----------------|
| `content_excludes`, `banned_words` | `words` | `patterns` |
| `max_length`, `length` | `max_characters`, `max_chars` | `max` |
| `min_length` | `min_characters`, `min_chars` | `min` |
| `sentence_count`, `max_sentences` | `max_sentences` | `max` |
| `field_presence`, `required_fields` | `required_fields` | `fields` |

`max_length` and `min_length` also read the legacy names directly.

### Param defaults

Applied when the param is absent.

| Check type | Param | Default |
|------------|-------|---------|
| `banned_words` (alias only) | `match_mode` | `word_boundary` |
| `topic_policy` | `direction` | `input` |
| `topic_policy` | `small_talk` | `allow` |
| `topic_policy` | `on_deny` | `block` |
| `topic_policy` | `on_unknown` | `deny` |
| `topic_policy` | `on_error` | `deny` |
| `topic_policy` | `recent_turns` | `4` |

`content_excludes` used under its own name, or as `content_not_includes`, defaults `match_mode` to `substring`.

---

## Content checks

| Type | Aliases | Params | Score |
|------|---------|--------|-------|
| `contains` | `content_includes` | `patterns` (string[], required) | Fraction of patterns found |
| `regex` | `content_matches` | `pattern` (string, required), `expect_match` (bool, default `true`) | 1 or 0 |
| `content_excludes` | `banned_words`, `content_not_includes` | `patterns` (string[]), `match_mode` | 1 or 0 |
| `contains_any` | `content_includes_any` | `patterns` (string[], required) | 1 or 0 |
| `min_length` | none | `min` (int, required, > 0) | `min(length / min, 1)` |
| `max_length` | `length` | `max` (int, required, > 0), `max_tokens` (int) | `min(max / length, 1)`, lowered by the token ratio |
| `sentence_count` | `max_sentences` | none | Always 1 |
| `field_presence` | `required_fields` | `fields` (string[]) | Fraction of fields found |
| `cosine_similarity` | none | `reference` (float[], required) | The similarity |

| Check | Behavior |
|-------|----------|
| `contains`, `contains_any`, `content_excludes`, `field_presence` | Matching is case-insensitive and by substring. |
| `contains`, `field_presence`, `regex`, lengths | Read `CurrentOutput`, the last assistant message. |
| `regex` | Go RE2 syntax. `expect_match: false` passes when the pattern does not match. |
| `content_excludes` | `match_mode` is `substring` or `word_boundary` (case-insensitive `\b` match around the escaped pattern). Any other value behaves as `substring`. No patterns passes. |
| `contains_any` | Passes when any pattern appears in any assistant message. |
| `min_length`, `max_length` | Length is in bytes. `max_tokens` estimates tokens as bytes divided by 4. |
| `sentence_count` | A measurement: the count is in `Details.count`. The handler reads no params; `max` and `max_sentences` have no effect. |
| `field_presence` | Looks for each name as a substring of the output. No fields passes. |
| `cosine_similarity` | The target embedding comes from `EvalContext.Metadata["embedding"]`. A missing embedding, or a dimension mismatch, scores 0. `min_similarity`, `min_score` and `max_score` are rejected with an error: set the threshold as `min_score` on the [`assertion`](#assertion-wrapper) wrapper. |

### Streaming

`content_excludes` (with its aliases) and `max_length` (with `length`) implement `StreamableEvalHandler`, so a guardrail evaluates them on each streaming chunk and can stop the stream early. While a response streams, `content_excludes` matches substrings whatever the `match_mode`, so a stream can stop on a word that the finished response would pass.

### Examples

Assertion (scenario YAML):

```yaml
assertions:
  - type: contains
    params:
      patterns: ["thank you", "welcome"]
```

Guardrail (pack YAML):

```yaml
validators:
  - type: banned_words
    params:
      patterns: ["competitor-name", "internal-only"]
```

Eval (pack YAML):

```yaml
evals:
  - id: response_length
    type: max_length
    trigger: every_turn
    params:
      max: 500
```

---

## JSON and structure checks

| Type | Aliases | Params |
|------|---------|--------|
| `json_valid` | `is_valid_json`, `valid_json` | `allow_wrapped` (bool), `extract_json` (bool) |
| `json_schema` | none | `schema` (object, required), `allow_wrapped` (bool), `extract_json` (bool) |
| `json_path` | none | `expression` (string, required; alias `jmespath_expression`), `expected`, `contains` (array), `min` / `max` (number), `min_results` / `max_results` (int), `allow_wrapped` (bool), `extract_json` (bool) |

All three parse `CurrentOutput` as JSON and score 1 or 0.

| Param | Effect |
|-------|--------|
| `allow_wrapped` | Parses the first fenced code block (with or without a `json` tag). |
| `extract_json` | Parses the first balanced `{...}` or `[...]` in the text. |
| `expression` | A [JMESPath](https://jmespath.org/) expression evaluated against the parsed JSON. |
| `expected` | The expression result must equal this value. Numbers compare numerically; arrays and objects compare deeply. |
| `contains` | The result must be an array containing every listed item. |
| `min`, `max` | The result must be a number in range. |
| `min_results`, `max_results` | The result must be an array of that length range. `0` leaves a bound unset. |

```yaml
assertions:
  - type: json_path
    params:
      expression: "order.status"
      expected: "confirmed"
```

---

## Tool checks (turn-level)

These checks read `EvalContext.ToolCalls`, the tool calls in the message history passed to the eval.

| Type | Aliases | Params | Score |
|------|---------|--------|-------|
| `tools_called` | `tool_called` | `tool_names` (string[], required; alias param `tools`), `min_calls` (int, default `1`), `max_calls` (int, default unbounded), `ignore_validation` (bool), `require_args` (bool) | Fraction of tools within `min_calls`; 0 when any tool exceeds `max_calls` |
| `tools_not_called` | none | `tool_names` (string[], required; alias param `tools`) | 1 or 0 |
| `tools_offered` | none | `tool_names` (string[], required; alias param `tools`), `absent` (bool), `exact` (bool) | Fraction of tools matching; 1 or 0 for `exact` |
| `tool_args` | none | `tool_name` (string, required), `expected_args` (object, required) | 1 or 0 |
| `tool_calls_with_args` | none | `tool_name`, `expected_args`, `args_match`, `result_includes`, `result_matches`, `no_error` | 1 or 0 |
| `tool_call_count` | none | `tool` (string), `min` (int), `max` (int) | 1 or 0 |
| `tool_call_sequence` | none | `sequence` (string[]) | Fraction of the sequence matched |
| `tool_call_chain` | none | `steps` (array of step objects) | Fraction of steps completed |
| `tool_anti_pattern` | none | `patterns` (array of `{sequence, message}`) | 1 or 0 |
| `tool_no_repeat` | none | `tools` (string[]), `max_repeats` (int, default `1`) | 1 or 0 |
| `tool_efficiency` | none | `max_calls` (int), `max_errors` (int), `max_error_rate` (float, 0 to 1) | See below |
| `no_tool_errors` | none | `tools` (string[]) | 1 minus the error ratio |
| `tool_result_includes` | none | `tool` (string), `patterns` (string[], required), `occurrence` (int, default `1`) | 1 or 0 |
| `tool_result_matches` | none | `tool` (string), `pattern` (string, required), `occurrence` (int, default `1`) | 1 or 0 |
| `tool_result_has_media` | none | `tool` (string), `media_type` (string, required) | 1 or 0 |
| `tool_result_media_type` | none | `tool` (string), `mime_type` (string, required) | 1 or 0 |

| Check | Behavior |
|-------|----------|
| `tools_called` | Counts calls that finished without an error. With `ignore_validation`, calls that failed argument validation also count. With `require_args`, calls with empty arguments do not count. `max_calls: 0` forbids the tool. |
| `tools_offered` | Reads `EvalContext.ToolsOffered`, the tool names the turn handed the provider. The tools must all be offered; `absent: true` requires that none is offered; `exact: true` requires the offered set to equal `tool_names`. An empty offered set scores 0, including when the host does not populate it. |
| `tool_args` | Passes when at least one call to `tool_name` has every expected argument. Values compare as strings. |
| `tool_calls_with_args` | Every call to `tool_name` (every call when empty) must satisfy all constraints, or the check scores 0. `expected_args` compares values as strings. `args_match` maps an argument name to a regex. `result_includes` (string[]) matches case-insensitively against the result. `result_matches` is a regex against the result. `no_error` requires no tool error. |
| `tool_call_count` | Counts calls to `tool`, or all calls when empty. `min` and `max` are unset by default. |
| `tool_call_sequence` | `sequence` must appear in order as a subsequence of the calls. An empty sequence passes. |
| `tool_call_chain` | Walks the calls in order. A step is `{tool, result_includes, result_matches, args_match, no_error}`. A call matching a step's tool must satisfy that step's constraints, or the check stops at that step. An empty chain passes. |
| `tool_anti_pattern` | Fails when any listed `sequence` appears as a subsequence. `message` is shown in the explanation. |
| `tool_no_repeat` | Fails when a tool is called more than `max_repeats` times in a row. An empty `tools` checks every tool. |
| `tool_efficiency` | Needs at least one threshold. Exceeding any scores 0; otherwise the score is `(max_calls + 1 - calls) / (max_calls + 1)` when `max_calls` is set, else 1. A run at exactly `max_calls` scores `1 / (max_calls + 1)`, above a run that exceeds it. |
| `tool_result_includes` | A call matches when its result contains every pattern (case-insensitive). The check passes when at least `occurrence` calls match. |
| `tool_result_matches` | A call matches when its result matches the regex. The check passes when at least `occurrence` calls match. |
| `tool_result_has_media` | Passes when a result has a content part whose type equals `media_type` (`image`, `audio`, `video` or `document`). |
| `tool_result_media_type` | Passes when a result has a non-text part whose MIME type equals `mime_type`. |

```yaml
assertions:
  - type: tool_call_sequence
    params:
      sequence: ["lookup_customer", "create_ticket"]
```

### Session tool checks

These types count every recorded call, including calls that returned an error.

| Type | Aliases | Params |
|------|---------|--------|
| `tools_called_session` | none | `tool_names` (string[]; alias param `tools`), `min_calls` (int, default `1`) |
| `tools_not_called_session` | none | `tool_names` (string[]; alias param `tools`) |
| `tool_args_session` | none | `tool_name` (string, required), `expected_args` (object) |
| `tool_args_excluded_session` | `tools_not_called_with_args` | `tool_name` (string, required), `excluded_args` (object) |

`tool_args_excluded_session` fails when any call to `tool_name` passes an excluded argument value. It also accepts the legacy `forbidden_args` param, which maps an argument name to a value or a list of values. `tools_called_session` scores the fraction of tools called at least `min_calls` times. `tool_args_session` with no `expected_args` passes.

### Tool invocation checks

`tool_exec` invokes a tool itself and checks the result, instead of inspecting calls the agent made. Typical uses are a sandbox's `run_tests` tool, a render-and-diff utility or an HTTP probe as a gate after the conversation completes.

#### `tool_exec`

Invokes a registered tool by name through the runtime tool registry. The call succeeds when `tools.Registry.Execute` returns no error and `ToolResult.Error` is empty. The handler works with any registered tool: MCP-discovered, HTTP, local executors and custom client tools.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `tool` | string | Yes | Registry name of the tool to invoke. |
| `args` | object | No | Arguments passed verbatim to the tool. Defaults to `{}`. |
| `timeout_seconds` | int | No | Per-call timeout. Default `120`. |
| `success_pattern` | string | No | Regex the tool's text output must match. |
| `failure_pattern` | string | No | Regex that fails the check when the tool's text output matches it. |
| `score_path` | string | No | [JMESPath](https://jmespath.org/) expression into the tool's JSON output selecting a number to report as the score, for example `summary.pass_rate`. |
| `score_pattern` | string | No | Regex with exactly one capture group, applied to the tool's text output; the captured number is the score, for example `PASS_RATE=([0-9.]+)`. |
| `score_scale` | number | No | Positive divisor applied to the extracted value, for example `100` for a percentage. Default `1`. Needs `score_path` or `score_pattern`. |

Without `score_path` or `score_pattern`, the score is `1.0` when the call succeeds and `0.0` otherwise. With one of them set, a successful call scores the extracted value divided by `score_scale`.

- Set one of `score_path` and `score_pattern`, not both.
- `score_path` reads a number, or a string holding a number (`"87.5"`).
- When no number is taken (the tool errors, a `success_pattern` misses, a `failure_pattern` matches, or the value cannot be extracted) the result is an error, not a score of `0.0`, and the explanation says why. An assertion fails on an error whatever its thresholds, so a `max_score: 0` findings gate does not pass when the scanner crashed.
- A bad `score_path`, regex or `score_scale` is rejected when the pack loads.
- The raw extracted value is reported in `Details.raw_score`, next to `Details.result`.
- The score is neither clamped nor thresholded. With no `min_score` or `max_score` on the assertion, it passes when the score is `1.0` or more, so an unthresholded count or latency passes at 1 or above and fails at 0.
- `min_score` and `max_score` among the check's own params are rejected.

The host must put a `*tools.Registry` in `EvalContext.Metadata["tool_registry"]`. Arena does this automatically; SDK consumers using the runtime evals API directly populate it themselves. Without it the check fails with a configuration message. The tool's own error text appears in the result explanation.

Gating on a sandbox's hidden test suite:

```yaml
conversation_assertions:
  - type: tool_exec
    params:
      tool: run_tests
    message: "Hidden tests must pass"
```

The `run_tests` tool comes from a [source-backed MCP entry](https://promptarena.altairalabs.ai/arena/how-to/provision-mcp-sandbox/).

Calling a pack-shipped validation tool:

```yaml
conversation_assertions:
  - type: tool_exec
    params:
      tool: validate_invoice
      args:
        strict: true
      timeout_seconds: 30
    message: "Final invoice must validate"
```

Gating on a test pass rate:

```yaml
conversation_assertions:
  - type: tool_exec
    params:
      tool: run_tests
      score_pattern: 'PASS_RATE=([0-9.]+)'
      score_scale: 100
      min_score: 0.8
    message: "At least 80% of hidden tests must pass"
```

Measuring Semgrep findings without gating:

```yaml
evals:
  - id: semgrep_findings
    type: tool_exec
    trigger: on_session_complete
    params:
      tool: run_semgrep
      score_path: findings_count
```

---

## Agent and skill checks

| Type | Params | Score |
|------|--------|-------|
| `agent_invoked` | `agents` (string[], required) | Fraction of agents invoked |
| `agent_not_invoked` | `agents` (string[]) | 1 or 0 |
| `agent_response_contains` | `agent` (string, required), `contains` (string) | 1 or 0 |
| `skill_activated` | `skill_names` (string[]), `min_calls` (int, default `1`) | Fraction of skills activated |
| `skill_not_activated` | `skill_names` (string[]) | 1 or 0 |
| `skill_activation_order` | `sequence` (string[], required) | Fraction of the sequence matched |

- Agents are invoked as tool calls, so the checks match `agents` against tool call names. `agent_response_contains` looks for `contains` (case-sensitive) in the result of a call to `agent`, or in a tool-result message from it.
- The skill checks read calls to the `skill__activate` tool and the skill name in its `name` argument. `skill_activated` and `skill_not_activated` ignore calls that returned an error.

```yaml
assertions:
  - type: agent_invoked
    params:
      agents: ["billing-agent"]
```

---

## Workflow checks

| Type | Params | Score |
|------|--------|-------|
| `workflow_complete` | none | 1 or 0 |
| `state_is` | `state` (string, required) | 1 or 0 |
| `transitioned_to` | `state` (string, required) | 1 or 0 |
| `workflow_transition_order` | `sequence` (string[], required) | Fraction of the sequence matched |
| `workflow_tool_access` | `rules` (array of `{state, allowed}`) | 1 or 0 |
| `spoke_in_state` | `state` (string, required) | 1 or 0 |

The registry has no `workflow_state_is` or `workflow_transitioned_to` type; the workflow checks are registered as `state_is` and `transitioned_to`.

| Check | Behavior |
|-------|----------|
| `workflow_complete` | Reads `Extras["workflow_complete"]`. Passes when it is `true`. |
| `state_is` | Passes when `Extras["workflow_current_state"]` equals `state`. |
| `transitioned_to` | Passes when a recorded transition has `to` equal to `state`. |
| `workflow_transition_order` | `sequence` must appear in order among the `to` values of the recorded transitions. |
| `workflow_tool_access` | Each rule lists the tools `allowed` in a state. A tool call made in a state that has a rule, and is not in its `allowed` list, fails the check. States with no rule, and turns with no known state, allow every tool. |
| `spoke_in_state` | Passes when an assistant message with non-empty text carries a workflow-state stamp equal to `state`. |

`state_is`, `transitioned_to`, `workflow_complete` and `workflow_transition_order` read the state machine's history, so a transition that produces no output satisfies all of them. `spoke_in_state` reads the conversation: whitespace-only text does not count, and a run with no per-message state stamps fails with an explanation that names the missing stamps (the workflow resolver is not wired).

```yaml
assertions:
  - type: transitioned_to
    params:
      state: handoff        # the machine advanced
  - type: spoke_in_state
    params:
      state: handoff        # the destination agent replied
```

---

## Composition checks

These checks assert on the internal execution of a [Workflow Composition](/concepts/compositions/). They read observability data in `EvalContext.Metadata`: `composition_step_outputs`, `composition_branch_taken` and `composition_parallel_status`, which the Arena `CompositionMetadataProvider` injects. They score 1 or 0 and put detail in `Details`. `min_score` and `max_score` among their own params produce an error result; put thresholds on the [`assertion`](#assertion-wrapper) wrapper.

### `composition_step_output`

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `step` | string | Yes | Step ID to inspect. |
| `contains` | string | No | The output must contain this substring. |
| `equals` | string | No | The output must equal this string exactly. Takes precedence over `contains`. |

With neither `contains` nor `equals`, passes when the step produced any non-empty output. A step with no recorded output scores 0.

```yaml
evals:
  - id: classify-output-monitor
    type: composition_step_output
    trigger: every_turn
    params:
      step: classify
      contains: '"type"'
```

### `composition_branch_taken`

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `branch` | string | Yes | Step ID of the `branch` step. |
| `expected` | string | Yes | The step ID the branch should have selected. |

```yaml
assertions:
  - type: composition_branch_taken
    params:
      branch: route
      expected: extract_paper
```

### `composition_parallel_complete`

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `parallel` | string | Yes | Step ID of the `parallel` step. |

Passes when the recorded status of the step is `complete`.

```yaml
assertions:
  - type: composition_parallel_complete
    params:
      parallel: meta
```

### `composition_output`

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `contains` | string | No | The final output must contain this substring. |
| `equals` | string | No | The final output must equal this string exactly. Takes precedence over `contains`. |

The final output is `CurrentOutput`. With neither param, passes when it is non-empty.

```yaml
conversation_assertions:
  - type: composition_output
    params:
      contains: '"key_findings"'
```

---

## Media checks

These checks read media parts from assistant messages and from the results of tool calls. They score 0 when the messages hold no media of the checked kind.

| Type | Params |
|------|--------|
| `image_format` | `formats` (string[], required) |
| `image_dimensions` | `min_width`, `max_width`, `min_height`, `max_height`, `width`, `height` (int) |
| `audio_format` | `formats` (string[], required) |
| `audio_duration` | `min_seconds`, `max_seconds` (number) |
| `video_duration` | `min_seconds`, `max_seconds` (number) |
| `video_resolution` | `min_width`, `max_width`, `min_height`, `max_height` (int), `presets` (string[]) |

- `formats` compares case-insensitively with the MIME subtype of each part. `audio/mpeg` is `mp3`.
- `width` and `height` on `image_dimensions` require an exact size.
- A part without width and height (or duration) metadata violates any constraint.
- `presets` matches the video height: `480p` or `sd`; `720p` or `hd`; `1080p`, `fhd` or `full_hd`; `1440p`, `2k` or `qhd`; `2160p`, `4k` or `uhd`; `4320p` or `8k`.

---

## Inference-backed checks

These checks call an `inference` provider through its one interface, `Infer` (content in, a probability per label out), and emit the model's score for a configured label. Threshold judgment lives on the [`assertion`](#assertion-wrapper) wrapper.

The checks need a provider with `role: inference`. When a check names no provider and the host sets no default, it is skipped, and a skipped check scores 1 and passes. A check that names a `provider` the host cannot resolve returns an error. [`topic_policy`](#topic_policy) is a guardrail that treats a missing classifier as an error and applies its `on_error` param, which defaults to deny.

Two declaration sites:

```yaml
# As a pack-level runtime eval: emits the raw signal as a metric every turn.
evals:
  - id: response-toxicity
    type: text_toxicity
    trigger: every_turn
    params: { model: unitary/toxic-bert, expected_label: toxic }

# As an Arena assertion: wrap with `type: assertion` to apply a threshold.
conversation_assertions:
  - type: assertion
    params:
      eval_type: text_toxicity
      eval_params: { model: unitary/toxic-bert, expected_label: toxic }
      max_score: 0.3
```

Common params (`audio_emotion`, `image_moderation`, `text_toxicity`, `text_sentiment`):

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | Yes | Backend model ID, for example `unitary/toxic-bert` or `superb/wav2vec2-base-superb-er`. |
| `expected_label` | string | Yes | Label whose score is emitted. Matching is case-insensitive. If the model does not return the label, the score is 0 and `Details.found` is `false`. |
| `message_role` | string | No | Whose messages to score. Default `user` for `audio_emotion`; `assistant` for the others. |
| `message_index` | int | No | Which matching message to score. Default `-1` (the latest). |
| `provider` | string | No | A logical provider name this pack declares in `requires`, which the host binds to a classifier. Empty uses the host's default inference provider. |

A message with no content of the checked kind, a model still loading, and a model the configured inference path cannot serve each skip the check. The legacy `classifier_id` param is rejected.

### `audio_emotion`

Picks an audio part from the chosen role's messages, runs it through the bound inference provider, and emits the model's score for the emotion label.

```yaml
conversation_assertions:
  - type: assertion
    params:
      eval_type: audio_emotion
      eval_params:
        model: superb/wav2vec2-base-superb-er
        message_role: user
        expected_label: ang     # this model emits truncated labels: ang/neu/hap/sad
        provider: screener      # a key this pack declares in requires
      min_score: 0.5
```

### `image_moderation`

Picks an image part from the chosen role's messages, runs it through an image classifier (for example `Falconsai/nsfw_image_detection`), and emits the score for `expected_label`. With the default `message_role: assistant`, it also covers images a tool produced during the turn, such as the `image__generate` tool.

Assertion:

```yaml
conversation_assertions:
  - type: assertion
    params:
      eval_type: image_moderation
      eval_params:
        model: Falconsai/nsfw_image_detection
        expected_label: nsfw
      max_score: 0.3
```

Guardrail:

```yaml
validators:
  - type: guardrail
    params:
      eval_type: image_moderation
      eval_params:
        model: Falconsai/nsfw_image_detection
        expected_label: nsfw
      max_score: 0.3
```

### `text_toxicity`

Classifier-backed toxicity score from a text-classification model. The LLM-judge `toxicity` under [Safety checks](#safety-checks) is a separate check.

```yaml
# "this output should NOT be toxic"
- type: assertion
  params:
    eval_type: text_toxicity
    eval_params:
      model: unitary/toxic-bert
      expected_label: toxic
    max_score: 0.3

# "this output should sit in the neutral class"
- type: assertion
  params:
    eval_type: text_toxicity
    eval_params:
      model: s-nlp/roberta_toxicity_classifier
      expected_label: neutral
    min_score: 0.7
```

### `text_sentiment`

Classifier-backed sentiment score for `expected_label`.

```yaml
- type: assertion
  params:
    eval_type: text_sentiment
    eval_params:
      model: cardiffnlp/twitter-roberta-base-sentiment-latest
      message_role: assistant
      expected_label: positive
    min_score: 0.7
```

### `topic_policy`

Confines a conversation to a declared subject scope, decided by an inference provider rather than by the model being governed. It is a guardrail: it gates the user's message before the primary provider is called, and a denied turn is replaced with the configured message and never reaches the agent. The policy is default-deny: an empty or missing allow-list is rejected when the pack loads.

Two declarations are required:

```yaml
# 1. The pack validator.
validators:
  - type: topic_policy
    message: "I can only help with AltairaLabs products."
    params:
      description: Helps users evaluate and operate AltairaLabs products.
      allowed: [Omnia and PromptKit, licensing and support]
      disallowed: [politics]

# 2. The host provider file: an ordinary role: inference declaration.
id: topic-control
role: inference
type: nvidia-topic-control
base_url: http://topic-control:8000/v1
additional_config:
  timeout_seconds: 20   # optional; default 20
```

Every backend receives the same prompt (the policy as the instruction, ending with NemoGuard topic control's required closing sentence) and is asked for `on-topic` or `off-topic`. The check allows whichever label gets the higher probability and records that probability as `confidence`. Equal probabilities, or none, resolve through `on_unknown`.

| Provider `type` | Backend | Notes |
|-----------------|---------|-------|
| `nvidia-topic-control` | NemoGuard topic control (`nvidia/llama-3.1-nemoguard-8b-topic-control`) on an OpenAI-compatible endpoint (NIM) | An alias for `openai` with that model; `base_url` is required, and the endpoint must return logprobs |
| `openai` | Any OpenAI-compatible chat model that returns logprobs (OpenAI, vLLM, LiteLLM) | `model` is required. `OPENAI_API_KEY` is used only for OpenAI's own endpoint; any other `base_url` needs an explicit `credential` |
| `systemone` | TypeSafe's Jev, through the Vercel AI Gateway (`base_url: https://ai-gateway.vercel.sh/typesafe/v1`, `model: typesafe-ai/jev`) or a self-hosted server speaking the same API | Needs an explicit `credential` unless `base_url` is loopback |

:::caution[Do not point this at a reasoning model]
A reasoning model starts its answer with its thinking rather than a bare label. An answer that is not a label is not a decision, so every classification falls to `on_unknown`, which denies, and the guardrail blocks every turn. Use a model that answers with `on-topic` or `off-topic` and nothing else.
:::

`timeout_seconds` (provider types `openai` and `nvidia-topic-control`, in `additional_config`) bounds a single classification call. The default for `nvidia-topic-control` is 20 seconds. A timeout is an error, so `on_error` applies. Setting `timeout_seconds` together with the provider file's `request_timeout` is rejected.

Params:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `description` | string | required | The application's purpose in prose. |
| `allowed` | string[] | required, non-empty | In-scope subjects. |
| `disallowed` | string[] | none | Explicit exclusions. |
| `small_talk` | `allow` \| `deny` | `allow` | Whether greetings, thanks and pleasantries that match no listed subject count as in scope. |
| `examples.allowed`, `examples.disallowed` | string[] | none | A labeled corpus folded into the prompt. |
| `on_deny` | `block` \| `respond` | `block` | What replaces a denied turn. Both skip the agent for an input check and replace its reply for an output check. `block` answers with `message`, else the generic blocked message. `respond` answers with the policy's own response, naming the allowed subjects: `I can only help with: Omnia and PromptKit; licensing and support.` A `message` you set still wins under `respond`. |
| `on_unknown` | `deny` \| `allow` | `deny` | Outcome when the classifier answers but gives no usable label, or the turn has no text to judge. |
| `on_error` | `deny` \| `allow` | `deny` | Outcome when the classifier cannot be reached or parsed, including when no provider is bound. |
| `recent_turns` | int (>= 0) | `4` | How many prior `user` and `assistant` turns to send for reference resolution. `0` judges the current message alone. |
| `provider` | string | none | A logical provider name this pack declares in `requires`. Empty uses the host's default topic classifier. |
| `direction` | `input` \| `output` \| `both` | `input` | `topic_policy` is the one check type whose default direction is not `output`. |
| `message` | string | generic blocked message (`on_deny: block`) or the policy's response (`on_deny: respond`) | The user-facing text for a denied turn. Normally the validator's top-level `message:` field; also accepted in `params`. |

Behavior:

- Declaring `topic_policy` as a `validators:` entry is the normal form. Wrapping it in `type: guardrail` or `type: assertion` behaves identically: the wrapper applies the inner check's `direction: input` default and param validation.
- Unknown keys are rejected, and so are `min_score` and `max_score`. A pack whose `topic_policy` params fail validation (a misspelled `dissallowed:`, an empty `allowed:`, a bad enum value) fails to load.
- A user message with only image, audio or video parts has no text to judge. It resolves through `on_unknown`.
- Tool-result messages are dropped from the `recent_turns` window before it is applied. Assistant messages that finished for `safety` (this check's replacement text, and provider-side content filtering) or were interrupted are dropped too. The denied user message is kept.
- `on_unknown` and `on_error` are separate params that describe the same events for the label-emitting backends: any response that is not one of the two labels falls to `on_unknown`; network errors, non-2xx responses and malformed JSON fall to `on_error`.
- `Details.policy_digest` is a SHA-256 hash of the normalized `description`, `allowed`, `disallowed`, `small_talk` and `examples`. It changes on any wording edit to `description` and does not say what changed.

### `assertion` (wrapper)

Turns any check into a thresholded pass/fail. It resolves `eval_type` in the registry, applies the inner type's param defaults and aliases, runs the inner type's param validation, evaluates it, and sets `Passed`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `eval_type` | string | Yes | The inner check type, for example `text_toxicity`, `audio_emotion` or `cosine_similarity`. |
| `eval_params` | object | Yes | Params passed to the inner check. |
| `min_score` | float | No | Pass when the inner score is at or above this. |
| `max_score` | float | No | Pass when the inner score is at or below this. |

If both are set, both must hold. If neither is set, `min_score` is `1.0`. Judge-backed and classifier-backed checks emit raw scores in `[0, 1]`; set an explicit threshold on the wrapper, or an unset one reports every score below 1.0 as failed.

### `guardrail` (wrapper)

Same shape as `assertion`, evaluated as a guardrail.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `eval_type` | string | Yes | The inner check type. |
| `eval_params` | object | Yes | Params passed to the inner check. |
| `action` | string | No | Default `block`. Recorded in `Details.action`. |
| `min_score` | float | No | Trigger when the inner score is below this. Default `1.0` when no threshold is set. |
| `max_score` | float | No | Trigger when the inner score is above this. |

The result sets `Passed` to the inverse of `Details.triggered`.

---

## LLM judge checks

LLM judge checks send the assistant output, or the full session, to a language model. The judge returns a score (0.0 to 1.0) and reasoning; the handler emits the score as `Score` and keeps the judge's own `passed` opinion in `Details`. Without a judge provider the check cannot run and scores 0. As a guardrail, a judge-backed check that names no `provider` needs a default judge from the host (`sdk.WithJudgeProvider`), or the pack fails to load.

Judge params, shared by `llm_judge`, `llm_judge_session`, `llm_judge_tool_calls`, the RAG checks and the safety checks:

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `criteria` | string | See each check | What the judge should evaluate. |
| `rubric` | string | No | Detailed scoring guidance. |
| `system_prompt` | string | No | Override the judge's system prompt. |
| `provider` | string | No | A logical provider name this pack declares in `requires`. Empty uses the host's default judge. |
| `extra` | object | No | Provider-specific parameters. They reach the judge as `JudgeOpts.Extra`; the built-in judges send them as the prediction request's `Metadata`, which each provider reads or ignores. |

A judge grades with the model of the provider it is bound to. There is no `model` param, and supplying one produces an error result rather than being ignored. To grade with another model, bind another provider: name it with `provider` or register it with `sdk.WithNamedProvider`.

### `llm_judge`

Turn-level evaluation: the judge sees `CurrentOutput` and the `criteria`.

**Rubric dimensions.** Numeric fields in the judge's JSON besides `score` become the result's `Metrics`. A judge asked for `{"score": 0.8, "clarity": 0.9, "accuracy": 0.7, "reasoning": "..."}` yields `clarity` and `accuracy` alongside the score. `passed`, `reasoning` and nested objects are not dimensions. Dimensions apply to `llm_judge`, `llm_judge_session` and `llm_judge_tool_calls`; the RAG and safety checks report none. See [eval dimensions](/sdk/how-to/observability/run-evals/#eval-dimensions) for exporting them to Prometheus.

### `llm_judge_session`

Alias: `llm_judge_conversation`. Session-level evaluation. The judge sees a role-labeled transcript of every user, assistant and other turn (system messages and tool-result rows are omitted), followed by every tool call with its arguments and result. Same params as `llm_judge`.

### `llm_judge_tool_calls`

Judges tool usage. The judge sees each call's name, arguments, result and error.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `tools` | string[] | No | Filter to these tool names. When no call matches, the check is skipped. |

Plus the shared judge params.

```yaml
assertions:
  - type: assertion
    params:
      eval_type: llm_judge
      eval_params:
        criteria: "Response is empathetic and addresses the customer's concern"
      min_score: 0.7
```

---

## RAG checks

RAG checks are thin wrappers over `llm_judge` with a built-in default prompt and criteria. Supplying `system_prompt` or `criteria` overrides the default. They take the shared judge params and report no `Metrics`. A check that needs retrieved context scores 0 with an explanation when none is supplied.

Every check that needs retrieved chunks accepts them in three forms, tried in this order:

| Form | Example |
|------|---------|
| `contexts` | `contexts: ["chunk-1", "chunk-2"]` |
| `context` | `context: "single chunk"` |
| `context_field` | `context_field: retrieved_chunks` reads that key from `EvalContext.Metadata` (a string, or a list of strings) |

| Type | Params | Measures |
|------|--------|----------|
| `faithfulness` | one of `contexts`, `context`, `context_field` | How directly the context supports the answer |
| `hallucination` | one of `contexts`, `context`, `context_field` | Freedom from unsupported or contradicting claims (1.0 means none) |
| `answer_relevancy` | `question` | How directly the answer addresses the question |
| `contextual_precision` | one of `contexts`, `context`, `context_field`; `question` | Fraction of retrieved chunks relevant to the question |
| `contextual_recall` | one of `contexts`, `context`, `context_field`; `reference` or `expected_output` (required) | How completely the chunks cover the ground-truth answer |
| `contextual_relevancy` | one of `contexts`, `context`, `context_field`; `question` | Mean per-chunk relevance to the question |

`question` defaults to the last user message. `faithfulness` and `hallucination` take no question.

```yaml
assertions:
  - type: assertion
    params:
      eval_type: hallucination
      eval_params:
        contexts:
          - "Paris is the capital of France."
      min_score: 0.9
```

---

## Safety checks

Safety checks score the assistant output for one concern. They run as LLM judges, so they take the shared judge params, all optional. Their usual wiring is a guardrail in a pack's `validators:` block, with a scenario observing the firing through `guardrail_triggered`:

```yaml
# In the pack: runtime enforcement
validators:
  - type: pii_leakage
    params:
      direction: output

# In a scenario turn: test predicate
assertions:
  - type: guardrail_triggered
    params:
      validator_type: pii_leakage
      should_trigger: true
```

| Type | Measures | Extra params |
|------|----------|--------------|
| `bias` | Demographic, stereotype, gender, racial or religious bias | none |
| `toxicity` | Insults, harassment, threats, hate speech (the LLM-judge counterpart of [`text_toxicity`](#text_toxicity)) | none |
| `pii_leakage` | Personally identifiable information | none |
| `role_violation` | Adherence to the assigned role, persona or instructions | `agent_role` (string) |

- `pii_leakage` first runs a regex pre-pass for emails, US-style SSNs and 16-digit card-shaped numbers. A hit scores 0 without an LLM call. Otherwise, when the check has a judge (a `provider` param, or a host default), the judge scores the output. With no judge, a clean pre-pass scores 1.
- `role_violation` gives the judge the active agent role: `agent_role`, else `EvalContext.Metadata["system_prompt"]`. With neither, the judge scores generic role consistency. `agent_role` is distinct from the shared `system_prompt` param, which sets the judge's own prompt.
### Guardrail `direction`

A check used as a guardrail accepts a `direction` param, inside `params`:

| Value | Evaluates | On a hit |
|-------|-----------|----------|
| `output` (default) | The assistant response | Rewrites the response (truncate or replace) and stops the provider round loop |
| `input` | The user's message, before the LLM call | Blocks the call (no tokens spent) and returns a canned assistant turn |
| `both` | Input, then output | Whichever fires first |

```yaml
validators:
  - type: pii_leakage
    params:
      direction: input
    message: "Please don't share personal details."
```

- On the pack YAML path, an unrecognized or non-string `direction` logs a warning and falls back to `output`.
- Subprocess (exec) hooks reach the same behavior through `before_call`: returning `{"allow": false, "enforced": true, "reason": "...", "metadata": {"replacement": "..."}}` blocks the call and uses `metadata.replacement` as the canned assistant text.
- Input guardrails evaluate once per user turn: the check runs only when the last message is a user message, so a tool-using turn does not re-run an LLM-judged check on every round.
- As an output guardrail, a check judges that response only. One tripped turn does not re-block later turns.
- The directional constructors set `direction` for you:

```go
sdk.WithGuardrail(
    guardrails.Input("banned_words", map[string]any{"words": []any{"wire transfer"}}),
    guardrails.Output("banned_words", map[string]any{"words": []any{"secret"}}),
)
```

### Validators that cannot be built

| Problem | Behavior |
|---------|----------|
| Unknown check `type` | `Open()` returns an error naming the type |
| The check's own param validation rejects the params | `Open()` returns an error |
| A judge-backed check with no `provider` param and no default judge | `Open()` returns an error |
| Unrecognized `direction` value | Logged as a warning and treated as `output` |

`sdk.ValidatePack` builds each validator without opening a conversation and reports every problem at once. With a custom registry (`sdk.WithEvalRegistry`), use `sdk.ValidatePackWithRegistry` with the same registry, or preflight reports your custom types as unknown. To preflight with the host's own configuration, use `sdk.ValidatePackWithOptions` and pass the options you pass to `Open()`. It honors `sdk.WithEvalRegistry` and `sdk.WithJudgeProvider`, so a judge-backed check that names no provider is reported as needing a judge unless you supply one, as `Open()` would fail.

---

## External checks

External checks delegate evaluation to HTTP endpoints or A2A agents.

### `rest_eval`

Sends the turn to an HTTP endpoint. The endpoint returns `{"score": float, "reasoning": string}`. A `passed` field is accepted and ignored. The check reports the endpoint's score as its own, and the [`assertion`](#assertion-wrapper) or guardrail wrapper decides pass or fail. `min_score` and `max_score` on the check itself are rejected with an error pointing at the wrapper, and the endpoint is not called. The same holds for `a2a_eval` and the session variants.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | Endpoint URL. |
| `method` | string | No | HTTP method. Default `POST`. |
| `headers` | object | No | Request headers; values support `${ENV_VAR}` expansion. |
| `timeout` | string | No | Go duration, for example `10s`. Default `30s`. |
| `include_messages` | bool | No | Include conversation messages in the payload. Default `true`. |
| `include_tool_calls` | bool | No | Include tool call records in the payload. Default `false`. |
| `criteria` | string | No | Evaluation criteria passed to the endpoint. |
| `extra` | object | No | Additional fields sent in the payload under `extra`. |

The request body holds `current_output`, `messages`, `tool_calls`, `criteria`, `variables` and `extra`. A non-2xx status, a timeout or an unparseable body scores 0.

### `rest_eval_session`

Same params as `rest_eval`. Sends all assistant messages joined by newlines as `current_output`.

### `a2a_eval`

Sends the turn to an A2A eval agent and reads `{"score", "reasoning"}` JSON from its reply.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_url` | string | Yes | URL of the A2A eval agent. |
| `auth_token` | string | No | Bearer token; supports `${ENV_VAR}` expansion. |
| `timeout` | string | No | Go duration. Default `60s`. |
| `criteria` | string | No | Evaluation criteria. |
| `include_messages` | bool | No | Default `true`. |
| `include_tool_calls` | bool | No | Default `false`. |
| `extra` | object | No | Additional fields sent in the payload. |

### `a2a_eval_session`

Same params as `a2a_eval`. Sends all assistant messages.

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
```

To require a score, put the threshold on the wrapper:

```yaml
evals:
  - id: safety_check
    type: assertion
    trigger: every_turn
    params:
      eval_type: rest_eval
      min_score: 0.9
      eval_params:
        url: "https://safety.internal/evaluate"
        criteria: "Content is safe for all audiences"
```

---

## Budget and performance checks

| Type | Params | Reads | Score |
|------|--------|-------|-------|
| `latency_budget` | `max_ms` (number, required) | `Metadata["latency_ms"]` | `min(max_ms / latency, 1)` |
| `cost_budget` | `max_cost_usd` (number), `max_input_tokens`, `max_output_tokens`, `max_total_tokens` (int) | `Metadata["total_cost"]`, `["input_tokens"]`, `["output_tokens"]` | 0 over budget; otherwise `1 - cost / max_cost_usd`, or 1 with no cost limit |

`cost_budget` needs at least one limit. A missing `latency_ms` scores 0. In a guardrail, the runtime derives these metadata values from the messages' cost info and the call's latency; a caller-supplied `total_cost` on the request metadata wins.

---

## Meta checks

| Type | Params | Score |
|------|--------|-------|
| `guardrail_triggered` | `validator_type` (string, required; alias param `validator`), `should_trigger` (bool, default `true`), `direction` (`input` \| `output` \| `both`) | 1 or 0 |
| `invariant_fields_preserved` | `tool` (string, required), `fields` (string[], required) | Fraction of fields preserved |

`guardrail_triggered` searches the prior results in the same batch, newest first, for one whose type or eval ID equals `validator_type`. The validator counts as triggered when its score is below `1.0`. Guardrail firings in the assistant message's validations are seeded into the prior results. With no matching result, the check passes only when `should_trigger` is `false`.

`direction` narrows the match to firings recorded on that side (`input`, before the LLM call; `output`, the assistant response). Omitted or `both` matches either side. A firing recorded without a direction never matches a direction-qualified check, and an unrecognized value is rejected when the eval definitions are validated.

```yaml
assertions:
  - type: guardrail_triggered
    params:
      validator_type: pii_leakage
      direction: input
      should_trigger: true
```

`invariant_fields_preserved` fails for a field that appeared in an earlier call to `tool` and is absent from a later call. With fewer than two calls, it passes.

---

## Behavioral testing checks

These checks compare a run against an expectation recorded in the params.

| Type | Params | Score |
|------|--------|-------|
| `outcome_equivalent` | `metric` (`tool_calls` \| `final_state` \| `content_hash`, required), `expected_tools` (string[]), `expected_state` (string), `expected_content` (string) | 1 or 0 |
| `directional` | `check` (`same_tool_calls` \| `same_outcome` \| `similar_content`, required), `baseline_tools` (string[]), `baseline_state` (string), `baseline_content` (string) | 1 or 0; the word-overlap ratio for `similar_content`. `threshold` is rejected with an error result; put the pass line on the [`assertion`](#assertion-wrapper) wrapper as `min_score`. |

| Metric or check | Compares |
|-----------------|----------|
| `tool_calls`, `same_tool_calls` | The set of distinct tool names called against the expected set. |
| `final_state`, `same_outcome` | `Extras["workflow_state"]` against the expected state. |
| `content_hash` | `CurrentOutput` against `expected_content`, for equality. |
| `similar_content` | Jaccard overlap of the word sets of `CurrentOutput` and `baseline_content`. The overlap ratio is the score. |

When the matching expectation param is absent, the check is skipped and passes.

---

## Extending the check system

### Custom `EvalTypeHandler` (Go)

Implement `EvalTypeHandler` and register it:

```go
type EvalTypeHandler interface {
    Type() string
    Eval(ctx context.Context, evalCtx *EvalContext, params map[string]any) (*EvalResult, error)
}
```

Register globally (available to every new registry):

```go
evals.RegisterDefault(handler)
```

Or on one registry instance, then pass it with `sdk.WithEvalRegistry`:

```go
registry.Register(handler)
```

A handler may also implement `ParamValidator` (`ValidateParams(params map[string]any) error`), which runs when a guardrail is built and in `ValidateEvalTypes`, after defaults and aliases apply.

### `StreamableEvalHandler` (Go)

Implement `StreamableEvalHandler` for incremental evaluation of each streaming chunk in a guardrail:

```go
type StreamableEvalHandler interface {
    EvalTypeHandler
    EvalPartial(ctx context.Context, content string, params map[string]any) (*EvalResult, error)
}
```

### Exec eval handlers (any language)

Bind a check type to a subprocess in RuntimeConfig. The subprocess receives JSON on stdin and writes JSON to stdout.

```yaml
spec:
  evals:
    my_python_eval:
      command: python3
      args: ["./evaluators/my_eval.py"]
      env: ["MY_API_KEY"]
      timeout_ms: 5000
```

| Field | Description |
|-------|-------------|
| `command`, `args` | The executable and its arguments. |
| `env` | Names of host environment variables to pass through to the process. |
| `timeout_ms` | Per-invocation timeout. Default 30 seconds. |

Stdin:

```json
{"type": "my_python_eval", "params": {}, "content": "...", "context": {}}
```

Stdout:

```json
{"score": 0.85, "detail": "Explanation text", "data": {}, "metrics": {}, "error": ""}
```

`metrics` holds named numeric sub-scores. A non-empty `error` means no measurement was taken, and fails any assertion. A crash, a timeout or output that is not this JSON is also an error. See the [exec protocol](/sdk/reference/exec-protocol/#eval-protocol) for every field.

`score` is the eval's output. Pass or fail comes from an assertion or guardrail wrapper.

### Custom `JudgeProvider` (Go)

```go
type JudgeProvider interface {
    Judge(ctx context.Context, opts JudgeOpts) (*JudgeResult, error)
}
```

Register with `sdk.WithJudgeProvider(provider)` when opening a conversation.

### Custom `ProviderHook` (Go)

For runtime guardrails beyond the built-in check types, implement `ProviderHook`:

```go
type ProviderHook interface {
    Name() string
    BeforeCall(ctx context.Context, req *ProviderRequest) Decision
    AfterCall(ctx context.Context, req *ProviderRequest, resp *ProviderResponse) Decision
}
```

A hook may also implement `ChunkInterceptor` for streaming:

```go
type ChunkInterceptor interface {
    OnChunk(ctx context.Context, chunk *providers.StreamChunk) Decision
}
```

Register with `sdk.WithProviderHook(hook)`.

---

## Related

- [Unified Check Model](/concepts/validation/)
- [Write Assertions](https://promptarena.altairalabs.ai/arena/reference/assertions/)
- [Add Guardrails](https://promptarena.altairalabs.ai/arena/reference/validators/)
- [Eval Framework](https://promptarena.altairalabs.ai/arena/explanation/eval-framework/)
- [Run Evals](/sdk/how-to/observability/run-evals/)
