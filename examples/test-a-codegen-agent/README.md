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
under `/workspace/kit`. When the conversation ends, five `conversation_assertions`
score the result as gates:

| Gate | Check |
|------|-------|
| Gate 1 | `promptarena validate` — config is schema-valid |
| Gate 2 | `packc compile` + `packc validate` — kit compiles to a PromptPack |
| Gate 3 | `promptarena run --ci` — generated scenarios run green |
| Gate 4 | `unused-files.sh` — no unreferenced files in the kit |
| Metric | `idiom-traps.sh` — non-gating idiom-trap + assertion-adequacy report |

The authoring system prompt (`packs/authoring-agent.yaml`) is generated from
`agentkb.AgentsBrief()` and kept in sync by a byte-parity test, so this kit
always tests the current shipped brief.

## Live run (real model + Docker)

Requires `ANTHROPIC_API_KEY` and Docker.

```bash
make build-codegen-agent-sandbox
cd examples/test-a-codegen-agent
promptarena run -c configs/live.arena.yaml --ci --format html
open out/report.html
```

## Wiring check (Docker, no API key, no cost)

`configs/mock.arena.yaml` uses a `type: mock` provider that scripts the agent's
tool calls. The gates execute for real against a known-good kit (`refund-assistant`)
and a deliberately broken kit (`refund-broken`) that must be detected. No model
calls are made — only the LLM is faked.

```bash
make build-codegen-agent-sandbox
cd examples/test-a-codegen-agent
promptarena run -c configs/mock.arena.yaml --ci --format html,json
```

## Summarizing results

```bash
bash report/summarize.sh out
```
