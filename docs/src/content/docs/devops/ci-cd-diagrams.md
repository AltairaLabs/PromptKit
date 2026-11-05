---
title: CI/CD diagrams
description: Mermaid diagrams of PromptKit's GitHub Actions workflows, job dependencies, required checks and release flow.
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/cla.yml
    - .github/workflows/dependabot-gate.yml
    - .github/workflows/docs-checks.yml
    - .github/workflows/docs-freshness.yml
    - .github/workflows/docs.yml
    - .github/workflows/mcp-conformance.yml
    - .github/workflows/promptpack-schema-sync.yml
    - .github/workflows/reference-drift.yml
    - .github/workflows/release.yml
    - .github/workflows/schemas.yml
---
Diagrams of how the workflows in `.github/workflows/` relate. The [pipelines reference](/devops/ci-cd-pipelines/) has the details behind each node.

## Events and workflows

This diagram shows which event starts which workflow.

```mermaid
flowchart LR
    PR[Pull request to main]
    PUSH[Push to main]
    REL[Release published]
    CRON[Schedule]
    DISP[Manual dispatch]

    PR --> ci[ci.yml]
    PR --> gate[dependabot-gate.yml]
    PR --> cla[cla.yml]
    PR --> dchecks[docs-checks.yml]
    PR --> docs[docs.yml: build only]
    PR --> drift[reference-drift.yml]
    PR --> schemas[schemas.yml]
    PR --> mcp[mcp-conformance.yml]

    PUSH --> ci
    PUSH --> drift
    PUSH --> schemas
    PUSH --> mcp

    REL --> docsdeploy[docs.yml: build and deploy]

    CRON --> schemas
    CRON --> sync[promptpack-schema-sync.yml]
    CRON --> fresh[docs-freshness.yml]

    DISP --> release[release.yml]
    DISP --> docsdeploy
    DISP --> dchecks
    DISP --> fresh
    DISP --> schemas
    DISP --> sync
    DISP --> mcp
```

`ci.yml`, `docs.yml`, `reference-drift.yml`, `schemas.yml` and `mcp-conformance.yml` filter by changed paths. `cla.yml` also reacts to issue comments, and `promptpack-schema-sync.yml` also reacts to a `promptpack-release` repository dispatch.

## CI job dependencies

This diagram shows how the jobs in `ci.yml` depend on each other. Only `sonarcloud` waits for other jobs.

```mermaid
flowchart TD
    test["test: Test & Coverage"]
    lint["lint: Lint (4 modules)"]
    sonar["sonarcloud: SonarCloud Analysis"]
    integ["integration-tests: SDK Integration Tests"]
    build["build: Build"]
    weak["weak-assertions: Weak Assertion Guard"]
    perf["perf-regression: Performance Regression Check"]

    test --> sonar
    lint --> sonar
```

`integration-tests`, `build`, `weak-assertions` and `perf-regression` have no dependencies. `weak-assertions` runs on pull requests only. `lint`, `sonarcloud` and `perf-regression` skip Dependabot pull requests.

## Coverage and lint data flow

This diagram shows how test coverage and lint results reach SonarCloud.

```mermaid
flowchart LR
    gotest["gotestsum -race per module"] --> profiles["per-module coverage profiles"]
    profiles --> merged["coverage.out, repo-relative paths"]
    merged --> artifact1[coverage-report artifact]
    golangci["golangci-lint per module"] --> artifact2[golangci-lint-module artifacts]
    artifact1 --> scan["sonarcloud job: SonarCloud scan"]
    artifact2 --> scan
    scan --> gate{Quality gate}
    gate --> check[SonarCloud Analysis status check]
```

## Required checks

This diagram shows which workflows report the three status checks that the `Protect Main` ruleset requires.

```mermaid
flowchart TD
    PR[Pull request to main] --> kind{Which files changed?}
    kind -->|Any file outside the ignored paths| ci["ci.yml: sonarcloud job"]
    kind -->|Only ignored paths| skipped["ci.yml skipped"]
    PR --> bot{Branch starts with dependabot/?}
    bot -->|Yes| gate["dependabot-gate.yml: SonarCloud Analysis succeeds"]
    bot -->|No| gateskip["dependabot-gate.yml: SonarCloud Analysis skipped"]
    PR --> cla["cla.yml: CLAAssistant"]
    PR --> dco[DCO status]
    ci --> merge[Ruleset satisfied]
    gate --> merge
    gateskip --> merge
    cla --> merge
    dco --> merge
```

A skipped job counts as passing for a required check, so a docs-only pull request merges with `SonarCloud Analysis` reported as skipped.

## Release flow

This diagram shows the job order in `release.yml` for `phase=full`.

```mermaid
flowchart TD
    validate["validate: version, previous go.mod, tests, dep skew, API compatibility"]
    libs["tag-dependencies: tag root, runtime, pkg"]
    tools["update-tools: tag server/a2a, sdk"]
    create["create-release: draft release, SBOM, attestation"]
    verify["verify-docs: publish release, wait for docs.yml"]
    docs["docs.yml: build and deploy"]
    notify["notify-downstream: wait for Go proxy, dispatch to adapters"]
    summary["summary"]

    validate --> libs
    libs --> tools
    libs --> create
    create --> verify
    verify -.->|release published| docs
    tools --> notify
    libs --> notify
    verify --> summary
    notify --> summary
    tools --> summary
```

`phase=libs-only` runs `validate` and `tag-dependencies`. `phase=tools-only` runs `validate` and `update-tools`. `summary` runs in every phase.

## Documentation deploy

This diagram shows what `docs.yml` does for each trigger.

```mermaid
flowchart LR
    trig{Trigger}
    trig -->|Pull request| build[build job]
    trig -->|Release published| build
    trig -->|Manual dispatch| build
    build --> mermaid[check:mermaid]
    mermaid --> site[make docs-build]
    site --> links[check-links and check-doc-urls]
    links --> upload[Upload Pages artifact]
    upload --> deploy{Pull request?}
    deploy -->|No| pages[deploy job: GitHub Pages]
    deploy -->|Yes| stop[No deploy]
```

## Related

- [CI/CD pipelines](/devops/ci-cd-pipelines/)
- [CI/CD quick reference](/devops/ci-cd-quickref/)
- [Branch protection](/devops/branch-protection/)
