---
title: CI/CD quick reference
description: Commands for running PromptKit's CI checks locally, watching workflow runs and triggering workflows.
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/docs-checks.yml
    - .github/workflows/docs-freshness.yml
    - .github/workflows/docs.yml
    - .github/workflows/mcp-conformance.yml
    - .github/workflows/promptpack-schema-sync.yml
    - .github/workflows/release.yml
    - .github/workflows/schemas.yml
    - .golangci.yml
    - Makefile
---
Commands for the CI/CD tasks you repeat most. The [pipelines reference](/devops/ci-cd-pipelines/) describes each workflow.

## Run the checks locally

There is no root Go module, so run the Makefile targets. They iterate `runtime`, `sdk`, `pkg` and `server/a2a`.

```bash
make verify        # lint-diff + test-fast on changed modules
make test          # all tests
make test-race     # all tests with the race detector
make build         # build every module
make coverage      # coverage profiles, merged into coverage.out
make lint          # go vet, go fmt, golangci-lint, gosec, message-pattern check
```

`make lint` runs `go fmt`, which rewrites files. Use `make lint-diff` to lint only changed code. Both require the `golangci-lint` version CI pins; `scripts/check-lint-version.sh` reports a mismatch.

For one package, use `go -C`:

```bash
go -C runtime test ./evals/... -count=1
go -C sdk test -race ./...
```

`make build` and `make test` skip the nested example modules under `sdk/examples` and `server/a2a/examples`. CI vets and tests them, so run `go -C <example-dir> vet ./...` for any example you changed.

## Reproduce a specific CI failure

| Failing job or step | Local command |
|---|---|
| `Test & Coverage` | `go -C runtime test -race ./...`, and the same for `sdk`, `pkg` and `server/a2a` |
| `SDK Integration Tests` | `go -C sdk test -race -count=1 ./integration/...` |
| `Lint` | `golangci-lint run ./...` inside the failing module directory, with the pinned version |
| `Build` | `make build`, then the guard scripts under `scripts/` that the job names |
| `Weak Assertion Guard` | `make check-weak-assertions` |
| `Reference Drift Check` | `make docs-reference docs-sdk-reference`, then commit the regenerated pages |
| `check-packspec` | `make packspec-check module-dep-skew-check modules-standalone-check spec-version-check` |
| `check-promptpack-schema` | `make promptpack-schema-check` |
| `Docs Checks` | `make docs-voice` and `make docs-scripts-test` |
| Release `validate` | `make api-compat-check VERSION=vX.Y.Z` |

## Watch runs

```bash
gh run list --workflow=ci.yml --limit 10   # recent CI runs
gh run watch <run-id> --exit-status        # follow one run
gh run view <run-id> --log-failed          # logs of failed steps only
gh run list --status failure               # failures across workflows
gh run download <run-id>                   # artifacts such as coverage-report
gh pr checks                               # checks on the current pull request
```

## Trigger workflows

Workflows with `workflow_dispatch`: `docs.yml`, `docs-checks.yml`, `docs-freshness.yml`, `schemas.yml`, `promptpack-schema-sync.yml`, `mcp-conformance.yml` and `release.yml`.

```bash
# Publish the documentation without a release
gh workflow run docs.yml -f reason="refresh after docs fix"

# Release the library modules
gh workflow run release.yml -f version=v2.16.2 -f phase=full
```

`release.yml` also accepts `phase=libs-only`, `phase=tools-only`, `skip_tests=true` and `ignore_previous_release_check=true`. See [Releases](/devops/ci-cd-pipelines/#releases-releaseyml).

## SonarCloud

The dashboard is at <https://sonarcloud.io/project/overview?id=AltairaLabs_PromptKit>. To scan locally:

```bash
export SONAR_TOKEN=<your-token>
make sonar-quick   # make coverage, then make sonar-scan
```

## Documentation

```bash
make docs-install   # first time: Astro dependencies and gomarkdoc
make docs-serve     # development server
make docs-build     # full build into docs/dist, as CI does
make docs-voice     # prose rules from docs/STYLE.md
```

## Broken main

Pull requests are the only way to change `main`, so fix a broken `main` with a pull request:

1. Find the breaking commit with `git log` and the failing run.
2. Open a pull request that reverts it (`git revert <sha>`) or fixes it.
3. Merge once the required checks pass.

## Linter false positives

Suppress a finding on the line that causes it, name the rule and give the reason:

```go
//nolint:staticcheck // SA1019: the deprecated call is the only way to reach the old API
```

## Configuration files

| File | Purpose |
|---|---|
| `.github/workflows/` | The eleven workflows |
| `.github/CODEOWNERS` | Review ownership |
| `.golangci.yml` | Linter configuration |
| `sonar-project.properties` | SonarCloud sources, exclusions and coverage paths |
| `Makefile` | Local build, test and docs targets |

## Related

- [CI/CD pipelines](/devops/ci-cd-pipelines/)
- [CI/CD diagrams](/devops/ci-cd-diagrams/)
- [Branch protection quick reference](/devops/branch-protection-quickref/)
- [GitHub Actions documentation](https://docs.github.com/en/actions)
