# test-a-codegen-agent

A PromptArena kit that dogfoods PromptArena itself: it tests whether AI coding
agents, given the PromptArena authoring brief, reliably produce valid, faithful,
runnable PromptArena kits. The subject under test is the authoring experience —
the AGENTS.md brief embedded in the system prompt, plus the `promptarena
explain`/`schema`/`examples`/`validate` CLI — not the agent model itself.

## How it works

A brief-equipped coding agent runs inside a Docker sandbox built via
`make build-codegen-agent-sandbox` (bakes `promptarena`, `packc`, and gate
scripts onto the codegen-sandbox image). The agent authors a PromptArena kit
under `/workspace/kit`. When the conversation ends, `conversation_assertions`
score the result — five gates plus one non-gating metric:

| Gate | Check |
|------|-------|
| Gate 1 | `promptarena validate` — config is schema-valid |
| Gate 2 | `packc compile` + `packc validate` — kit compiles to a PromptPack |
| Gate 3 | `promptarena run --ci` — generated scenarios run green |
| Gate 4 | `unused-files.sh` — no unreferenced files in the kit |
| Gate 5 | `llm_judge_session` — kit faithfully implements the request |
| Metric | `idiom-traps.sh` — non-gating idiom-trap + assertion-adequacy report |

Gates 1–4 and the metric are shared across all authoring tasks (defined once in
`config.arena.yaml` under `spec.globals.conversation_assertions`); Gate 5's
faithfulness criteria are written per scenario.

The authoring system prompt (`prompts/authoring-agent.yaml`) is generated from
`agentkb.AgentsBrief()` and kept in sync by a byte-parity test, so this kit
always tests the current shipped brief.

## Live run (real model + Docker)

Requires `ANTHROPIC_API_KEY` and Docker.

```bash
make build-codegen-agent-sandbox
cd examples/test-a-codegen-agent
promptarena run -c config.arena.yaml --ci --format html
open out/report.html
```

## Wiring check (Docker, no API key, no cost)

`mock.arena.yaml` uses a `type: mock` provider that scripts the agent's
tool calls. The gates execute for real against a known-good kit (`refund-assistant`)
and a deliberately broken kit (`refund-broken`) that must be detected. No model
calls are made — only the LLM is faked.

```bash
make build-codegen-agent-sandbox
cd examples/test-a-codegen-agent
promptarena run -c mock.arena.yaml --ci --format html,json
```

## Capturing the generated workspace zip

The sandbox image exposes a `/api/download` endpoint (port 8090) that streams
the `/workspace` directory as a zip. Pass `--capture-workspace` to save it:

```bash
promptarena run -c config.arena.yaml --ci --format html --capture-workspace
```

The zip lands at `out/kit/<runID>/sandbox.zip` — one file per run, container
still alive at capture time. Omit the flag to skip capture (the default).

## Summarizing results

```bash
bash report/summarize.sh out
```

## Trying a cheaper model (Gemini)

`live-gemini.arena.yaml` points the same harness at Gemini 2.5 Flash
(cheaper per token than Claude Haiku). Run it with `GEMINI_API_KEY` set:

```bash
promptarena run -c live-gemini.arena.yaml --ci --format html
```

> **Known limitation (2026-06):** Gemini's function-calling API rejects tool
> declarations / tool results that contain JSON-Schema `$ref`/`$defs`
> (`400 INVALID_ARGUMENT: ... #/$defs/ObjectMeta ...`). Because the brief's
> discovery commands (e.g. `promptarena schema`) emit `$defs`-bearing schemas,
> the authoring loop currently errors on Gemini when such output is fed back as
> a tool result. Claude (Haiku/Sonnet/Opus) is unaffected. Tracking a runtime
> fix to flatten `$ref`/`$defs` for the Gemini provider.
