---
title: ci cd quickref
description: DevOps and release management documentation
---
Quick commands and workflows for common CI/CD tasks.

## Running CI Checks Locally

Before pushing code, run the same checks that CI runs:

```bash
# Full CI suite
make test          # Run all tests
make test-race     # Run tests with race detector
make lint          # Run linting
make build         # Build all components
make coverage      # Generate coverage report

# Individual components
cd runtime && go test ./...
cd sdk && go test ./...
cd tools/arena && go test ./...
```

## Checking CI Status

### View Current Runs
```bash
# Using GitHub CLI
gh run list --limit 10

# View specific workflow
gh run list --workflow=ci.yml

# Watch a run in progress
gh run watch
```

### View Logs
```bash
# Get latest run logs
gh run view --log

# Get specific run
gh run view <run-id> --log

# Download logs
gh run download <run-id>
```

## Triggering Workflows

### Manual Trigger (Documentation)
```bash
# Trigger docs build manually
gh workflow run docs.yml
```

### Release Test Workflow
```bash
# Test arena release
gh workflow run release-test.yml -f tool=arena -f version=v0.0.1-test

# Test packc release
gh workflow run release-test.yml -f tool=packc -f version=v0.0.1-test
```

## Common CI Issues & Fixes

### Test Failures

**Issue:** Tests fail on CI but pass locally
```bash
# Run with same conditions as CI
go test -race -v ./...

# Check for timing issues
go test -count=10 ./...
```

### Coverage Issues

**Issue:** Coverage report not generated
```bash
# Generate locally to debug
make coverage

# Check merged coverage
cat coverage.out | head -20
```

### Linting Failures

**Issue:** Linter catches issues
```bash
# Run all linters locally
make lint

# Auto-fix formatting
go fmt ./...

# Check specific issues
go vet ./...
golangci-lint run
```

### Build Failures

**Issue:** Build fails on CI
```bash
# Clean and rebuild
make clean
make build

# Check for workspace sync issues
go work sync
```

## SonarCloud Integration

### Viewing Results

- Dashboard: https://sonarcloud.io/project/overview?id=AltairaLabs_PromptKit
- Coverage: Check "Coverage" tab
- Quality Gate: View overall project quality

### Local Analysis

```bash
# Run SonarScanner locally (requires SONAR_TOKEN)
export SONAR_TOKEN=your_token_here
make sonar-scan

# Quick check: coverage + sonar
make sonar-quick
```

## Documentation Pipeline

### Testing Docs Locally

```bash
# Install Jekyll dependencies (first time)
cd docs
gem install bundler
bundle install

# Serve locally
bundle exec jekyll serve

# View at: http://localhost:4000
```

### Forcing Docs Rebuild

```bash
# Trigger manual deploy
gh workflow run docs.yml

# Or push a docs change
echo "<!-- trigger -->" >> docs/README.md
git add docs/README.md
git commit -m "docs: trigger rebuild"
git push
```

## Release Testing

### Safe Local Test

```bash
# Dry run - no git changes
./scripts/test-release.sh arena v0.0.1-test

# Review output, no files modified
git status  # Should show no changes
```

### GitHub Actions Test

```bash
# Create test branch
git checkout -b release-test/my-test
git push origin release-test/my-test

# Watch workflow
gh run watch

# Clean up
git push origin --delete release-test/my-test
git checkout main
git branch -d release-test/my-test
```

## Monitoring & Debugging

### Check Workflow Status

```bash
# List recent runs with status
gh run list --limit 20

# Show only failures
gh run list --status failure

# Watch specific workflow
gh run watch --workflow=ci.yml
```

### Download Artifacts

```bash
# List artifacts from a run
gh run view <run-id> --json artifacts

# Download specific artifact
gh run download <run-id> -n release-checklist
```

### Re-run Failed Jobs

```bash
# Re-run failed jobs only
gh run rerun <run-id> --failed

# Re-run entire workflow
gh run rerun <run-id>
```

## Quality Metrics

### Check Coverage

```bash
# Generate and view coverage locally
make coverage
go tool cover -html=coverage.out
```

### View Test Results

After CI run:
1. Go to Actions tab
2. Click on workflow run
3. Click "Test Results" or "Coverage Test Results"
4. View detailed test report

### Code Quality

- **Go Report Card:** https://goreportcard.com/report/github.com/AltairaLabs/PromptKit
- **SonarCloud:** https://sonarcloud.io/project/overview?id=AltairaLabs_PromptKit

## Emergency Procedures

### CI is Down

1. Check GitHub Status: https://www.githubstatus.com/
2. Run tests locally: `make test-race`
3. If urgent, can bypass with admin approval
4. Document in PR why CI was bypassed

### False Positive in Linter

```bash
# Disable specific rule (use sparingly)
//nolint:rulename

# Document why in comment
// nolint:staticcheck // SA1019: using deprecated method for compatibility
```

### Broken Main Branch

1. Identify breaking commit
2. Revert or fix forward
3. Create hotfix PR
4. Fast-track review

```bash
# Revert last commit
git revert HEAD
git push origin main

# Or fix forward
git checkout -b hotfix/fix-ci
# make fixes
git push origin hotfix/fix-ci
# create PR
```

## Useful GitHub CLI Commands

```bash
# Install GitHub CLI
brew install gh  # macOS
# or visit: https://cli.github.com/

# Authenticate
gh auth login

# Common commands
gh pr status          # Check PR status
gh pr checks          # View CI checks on current PR
gh run list           # List workflow runs
gh run view           # View run details
gh workflow list      # List all workflows
```

## Configuration Files

Key files for CI/CD:

```
.github/workflows/
├── ci.yml              # Main CI pipeline
├── docs.yml            # Documentation deployment
└── release-test.yml    # Release testing

Makefile                # Local build commands
sonar-project.properties # SonarCloud config
codecov.yml             # Coverage config (if used)
.golangci.yml           # Linter configuration
```

## Further Reading

- [CI/CD Pipeline Documentation](./ci-cd-pipelines.md) - Detailed pipeline documentation
- [Testing Releases](./testing-releases-quickstart.md) - Release workflow
- [GitHub Actions Docs](https://docs.github.com/en/actions) - Official documentation
- [SonarCloud Docs](https://docs.sonarcloud.io/) - Code quality analysis

---

*Last Updated: 2 November 2025*
