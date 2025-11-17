---
title: testing releases
description: DevOps and release management documentation
docType: guide
---

# Testing Release Process Without Polluting Git History

This guide shows you how to safely test the monorepo release process before doing it for real.

## Quick Start - Test Locally (Safest)

```bash
# Run the dry-run script - doesn't modify any files
./scripts/test-release.sh arena v0.0.1-test

# Or test packc
./scripts/test-release.sh packc v0.0.1-test
```

This script will:
- ✅ Show you what would change
- ✅ Test building with modified go.mod
- ✅ Restore everything to original state
- ❌ **NOT** create any tags or commits

## Testing Strategies (Choose One)

### Strategy 1: Local Script (Recommended)

**Best for:** Testing the release process safely without side effects

```bash
# Test locally without any git operations
./scripts/test-release.sh arena v0.0.1-test

# Or test packc
./scripts/test-release.sh packc v0.0.1-test
```

**Pros:**
- ✅ Zero risk - no file modifications
- ✅ Instant feedback
- ✅ No cleanup needed
- ✅ Tests the same build process as production

**Cons:**
- ❌ Doesn't test actual go install from Go proxy
- ❌ Doesn't test Go proxy caching behavior

**Recommendation:** Use this first. It tests 95% of the release process with zero risk.

---

### Strategy 2: Test Tags in Main Repo (Use With Caution)

**Best for:** Testing the actual Go proxy behavior with real tags

**⚠️ WARNING:** Tags are cached by Go proxy after 5-10 minutes and stuck for 24 hours minimum. Only use if you're confident and can delete quickly!

```bash
# Use '-test' suffix - can be deleted if you're FAST (< 5 minutes)
# The -test suffix is a valid pre-release identifier per semantic versioning
# See: https://semver.org/#spec-item-9
git tag runtime/v0.0.1-test
git tag pkg/v0.0.1-test
git tag sdk/v0.0.1-test
git tag tools/arena/v0.0.1-test
git tag tools/packc/v0.0.1-test
git push origin runtime/v0.0.1-test pkg/v0.0.1-test sdk/v0.0.1-test tools/arena/v0.0.1-test tools/packc/v0.0.1-test

# Wait 2-3 minutes, then test
mkdir -p /tmp/test-sdk && cd /tmp/test-sdk
go mod init example.com/test
go get github.com/AltairaLabs/PromptKit/sdk@v0.0.1-test
cd -

go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@v0.0.1-test
go install github.com/AltairaLabs/PromptKit/tools/packc@v0.0.1-test

# If successful, DELETE IMMEDIATELY (within 5 min)
git push origin --delete runtime/v0.0.1-test pkg/v0.0.1-test sdk/v0.0.1-test tools/arena/v0.0.1-test tools/packc/v0.0.1-test
git tag -d runtime/v0.0.1-test pkg/v0.0.1-test sdk/v0.0.1-test tools/arena/v0.0.1-test tools/packc/v0.0.1-test
```

**Pros:**
- ✅ Tests in real repo
- ✅ Tests actual `go install` and `go get`
- ✅ Tests Go proxy behavior
- ✅ Deletable if quick (< 5 min)

**Cons:**
- ⚠️ **DANGEROUS**: Cached by Go proxy after 5-10 minutes
- ⚠️ **RISKY**: Must delete quickly or stuck for 24h
- ⚠️ **NOT RECOMMENDED**: Easy to forget and pollute history

**Recommendation:** Skip this unless you absolutely need to test Go proxy caching. Use Strategy 1 instead.

---

### Strategy 3: GitHub Actions Testing

**Best for:** Testing automation and CI/CD workflows

```bash
# 1. Create a release test branch
git checkout -b release-test/arena-v0.0.1

# 2. Push to trigger the workflow
git push origin release-test/arena-v0.0.1

# 3. Check GitHub Actions tab for results

# 4. Delete branch when done
git push origin --delete release-test/arena-v0.0.1
git branch -d release-test/arena-v0.0.1
```

**Pros:**
- ✅ Tests GitHub Actions workflow
- ✅ No tags created
- ✅ Branches are deletable anytime
- ✅ Can iterate quickly

**Cons:**
- ❌ Doesn't test actual `go install`
- ❌ Doesn't test Go proxy

**Recommendation:** Use after Strategy 1 to test CI/CD automation.

---

## Recommended Testing Flow

### Before First Release

```bash
# Day 1: Test locally (5 minutes)
./scripts/test-release.sh arena v0.0.1-test

# Day 2: Test GitHub Actions (10 minutes)
git checkout -b release-test/arena-v0.0.1
git push origin release-test/arena-v0.0.1
# Check workflow, then delete branch

# Day 3: Do real release
./scripts/release.sh v1.0.0
```

**That's it.** Don't overcomplicate it with test repos or risky tag testing.

---

## Common Pitfalls to Avoid

### ❌ DON'T: Push test tags without '-test' suffix
```bash
# BAD - looks like a real release!
git tag runtime/v0.0.1
git push origin runtime/v0.0.1
```

### ✅ DO: Use -test suffix for experimental tags
```bash
# GOOD - clearly marked as test
git tag runtime/v0.0.1-test
git push origin runtime/v0.0.1-test
```

### ❌ DON'T: Leave test tags for more than 5 minutes
```bash
# If you push a test tag, delete it within 5 minutes
# or it will be cached by Go proxy for 24 hours
```

**Better:** Just use the local test script (Strategy 1) instead of risking it.

---

## Go Proxy Caching Behavior

Important facts about Go's module proxy:

1. **First Request**: When someone runs `go get`, the proxy caches the module
2. **Cache Duration**: Modules are cached for **24 hours** minimum
3. **Cannot Un-publish**: Once cached, you **cannot remove** a version
4. **Test Window**: You have ~5-10 minutes before proxy caches it
5. **Best Practice**: Always test in separate repo or with -test suffix

## Cleanup Commands

### Remove Local Test Tags
```bash
git tag -d runtime/v0.0.1-test
git tag -d pkg/v0.0.1-test
git tag -d sdk/v0.0.1-test
git tag -d tools/arena/v0.0.1-test
git tag -d tools/packc/v0.0.1-test
```

### Remove Remote Test Tags (QUICKLY!)
```bash
# Only works if done within ~5 minutes of pushing
git push origin --delete runtime/v0.0.1-test
git push origin --delete pkg/v0.0.1-test
git push origin --delete sdk/v0.0.1-test
git push origin --delete tools/arena/v0.0.1-test
git push origin --delete tools/packc/v0.0.1-test
```

### Delete Test Branches
```bash
# Local
git branch -d release-test/arena-v0.0.1

# Remote
git push origin --delete release-test/arena-v0.0.1
```

---

## Manual Testing Workflow

For manual testing without scripts:

```bash
# 1. Create a backup branch
git checkout -b backup-before-release-test
git push origin backup-before-release-test

# 2. Create release test branch
git checkout -b release-test-manual

# 3. Modify go.mod in tools/arena
cd tools/arena
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg
cat go.mod  # Review changes

# 4. Try to build (will fail if deps not published)
go build ./...

# 5. Restore original
git checkout go.mod go.sum
cd ../..

# 6. Delete test branch
git checkout main
git branch -D release-test-manual
```

## Success Criteria

Before doing a real release, ensure:

- [ ] Local test script runs successfully
- [ ] GitHub Actions workflow runs without errors
- [ ] Test repository installation works: `go install ...@version`
- [ ] You understand the Go proxy caching behavior
- [ ] You have a rollback plan
- [ ] Documentation is updated with install instructions

## Questions?

- **Q: Can I undo a published tag?**
  - A: No, once cached by Go proxy (5-10 min), it's there for 24h minimum

- **Q: What if I mess up a version?**
  - A: Publish a new patch version (v1.0.1) - don't try to delete/overwrite

- **Q: How long should I wait between dependency and tool tags?**
  - A: 5-10 minutes for Go proxy to cache the dependencies

- **Q: Can I test `go install` without publishing?**
  - A: Yes, use a private test repository or local replace directives

## Next Steps

1. Run `./scripts/test-release.sh arena v0.0.1-test` to understand the process
2. Create a test repository for full integration testing
3. Review the automated release workflow in `.github/workflows/release-test.yml`
4. When ready, follow the real release process in `docs/RELEASE.md` (to be created)
