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
| `tools_called` | `tool_called` | `tool_names` (string[]), `min_calls` (int, default 1), `ignore_validation` (bool), `require_args` (bool) | A G E |
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

## Tool Invocation Checks

Unlike the tool checks above (which evaluate tools the agent already
called), this check **invokes a tool itself** and asserts on the
result. Typical use is to run a verification tool — a sandbox's
`run_tests`, a render-and-diff utility, a custom HTTP probe — as the
hard gate after the conversation completes.

### `tool_exec`

Invokes a registered tool by name through the runtime tool registry
and passes if the call succeeded. The pass condition is:

- `tools.Registry.Execute` returns no error, **and**
- the resulting `ToolResult.Error` field is empty.

This makes `tool_exec` a generic "is this tool happy" gate that works
with any registered tool — MCP-discovered (e.g. a sandbox's
`run_tests`), HTTP, local executors, custom client tools. The handler
doesn't know or care about the transport.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `tool` | string | Yes | Registry name of the tool to invoke. |
| `args` | object | No | Arguments passed verbatim to the tool. Defaults to `{}`. |
| `timeout_seconds` | int | No | Per-call timeout. Default `120`. Generous because the typical use case is a long-running test suite inside a sandbox. |

**Surfaces:** A E (conversation-level / session-level — invoke at the end of the session, not per turn)

**Example — gating on a sandbox's hidden test suite:**

```yaml
conversation_assertions:
  - type: tool_exec
    params:
      tool: run_tests
    message: "Hidden tests must pass"
```

Pair this with a [source-backed MCP entry](/arena/how-to/provision-mcp-sandbox/)
that supplies the `run_tests` tool — the sandbox lives for the
session, runs the agent's edits, and the gate checks them at the end.

**Example — pack-shipped validation tool:**

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

**Notes**

- The host (arena, SDK, …) must inject a `*tools.Registry` into
  `EvalContext.Metadata["tool_registry"]`. Arena does this
  automatically; SDK consumers using the runtime evals API directly
  need to populate it themselves.
- Because the gate _calls_ a tool, it counts toward whatever cost /
  side-effect budget the tool implies (e.g. running the test suite
  costs CPU time, an HTTP probe costs a request).
- Errors from the tool surface in the assertion's `Explanation` so
  failures are debuggable from the report without re-running.

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

## Classify-backed Checks

These eval primitives call an `inference` provider (HuggingFace today; ONNX in flight) via the `runtime/classify` task interfaces and emit the model's score for a configured label. They are **pure eval primitives** — they do **NOT** apply pass/fail thresholds themselves. Threshold judgment lives on the [`assertion`](#assertion-wrapper) wrapper.

They depend on a provider with `role: inference` being declared in the arena config; without one — for example a keyless CI run with no `HF_TOKEN` — the check **skips cleanly** rather than failing.

**Two declaration sites:**

```yaml
# As a pack-level runtime eval — emits the raw signal as a metric every turn.
evals:
  - id: response-toxicity
    type: text_toxicity
    trigger: every_turn
    params: { model: unitary/toxic-bert, expected_label: toxic }

# As an Arena assertion — wrap with `type: assertion` to apply a threshold.
conversation_assertions:
  - type: assertion
    params:
      eval_type: text_toxicity
      eval_params: { model: unitary/toxic-bert, expected_label: toxic }
      max_score: 0.3
```

Putting `min_score` or `max_score` directly on a classify-backed handler is rejected at parse time; the error points at the wrapper. This stops the eval and assertion roles from drifting into one undifferentiated blob.

**Common params** (shared across the family):

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `model` | string | Yes | Backend model id (e.g. `unitary/toxic-bert`, `superb/wav2vec2-base-superb-er`) |
| `expected_label` | string | Yes | Label whose score is emitted |
| `message_role` | string | No | Whose messages to score (`user` for audio, `assistant` for text by default) |
| `message_index` | int | No (default -1) | Pick a specific message (`-1` = latest) |
| `classifier_id` | string | No | Explicit registry id; empty uses `defaults.inference.<task>_classifier` |

### `audio_emotion`

Speech-emotion-recognition gate. Picks an audio part from the chosen role's messages, runs it through an `AudioClassifier`, and emits the model's score for the chosen emotion label. Used in the voice-refund-demo (wrapped in `type: assertion`) to verify aggressive selfplay callers actually sound aggressive in their TTS audio.

**Surfaces:** A E (conversation assertion when wrapped; runtime eval when declared in `evals:`)

**Example (Arena assertion with threshold):**

```yaml
conversation_assertions:
  - type: assertion
    params:
      eval_type: audio_emotion
      eval_params:
        model: superb/wav2vec2-base-superb-er
        message_role: user
        expected_label: ang     # this model emits truncated labels: ang/neu/hap/sad
        classifier_id: hf
      min_score: 0.5
```

### `text_toxicity`

Classifier-backed toxicity eval. Distinct from the LLM-judge [`toxicity`](#toxicity) — `text_toxicity` is the deterministic path through a HuggingFace text-classification model. Emits the model's score for `expected_label`; the `assertion` wrapper decides pass/fail based on `min_score` or `max_score`:

**Surfaces:** A E

**Examples:**

```yaml
# Negative framing — "this output should NOT be toxic"
- type: assertion
  params:
    eval_type: text_toxicity
    eval_params:
      model: unitary/toxic-bert
      expected_label: toxic
    max_score: 0.3

# Positive framing — "this output should sit in the neutral class"
- type: assertion
  params:
    eval_type: text_toxicity
    eval_params:
      model: s-nlp/roberta_toxicity_classifier
      expected_label: neutral
    min_score: 0.7
```

### `text_sentiment`

Classifier-backed sentiment eval. Emits the model's score for `expected_label`. Wrap with `type: assertion` and `min_score` for the typical "assistant should sound positive" framing; for inverse assertions point at the opposing label.

**Surfaces:** A E

**Example:**

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

### `assertion` (wrapper) {#assertion-wrapper}

Generic wrapper that turns any eval primitive into a thresholded pass/fail. Cleanest way to use any classify-backed eval as a scenario assertion.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `eval_type` | string | Yes | The inner eval handler's type (e.g. `text_toxicity`, `audio_emotion`, `cosine_similarity`) |
| `eval_params` | object | Yes | Params passed verbatim to the inner eval |
| `min_score` | float | No | Pass when inner Score is at-or-above this |
| `max_score` | float | No | Pass when inner Score is at-or-below this |

If both `min_score` and `max_score` are set, both must hold. If neither is set, the default is `min_score: 1.0` (inner eval must fully pass). For LLM-judge and classify-backed evals that emit raw scores in `[0,1)`, **set an explicit `min_score`** on the wrapper — leaving it unset will report every result as failed.

Parallel handler: `guardrail` — same shape but consumes the eval at runtime to block, not assert.

---

## LLM Judge Checks

LLM judge checks are pure eval primitives: they send the assistant output (or full session) to a language model for evaluation. The judge returns a score (0.0–1.0) and reasoning; the handler emits the raw score as `EvalResult.Score` and preserves the judge's `passed` opinion in `Details`. Threshold judgment lives on the [`assertion`](#assertion-wrapper) wrapper — putting `min_score` / `max_score` directly on an LLM-judge handler is rejected at parse time.

### `llm_judge`

Turn-level LLM evaluation. The judge sees the current assistant response and evaluates it against the provided criteria.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `criteria` | string | Yes | What the judge should evaluate |
| `rubric` | string | No | Detailed scoring guidance |
| `model` | string | No | Model to use for judging |
| `system_prompt` | string | No | Override the default judge system prompt |
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

Plus all standard judge params (`rubric`, `model`, `system_prompt`, `extra`). **Surfaces:** A E

**Example:**

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

## RAG Checks

RAG checks are named eval primitives for retrieval-augmented generation: they score the answer against retrieved context (`faithfulness`, `hallucination`), the answer against the question (`answer_relevancy`), or the retrieved chunks against the question / ground truth (`contextual_precision`, `contextual_recall`, `contextual_relevancy`).

Each handler is a thin wrapper over `llm_judge` with a hardened default prompt drawn from public DeepEval / Ragas reference implementations (Apache 2.0). Like `llm_judge`, the RAG handlers are pure eval primitives — wrap with [`assertion`](#assertion-wrapper) to apply a threshold. The standard judge params (`rubric`, `model`, `system_prompt`, `extra`) all apply; supplying `system_prompt` or `criteria` overrides the default.

**Context sources** — every handler that needs retrieved chunks accepts them in three forms:

| Form | Example |
|------|---------|
| `contexts: ["chunk-1", "chunk-2"]` | Canonical list form |
| `context: "single chunk"` | Convenience form for one chunk |
| `context_field: retrieved_chunks` | Looks up the named key in `evalCtx.Metadata` — use this when a retrieval tool writes chunks to metadata at runtime |

### `faithfulness`

Scores how directly the answer is supported by the supplied context. Equivalent in name to DeepEval / Ragas `faithfulness`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `contexts` \| `context` \| `context_field` | string[] / string / string | Yes (one of) | Retrieved context the answer should be grounded in |

Plus standard judge params. **Surfaces:** A E

```yaml
assertions:
  - type: assertion
    params:
      eval_type: faithfulness
      eval_params:
        context_field: retrieved_chunks
      min_score: 0.8
```

### `answer_relevancy`

Scores how directly the answer addresses the user's question. Equivalent in name to DeepEval / Ragas `answer_relevancy`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `question` | string | No | Defaults to the last user turn in the session |

Plus standard judge params. **Surfaces:** A E

### `contextual_precision`

Scores the fraction of retrieved chunks that are relevant to the question (relevant chunks / total chunks). Equivalent in name to DeepEval `contextual_precision`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `contexts` \| `context` \| `context_field` | — | Yes | Retrieved chunks |
| `question` | string | No | Defaults to the last user turn |

Plus standard judge params. **Surfaces:** A E

### `contextual_recall`

Scores how completely the retrieved chunks cover the information the ground-truth answer relies on. Equivalent in name to DeepEval / Ragas `contextual_recall`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `contexts` \| `context` \| `context_field` | — | Yes | Retrieved chunks |
| `reference` \| `expected_output` | string | Yes | Ground-truth answer |

Plus standard judge params. **Surfaces:** A E

### `contextual_relevancy`

Scores the mean per-chunk relevance of retrieved chunks to the question (distinct from `contextual_precision`: precision is binary relevant/not; relevancy is the mean of graded scores). Equivalent in name to DeepEval `contextual_relevancy`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `contexts` \| `context` \| `context_field` | — | Yes | Retrieved chunks |
| `question` | string | No | Defaults to the last user turn |

Plus standard judge params. **Surfaces:** A E

### `hallucination`

Scores how free the answer is of unsupported / contradicting claims relative to the context — the inverse framing of `faithfulness`, kept as a distinct handler so users coming from DeepEval find the vocabulary they expect. 1.0 = no hallucination; 0.0 = entirely hallucinated. Equivalent in name to DeepEval `hallucination`.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `contexts` \| `context` \| `context_field` | — | Yes | Retrieved context the answer should be grounded in |

Plus standard judge params. **Surfaces:** A E

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

:::note[Three-role model]
RAG checks are eval primitives invoked as assertions. They can also be wired as monitor-only guardrails via `runtime/hooks/guardrails/factory.go` — but for retrieval quality, the assertion shape is the natural default. See the [Validators reference](/arena/reference/validators/) for the guardrail-side wiring.
:::

---

## Safety Checks

Safety checks score the assistant output for a specific concern: bias, toxicity, PII leakage, role violation. Each is an eval primitive — but **the demo-default wiring is as a guardrail**, with scenario tests observing the firing via `guardrail_triggered`. This pairs production enforcement (the guardrail mutates / blocks unsafe content) with test observation (the assertion confirms the guardrail fired on the expected input), from a single primitive.

The shape:

```yaml
# In the pack's prompt config — runtime enforcement
validators:
  - type: pii_leakage
    params:
      direction: output

# In a scenario turn — test predicate
assertions:
  - type: guardrail_triggered
    params:
      validator: pii_leakage
      should_trigger: true
```

For direct scenario invocation as a pass/fail assertion, wrap the safety primitive with `type: assertion` and set `min_score` on the wrapper. Putting `min_score` / `max_score` directly on a safety handler is rejected — see the [eval/assertion wrapper](#assertion-wrapper) for the canonical shape.

LLM-judged safety checks (`bias`, `toxicity`, `role_violation`, and the LLM-judged path of `pii_leakage`) carry a known false-positive rate. Tune the wrapper's `min_score` for your scenarios and prefer the regex pre-pass (`pii_leakage`) for high-confidence patterns.

### `bias`

Scores the answer for demographic, stereotype, gender, racial, or religious bias. Equivalent in name to DeepEval `bias`.

Standard judge params (`rubric`, `model`, `system_prompt`, `criteria`, `extra`) — all optional. **Surfaces:** A G E

### `toxicity`

Scores the answer for toxic content: insults, harassment, threats, hate speech. Equivalent in name to DeepEval `toxicity`. Distinct from the classifier-backed [`text_toxicity`](#text_toxicity); `toxicity` is the LLM-judge path.

Same params as `bias`. **Surfaces:** A G E

### `pii_leakage`

Scores the answer for personally-identifiable information leakage. Equivalent in name to DeepEval `pii_leakage`.

Implementation runs a **regex pre-pass** for high-confidence patterns (emails, US-style SSN, 16-digit card-shape numbers) before the LLM-judged path. A regex hit returns score 0 immediately without an LLM call — keeps the obvious cases cheap and deterministic. Ambiguous patterns fall through to the LLM judge.

Same params as `bias`. **Surfaces:** A G E

### `role_violation`

Scores the answer for adherence to the assigned role / persona / instruction set. Equivalent in name to DeepEval `role_violation`.

The judge sees the active agent role (sourced in priority order from `params["agent_role"]`, then `evalCtx.Metadata["system_prompt"]`) so it can decide whether the answer deviates. If no role is available, the judge falls back to generic role-consistency scoring.

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_role` | string | No | The persona / system prompt the answer should follow. Distinct from the standard `system_prompt` param, which controls the JUDGE's prompt. |

Plus standard judge params. **Surfaces:** A G E

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
