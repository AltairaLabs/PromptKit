---
title: release automation
description: DevOps and release management documentation
---
Automated tools to simplify the release process and reduce manual steps.

## Overview

The release process has been automated in two ways:

1. **Local Script** (`scripts/release.sh`) - Run releases from your machine
2. **GitHub Actions** (`.github/workflows/release.yml`) - Automated CI/CD releases

Both tools handle the complete workflow including dependency tagging, waiting for Go proxy, and verifying installations.

## Local Automated Release

### Quick Start

```bash
# Full automated release
./scripts/release.sh v1.0.0

# With auto-confirmation (no prompts)
./scripts/release.sh --yes v1.0.0

# Dry run to see what would happen
./scripts/release.sh --dry-run v1.0.0
```

### Script Features

✅ **Automatic validation**
- Checks version format
- Verifies clean working directory
- Ensures you're on main branch
- Runs tests before release

✅ **Intelligent waiting**
- Automatically waits for Go proxy caching
- Shows countdown timers
- Verifies modules are available

✅ **Error handling**
- Stops on first error
- Provides clear error messages
- Validates each step

✅ **Flexible options**
- Dry run mode for testing
- Skip tests for speed
- Auto-confirm for CI/CD
- Split into phases for complex releases

### All Options

```bash
./scripts/release.sh [OPTIONS] VERSION

OPTIONS:
    -h, --help              Show help message
    -d, --dry-run          Show what would be done without doing it
    -s, --skip-tests       Skip running tests before release
    -w, --skip-wait        Skip waiting periods (for testing)
    -y, --yes              Auto-confirm all prompts
    --deps-only            Only tag dependencies (runtime, pkg)
    --tools-only           Only tag tools (assumes deps already tagged)

EXAMPLES:
    # Full release with prompts
    ./scripts/release.sh v1.0.0

    # Dry run first
    ./scripts/release.sh --dry-run v1.0.0

    # Auto-confirm everything
    ./scripts/release.sh --yes v1.0.0

    # Split release (if you want more control)
    ./scripts/release.sh --deps-only v1.0.0
    # Wait 10 minutes...
    ./scripts/release.sh --tools-only v1.0.0
```

### What the Script Does

**Phase 1: Validation** (2 minutes)
- Checks git status
- Validates version format
- Pulls latest changes
- Runs test suite

**Phase 2: Tag Dependencies** (1 minute)
- Tags `runtime/vX.Y.Z`
- Tags `pkg/vX.Y.Z`
- Pushes to GitHub

**Phase 3: Wait for Go Proxy** (10 minutes)
- Automatic countdown
- Verifies modules are cached
- Can be skipped with `--skip-wait`

**Phase 4: Update Tools** (3 minutes)
- Removes replace directives
- Updates to versioned dependencies
- Tests builds

**Phase 5: Commit Branch** (1 minute)
- Creates release branch
- Commits changes
- Pushes to GitHub

**Phase 6: Tag Tools** (1 minute)
- Tags `tools/arena/vX.Y.Z`
- Tags `tools/packc/vX.Y.Z`
- Pushes tags

**Phase 7: Verify** (5 minutes)
- Waits for Go proxy
- Tests `go install`
- Confirms installations work

**Phase 8: Instructions**
- Shows next steps
- GitHub release creation
- Documentation updates

**Total Time: ~23 minutes** (mostly waiting)

## GitHub Actions Automated Release

### Quick Start

1. Go to **Actions** tab in GitHub
2. Select **"Release"** workflow
3. Click **"Run workflow"**
4. Fill in:
   - Version: `v1.0.0`
   - Phase: `full`
   - Skip tests: `false`
5. Click **"Run workflow"**

### Workflow Features

✅ **Fully automated**
- Runs entirely in CI/CD
- No local machine needed
- Consistent environment

✅ **Phased approach**
- Can run full release
- Or split into dependencies/tools
- Useful for troubleshooting

✅ **Built-in verification**
- Tests before tagging
- Verifies Go proxy caching
- Tests installation

✅ **Auto-creates GitHub release**
- Draft release created
- Pre-filled release notes
- Ready to edit and publish

### Workflow Options

**Version** (required)
- Format: `v1.0.0` or `1.0.0`
- Must be valid semver
- Cannot already exist

**Phase** (required)
- `full` - Complete release (default)
- `dependencies-only` - Only tag runtime/pkg
- `tools-only` - Only tag tools (deps must exist)

**Skip Tests** (optional)
- `false` - Run full test suite (default)
- `true` - Skip tests (faster, riskier)

### Workflow Jobs

**1. Validate**
- Checks version format
- Verifies version doesn't exist
- Runs tests (unless skipped)

**2. Tag Dependencies**
- Tags runtime and pkg modules
- Waits for Go proxy
- Verifies caching

**3. Update Tools**
- Creates release branch
- Updates tool dependencies
- Tags arena and packc
- Tests installation

**4. Create Release**
- Generates release notes
- Creates draft GitHub release
- Includes installation instructions

**5. Summary**
- Shows status of all jobs
- Provides next steps
- Links to draft release

### Using the Workflow

#### Full Release (Recommended)

```bash
# Via GitHub UI:
Actions → Release → Run workflow
- Version: v1.0.0
- Phase: full
- Skip tests: false

# Or via GitHub CLI:
gh workflow run release.yml \
  -f version=v1.0.0 \
  -f phase=full \
  -f skip_tests=false
```

#### Split Release (Advanced)

If you need more control or want to pause between phases:

```bash
# Step 1: Tag dependencies
gh workflow run release.yml \
  -f version=v1.0.0 \
  -f phase=dependencies-only

# Wait and verify...
# Check Go proxy manually

# Step 2: Tag tools
gh workflow run release.yml \
  -f version=v1.0.0 \
  -f phase=tools-only
```

## Comparison: Manual vs Automated

| Aspect | Manual Process | Automated (Script) | Automated (GitHub) |
|--------|----------------|--------------------|--------------------|
| **Active time** | 45+ min | 2 min | 1 min |
| **Total time** | 45+ min | ~25 min | ~25 min |
| **Steps** | 15 manual steps | 1 command | 1 trigger |
| **Error prone** | ⚠️ Very | ✅ Minimal | ✅ Minimal |
| **Repeatable** | ❌ No | ✅ Yes | ✅ Yes |
| **Testable** | ❌ No | ✅ Dry run | ✅ Test workflow |
| **Documented** | 📄 In docs | 💻 In code | 💻 In workflow |
| **Validation** | ⚠️ Manual | ✅ Automatic | ✅ Automatic |
| **Rollback** | ⚠️ Complex | ⚠️ Manual | ⚠️ Manual |
| **Components** | runtime, pkg, SDK, tools | runtime, pkg, SDK, tools | runtime, pkg, SDK, tools |

### Manual Process (for reference)
1. Validate version format
2. Check branch is main
3. Run all tests
4. Tag runtime/v1.0.0
5. Tag pkg/v1.0.0
6. Tag sdk/v1.0.0
7. Wait 10 minutes (watching clock)
8. Verify on Go proxy
9. Edit tools/arena/go.mod
10. Edit tools/packc/go.mod
11. Test builds
12. Commit and push
13. Tag tools/arena/v1.0.0
14. Tag tools/packc/v1.0.0
15. Wait 5 minutes
16. Test go install
17. Create GitHub release
18. Update documentation

**45+ minutes, many error opportunities**

### Automated Process
```bash
./scripts/release.sh v1.0.0
```
**2 minutes active, ~25 minutes total, consistent results**

## Recommended Workflow

### For Regular Releases

```bash
# 1. Test locally first
./scripts/release.sh --dry-run v1.0.0

# 2. Run actual release with script
./scripts/release.sh --yes v1.0.0

# 3. Create GitHub release
gh release create v1.0.0 --draft

# 4. Announce!
```

### For Team Releases

```bash
# 1. Create release PR
gh pr create --title "Release v1.0.0"

# 2. After PR merged, use GitHub Actions
gh workflow run release.yml -f version=v1.0.0

# 3. Review and publish draft release

# 4. Announce!
```

## Troubleshooting

### Script Fails at Go Proxy Check

**Problem:** Dependencies not cached after 10 minutes.

**Solutions:**
1. Wait another 5 minutes and retry
2. Check Go proxy manually:
   ```bash
   curl https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/runtime/@v/v1.0.0.info
   ```
3. Use `--skip-wait` and manually verify

### Workflow Times Out

**Problem:** GitHub Actions timeout (360 minutes max).

**Solutions:**
1. Use split release approach
2. Run dependencies phase first
3. Wait, then run tools phase

### Build Fails After Updating Dependencies

**Problem:** `go build` fails in tools.

**Solutions:**
1. Check that dependency versions exist
2. Verify Go proxy cached them
3. Try: `go clean -modcache && go build`

### Can't Install After Release

**Problem:** `go install` fails or installs wrong version.

**Solutions:**
1. Wait more time (up to 15 minutes)
2. Clear local cache: `go clean -modcache`
3. Check proxy: `GOPROXY=https://proxy.golang.org go install ...`

## Advanced Usage

### Custom Automation

You can wrap the release script in your own automation:

```bash
#!/bin/bash
# custom-release.sh

VERSION=$1

# Pre-release checks
./scripts/custom-checks.sh

# Run release
./scripts/release.sh --yes "$VERSION"

# Post-release actions
./scripts/update-docs.sh "$VERSION"
./scripts/notify-team.sh "$VERSION"
```

### CI/CD Integration

Use the script in other CI/CD systems:

```yaml
# .gitlab-ci.yml
release:
  stage: deploy
  script:
    - ./scripts/release.sh --yes --skip-wait $CI_COMMIT_TAG
  only:
    - tags
```

### Scheduled Releases

Automate releases on a schedule:

```yaml
# .github/workflows/scheduled-release.yml
on:
  schedule:
    - cron: '0 10 * * 1'  # Every Monday at 10 AM
  
jobs:
  auto-release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Determine version
        id: version
        run: |
          # Logic to determine next version
          echo "version=v1.0.0" >> $GITHUB_OUTPUT
      - name: Release
        run: ./scripts/release.sh --yes ${{ steps.version.outputs.version }}
```

## Rollback

If a release has issues:

```bash
# 1. Don't delete tags (Go proxy has cached them)

# 2. Create hotfix version
./scripts/release.sh v1.0.1

# 3. Mark old version deprecated
gh release edit v1.0.0 \
  --notes "⚠️ Deprecated: Use v1.0.1 instead"
```

## Future Improvements

Potential enhancements:

- [ ] Automatic changelog generation
- [ ] Automatic version bumping (from commit messages)
- [ ] Binary building and attaching to releases
- [ ] Homebrew formula updating
- [ ] Announcement posting (Slack, Discord, etc.)
- [ ] Rollback automation
- [ ] Multi-platform testing before release

## Related Documentation

- [Release Process](./release-process.md) - Manual process (if automation fails)
- [Testing Releases](./testing-releases-quickstart.md) - How to test safely
- [CI/CD Pipelines](./ci-cd-pipelines.md) - All workflows

---

*Last Updated: 2 November 2025*
