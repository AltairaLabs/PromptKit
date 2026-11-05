---
title: DevOps & Infrastructure
sidebar:
  order: 5
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/release.yml
---

This section is for maintainers and contributors. It covers the CI/CD pipelines, the release process and branch protection.

## CI/CD

- [CI/CD quick reference](/devops/ci-cd-quickref/): lookup for common CI/CD tasks
- [CI/CD pipelines overview](/devops/ci-cd-pipelines/): the automated workflows
- [CI/CD architecture diagrams](/devops/ci-cd-diagrams/): the CI/CD system as diagrams

The workflow definitions are in `.github/workflows/`.

## Release management

The `release.yml` workflow is the only way to cut a release. Do not run `gh release create` or push tags by hand.

```bash
gh workflow run release.yml -f version=v2.16.2 -f phase=full
```

[CI/CD pipelines](/devops/ci-cd-pipelines/#releases-releaseyml) lists the inputs, phases and jobs.

The Arena and PackC CLIs release from their own repository. See the [PromptArena docs](https://promptarena.altairalabs.ai/).

## Branch protection

- [Branch protection rules](/devops/branch-protection/): the rules in detail
- [Branch protection quick reference](/devops/branch-protection-quickref/): a summary

## For maintainers

You configure the CI/CD pipelines, run releases, set branch protection rules and troubleshoot build failures.

## For contributors

Read these two pages to know what runs on your pull request:

- [CI/CD pipelines](/devops/ci-cd-pipelines/)
- [Branch protection](/devops/branch-protection/)

## Getting help

- Check the [CI/CD quick reference](/devops/ci-cd-quickref/) for common issues.
- Read the workflows in `.github/workflows/`.
- Ask the maintainers in a GitHub issue.
