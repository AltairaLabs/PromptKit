---
title: uranch protection
description: DevOps and release management documentation
docType: guide
---

# Branch Protection Rules

This document describes the branch protection rules configured for the PromptKit repository to ensure code quality, security, and stability.

## Protected Branches

### `main` Branch

The `main` branch contains production-ready code and must be protected with the following rules:

#### Required Status Checks

All status checks must pass before merging:

**CI Workflow (`ci.yml`):**
- ✅ `test` - Unit tests with Go 1.25.1
- ✅ `lint` - Linting across all modules (runtime, sdk, pkg, arena, packc, inspect-state)
- ✅ `build` - Build verification for all components
- ✅ `coverage` - Coverage reporting and SonarCloud analysis

#### Pull Request Requirements

- **Require pull request reviews before merging**: ✅ Enabled
  - **Required approving reviews**: 1 minimum
  - **Dismiss stale pull request approvals when new commits are pushed**: ✅ Enabled
  - **Require review from Code Owners**: ✅ Enabled (if CODEOWNERS file exists)
  - **Allow specified actors to bypass pull request requirements**: ❌ Disabled
  
- **Require conversation resolution before merging**: ✅ Enabled
  - All review comments must be resolved

- **Require signed commits**: ✅ Enabled
  - All commits must be signed with GPG/SSH keys
  - Enforces DCO (Developer Certificate of Origin)

- **Require linear history**: ✅ Enabled
  - No merge commits allowed
  - Use squash or rebase merge only

#### Additional Protections

- **Require status checks to pass before merging**: ✅ Enabled
  - **Require branches to be up to date before merging**: ✅ Enabled
  - All required checks listed above must pass

- **Require deployments to succeed before merging**: ❌ Disabled
  - No deployment checks for main branch

- **Lock branch**: ❌ Disabled
  - Allow pushes via PRs

- **Do not allow bypassing the above settings**: ✅ Enabled
  - Even administrators must follow these rules

- **Restrict who can push to matching branches**: ✅ Enabled
  - Restrict pushes that create matching branches: ✅ Enabled
  - Only GitHub Actions and maintainers can push
  - **Allowed actors**: 
    - `github-actions[bot]` (for automated releases)
    - Repository maintainers/admins (emergency only)

- **Allow force pushes**: ❌ Disabled
  - No force pushes allowed

- **Allow deletions**: ❌ Disabled
  - Branch cannot be deleted

### Tag Protection (`v*`, `runtime/v*`, `sdk/v*`, etc.)

Tags are immutable version markers and must be protected.

#### Tag Protection Rules

- **Pattern**: `v*`, `runtime/v*`, `sdk/v*`, `pkg/v*`, `tools/arena/v*`, `tools/packc/v*`, `tools/inspect-state/v*`
- **Restrict tag creation**: ✅ Enabled
  - Only release managers and `github-actions[bot]`
- **Allow deletions**: ❌ Disabled
- **Allow updates**: ❌ Disabled

## CODEOWNERS Configuration

Create a `.github/CODEOWNERS` file to automatically assign reviewers:

```
# Global owners
*                                   @AltairaLabs/maintainers

# Runtime and core components
/runtime/**                         @AltairaLabs/runtime-team
/pkg/**                             @AltairaLabs/runtime-team

# SDK
/sdk/**                             @AltairaLabs/sdk-team

# Tools
/tools/arena/**                     @AltairaLabs/arena-team
/tools/packc/**                     @AltairaLabs/tools-team
/tools/inspect-state/**             @AltairaLabs/tools-team

# CI/CD and infrastructure
/.github/workflows/**               @AltairaLabs/devops-team
/.github/BRANCH_PROTECTION.md       @AltairaLabs/maintainers
/.goreleaser.yml                    @AltairaLabs/devops-team
/Makefile                           @AltairaLabs/devops-team

# Documentation
/docs/**                            @AltairaLabs/docs-team
/*.md                               @AltairaLabs/docs-team

# Security
/SECURITY.md                        @AltairaLabs/security-team
/CODE_OF_CONDUCT.md                 @AltairaLabs/maintainers
```

## Setting Up Branch Protection

### Via GitHub UI

1. Navigate to **Settings** → **Branches**
2. Click **Add branch protection rule**
3. Enter branch name pattern (e.g., `main`)
4. Configure settings as specified above
5. Click **Create** or **Save changes**

### Via GitHub CLI

```bash
# Install GitHub CLI if not already installed
# brew install gh

# Protect main branch
gh api repos/{owner}/{repo}/branches/main/protection \
  --method PUT \
  -H "Accept: application/vnd.github+json" \
  -f required_status_checks[strict]=true \
  -f required_status_checks[contexts][]=test \
  -f required_status_checks[contexts][]=lint \
  -f required_status_checks[contexts][]=build \
  -f required_status_checks[contexts][]=coverage \
  -f required_pull_request_reviews[required_approving_review_count]=1 \
  -f required_pull_request_reviews[dismiss_stale_reviews]=true \
  -f required_pull_request_reviews[require_code_owner_reviews]=true \
  -f required_conversation_resolution=true \
  -f required_signatures=true \
  -f required_linear_history=true \
  -f restrictions[users][]=github-actions[bot] \
  -f allow_force_pushes=false \
  -f allow_deletions=false \
  -f enforce_admins=true
```

## Enforcement and Exceptions

### Emergency Procedures

In case of critical production issues:

1. Emergency fixes can be pushed directly by maintainers with admin access
2. Post-incident review required within 24 hours
3. Retroactive PR must be created documenting the change
4. Document in `SECURITY.md` if security-related

### Temporary Rule Modifications

For special cases (e.g., initial setup, major refactoring):

1. Require approval from 2+ maintainers
2. Document reason in GitHub issue
3. Set time-bound re-enablement
4. Announce in team channels

## Monitoring and Compliance

### Regular Audits

- **Monthly**: Review branch protection settings
- **Quarterly**: Audit access permissions and team assignments
- **After each release**: Verify tag protections are working

### Alerts

Configure GitHub Actions to alert on:
- Failed CI checks on protected branches
- Unauthorized push attempts
- Branch protection rule changes

### Metrics to Track

- PR merge time
- Number of reviews per PR
- CI failure rate
- Branch protection violations

## FAQ

**Q: Can I push directly to `main`?**  
A: No. All changes must go through pull requests with required reviews and passing CI checks.

**Q: What if CI is failing due to external service issues?**  
A: Contact a maintainer. Branch protection can be temporarily disabled in emergencies, but this requires approval and documentation.

**Q: How do I get my PR merged faster?**  
A: Ensure all tests pass, address review comments promptly, and follow the contribution guidelines. Smaller, focused PRs are reviewed faster.

**Q: Can I create a release without these checks?**  
A: No. The release automation workflow (`release.yml`) runs through GitHub Actions with proper permissions. Manual releases must follow the documented process.

**Q: What happens if I forget to sign my commits?**  
A: Your PR will be blocked. You'll need to sign the commits retroactively using `git rebase --exec 'git commit --amend --no-edit -n -S'` and force push to your branch.

## Related Documentation

- [Contributing Guidelines](../../CONTRIBUTING.md)
- [Release Process](release-process.md)
- [CI/CD Pipelines](ci-cd-pipelines.md)
- [Security Policy](../../SECURITY.md)
- [Code of Conduct](../../CODE_OF_CONDUCT.md)

## Updates to This Document

This document should be updated when:
- Branch protection rules change
- New protected branch patterns are added
- CI/CD workflows are modified
- Team structure changes

**Last Updated**: 2025-11-02  
**Approved By**: Repository Maintainers
