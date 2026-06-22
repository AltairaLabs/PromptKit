---
title: Build a PromptPack with an AI coding agent
description: Drive Claude Code, Codex, or Gemini CLI to author a measured PromptArena kit. promptarena init briefs your agent with the conventions, catalogs, and a success-first workflow.
sidebar:
  order: 20
---

PromptArena briefs your coding agent for you. `promptarena init` writes an authoring
skill and reference catalogs into the project, so Claude Code, Codex, and Gemini CLI know
the conventions before they touch a file — no repeated trips to the docs.

## 1. Scaffold and brief

```bash
npm install -g @altairalabs/promptarena   # or your preferred install
promptarena init my-kit --template quick-start
```

Alongside the sample kit, `init` writes:

- `AGENTS.md` — a shim that points the agent at the skill and lists the key idioms.
- `.claude/skills/promptarena-authoring/SKILL.md` — the full authoring skill with a
  success-first workflow and minimal config skeletons.
- `.claude/skills/promptarena-authoring/reference/` — generated catalogs the agent reads
  locally instead of guessing: every assertion/eval type, the fields for each config
  kind, and the CLI surface.

## 2. Open your agent

- **Claude Code** — loads `.claude/skills/` and `AGENTS.md` automatically.
- **Codex / Gemini CLI** — read `AGENTS.md`.

Ask it to build your kit. It follows a **success-first** workflow: there is no point
building an agent without measurable success, so it pins down success criteria, turns them
into assertions, scaffolds to the canonical layout, builds against a mock provider, then
runs.

## 3. Bring your own tools

A PromptPack defines tools; it does not implement them. A `kind: Tool` config is a
contract (`name`, `description`, `input_schema`, `output_schema`) plus a binding — never
backend code. So you can:

- **Mock it** (`mode: mock`) for tests,
- **Bind an existing API** you already run (`mode: live`/`http`, `mcp`, or `exec`), or
- **have the agent write a new tool service** — deployed separately from the pack.

Either way the implementation lives outside the pack and the deploy mechanism.

## 4. Run it

Pick how you want to see results:

- **TUI** — run `promptarena run` in a **separate terminal**. The interactive TUI does not
  run inside an agent session.
- **Web** — `promptarena serve`. The agent can start this as a background task.
- **Offline** — `promptarena run --ci --formats html,json` for headless reports.

Once the kit is green against mocks, switch to a real provider, use `promptarena chat` to
play with it conversationally, and `promptarena deploy` (or a CI workflow) to ship it.

<!-- TODO(video): embed the getting-started screencast here once recorded. -->
