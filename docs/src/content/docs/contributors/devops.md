---
title: DevOps & Infrastructure
sidebar:
  order: 5
---

This section contains documentation for maintainers and contributors about the project's DevOps infrastructure, CI/CD pipelines, release processes, and development workflows.

## Quick References

- [CI/CD Quick Reference](/devops/ci-cd-quickref/) - Fast lookup for common CI/CD tasks
- [Branch Protection Quick Reference](/devops/branch-protection-quickref/) - Branch protection rules summary

## CI/CD Pipelines

- [CI/CD Pipelines Overview](/devops/ci-cd-pipelines/) - Comprehensive guide to our automated workflows
- [CI/CD Architecture Diagrams](/devops/ci-cd-diagrams/) - Visual representation of our CI/CD system

## Release Management

PromptKit ships libraries only (`runtime`, `pkg`, `sdk`). Releases are cut by
the automated `release.yml` GitHub Actions workflow, which tags the library
modules and publishes them to the Go module proxy:

```bash
gh workflow run release.yml -f version=v1.3.9 -f phase=full
```

The Arena and PackC CLIs release from their own repository; see the
[PromptArena docs](https://promptarena.altairalabs.ai/) for their release
process.

## Branch Protection

- [Branch Protection Rules](/devops/branch-protection/) - Detailed branch protection documentation
- [Branch Protection Setup](/devops/branch-protection-quickref/) - Quick reference for branch protection

## For Maintainers

This documentation is primarily for project maintainers who need to:

- Configure and manage CI/CD pipelines
- Handle releases and versioning
- Set up branch protection rules
- Troubleshoot build and deployment issues
- Maintain project infrastructure

## For Contributors

Contributors should be familiar with:

- [CI/CD Pipelines](/devops/ci-cd-pipelines/) - Understanding what checks run on PRs
- [Branch Protection](/devops/branch-protection/) - What rules apply to contributions

## Getting Help

- Check the [CI/CD Quick Reference](/devops/ci-cd-quickref/) for common issues
- Review existing GitHub Actions workflows in `.github/workflows/`
- Ask maintainers in GitHub issues or discussions
