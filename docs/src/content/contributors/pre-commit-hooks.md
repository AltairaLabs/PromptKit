---
title: Pre-Commit Hooks
docType: guide
order: 4
---

PromptKit uses pre-commit hooks to maintain code quality. The hooks run automatically before each commit and only check **changed code**, making them fast and developer-friendly.

## Quick Setup

```bash
# Install required tools
brew install golangci-lint gosec
pip3 install diff-cover

# Enable the hook (one-time)
chmod +x .git/hooks/pre-commit
```

Or use the installation script:

```bash
./scripts/install-hooks.sh
```

## What Gets Checked

The pre-commit hook runs four checks:

1. **Linting** - Only on changed Go files using `golangci-lint --new-from-rev=HEAD`
2. **Security** - Security scanning with `gosec` on changed code (if installed)
3. **Tests** - Only for packages with changes
4. **Coverage** - Requires ≥80% test coverage on changed lines only (via `diff-cover`)

**Speed**: Typically 15-35 seconds (vs. 2-4 minutes for full repo checks)

## Usage

### Normal Workflow

Just commit as usual - the hook runs automatically:

```bash
git add .
git commit -m "feat: add new feature"
```

### Skipping the Hook

For emergencies or work-in-progress commits, include `[skip-pre-commit]` in your message:

```bash
git commit -m "wip: experimental changes [skip-pre-commit]"
```

**Note**: CI will still run full checks on your PR.

### Manual Verification

Run the same checks manually before pushing:

```bash
make verify                 # Run all checks (recommended)
make lint-diff              # Lint changed files only
make test-coverage-diff     # Check coverage on changes
```

## How It Works

The pre-commit system only checks code you've changed:

- **Linting**: Uses `--new-from-rev=HEAD` to only report issues in new/modified lines
- **Coverage**: Uses `diff-cover` to calculate coverage percentage for changed lines only

This means:
- ✅ You're never blocked by legacy code issues
- ✅ All new code must pass quality checks
- ✅ Coverage improves gradually over time

## CI Integration

GitHub Actions runs the same checks in the `verify` job:

- Downloads coverage from the test job
- Runs `diff-cover` against the PR base branch
- Posts results as a PR comment
- Fails the PR if coverage on changed lines is below 80%

## Troubleshooting

### Hook Not Running

```bash
chmod +x .git/hooks/pre-commit
```

### Missing Tools

```bash
# golangci-lint
brew install golangci-lint

# gosec (security scanner)
brew install gosec

# diff-cover
pip3 install diff-cover
```

### Coverage Check Fails

The hook shows which lines need test coverage. Options:

1. Add tests for the changed lines (recommended)
2. Use `[skip-pre-commit]` if the code is truly not testable
3. Run `make test-coverage-diff` to see a detailed report

## Configuration

### Changing Coverage Threshold

Edit the `--fail-under` value in:
- `Makefile` (test-coverage-diff target)
- `.git/hooks/pre-commit`
- `.github/workflows/ci.yml` (verify job)

### Disabling Specific Linters

Edit `.golangci.yml` to disable linters that don't fit your workflow.
