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
  - Release Test Pipeline - Safe release workflow validation
- **[ci-cd-quickref](/devops/ci-cd-quickref/)** - Quick reference for common CI/CD commands and tasks
- **[ci-cd-diagrams](/devops/ci-cd-diagrams/)** - Visual diagrams of pipeline architecture and flows

### GitHub Actions

- **[promptarena-action](/devops/promptarena-action/)** - PromptArena GitHub Action for CI/CD integration
  - Run prompt tests in GitHub workflows
  - Native test reporting integration
  - Multi-platform support
- **[packc-action](/devops/packc-action/)** - PackC GitHub Action for CI/CD integration
  - Compile prompt packs in GitHub workflows
  - Publish to OCI registries (GHCR, Docker Hub, ECR)
  - Supply chain security with Cosign signing

### Release Management

- **[release-automation](/devops/release-automation/)** - ⭐ **Start here!** Automated release tools (local script + GitHub Actions)
- **[goreleaser-integration](/devops/goreleaser-integration/)** - Binary builds and GitHub releases with GoReleaser
- **[release-process](/devops/release-process/)** - Manual step-by-step guide (if automation fails)
- **[testing-releases-quickstart](/devops/testing-releases-quickstart/)** - Quick reference for safely testing the release process
- **[testing-releases](/devops/testing-releases/)** - Complete guide to testing releases without polluting git history

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

1. Review [testing-releases-quickstart](/devops/testing-releases-quickstart/) first
2. Test thoroughly in a separate repository
3. Follow the [release-process](/devops/release-process/) step-by-step guide

## For Maintainers

This documentation is primarily for:
- Repository maintainers preparing releases
- Contributors working on CI/CD improvements
- Anyone needing to understand the release workflow

For general development documentation, see [contributors guide](/contributors/).
