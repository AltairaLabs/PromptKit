# Codegen Sandbox Example

A runnable demo of an agent doing real codegen inside a
[codegen-sandbox](https://github.com/AltairaLabs/CodeGen-Sandbox) container.

## What this demonstrates

- Arena's new `source: docker` MCP entry — a host-provisioned MCP server
  whose lifecycle is managed per-session.
- Scope-level lifecycle: one fresh sandbox container per scenario session,
  torn down when the session ends.
- Skill staging: `skills/codegen/` is bind-mounted read-only into the
  sandbox at `/skills/codegen/`, so the agent can `Bash` any scripts
  shipped with the skill.
- The HTTP+SSE MCP transport (from the SSE MCP client work).

## Live demo (requires Docker)

```bash
make -C ../.. build-arena
docker pull ghcr.io/altairalabs/codegen-sandbox:latest
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --ci --format html
open out/report.html
```

What happens:

1. Arena reads `config.arena.yaml`, sees `source: docker` on the `sandbox`
   MCP entry, resolves the `docker` source from the in-process registry.
2. For each scenario session, the docker source runs the image, publishes a
   free host port, polls `/health`, returns the MCP URL.
3. Arena registers the URL in the runtime MCP registry; the HTTP+SSE MCP
   client connects and discovers the 13 codegen tools.
4. `skills/codegen/` is bind-mounted at `/skills/codegen/` inside the
   container so the agent can reach skill scripts via `Bash`.
5. Agent runs the scenario, verifies with `run_tests`, container is
   stopped + removed when the session ends.

## Offline (CI) demo

```bash
PROMPTKIT_SCHEMA_SOURCE=local ../../bin/promptarena run --mock-provider --ci --format html
```

Uses `mock-responses.yaml` to simulate the LLM's decisions. The
`--mock-provider` flag replaces real LLM calls with canned responses; no
sandbox is launched, so this works in CI without Docker.

Top-level shortcut: `make codegen-demo` from the repo root.

## Files

- `config.arena.yaml` — the arena config. Source-backed MCP entry lives here.
- `prompts/codegen-agent.yaml` — pack wiring the codegen skill + sandbox tools.
- `scenarios/simple-fix.scenario.yaml` — one bugfix scenario.
- `skills/codegen/SKILL.md` — the reusable codegen discipline skill.
- `providers/mock-provider.yaml` — mock provider for offline demos.
- `mock-responses.yaml` — canned LLM responses.
