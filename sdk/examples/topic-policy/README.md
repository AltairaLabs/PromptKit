# Topic Policy

Confines a conversation to a declared subject scope with the `topic_policy`
guardrail. A topic classifier — not the model being governed — decides whether
each user message is in scope, and a denied turn never reaches the agent.

```bash
go run .
```

No API keys needed.

## What it shows

```
[ALLOWED] (in scope)   Can Omnia run on OpenShift?
           -> Here's what I can tell you about that.

[DENIED ] (out of scope) Who should I vote for?
           -> I can only help with AltairaLabs products — Omnia, PromptKit, licensing and support.

Turns that reached the agent: 2 of 4
```

The count is the point. An off-topic answer is not generated and then
suppressed — it is never generated, so it costs nothing and cannot leak. The
`countingProvider` in `main.go` wraps the agent to make that visible.

An enforced guardrail is **not** an error: `Send` returns normally, with the
validator's `message` substituted for the model's reply.

## The two declaration sites

The policy is a pack validator, in `support.pack.json`:

```json
{
  "type": "topic_policy",
  "message": "I can only help with AltairaLabs products — Omnia, PromptKit, licensing and support.",
  "params": {
    "description": "Helps users evaluate and operate AltairaLabs products.",
    "allowed": ["Omnia and PromptKit", "licensing and support"],
    "disallowed": ["politics", "medical advice"],
    "small_talk": "allow",
    "recent_turns": 4
  }
}
```

The classifier is a host concern, declared separately — in a deployment, an
ordinary provider file:

```yaml
id: topic-control
role: inference
type: nvidia-topic-control
base_url: http://topic-control:8000/v1
```

Both are required. With no classifier bound the guardrail denies every turn:
`on_error` defaults to deny, and "nothing configured at all" is an error. That
is deliberate — a safety control that silently does not run is the failure it
exists to prevent.

## The classifier this example uses

By default `main.go` binds `keywordClassifier`, a deterministic substring
matcher, so the example runs offline and prints the same thing every time. It is
**not** a topic classifier: it cannot resolve "what about that one?" against the
history, and it has no opinion on anything it has no keyword for.

To run the same policy against a real endpoint:

```bash
TOPIC_CONTROL_BASE_URL=http://localhost:8000/v1 \
TOPIC_CONTROL_MODEL=nvidia/llama-3.1-nemoguard-8b-topic-control \
go run .
```

The backend is an OpenAI-compatible chat client that asks for one of two labels,
so any endpoint speaking that protocol works. Do not point it at a reasoning
model: it answers with its thinking rather than a bare label, every
classification parses as unknown, `on_unknown` denies, and the guardrail blocks
every turn.

## Reference

[Checks reference → `topic_policy`](https://promptkit.altairalabs.ai/reference/checks/#topic_policy)
