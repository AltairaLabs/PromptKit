---
title: Pre-Commit Hooks
sidebar:
  order: 4
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/reference-drift.yml
    - Makefile
    - scripts/commit-msg.sh
    - scripts/install-hooks.sh
    - scripts/pre-commit.sh
---

PromptKit ships two git hooks. `pre-commit` checks the Go code you changed, and `commit-msg` enforces the DCO sign-off. Both run on every commit and look only at what the commit touches.

## Setup

Install the hooks once after cloning:

```bash
./scripts/install-hooks.sh
```

The script copies `scripts/pre-commit.sh` and `scripts/commit-msg.sh` into `.git/hooks/` and reports which tools are missing. The installed copies are not tracked, so rerun the script after the sources change.

Install golangci-lint at the version pinned in `.github/workflows/ci.yml`. `scripts/check-lint-version.sh` fails the lint step on any other version, and prints the `go install` command for the pinned one. `gosec` is optional.

## What the pre-commit hook checks

The hook exits immediately when no Go files are staged. Otherwise it resolves each staged file to its nearest `go.mod` and runs lint, build and tests per changed module, and the other steps once:

| Step | What it does |
|---|---|
| Lint | `golangci-lint run --new-from-rev=HEAD`, after the version check |
| Build | `go build -o /dev/null ./...` |
| Reference drift | `make docs-reference-check` and `make docs-sdk-reference-check`, only when staged source feeds the generated API reference |
| Weak assertions | `scripts/check-weak-assertions.sh HEAD`, only when `_test.go` files are staged |
| Tests and coverage | `go test` with a coverage profile, then at least 80% statement coverage per staged non-test Go file |

The coverage check skips test files, files under `examples/` and `tests/`, `benchmarks/`, `tools/`, and a short allowlist of live-I/O files (`COVERAGE_EXEMPT_FILES` in `scripts/pre-commit.sh`, mirrored in `sonar.coverage.exclusions`). A file name ending in `_integration.go` or `_interactive.go` does not exempt a file. Extract the pure logic into a covered file instead.

If the reference drift step fails, regenerate and stage the pages:

```bash
make docs-reference && make docs-sdk-reference
git add docs/src/content/docs
```

## What the commit-msg hook checks

The hook requires a `Signed-off-by: Name <email>` trailer whose email matches the commit author. Merge commits are skipped. Commit with `git commit -s`, or fix the last commit with `git commit --amend -s --no-edit`.

## Run the checks manually

```bash
make verify        # lint-diff + test-fast on changed modules; not the whole hook
make lint-diff     # golangci-lint on changed code, then gosec if installed
make test-fast     # tests for modules with changes
```

`make verify` compares against `HEAD`, so it reports nothing on a clean tree. It skips the hook's build, coverage, weak-assertion and reference-drift steps.

## CI

CI runs the equivalent checks on every pull request that touches files outside its ignored paths (`.github/workflows/ci.yml`): `test`, `lint`, `build`, `weak-assertions`, `integration-tests`, `perf-regression` and a SonarCloud analysis. The reference drift check runs in `reference-drift.yml`. SonarCloud enforces 80% coverage on new and changed lines.

Passing the hook does not skip CI, and a failure in the hook will also fail in CI. Fix the findings, including ones in pre-existing code in files you changed.

## Troubleshooting

### The hook does not run

The installed hook is missing or not executable. Rerun `./scripts/install-hooks.sh`.

### golangci-lint version mismatch

Install the pinned version with the command `scripts/check-lint-version.sh` prints.

### Coverage check fails

The hook lists each file with its coverage. Add tests for the uncovered statements, then commit again. Run `make coverage` for a full report.

## Configuration

| To change | Edit |
|---|---|
| Linter set | `.golangci.yml` |
| Per-file coverage threshold | `COVERAGE_THRESHOLD` in `scripts/pre-commit.sh`, plus the SonarCloud quality gate |
| Coverage exemptions | `COVERAGE_EXEMPT_FILES` in `scripts/pre-commit.sh` and `sonar.coverage.exclusions` in `sonar-project.properties` |
| Pinned linter version | the `golangci-lint-action` step in `.github/workflows/ci.yml` |
