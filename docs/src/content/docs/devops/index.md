---
title: DevOps Guide
description: DevOps and release management documentation
sidebar:
  order: 0
---
This directory contains operational and release management documentation for PromptKit maintainers.

## Contents

### Repository Management

- **[branch-protection](/devops/branch-protection/)** - Complete branch protection rules and configuration guide
- **[branch-protection-quickref](/devops/branch-protection-quickref/)** - Quick reference for working with protected branches

### CI/CD & Pipelines

- **[ci-cd-pipelines](/devops/ci-cd-pipelines/)** - Complete documentation of all GitHub Actions workflows
  - CI Pipeline - Automated testing, linting, and quality checks
  - Documentation Pipeline - GitHub Pages deployment
- **[ci-cd-quickref](/devops/ci-cd-quickref/)** - Quick reference for common CI/CD commands and tasks
- **[ci-cd-diagrams](/devops/ci-cd-diagrams/)** - Visual diagrams of pipeline architecture and flows

### Release Management

PromptKit ships libraries only (`runtime`, `pkg`, `sdk`). Releases are cut by
the automated `release.yml` GitHub Actions workflow, which tags the library
modules and publishes them to the Go module proxy. The Arena and PackC CLIs
release from their own repository — see the
[PromptArena docs](https://promptarena.altairalabs.ai/).

### Workflows

GitHub Actions workflows:

- `.github/workflows/release.yml` - **Automated library release workflow**
- `.github/workflows/ci.yml` - Continuous integration
- `.github/workflows/docs.yml` - Documentation deployment

## Quick Links

### Releasing a New Version

```bash
# Trigger the automated library release workflow
gh workflow run release.yml -f version=v1.0.0 -f phase=full
```

## For Maintainers

This documentation is primarily for:
- Repository maintainers preparing releases
- Contributors working on CI/CD improvements
- Anyone needing to understand the release workflow

For general development documentation, see [contributors guide](/contributors/).
