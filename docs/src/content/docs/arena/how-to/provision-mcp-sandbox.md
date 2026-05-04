---
title: Provision an MCP Sandbox per Scenario
description: Stand up an MCP server (e.g. a containerised codegen sandbox) on demand at run, scenario, or session scope.
sidebar:
  order: 13
---

Some MCP servers can't run as a singleton — codegen sandboxes, browser
automation, ephemeral DB fixtures, or any tool that needs a fresh
workspace per test. Arena provisions these via **MCPSources**: named
factories that open an MCP endpoint at a chosen lifecycle boundary and
tear it down when the boundary closes.

This is the third MCP transport, alongside stdio (`command`) and static
HTTP+SSE (`url`). For long-lived shared servers, see
[Test MCP Tools](/arena/how-to/test-mcp-tools/).

---

## When to use this

Use a source-backed entry when **any** of the following hold:

- The server requires per-test isolation (filesystems, side effects).
- The server is provisioned by infrastructure you control (containers,
  reserved hosts) rather than spawned in-process.
- The server's URL or auth varies per scenario — e.g. a different repo
  or branch per test case.

If a static `url:` or stdio `command:` is enough, prefer that — it has
fewer moving parts.

---

## Anatomy of a source-backed entry

```yaml
spec:
  mcp_servers:
    - name: sandbox            # registry key — used in qualified tool names
      source: docker           # name of a registered MCPSource
      scope: session           # when to open and close
      source_args:             # opaque, source-specific
        image: ghcr.io/altairalabs/codegen-sandbox:latest
        repo: https://github.com/example/some-project
        branch: main
        env:
          DEV_MODE: "1"
```

Three fields drive the lifecycle:

| Field         | Purpose                                                              |
| ------------- | -------------------------------------------------------------------- |
| `source`      | Name of an `MCPSource` registered in the running binary (e.g. `docker`). |
| `scope`       | When to open and close. One of `run`, `scenario`, `session`.         |
| `source_args` | Free-form map handed to the source's `Open()`. Schema is per-source. |

`source` and `url`/`command` are mutually exclusive — pick one transport
per entry.

---

## Scopes

| Scope      | Opens                              | Closes                                | Use for                                                          |
| ---------- | ---------------------------------- | ------------------------------------- | ---------------------------------------------------------------- |
| `run`      | Once at arena startup              | At arena shutdown                     | Heavy infra shared across all tests (a warm DB, a model server). |
| `scenario` | Each scenario start                | Each scenario end                     | Per-test fixtures that survive multiple repetitions.             |
| `session`  | Each repetition (each `executeRun`) | After that repetition's assertions    | Codegen sandboxes, anything that must be fresh per agent run.    |

Inner scopes always close before outer scopes. Closer errors are logged
as warnings; they never fail the parent scope.

If `Open()` fails partway through a scope's entries, the
already-opened entries in that scope are torn down before the error
propagates — so the host doesn't leak containers on a partial failure.

---

## Templating from scenario variables

`source_args` is templated against each scenario's `variables` block
before the source sees it. `{{scenario.<key>}}` substitution is the only
form supported (no fallbacks, no expressions).

```yaml
# scenario
variables:
  repo: https://github.com/example/foo
  branch: feature-x

# arena config
mcp_servers:
  - name: sandbox
    source: docker
    scope: session
    source_args:
      image: ghcr.io/altairalabs/codegen-sandbox:latest
      repo: "{{scenario.repo}}"
      branch: "{{scenario.branch}}"
```

Each session opens a fresh container with that scenario's repo cloned
into `/workspace`.

---

## How tools become callable

When the source's `Open()` returns, Arena:

1. Registers the resulting URL + headers in the runtime MCP registry
   under the `name:` you gave.
2. Calls `tools/list` against that server and registers each discovered
   tool as a `ToolDescriptor` in the tools registry under its **raw**
   MCP name (`Read`, `Edit`, …) — not the namespaced
   `mcp__server__tool` form used by static MCP entries. This keeps
   pack-author ergonomics simple: the sandbox is "just another set of
   tools".
3. Routing is unchanged — `MCPExecutor` looks up the owning server via
   the registry's tool index, regardless of namespace.

Reference these tools in your prompt config's `allowed_tools`:

```yaml
allowed_tools:
  - Read
  - Edit
  - Bash
  - run_tests
```

If two source-backed servers expose tools with the same name, the
second registration wins and overwrites the first. For sandboxes this
is rarely an issue (one sandbox per pack); use stdio/url MCP entries
when you need namespaced isolation.

---

## Reference: the `docker` source

PromptArena ships with a `docker` source registered automatically. It
shells out to the local `docker` CLI to run, exec, and stop the
container.

| Arg                | Type                | Required | Default     | Notes                                                                                                                  |
| ------------------ | ------------------- | -------- | ----------- | ---------------------------------------------------------------------------------------------------------------------- |
| `image`            | string              | yes      | —           | Image reference. The container must expose an MCP HTTP+SSE server on port 8080.                                        |
| `repo`             | string              | no       | —           | If set, after the container starts the source runs `docker exec <cid> git clone [--branch <branch>] <repo> /workspace`. |
| `branch`           | string              | no       | repo default | Branch to clone. Ignored when `repo` is empty.                                                                         |
| `env`              | map\<string,string> | no       | —           | Environment variables passed via `-e`.                                                                                 |
| `mounts`           | list of objects     | no       | —           | Bind mounts. Each entry takes `source` (host path), `target` (container path), `readonly` (bool).                       |

The source picks a free local port, publishes the container's `8080`
to it, polls `<url>/sse` until the server is ready (20s budget), then
returns `MCPConn{URL: "http://localhost:<port>"}`. On `Close()`, the
container is stopped and removed.

Cloning a private repo requires either credentials baked into the
image or a host-side wrapper around the source — the built-in source
runs `git clone` unauthenticated.

---

## Worked example

The repo includes a runnable end-to-end demo at
[`examples/codegen-sandbox/`](https://github.com/AltairaLabs/PromptKit/tree/main/examples/codegen-sandbox)
that:

- Provisions `ghcr.io/altairalabs/codegen-sandbox:latest` per session.
- Mounts the local `skills/codegen/` directory read-only at
  `/skills/codegen` inside the container.
- Runs a mock-LLM scenario that seeds a buggy Go module via `Bash`,
  reads + edits the file, and verifies via `run_tests`.

Run it:

```bash
make build-arena
make codegen-demo
open examples/codegen-sandbox/out/report.html
```

For a no-Docker variant against the canned LLM responses, use
`make codegen-demo-mock`.

---

## Hard gating on a sandbox tool

Once the sandbox is wired, the natural pairing is the [`tool_exec`
check](/reference/checks/#tool-invocation-checks) — it invokes a
registered tool at the end of the session and asserts the call
succeeded. Codegen sandboxes typically expose `run_tests` /
`run_lint` / `run_typecheck`, all of which return structured
success/failure that `tool_exec` reads directly. The result is a
hard "did the agent's edits actually pass tests" gate on the run:

```yaml
spec:
  mcp_servers:
    - name: sandbox
      source: docker
      scope: session
      source_args:
        image: ghcr.io/altairalabs/codegen-sandbox:latest

  scenarios:
    - file: scenarios/fix-the-bug.scenario.yaml
```

```yaml
# scenarios/fix-the-bug.scenario.yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: fix-the-bug
spec:
  id: fix-the-bug
  task_type: codegen-agent
  turns:
    - role: user
      content: "There's a bug in /workspace/add.go. Fix it."
  conversation_assertions:
    - type: tool_exec
      params:
        tool: run_tests
      message: "Hidden test suite must pass"
```

The session-scoped MCP source keeps the container alive across both
the agent's tool calls and the `tool_exec` gate's call — the gate
just runs `run_tests` one more time after the agent declares done,
and the test result drives the hard gate.

---

## Skill staging

When a pack declares skills and the source supports it, Arena
automatically populates `source_args.mounts` with one entry per skill
directory:

| Field      | Value                                              |
| ---------- | -------------------------------------------------- |
| `source`   | Absolute host path to the skill directory.         |
| `target`   | `/skills/<skill-name>` inside the container.       |
| `readonly` | `true`.                                            |

The docker source translates each entry into a `-v <src>:<tgt>:ro` flag
on `docker run`, so any scripts shipped with the skill are runnable
inside the sandbox via `Bash /skills/<name>/scripts/<script>`.

You don't need to write the `mounts` block by hand for this case —
declare the skill in the pack and Arena does the rest.

---

## Failure modes

| Symptom                                               | Likely cause                                                                                  |
| ----------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `unknown source "X"` at config load                   | The named source isn't registered in the running binary. The error lists the registered names. |
| `container not healthy: health timeout after 20s`     | The image isn't serving HTTP+SSE on port 8080, or `/sse` returns non-2xx.                      |
| `tool <name> validation error (args_invalid)`         | Tool call args don't match the MCP server's input schema. Check the server's tool definitions. |
| `tool not found: <name>` after a successful Open      | Either `tools/list` returned nothing for that server, or two source-backed servers collided.   |

---

## See also

- [Test MCP Tools](/arena/how-to/test-mcp-tools/) — static stdio / url MCP servers.
- [Configuration Schema](/arena/reference/config-schema/#mcp_servers) — full field reference.
- [Write Scenarios](/arena/how-to/write-scenarios/) — `variables:` block used for templating.
- [Integrate MCP (Runtime)](/runtime/how-to/integrate-mcp/) — low-level MCP registry API.
