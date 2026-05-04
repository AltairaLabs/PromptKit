---
name: codegen-disciplined
description: Disciplined code-generation workflow for agents with a sandbox (Read/Edit/Bash/run_tests/run_lint/run_typecheck). Use whenever the task is to modify source code.
metadata:
  tags: "code, editing, testing"
---

# Disciplined Codegen

You are writing code inside a container sandbox. Tools execute real commands against a real filesystem; mistakes are cheap to undo but waste turns.

This skill is the variable under test in the codegen-eval spike. The baseline bundle does NOT load this skill; this bundle does. The hypothesis: explicit discipline raises pass-rate enough to justify the extra tool calls.

## Discipline

**1. Read before Edit.**
Never call `Edit` on a file you have not `Read` this session. If you discover the file doesn't look like you expected, Read it again before editing.

**2. Plan multi-step work.**
For any task with 3+ logical steps (e.g., a bugfix that needs seed, edit, verify), enumerate the steps before starting. Mark each as you finish it.

**3. Verify before declaring done.**
Before saying "done", run the verification tool:
- `run_tests` — the project's test suite must pass.
- `run_lint` — no new lint errors on files you touched.
- `run_typecheck` — no new type errors.

For a bugfix, the workflow is: seed → run_tests (red) → fix → run_tests (green). The failing-first run proves the test is actually exercising the bug.

**4. Small, focused diffs.**
Change only what the task requires. Unrelated edits make review harder and tests flakier.

## Failure mode recovery

- Tests fail after your edit → Read the test output, find the first failing assertion, understand the gap between expected and actual, make one focused fix, retest. Don't edit the test to make it pass unless the test itself was wrong.
- Build fails (compile / type error) → Fix the compile error first before running tests. A failing build invalidates everything.
- `Bash` returns non-zero unexpectedly → Read the full stderr before retrying.

## Go specifics

`gofmt`/`goimports` are enforced. Imports grouped: stdlib, then third-party, then local. Check with `run_lint` (uses golangci-lint).
