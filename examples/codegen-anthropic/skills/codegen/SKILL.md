---
name: codegen
description: Disciplined code-generation workflow for agents with a sandbox (Read/Edit/Bash/run_tests/run_lint/run_typecheck). Use whenever the task is to modify source code.
metadata:
  tags: "code, editing, testing"
---

# Codegen Skill

You are writing code inside a container sandbox. Your tools execute real commands against a real filesystem; mistakes are cheap to undo but waste turns.

When a sandbox is mounted, your reference skill files are available read-only at `/skills/codegen/` inside the sandbox. Scripts can be executed via `Bash`; reference files can also be read via `skill__read_resource` (host-side).

## Discipline

**1. Read before Edit.**
Never call `Edit` on a file you have not `Read` this session. If you discover the file doesn't look like you expected, Read it again before editing.

**2. Plan multi-step work with TodoWrite.**
For any task with 3+ logical steps (e.g., a bugfix that needs test, fix, and verification), write a todo list first. Mark each item as you finish it. This keeps you on track when tool responses push context.

**3. Verify before declaring done.**
Before saying "done", run the appropriate verification tool:
- `run_tests` — the project's test suite must pass.
- `run_lint` — no new lint errors on files you touched.
- `run_typecheck` — no new type errors.

For a bugfix, write a failing test *first*, then the fix, then confirm the test now passes. Do not skip the failing-first step — a test that was always green doesn't verify your fix.

**4. Small, focused diffs.**
Change only what the task requires. Unrelated "while I was here" edits make review harder and tests flakier.

## Language notes

**Go:** `gofmt`/`goimports` are enforced. Imports grouped: stdlib, then third-party, then local. Check with `run_lint` (uses golangci-lint).

**Node:** Detect the package manager from the lockfile (`package-lock.json` → npm, `yarn.lock` → yarn, `pnpm-lock.yaml` → pnpm). Use the project's lockfile convention for install/add.

**Python:** If the repo has a virtualenv (`.venv/`) or manages deps via Poetry (`pyproject.toml` with `[tool.poetry]`), activate/use it. Never `pip install` into the system Python.

**Rust:** `cargo fmt` and `cargo clippy` before saying done; these map to `run_lint`.

## Failure mode recovery

- Tests fail after your edit → Read the test output, find the first failing assertion, understand the gap between expected and actual, make one focused fix, retest. Don't edit the test to make it pass unless the test itself was wrong.
- Build fails (compile / type error) → Fix the compile error first before running tests. A failing build invalidates everything.
- `Bash` returns non-zero unexpectedly → Read the full stderr before retrying. Infra errors (disk full, permission denied) need different treatment from code errors.

## Not your job

- Setting up the repo (it's already cloned at `/workspace`).
- Installing new runtimes (the sandbox image ships pinned toolchains).
- Remembering what you did in a previous session (you don't have one).
