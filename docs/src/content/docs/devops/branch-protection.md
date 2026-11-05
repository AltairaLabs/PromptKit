---
title: Branch protection
description: The repository ruleset that protects main in PromptKit, the status checks it requires and how to read it.
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - .github/workflows/ci.yml
    - .github/workflows/cla.yml
    - .github/workflows/dependabot-gate.yml
    - .github/workflows/release.yml
    - scripts/commit-msg.sh
    - scripts/setup-branch-protection.sh
---
A repository ruleset named `Protect Main` protects the default branch (`main`). The repository has no classic branch protection rule and no ruleset for tags. Read the live settings with the GitHub CLI:

```bash
gh api repos/AltairaLabs/PromptKit/rulesets
gh api repos/AltairaLabs/PromptKit/rulesets/9963634
gh api repos/AltairaLabs/PromptKit/rules/branches/main
```

## Ruleset rules

| Rule | Setting |
|---|---|
| Enforcement | Active |
| Target | The default branch |
| Restrict deletions | Enabled |
| Block force pushes (non-fast-forward) | Enabled |
| Restrict creation | Enabled |
| Require a pull request | Enabled, 0 required approvals |
| Dismiss stale approvals on push | Disabled |
| Require code owner review | Disabled |
| Require last-push approval | Disabled |
| Require conversation resolution | Disabled |
| Allowed merge methods | Merge, squash and rebase |
| Require status checks | Enabled; branches need not be up to date |
| Require signed commits | Not in the ruleset |
| Require linear history | Not in the ruleset |

One GitHub App integration is a bypass actor with mode `always`. Repository admins are not bypass actors, so an admin cannot push to `main` or skip the required checks. Every change reaches `main` through a pull request.

The repository setting "Automatically delete head branches" is on, and all three merge methods are allowed. The project convention is a squash merge.

## Required status checks

| Check | Reported by |
|---|---|
| `CLAAssistant` | `cla.yml` |
| `DCO` | The DCO status check, which fails a pull request that has a commit without a matching `Signed-off-by:` trailer |
| `SonarCloud Analysis` | The `sonarcloud` job in `ci.yml`, or the job of the same name in `dependabot-gate.yml` |

The other CI jobs (`Test & Coverage`, `Lint`, `Build`, `SDK Integration Tests`, `Weak Assertion Guard`, `Performance Regression Check`) are not required by the ruleset. They run on every code pull request, but only the three checks above block a merge.

`ci.yml` does not run for changes that touch only docs, markdown or the other ignored paths. A required check that never reports would block such a pull request. `dependabot-gate.yml` runs on every pull request to `main`; its `SonarCloud Analysis` job is skipped unless the branch starts with `dependabot/`, and GitHub counts a skipped job as passing. This satisfies the requirement for docs-only pull requests.

## Commit sign-off

Every commit needs a DCO sign-off: a `Signed-off-by: Name <email>` trailer whose email matches the commit author. Create it with `git commit -s`. The `commit-msg` hook in `scripts/commit-msg.sh` rejects a commit without the trailer. The ruleset does not require GPG or SSH signatures.

## Code owners

`.github/CODEOWNERS` assigns `@AltairaLabs/maintainers` to these paths:

| Area | Paths |
|---|---|
| Modules | `/runtime/`, `/pkg/`, `/sdk/`, `/server/a2a/`, `/examples/`, `/scripts/` |
| CI and build | `/.github/workflows/`, `/.github/CODEOWNERS`, `/docs/devops/branch-protection*.md`, `/Makefile`, `go.work`, `**/go.mod`, `**/go.sum` |
| Documentation | `/docs/`, `/*.md` |
| Governance | `/SECURITY.md`, `/CODE_OF_CONDUCT.md`, `/GOVERNANCE.md`, `/LICENSE`, `/CONTRIBUTING.md` |

The ruleset does not require code owner review, so ownership assigns reviewers without blocking a merge.

## Tags

No ruleset targets tags. The release workflow creates `vX.Y.Z`, `runtime/vX.Y.Z`, `pkg/vX.Y.Z`, `sdk/vX.Y.Z` and `server/a2a/vX.Y.Z` with `github-actions[bot]`. Go proxies cache published module versions, so a tag cannot be repaired after the proxy has served it. See [Releases](/devops/ci-cd-pipelines/#releases-releaseyml).

## Changing the ruleset

Edit `Protect Main` under Settings, Rules, Rulesets. A change to the required checks must name the job as GitHub reports it: a job's `name:` in the workflow, not its key. Renaming a required job in a workflow without updating the ruleset blocks every pull request.

:::caution
`scripts/setup-branch-protection.sh` creates classic branch protection that requires the checks `test`, `lint`, `build` and `coverage`, one approving review, signed commits and linear history. No workflow reports those check names, so running the script against the repository would block every pull request.
:::

## Related

- [Branch protection quick reference](/devops/branch-protection-quickref/)
- [CI/CD pipelines](/devops/ci-cd-pipelines/)
- [Contributing](/contributors/contributing/)
- [Governance](/contributors/governance/)
