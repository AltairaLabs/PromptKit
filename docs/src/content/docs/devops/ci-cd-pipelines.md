---
title: CI/CD pipelines
description: Every GitHub Actions workflow in the PromptKit repository, what triggers it and what it checks.
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/cla.yml
    - .github/workflows/dependabot-gate.yml
    - .github/workflows/deprecation-gate.yml
    - .github/workflows/docs-checks.yml
    - .github/workflows/docs-freshness.yml
    - .github/workflows/docs.yml
    - .github/workflows/mcp-conformance.yml
    - .github/workflows/promptpack-schema-sync.yml
    - .github/workflows/reference-drift.yml
    - .github/workflows/release.yml
    - .github/workflows/schemas.yml
    - Makefile
    - scripts/check-new-deprecations.sh
    - scripts/check-no-audio-device-leak.sh
    - scripts/check-weak-assertions.sh
    - scripts/perf-compare.sh
    - scripts/perf-interleave.sh
    - scripts/perf-run.sh
---
PromptKit ships four Go modules: `runtime`, `pkg`, `sdk` and `server/a2a`. Twelve workflows in `.github/workflows/` test them, check generated artifacts, deploy the documentation and release the modules. The CLIs and their binaries release from the `promptarena` repository.

## Workflows

| File | Triggers | Jobs |
|---|---|---|
| `ci.yml` | Push to `main` and pull requests to `main`, except changes that touch only the ignored paths below | `test`, `sonarcloud`, `integration-tests`, `lint`, `build`, `weak-assertions`, `perf-regression` |
| `dependabot-gate.yml` | Pull requests to `main` | `sonarcloud` |
| `cla.yml` | Pull request opened, closed or synchronized; issue comments | `CLAAssistant` |
| `deprecation-gate.yml` | Pull requests to `main` | `deprecations` |
| `docs.yml` | Release published, manual dispatch, pull requests touching `docs/`, example READMEs, the docs preparation scripts, the `Makefile` or `docs.yml` | `build`, `deploy` |
| `docs-checks.yml` | Pull requests to `main`, manual dispatch | `docs-checks` |
| `docs-freshness.yml` | Mondays 07:00 UTC, manual dispatch | `report` |
| `reference-drift.yml` | Push to `main` and pull requests touching Go sources or generated reference pages | `reference-drift` |
| `schemas.yml` | Daily 06:00 UTC, manual dispatch, push and pull requests touching the PromptPack schema or any `go.mod` | `check-promptpack-schema`, `check-packspec` |
| `promptpack-schema-sync.yml` | Daily 06:00 UTC, manual dispatch, `promptpack-release` repository dispatch | `sync` |
| `mcp-conformance.yml` | Push and pull requests touching the MCP client, manual dispatch | `spec-parity`, `mirror`, `official-suite` |
| `release.yml` | Manual dispatch | `validate`, `tag-dependencies`, `update-tools`, `create-release`, `verify-docs`, `notify-downstream`, `summary` |

`ci.yml` ignores `docs/**`, `*.md`, `LICENSE`, `.gitignore`, `.github/workflows/docs.yml`, `.github/workflows/release.yml` and `.github/CODEOWNERS`.

## CI (`ci.yml`)

All jobs run on `ubuntu-latest` with Go 1.26.0.

### Test and coverage

The `test` job (reported as `Test & Coverage (1.26.0)`) runs these steps:

1. Install the ALSA development headers, then `make install`.
2. Run `gotestsum` with `-race` and a coverage profile over `./runtime/...`, `./sdk/...`, `./pkg/...`, `./server/a2a/...` and `./sdk/integration/...`.
3. Merge the profiles into `coverage.out` and rewrite module paths to repository-relative paths for SonarCloud.
4. Run `go vet` in every nested example module under `sdk/examples` and `server/a2a/examples`, then `go test` in those with tests. A module whose packages sit behind build tags is reported as skipped.
5. Publish the JUnit results as `Test Results` and upload the `coverage-report` artifact (5 days).

### Integration tests

`integration-tests` (reported as `SDK Integration Tests`) runs `./sdk/integration/...` with `-race -count=1` and publishes the results.

### Lint

`lint` runs `golangci-lint` v2.14.0 once per module (`./runtime`, `./sdk`, `./pkg`, `./server/a2a`) with the repository's `.golangci.yml`. Any issue in a module fails the job. It also writes a checkstyle report and uploads it as `golangci-lint-<module>` for SonarCloud. Dependabot pull requests skip it.

### SonarCloud

`sonarcloud` (reported as `SonarCloud Analysis`) needs `test` and `lint`. It downloads the coverage and lint artifacts and runs the SonarCloud scan with `SONAR_TOKEN`. Dependabot pull requests skip it. Settings live in `sonar-project.properties`.

### Build

`build` runs `make build`, then four guards:

| Script | Checks |
|---|---|
| `scripts/check-no-audio-device-leak.sh` | `runtime/providers/base`, `pkg/config` and `sdk` do not import the `purego` audio device binding |
| `scripts/test-verify-published-gomod.sh` | Fixture tests for the published `go.mod` release guard |
| `scripts/test-api-compat-allowlist.sh` | Fixture tests for the API-compatibility allowlist |
| `scripts/check-runtime-is-leaf.sh` | `runtime` requires no sibling module |

### Weak assertion guard

`weak-assertions` runs on pull requests only. It runs `scripts/check-weak-assertions.sh` against the pull request's base commit and fails on new tests whose assertions cannot fail. Tests that already existed at the base do not count.

### Performance regression check

`perf-regression` benchmarks a mock-upstream chat server and skips Dependabot pull requests. It builds the benchmark server at the head commit and at the base commit on the same runner. For a pull request the base is the pull request's base commit; for a push it is the commit before the push.

`scripts/perf-interleave.sh` alternates base and head runs for 2 rounds, each with 500 concurrent clients and 5000 requests. It fails when:

- the median head throughput is below 85% of the median base throughput;
- the median head throughput is below 600 requests per second.

If the base revision cannot be built, the check falls back to the absolute floor in the table below. `scripts/perf-compare.sh` does the comparison.

The job then applies absolute ceilings to the worst head run:

| Run | Fallback floor (requests per second) | Memory ceiling | CPU ceiling |
|---|---|---|---|
| Plain | 1300 | 512 MB RSS | 150% |
| With an event store subscribed | 1100 | 512 MB RSS | 175% |

The event-store run also fails when any started pipeline lacks a completed or failed event, when fewer than 90% of the requests start a pipeline, or when 2000 or more goroutines remain after the run.

## Dependabot gate (`dependabot-gate.yml`)

The job is named `SonarCloud Analysis` to match the required status check. It runs only when the pull request branch starts with `dependabot/` and does nothing but report success, because `ci.yml` skips SonarCloud for Dependabot.

## CLA (`cla.yml`)

`CLAAssistant` records contributor license agreement signatures in the `AltairaLabs/cla-signatures` repository. A contributor signs by commenting on the pull request; a `recheck` comment reruns the check. Bot accounts are allowlisted. The workflow uses `PERSONAL_ACCESS_TOKEN`.

## Deprecation gate (`deprecation-gate.yml`)

The `deprecations` job (reported as `New deprecations need approval`) runs `scripts/check-new-deprecations.sh` against the pull request's base branch. It fails when the diff adds a Go `// Deprecated:` marker. A maintainer applies the `deprecation-approved` label to record the decision for that pull request, which skips the job.

## Documentation

### Deploy (`docs.yml`)

The `build` job:

1. Sets up Node 22, Go 1.26.0, `gomarkdoc` and D2.
2. Installs the docs dependencies with `make docs-install`, which runs `npm ci` and installs exactly what the committed lockfile pins. A `NODE_AUTH_TOKEN` secret reads a private GitHub Packages dependency.
3. Checks the Mermaid diagrams (`npm run check:mermaid`).
4. Builds the site with `make docs-build`.
5. Checks links (`check-links`) and hardcoded documentation URLs (`check-doc-urls`).
6. Uploads `docs/dist` as the Pages artifact.

The `deploy` job publishes the artifact to GitHub Pages and runs for every trigger except pull requests. A pull request runs `build` only, so a docs change is checked before merge and published at the next release or manual dispatch. The `pages` concurrency group never cancels a running deployment.

The site is served at `https://promptkit.altairalabs.ai`. To publish without a release, run `gh workflow run docs.yml -f reason="..."`.

### Checks (`docs-checks.yml`)

`docs-checks` runs on every pull request. It tests the docs scripts, runs `scripts/check-docs-voice.sh` (which fails only for directories listed in `scripts/docs-voice-enforced.txt`) and lists in the job summary the pages that cite code the pull request changes. The staleness list is a warning.

### Freshness (`docs-freshness.yml`)

`report` runs weekly and creates or updates one open issue labeled `docs-freshness`. The issue lists stale pages and public surface that no page cites. Run `/docs-review --stale` to fix what it lists.

## Generated artifacts

### Reference drift (`reference-drift.yml`)

`Reference Drift Check` runs `make docs-reference-check` and `make docs-sdk-reference-check`. It runs on changes to the Go sources and to the generated pages, so a hand edit of a generated page fails even when `ci.yml` is skipped. Regenerate with `make docs-reference` and `make docs-sdk-reference`.

### Schemas (`schemas.yml`)

| Job | Runs on | Command |
|---|---|---|
| `check-promptpack-schema` | Push and pull requests only | `make promptpack-schema-check`: the embedded schema equals the published PromptPack release |
| `check-packspec` | Every trigger | `make packspec-check`, `make module-dep-skew-check`, `make modules-standalone-check`, `make spec-version-check` |

### PromptPack schema sync (`promptpack-schema-sync.yml`)

`sync` fetches the published PromptPack schema. When it differs from `runtime/prompt/schema/promptpack.schema.json`, the job regenerates the pack types, restates the spec version, runs the spec-parity tests and opens a `chore/promptpack-schema-v<version>` pull request. It never merges the pull request. The PR body says whether the parity guards passed.

### MCP conformance (`mcp-conformance.yml`)

| Job | Checks |
|---|---|
| `spec-parity` | `TestMCPSpec*` in `runtime/mcp`: wire types and the "Spec support" section of the MCP how-to match the claimed protocol revision |
| `mirror` | `make mcp-schema-check`: the mirrored MCP schemas equal the published ones |
| `official-suite` | `make mcp-conformance`: the MCP client against `@modelcontextprotocol/conformance` |

## Releases (`release.yml`)

The workflow is dispatched by hand with these inputs:

| Input | Values |
|---|---|
| `version` | A semantic version such as `v2.16.2`; a missing leading `v` is added |
| `phase` | `full` (default), `libs-only` or `tools-only` |
| `skip_tests` | Skip `make test` and `make test-race` in `validate` |
| `ignore_previous_release_check` | Release even if the previous release's published `go.mod` files are bad |

```bash
gh workflow run release.yml -f version=v2.16.2 -f phase=full
```

| Job | Phases | What it does |
|---|---|---|
| `validate` | all | Checks the version format, that it is newer than the latest `runtime/v*` tag (not for `tools-only`), and that the previous release's published `go.mod` files are clean. Runs the tests unless skipped. Runs `module-dep-skew-check`, `modules-standalone-check` and `scripts/check-api-compatibility.sh`, which fails when the API changes do not fit the version |
| `tag-dependencies` (Tag Libraries) | `full`, `libs-only` | Checks `runtime` is a leaf module. Tags the root `vX.Y.Z` and `runtime/vX.Y.Z` from `main`. On a `release/libs/vX.Y.Z` branch, sets `pkg`'s `runtime` requirement to the new version, drops its `replace` and tags `pkg/vX.Y.Z`. Verifies the published `go.mod` files through the Go proxy |
| `update-tools` (Update and Tag SDK Modules) | `full`, `tools-only` | On a `release/tools/vX.Y.Z` branch, sets the `server/a2a` and `sdk` requirements to the new version, drops their `replace` directives and tags `server/a2a/vX.Y.Z` and `sdk/vX.Y.Z`. Verifies the published `go.mod` files |
| `create-release` | `full` | Creates a draft GitHub release with generated notes, attaches an SPDX SBOM (`promptkit-vX.Y.Z.spdx.json`) and attests its provenance |
| `verify-docs` | `full` | Publishes the draft release with `HOMEBREW_TAP_TOKEN`, which triggers `docs.yml`, then waits for that run and fails if it fails |
| `notify-downstream` | `full` | Waits until the four modules resolve through `proxy.golang.org` and `sum.golang.org`, then sends a `promptkit-release` dispatch to the `promptarena-deploy-agentcore`, `PromptArena-deploy-omnia`, `promptarena-deploy-vertex` and `promptarena-deploy-foundry` repositories using `DOWNSTREAM_DISPATCH_TOKEN` |
| `summary` | all | Writes the job results and the `go get` commands to the run summary |

A published tag cannot be repaired. If a tag carries a bad `go.mod`, release again with a higher version and `ignore_previous_release_check=true`. To finish tagging `sdk` and `server/a2a` after `runtime` and `pkg` were tagged, run `phase=tools-only` with the same version.

## Secrets

| Secret | Used by |
|---|---|
| `GITHUB_TOKEN` | Provided by Actions to every workflow |
| `SONAR_TOKEN` | `ci.yml` SonarCloud scan |
| `NODE_AUTH_TOKEN` | `docs.yml` private package install |
| `PERSONAL_ACCESS_TOKEN` | `cla.yml`, `promptpack-schema-sync.yml` |
| `HOMEBREW_TAP_TOKEN` | `release.yml` publishing the release, so that the published event starts `docs.yml` |
| `DOWNSTREAM_DISPATCH_TOKEN` | `release.yml` dispatches to the adapter repositories |

## Debugging a failed run

1. Open the run in the Actions tab and expand the failing step.
2. Reproduce with the step's own command. The `test` job runs with `-race`; the integration job also passes `-count=1`.
3. For lint failures, install `golangci-lint` v2.14.0; `make lint` checks the version.
4. For `Reference Drift Check`, run `make docs-reference docs-sdk-reference` and commit the result.

## Related

- [CI/CD quick reference](/devops/ci-cd-quickref/)
- [CI/CD diagrams](/devops/ci-cd-diagrams/)
- [Branch protection](/devops/branch-protection/)
- [Contributing](/contributors/contributing/)
