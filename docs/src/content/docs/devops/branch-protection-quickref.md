---
title: Branch protection quick reference
description: What the main branch ruleset means for a contributor, and how to fix the checks that block a pull request.
verified:
  commit: 8489bf3d95c7498f9de0f024769db9399c24e884
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/cla.yml
    - .github/workflows/dependabot-gate.yml
    - .github/workflows/release.yml
    - Makefile
    - scripts/commit-msg.sh
    - scripts/install-hooks.sh
---
A ruleset protects `main`. This page lists what it means for your pull request. [Branch protection](/devops/branch-protection/) has the full settings.

## What the ruleset enforces

- No direct pushes to `main`: changes arrive through a pull request.
- No force pushes to `main` and no deletion of `main`.
- Three required checks: `CLAAssistant`, `DCO` and `SonarCloud Analysis`.

The ruleset requires no approving review, no code owner review, no signed (GPG or SSH) commits and no conversation resolution. It does not require the branch to be up to date.

## Make a change

1. Install the git hooks once per clone:

   ```bash
   ./scripts/install-hooks.sh
   ```

2. Create a branch from `main`:

   ```bash
   git switch main
   git pull origin main
   git switch -c feat/your-change
   ```

3. Run golangci-lint and the tests on your changed modules:

   ```bash
   make verify
   ```

4. Commit with a sign-off and a conventional commit message:

   ```bash
   git commit -s -m "feat: describe the change"
   ```

5. Push the branch and open a pull request against `main`:

   ```bash
   git push -u origin feat/your-change
   gh pr create
   ```

6. Wait for the required checks, then merge. The project convention is a squash merge:

   ```bash
   gh pr merge --squash
   ```

## Required checks

| Check | Passes when | Fix |
|---|---|---|
| `CLAAssistant` | You signed the contributor license agreement | Comment `I have read the CLA Document and I hereby sign the CLA` on the pull request, or comment `recheck` after signing |
| `DCO` | Every commit has a `Signed-off-by:` trailer matching its author | See below |
| `SonarCloud Analysis` | The SonarCloud quality gate passes, or the pull request touches only ignored paths or comes from Dependabot | Open the SonarCloud link on the check and fix the new issues or the coverage on new lines |

`Lint`, `Test & Coverage`, `Build`, `SDK Integration Tests`, `Weak Assertion Guard` and `Performance Regression Check` do not block a merge, but `SonarCloud Analysis` needs the `Test & Coverage` and `Lint` jobs to succeed. See [CI/CD pipelines](/devops/ci-cd-pipelines/).

## Fix a missing sign-off

Sign the last commit:

```bash
git commit --amend -s --no-edit
git push --force-with-lease
```

Sign every commit on the branch:

```bash
git rebase --signoff origin/main
git push --force-with-lease
```

The sign-off email must match the commit author.

## Update a branch

The ruleset does not require an up-to-date branch. To pick up changes from `main` anyway:

```bash
git fetch origin
git rebase origin/main
git push --force-with-lease
```

## Reproduce failing jobs

```bash
make test
make test-race
make lint-diff
make build
```

`make lint` runs `go fmt`, which rewrites files. See the [CI/CD quick reference](/devops/ci-cd-quickref/) for a command per failing job.

## Tags

The release workflow creates the version tags (`vX.Y.Z`, `runtime/vX.Y.Z`, `pkg/vX.Y.Z`, `sdk/vX.Y.Z`, `server/a2a/vX.Y.Z`). Do not create them by hand. See [Releases](/devops/ci-cd-pipelines/#releases-releaseyml).

## Urgent fixes

The ruleset has no user or admin bypass. Open a pull request that fixes or reverts the change and merge it once the required checks pass.

## Related

- [Branch protection](/devops/branch-protection/)
- [Contributing](/contributors/contributing/)
- [CI/CD pipelines](/devops/ci-cd-pipelines/)
