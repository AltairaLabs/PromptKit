---
title: DevOps Guide
description: DevOps and release management documentation
sidebar:
  order: 0
---
This directory contains operational and release management documentation for PromptKit maintainers.

## Contents

### Repository Management

- **[branch-protection.md](./branch-protection.md)** - Complete branch protection rules and configuration guide
- **[branch-protection-quickref.md](./branch-protection-quickref.md)** - Quick reference for working with protected branches

### CI/CD & Pipelines

- **[ci-cd-pipelines.md](./ci-cd-pipelines.md)** - Complete documentation of all GitHub Actions workflows
  - CI Pipeline - Automated testing, linting, and quality checks
  - Documentation Pipeline - GitHub Pages deployment
  - Release Test Pipeline - Safe release workflow validation
- **[ci-cd-quickref.md](./ci-cd-quickref.md)** - Quick reference for common CI/CD commands and tasks
- **[ci-cd-diagrams.md](./ci-cd-diagrams.md)** - Visual diagrams of pipeline architecture and flows

### GitHub Actions

- **[promptarena-action.md](./promptarena-action.md)** - PromptArena GitHub Action for CI/CD integration
  - Run prompt tests in GitHub workflows
  - Native test reporting integration
  - Multi-platform support
- **[packc-action.md](./packc-action.md)** - PackC GitHub Action for CI/CD integration
  - Compile prompt packs in GitHub workflows
  - Publish to OCI registries (GHCR, Docker Hub, ECR)
  - Supply chain security with Cosign signing

### Release Management

- **[release-automation.md](./release-automation.md)** - ⭐ **Start here!** Automated release tools (local script + GitHub Actions)
- **[goreleaser-integration.md](./goreleaser-integration.md)** - Binary builds and GitHub releases with GoReleaser
- **[release-process.md](./release-process.md)** - Manual step-by-step guide (if automation fails)
- **[testing-releases-quickstart.md](./testing-releases-quickstart.md)** - Quick reference for safely testing the release process
- **[testing-releases.md](./testing-releases.md)** - Complete guide to testing releases without polluting git history

### Scripts

Release and testing scripts are located in `/scripts/`:

- `scripts/release.sh` - **Automated release script** (recommended!)
- `scripts/test-release.sh` - Local dry-run testing for releases

### Workflows

GitHub Actions workflows:

- `.github/workflows/release.yml` - **Automated release workflow** (recommended!)
- `.github/workflows/release-test.yml` - Release testing workflow
- `.github/workflows/ci.yml` - Continuous integration
- `.github/workflows/docs.yml` - Documentation deployment

## Quick Links

### Releasing a New Version

```bash
# 🚀 RECOMMENDED: Automated release
./scripts/release.sh v1.0.0

# Or use GitHub Actions
gh workflow run release.yml -f version=v1.0.0
```

### Testing a Release

```bash
# Safe local test (no git changes)
./scripts/test-release.sh arena v0.0.1-test
```

### Creating a Real Release (Manual)

1. Review [testing-releases-quickstart.md](./testing-releases-quickstart.md) first
2. Test thoroughly in a separate repository
3. Follow the [release-process.md](./release-process.md) step-by-step guide

## For Maintainers

This documentation is primarily for:
- Repository maintainers preparing releases
- Contributors working on CI/CD improvements
- Anyone needing to understand the release workflow

For general development documentation, see [/docs/guides/](../guides/).
