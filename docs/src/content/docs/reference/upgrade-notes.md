---
title: Upgrade Notes
description: Changes that need action when moving between PromptKit versions.
---

Release notes are generated from merged pull requests. This page carries the
changes that need you to do something, with what to change and why.

## Unreleased

### Conversations use your provider's `max_tokens` and `temperature`, and send no output-token limit by default

A conversation sent `max_tokens: 4096` and `temperature: 0.7` on every turn
whose prompt set no `parameters`, so a provider's own defaults never applied.
It now sends neither, and the provider's defaults decide. With no output-token
limit from the prompt or the provider, none is sent and the model's own maximum
applies. Claude requires a limit, so it falls back to 4096. Providers that
`sdk.Open` builds for you keep `temperature: 0.7` and no longer set a limit.

| If you | You will see | Change |
|---|---|---|
| Pass a provider with `Defaults.MaxTokens` or `Defaults.Temperature` to `sdk.WithProvider` | those values on the wire | nothing; remove any workaround that rewrote 4096 / 0.7 |
| Pass a provider with no `Defaults` | no output-token limit (Claude: 4096); temperature `0` on OpenAI-compatible providers, and the API's own default on Claude | set `Defaults`, or set `parameters` on the prompt |
| Rely on the SDK capping replies at 4096 tokens | longer replies, and output cost up to the model's maximum | set `max_tokens` in the prompt's `parameters` or in the provider's defaults |
| Want no limit and set a large number to get it | nothing breaks | set `max_tokens: -1` (`providers.MaxTokensUnlimited`), or leave it unset |
| Set `max_tokens: -1` on a Claude provider | `CreateProviderFromSpec` fails: Claude requires a limit | set a positive limit, or leave it unset for 4096 |

### The A2A server and client speak A2A 1.0 and 0.3, not a mix of both

The A2A server used to answer in a shape of its own: 0.3's method names, 1.0's
parts, and state names from neither. Standard A2A clients could not parse it.
It now answers each request in the version the request asks for. That
is the `A2A-Version` header, or else the version of the method name used; a
request with no version is 0.3, as the spec says. The runtime client sends
`A2A-Version: 1.0` and falls back to 0.3 on its own.

| If you | You will see | Change |
|---|---|---|
| Parse server responses by hand | a 1.0 `SendMessage` result is `{"task": {...}}`; stream results are wrapped (`{"statusUpdate": ...}`); states are `TASK_STATE_*` and roles `ROLE_*` | use `a2a.Client`, which reads every version, or send 0.3 method names and parse 0.3 |
| Call `SendMessage` (1.0) and expect it to return at once | it now waits for the task to finish or need input, as 1.0 requires | set `configuration.returnImmediately: true` |
| Serve slow agents behind a proxy or load balancer with an idle timeout | a blocking `SendMessage` holds its request for the whole turn (a caller that disconnects releases it; the turn runs on) | set `WithMaxBlockingWait` below the proxy's timeout: past it the caller gets the working task and polls `GetTask` |
| Read a stream's first event as a `working` status | the first event is the Task | read `StreamEvent.Task` |
| Expect one artifact per streamed chunk (`artifact-0`, `artifact-1`, ...) | a text run is one artifact, extended with `append` and closed with `lastChunk`; the stored task holds it once, whole | key on `ArtifactID` and concatenate appended chunks |
| Call `tasks/list` or `ListTasks` without a `contextId` | `-32602`: without caller scoping it listed every caller's tasks | pass the `contextId`, or set `WithTaskOwner` to list the caller's own |
| Subscribe to a task that has finished | `-32004` UnsupportedOperation | read it with `GetTask` |
| Use `a2a.MethodSendMessage` and friends | nothing: they keep their old values (`"message/send"`, ...) and the server still answers them, but they are deprecated | use `a2a.MethodV1*` for 1.0 names, `a2a.MethodV03*` for 0.3, or `a2a.LookupMethod` |
| Match error codes | cancel of a finished task is `-32002`; an unsupported operation is `-32004`; push notification config is `-32003`; an internal failure is `-32603` without the cause | update the codes you match |
| Run PromptKit A2A clients from before this release against an upgraded server | a task that stops for client tools arrives as 0.3's `input-required`, which the old client cannot decode | upgrade clients with (or before) servers; an upgraded client still works with an old server, falling back to 0.3 names and the old card path |
| Serve the agent card from `/.well-known/agent.json` only | the card is also at `/.well-known/agent-card.json`, and the client looks there first | nothing, unless a proxy only forwards the old path |

`a2a.TaskState` constants keep their Go values. Stored tasks written in the old
JSON form still decode.

### A2A tasks can be scoped to their caller, and cancel/subscribe can span replicas

`a2aserver.WithTaskOwner` scopes every task to the caller that created it;
see [Callers and replicas](/sdk/how-to/interop/choose-a2a-server-mode/#callers-and-replicas).

| If you | You will see | Change |
|---|---|---|
| Implement your own `TaskStore` and want caller scoping | `NewServer` panics when `WithTaskOwner` is set | implement `OwnedTaskStore` (`CreateOwned`, `Owner`); if you implement `TaskQuerier`, filter by `TaskQuery.Owner` |
| Run several replicas behind a shared task store | `CancelTask` and `SubscribeToTask` only reached the replica running the task, as before | supply `WithTaskCanceler` and `WithTaskEventBus`, or keep session affinity |

### Inference providers share one interface; `runtime/classify` is superseded

`role: inference` providers now implement a single interface,
`inference.Provider` (`Infer`: content in, a probability per label out),
replacing the per-task `runtime/classify` interfaces. Each `type` calls one
vendor API: `huggingface`, `openai` (chat completions read through logprobs)
and `systemone` (TypeSafe's Jev, direct or through the Vercel AI Gateway). Pack
params for every check are unchanged. `runtime/classify` still compiles but
nothing uses it; it is removed in v3.

| If you | You will see | Change |
|---|---|---|
| Pass a custom backend to `sdk.WithClassifier` | `Open()` fails: the value "is not an inference.Provider" | implement `Infer(ctx, inference.Request) (inference.Response, error)`; audio and image arrive as media parts on `Request.Inputs` |
| Implement `evals.ProviderBinding` | a check whose `Classifier(key)` returns a classify backend errors: the bound value "is not an inference provider" | return an `inference.Provider` from `Classifier` |
| Set `stage.PipelineConfig.ClassifyRegistry` | it is ignored | set `InferenceRegistry` |
| Declare several inference providers without naming them in checks | every check now uses the **first** one registered | name the provider a check needs with `params.provider` (a key the pack declares in `requires`) |
| Use HF embeddings through the inference role | they are no longer served there | declare `role: embedding`, `type: huggingface` |
| Use `type: nvidia-topic-control` | no change needed, if the endpoint returns logprobs (NIM does) | it is now an alias for `type: openai` with NemoGuard topic control's model, keeping its 20s call timeout; `topic_policy` records the label's probability as `confidence` |
| Point `type: openai` at a non-OpenAI `base_url` without a `credential` | no key is sent (`OPENAI_API_KEY` is only sent to OpenAI) | add an explicit `credential` for that host |

`topic_policy` now sends every backend NemoGuard topic control's trained prompt
and asks for `on-topic` or `off-topic`, allowing whichever gets the higher
probability.

### Guardrail checks are bounded, and a timed-out guardrail returns its message

A guardrail's check is now bounded by a timeout (default 30s,
`evals.DefaultEvalTimeout`). A check that exceeds it (or whose classifier
errors) is enforced with the validator's `message`, where previously an
output guardrail could release the unchecked response and a timed-out input
guardrail returned empty text.

| If you | You will see | Change |
|---|---|---|
| Run a guardrail whose judge regularly takes over 30s, having raised `WithIdleTimeout` for it | the turn is blocked with the validator's message at 30s | raise the bound with `sdk.WithGuardrailTimeout(d)` |
| Declare a guardrail with `guardrails.InputFunc` / `OutputFunc` whose function can run past the bound | the turn is blocked with the default blocked message, `reason: timeout` | return sooner, or raise the bound with `sdk.WithGuardrailTimeout(d)` — it applies to func guardrails too |

A func guardrail's function receives a context carrying the deadline. One
that ignores it keeps running in the background after the turn moves on; its
result is discarded and it can no longer change the response.

Inference calls also emit `inference_requests_total`,
`inference_request_duration_seconds`, `inference_input_tokens_total` and
`inference_cost_total` (labels `provider`, `model`, `source`, plus `status`).

Provider HTTP retries (every LLM and inference provider) are counted in
`provider_retries_total{provider, outcome}` (under the metrics collector's namespace): `retry` per retried
attempt, `success` when a call recovers after retrying, `exhausted` when
every attempt fails. A backend that only answers on its second try is now
visible. The OpenAI inference provider's retries can be configured with
`openai.Config.RetryPolicy`.

### Embedding providers no longer guess a model's vector size

`EmbeddingDimensions()` used to answer with a fixed default for any model a
provider did not recognize (1536 for `openai`, 3072 for `gemini`, 768 for
`ollama`, 1024 for `voyageai`), so a self-hosted model reached as `type: openai`
reported 1536 while returning 768-length vectors. A declared `dimensions` was
also ignored by `openai` and `gemini`, and never sent by `ollama` or Bedrock
Titan v2.

Now the size is the declared `dimensions`, else the known size of the model,
else the length of the first vector returned. A declared size is sent to the
API, and every response is checked against the reported size.

| If you | You will see | Change |
|---|---|---|
| Call `EmbeddingDimensions()` before any `Embed` for a model the provider does not know | `0` | set `dimensions`, or embed one text first and read the size then |
| Declare a `dimensions` the model cannot produce | `Embed` errors, naming both sizes (or the API rejects the field, e.g. `text-embedding-ada-002`) | set it to the model's real size, or remove it |
| Rely on a declared `dimensions` for `openai`/`gemini` that was previously ignored | vectors now come back at that size | size existing vector storage to match, or remove the setting |

### Checks name the provider they need, and the host binds it

Checks that need a model they do not own (a judge for `toxicity`, a classifier
for `text_sentiment`) now name it by a **logical key the pack declares in
`requires`**. The host binds that name to a concrete provider and can rebind it
whenever it likes, without the pack changing.

```yaml
requires:
  providers:
    - key: grader          # a name this pack invented
      role: llm
      description: grades the toxicity guardrail
      required: true

prompts:
  chat:
    validators:
      - type: toxicity
        params:
          provider: grader
```

```go
conv, _ := sdk.Open("./app.pack.json", "chat",
    sdk.WithProvider(agent),
    sdk.WithNamedProvider(sdk.ProviderSpec{
        ID: "grader", Type: "openai", Model: "gpt-4.1-mini",
    }),
)
```

**What breaks, and what to do:**

| If you | You will see | Change |
|---|---|---|
| Use `classifier_id` in a pack | the check errors, naming the replacement | rename it to `provider`, and declare that name in `requires` |
| Declare a judge-backed check (`toxicity`, `bias`, `pii_leakage`, `role_violation`, `llm_judge`, the RAG primitives) that names no provider | `Open()` fails | add `params.provider` naming a key from `requires`, or pass `sdk.WithJudgeProvider` as a host default |
| Bind an ancillary provider with `sdk.WithLLMProvider` | it silently became your agent | use `sdk.WithNamedProvider`, which registers it for a pack to name without touching the agent |
| Run `sdk.Evaluate` on a pack whose checks name providers | the checks error | pass `EvaluateOpts.ProviderBinding` (or `JudgeTargets`, which is treated as one) |
| Relied on `sdk.JudgeProviderKey` | it is gone | the runtime no longer names providers; the pack does |

**Why the break is worth it.** A judge-backed guardrail with no judge used to
build successfully and then block **every turn**, reporting a content violation
as the reason — or, for `pii_leakage`, run only its regex layer with the LLM
half silently absent. Both now fail at `Open()`, naming the check and whether
the fix belongs in the pack or in your wiring. A check that names a provider it
cannot get is an error rather than a skip, because a skip scores 1.0 and passes:
a safety control that never ran must not report clean.

### `tools_offered` now answers for the turn, not the conversation

`tools_offered` was documented as checking what **a turn** handed the provider
and in practice reported the union over the whole conversation, in two places
at once: the pipeline never reset its record between turns, and the eval
context extracted over the full history.

Both are fixed. The set a check sees is now the tools offered across the
current turn's rounds — a skill grant that widens the set mid-turn still
counts, a grant from three turns ago does not.

**What to expect:** a check that was passing on stale evidence can start
failing, and that failure is the correct answer:

| Check | Before | Now |
|---|---|---|
| `tool_names: [refund]` | passed if `refund` was offered in **any** earlier turn | passes only if this turn offered it |
| `tool_names: [refund], absent: true` | could never fail once `refund` had been offered once | fails when this turn offers it |

If an assertion flips to failing, read it as the grant not being active on that
turn rather than as a regression in the check.

### Voice (VAD) sessions now run guardrails

A pack's `validators:` did nothing in a VAD voice session. Guardrails are
provider hooks, and the VAD pipeline built its provider stage with a nil hook
registry, so every hook path early-returned: no `validation.started` /
`passed` / `failed` events, and no enforcement. The docs said guardrails
applied everywhere; for voice they did not.

They now run, exactly as they do in text.

**What to expect:** a voice conversation that was passing because nothing
checked it can start being blocked. That is the configured policy taking
effect, not a regression. Before upgrading, check what your pack's
`validators:` would do to the voice path — they have never been exercised
there.

This covers VAD mode (`OpenVoice` and the VAD topology). Native realtime and
duplex sessions are a separate stage with no hook support at all;
guardrails still do not run there.

### Provider adapters now rewrite your JSON Schema for the vendor

A schema you write for a pack went to the wire as written, and the three
providers enforce rules that contradict each other:

| | Anthropic | Gemini | OpenAI (strict) |
|---|---|---|---|
| `additionalProperties: false` on every object | required | **rejected** | required |
| every property in `required` | not required | n/a | required |
| `minimum` / `maxItems` / `uniqueItems` / … | **rejected** | accepted | accepted |

So no schema was portable, and a perfectly valid one failed on whichever
provider you had not tried. The adapters now rewrite it on the way out —
output schemas and tool schemas alike.

**What to expect:**

- A schema that used to be rejected now works. The failure was a 400 naming a
  JSON path, arriving at the first real invocation rather than at deploy.
- On Anthropic, constraints its grammar cannot carry (`minimum`, `maxItems`,
  `uniqueItems`, …) are removed from the wire and restated in the node's
  `description`, so the model still sees them. Tool arguments stay validated
  against the schema **you** wrote — the rewrite never touches your descriptor.
- `not` is the one keyword left in place: it has no equivalent in the accepted
  subset, so the request still fails, with an error naming the keyword.

### Claude tools use Anthropic's native strict tool use

Tool definitions now carry `strict: true`, so the API constrains decoding to
your schema and `tool_use.input` is guaranteed to validate rather than merely
likely to.

**What to expect:** better-formed tool arguments, and a schema adapted as
above, because strict mode enforces the same rules. If you need a schema sent
exactly as written, turn it off per provider:

```yaml
providers:
  - id: claude
    type: claude
    model: claude-sonnet-5
    additional_config:
      strict_tools: false
```

## v2.4.0

### Judge-backed guardrails need a judge

Superseded by the entry above, which changes how the judge is supplied. If you
adopted `sdk.JudgeProviderKey` from this release, move to naming the provider in
the pack.

### A tool call slower than 30s no longer kills its own turn

No action needed. The pipeline's idle timer is held open while tools run, so a
slow tool is bounded by its own `TimeoutMs` rather than by `IdleTimeout`. If you
raised `IdleTimeout` to work around this, you can put it back.

### A2A `message/send` keeps caller context values

No action needed. Values your HTTP middleware puts on the request context
(identity, tenant, request-scoped config) now reach the conversation on
`message/send` as they always did on `message/stream`. If you worked around this
by keying state on `contextID`, that workaround can go.
