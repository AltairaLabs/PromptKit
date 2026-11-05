---
title: DevOps
description: How PromptKit's CI/CD workflows, branch protection and releases work.
sidebar:
  order: 0
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - .github/workflows/docs.yml
    - .github/workflows/release.yml
---
Operational documentation for people who maintain PromptKit or change its CI/CD. For general development, see the [contributors guide](/contributors/).

## Pages

| Page | Contents |
|---|---|
| [CI/CD pipelines](/devops/ci-cd-pipelines/) | Every GitHub Actions workflow: triggers, jobs, required secrets and the release process |
| [CI/CD quick reference](/devops/ci-cd-quickref/) | Commands to run CI checks locally, watch runs and trigger workflows |
| [CI/CD diagrams](/devops/ci-cd-diagrams/) | Mermaid diagrams of workflow triggers, job dependencies, required checks and the release flow |
| [Branch protection](/devops/branch-protection/) | The ruleset that protects `main` and the checks it requires |
| [Branch protection quick reference](/devops/branch-protection-quickref/) | What the ruleset means for a pull request and how to fix a blocked one |

## Releases

PromptKit ships Go library modules only: `runtime`, `pkg`, `sdk` and `server/a2a`. The `release.yml` workflow tags them and creates the GitHub release. The PromptArena and PackC CLIs, their binaries and their packages release from the [promptarena](https://github.com/AltairaLabs/promptarena) repository.

```bash
gh workflow run release.yml -f version=v2.16.2 -f phase=full
```

[CI/CD pipelines](/devops/ci-cd-pipelines/#releases-releaseyml) lists the inputs, phases and jobs.
