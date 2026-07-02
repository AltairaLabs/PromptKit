# Codegen Sandbox — Real Anthropic Agent

A live coding agent (Claude Sonnet 4.6) that writes a fresh Go module
inside a [codegen-sandbox](https://github.com/AltairaLabs/CodeGen-Sandbox)
container, runs the tests, and reports back.

This is the companion to [`examples/codegen-sandbox/`](../codegen-sandbox/),
which uses a mock provider for offline CI. Use that one to verify wiring;
use this one to see an actual model do the work.

## Prerequisites

- Docker daemon running.
- `ANTHROPIC_API_KEY` set in your environment.
- `bin/promptarena` built (`make build-arena` from repo root).

```bash
cp .env.example .env
# fill in ANTHROPIC_API_KEY
export ANTHROPIC_API_KEY=...
docker pull ghcr.io/altairalabs/codegen-sandbox:latest
make -C ../.. build-arena
```

## Run

```bash
../../bin/promptarena run --ci --format html
open out/report.html
```

What you should see:

1. Arena starts a fresh codegen-sandbox container (port 8080 mapped to
   a random local port).
2. The runtime discovers the sandbox's MCP tools (Read, Edit, Bash,
   run_tests, run_lint, …) and registers them under their raw names so
   the agent can reference `Read`, not `mcp__sandbox__Read`.
3. Claude Sonnet receives the scenario and starts working — typically
   `Bash` to `go mod init`, `Write` for the implementation and tests,
   `run_tests` to verify, then a final summary.
4. Container is stopped and removed when the session ends.
5. The HTML report shows every tool call with its arguments and
   results, plus pass/fail status on the two `tools_called` assertions.

The scenario asks for `IsPalindrome(s string) bool` — case-insensitive,
whitespace-ignoring, with a table-driven test. Sonnet typically lands
this in 6–10 tool calls.

## Files

- `config.arena.yaml` — arena config; the `mcp_servers` block declares
  the source-backed sandbox.
- `prompts/codegen-agent.yaml` — system prompt + `allowed_tools` listing
  the sandbox tools.
- `providers/claude-sonnet.provider.yaml` — Claude Sonnet 4.6.
- `scenarios/implement-palindrome.scenario.yaml` — the coding task.
- `skills/codegen/SKILL.md` — agent discipline (Read-before-Edit,
  verify-before-done, etc.). Mounted read-only into the sandbox at
  `/skills/codegen/`.

## Cost

A successful run is typically 5–10k input + 1–3k output tokens against
Sonnet ≈ $0.05–$0.10 per run.

## Failure modes

- `tool_filter` rejected: the agent asked for a tool not in
  `allowed_tools`. Add it to the prompt config.
- `run_tests`: returns "no supported project detected" → the agent
  hasn't initialised `go.mod` yet. Usually self-corrects on the next
  turn.
- HTTP rate limit from Anthropic: bump `max_tokens` down and retry, or
  wait and rerun.

## See also

- [Provision an MCP Sandbox per Scenario](https://promptarena.altairalabs.ai/arena/how-to/provision-mcp-sandbox/) — reference docs for the source/scope/source_args fields.
- [`examples/codegen-sandbox/`](../codegen-sandbox/) — mock-provider variant for CI.
