# Hooks & Guardrails Example

Demonstrates how to use SDK hooks and guardrails to enforce policy on both
sides of an LLM call — the user's input before it's sent, and the assistant's
response after it comes back.

## What You'll Learn

- Registering input guardrails that gate the user's message **before** the
  LLM call (`guardrails.Input`, `guardrails.InputFunc`) — a hit spends zero
  tokens, no provider request is made
- Registering output guardrails that gate the assistant's response **after**
  the LLM call (`guardrails.Output`, built-in `banned_words`/`length`)
- Declaring a guardrail entirely in the pack (`hooks.pack.json`'s
  `validators` block) — no Go code at all
- The two ways a guardrail can respond, and how to tell them apart:
  - **Enforced** — a graceful block. `Send()` returns no error; the turn
    carries a canned message and a validation record instead.
  - **Deny** — a hard error. `Send()` returns an error you catch with
    `errors.As`.
- Writing a custom `ProviderHook` with `ChunkInterceptor` for streaming
  support (the full-interface path, used here for a Deny example)
- Streaming with chunk-level guardrail enforcement

## Prerequisites

- Go 1.26+
- An API key for one of: OpenAI, Anthropic, or Google (the packs do not pin a provider)

## Running the Example

```bash
export OPENAI_API_KEY=your-key
go run .
```

## How It Works

### Input guardrails — graceful blocking, before any LLM call

Hooks are registered as SDK options when opening a conversation.
`guardrails.Input` gates the user's message; a hit means the provider is
never called:

```go
conv, err := sdk.Open("./hooks.pack.json", "chat",
    sdk.WithGuardrail(
        guardrails.Input("banned_words", map[string]any{
            "words": []any{"wire transfer"},
        }, guardrails.WithMessage("I can't help with transfers.")),
    ),
)

resp, err := conv.Send(ctx, "Can you help me set up a wire transfer?")
// err is nil — an Enforced guardrail is not an error.
// resp.Text() == "I can't help with transfers."
```

For a bespoke check with no interface to implement, use `guardrails.InputFunc`:

```go
guardrails.InputFunc("no-ssn", func(ctx context.Context, in *hooks.InputRequest) hooks.Decision {
    if strings.Contains(strings.ToLower(in.UserInput), "social security number") {
        in.Replacement = "I can't help with that request."
        return hooks.Enforced("ssn requested", map[string]any{"validator_type": "no-ssn"})
    }
    return hooks.Allow
})
```

`hooks.InputRequest` gives you `UserInput`, the full `Messages` history,
the current `Round`, and `Replacement` (the canned text to send back).
There's a matching `guardrails.OutputFunc` for the response side, working
against `hooks.OutputRequest` (`Content`, `Message` — mutate it in place to
rewrite, `Round`).

**Direction is the only thing that changes.** A content check evaluates
whichever side `direction` selects, so the same type works either way —
`guardrails.Input("banned_words", …)` gates the user's message and
`guardrails.Output("banned_words", …)` gates the reply. As an output
guardrail a check judges that response alone; an earlier turn does not
affect the verdict.

### Pack-declared validators — the same gating, zero Go code

`hooks.pack.json`'s `chat` prompt declares an input guardrail directly:

```json
"validators": [
  {
    "type": "regex",
    "enabled": true,
    "params": {
      "pattern": "(?i)\\brouting number\\b",
      "expect_match": false,
      "direction": "input",
      "message": "I can't help with bank routing numbers."
    }
  }
]
```

The promptpack schema's `Validator` object has no top-level `message` or
`direction` property (`additionalProperties: false`) — both live inside
`params`. `direction` accepts `"input"`, `"output"`, or `"both"`; it defaults
to `"output"` if omitted. Pack-declared validators are wired in before any
`WithGuardrail` hooks registered in code, so they always get first look at a
call.

### Enforced vs. Deny

Every eval-backed and func-backed guardrail in this example (`Input`,
`InputFunc`, `Output`, and the pack validator) answers with `hooks.Enforced`
on a hit: `Send()` succeeds, the turn is replaced with a canned message, and
it's marked with `types.FinishReasonSafety` plus a
`types.ValidationResult` naming the guardrail. The pipeline is **not**
aborted — downstream stages still run.

The custom `PIIHook` in this example is different: it implements the full
`hooks.ProviderHook` interface and answers with `hooks.Deny` — a genuine hard
error:

```go
resp, err := conv.Send(ctx, "Make up a fake contact card...")
if err != nil {
    var denied *hooks.HookDeniedError
    var aborted *providers.ValidationAbortError
    if errors.As(err, &denied) {
        fmt.Printf("Blocked: %s\n", denied.Reason)
    } else if errors.As(err, &aborted) {
        fmt.Printf("Blocked during streaming: %s\n", aborted.Reason)
    }
}
```

Hooks execute in registration order; the first non-`Allow` decision (Deny or
Enforced) short-circuits the rest for that call.

### Detecting a graceful block without string-matching the text

Because an `Enforced` block returns no error, you can't use `err != nil` to
notice one. This example's `blockedByGuardrail` helper checks
`resp.Validations()` instead:

```go
func blockedByGuardrail(resp *sdk.Response) (validatorType string, blocked bool) {
    for _, v := range resp.Validations() {
        if !v.Passed {
            return v.ValidatorType, true
        }
    }
    return "", false
}
```

**The two signals, and when to use which.** The pipeline marks a blocked turn's
message with `FinishReason == types.FinishReasonSafety`, and that now reaches
the caller on both paths — `resp.Message().FinishReason` after `Send()` (#1681)
and after the terminal `ChunkDone` of `Stream()` (#1715). There is no dedicated
`Response.FinishReason()` accessor; go through `Message()`. That's the cheapest
check for *whether* a turn was blocked, and it also surfaces other terminal
states such as `max_output_tokens` and `refusal`.

`resp.Validations()` is the only signal that names *which* guardrail fired, so
the helper above stays the right tool when you want to log or branch on the
specific policy. It is populated only when the firing guardrail's
`hooks.Enforced(...)` call includes a `"validator_type"` key in its metadata —
`guardrails.Input`/`Output` always do; a bespoke `InputFunc`/`OutputFunc` only
does if you add it yourself, same as the `no-ssn` example above.

## Next Steps

- [Streaming Example](../streaming/) - Response streaming basics
- [Tools Example](../tools/) - Function calling
- [HITL Example](../hitl/) - Human-in-the-loop approval
