# test-a-codegen-agent

A PromptArena kit that dogfoods PromptArena itself: it tests whether our
**agent-assist tooling** is actually good enough to get a developer who knows
nothing about PromptArena from "I have an agent idea" to a valid, faithful,
runnable test kit. The subject under test is the tooling — the emitted AGENTS.md
brief, the installed `.claude/skills/promptarena-authoring` skill, and the
`promptarena explain`/`schema`/`examples`/`validate` CLI — not the agent model.

Each scenario is a **naive developer request** (someone describing the agent they
want to build and what they're worried about, in plain language — no PromptArena
jargon). A good run is one where the tooling steers the coding agent to a kit that
faithfully tests *that* developer's agent.

## How it works

A coding agent runs inside a Docker sandbox built via
`make build-codegen-agent-sandbox` (bakes `promptarena`, `packc`, and gate
scripts onto the codegen-sandbox image). The sandbox is **briefed like a real
project**: the image runs `promptarena agent-brief /workspace`, which installs
`AGENTS.md` and the full `.claude/skills/promptarena-authoring/SKILL.md` — so the
agent starts with the same tooling a developer's coding agent would have. Given a
naive request, the agent authors a kit under `/workspace/kit`. When the
conversation ends, `conversation_assertions` score the result — five gates plus
one non-gating metric:

| Gate | Check |
|------|-------|
| Gate 1 | `promptarena validate` — config is schema-valid |
| Gate 2 | `packc compile` + `packc validate` — kit compiles to a PromptPack |
| Gate 3 | `promptarena run --ci` — generated scenarios run green |
| Gate 4 | `unused-files.sh` — no unreferenced files in the kit |
| Gate 5 | `kit-quality.sh` — deterministic adequacy: ≥1 scenario, each with a non-trivial assertion |
| Metric | `idiom-traps.sh` — non-gating idiom-trap + assertion-adequacy report |

All five gates and the metric are deterministic and shared across authoring
tasks (defined once in `config.arena.yaml` under
`spec.globals.conversation_assertions`). Each runs a command **inside the live
sandbox** against the real `/workspace/kit` files (via the MCP tool registry) and
passes only when a `__GATE_OK__` sentinel is present — so a failed command
actually fails the gate. There is deliberately **no LLM judge**: a structured
YAML kit's quality is checked deterministically (validity, compilation, green
run, no orphans, non-trivial assertions). An LLM code-reviewer is a possible
future addition for more complex artifacts, reading them via the sandbox API.

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
