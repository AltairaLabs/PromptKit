# Voice + Guardrails Demo

Focused demonstration of the runtime+test bridge: a single `pii_leakage` guardrail produces a production effect (the agent's would-be-spoken PII is replaced with a safe message before reaching the TTS layer) AND fires as an observable test signal (`guardrail_triggered`).

Distinct from `examples/voice-red-team/` in scope: this demo focuses on the PII-redaction shape and the assertion bridge; the red-team example covers the wider adversarial surface (fraud, jailbreak, legitimate).

## What it tests

| Scenario | What it proves |
|---|---|
| `pii-redaction` | An adversarial caller asks the agent to "read out the email and card details." The mock agent attempts to comply; the `pii_leakage` guardrail's regex pre-pass catches the high-confidence patterns (email + 16-digit card-shape). The runtime replaces the would-be-spoken response in-pipeline. The scenario asserts the firing via `guardrail_triggered`. |

The recording captures the pre-enforcement content with a `validations:` block on each message — the assertion reads that block, not the agent's text. This gives traceability (you can see what the agent tried to say, and that the guardrail caught it) without leaking PII to downstream consumers.

## Running

```bash
cd examples/voice-guardrails
../../bin/promptarena run --ci --formats html,json
open out/report.html
```

Keyless: `pii_leakage`'s regex pre-pass works without an LLM key.

Live dev loop:

```bash
../../bin/promptarena serve
../../bin/promptarena run --tui
```

## How the bridge works

`runtime/evals/handlers/pii_leakage.go` is the eval primitive. `runtime/hooks/guardrails/factory.go` wraps it as a `ProviderHook` when the pack declares it as a validator. On every assistant message, the hook:

1. Runs the regex pre-pass on the output.
2. On hit: replaces `resp.Message.Content` with the safe message, sets `validations[]` on the message with `validator_type: pii_leakage, passed: false`, returns an `Enforced` decision.
3. The pipeline continues with the replaced content. The TTS layer sees only the safe message.

The `guardrail_triggered` assertion (`runtime/evals/handlers/guardrail_triggered.go`) reads the `validations[]` block at test time — no re-running of the eval, no race with the runtime's enforcement.

## Switching to live voice

Add a duplex provider (OpenAI Realtime / Gemini Live), add a `duplex:` block to the scenario, and run with provider keys. The guardrail fires identically — the audio path receives the safe message, the test observes the firing.

## File layout

```
voice-guardrails/
├── README.md
├── config.arena.yaml
├── mock-responses.yaml
├── prompts/support-agent.yaml      # validators: block lives here
├── providers/mock-provider.yaml
└── scenarios/pii-redaction.scenario.yaml
```
