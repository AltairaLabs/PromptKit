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

### Strategy 1: Local Script (Recommended First)

**Best for:** Initial testing and understanding the process

```bash
# Test locally without any git operations
./scripts/test-release.sh arena v0.0.1-test
```

**Pros:**
- ✅ Zero risk
- ✅ Instant feedback
- ✅ No cleanup needed

**Cons:**
- ❌ Doesn't test actual go install
- ❌ Doesn't test Go proxy behavior

---

### Strategy 2: Test Repository (Recommended for Full Testing)

**Best for:** Testing the complete workflow including `go install` and SDK

```bash
# 1. Create a private test repo
gh repo create AltairaLabs/promptkit-release-test --private --clone

# 2. Copy your monorepo
cd promptkit-release-test
git remote add upstream https://github.com/AltairaLabs/PromptKit.git
git pull upstream main
git push origin main

# 3. Test the full release process there (all libraries)
git tag runtime/v0.0.1-test
git tag pkg/v0.0.1-test
git tag sdk/v0.0.1-test
git push origin runtime/v0.0.1-test pkg/v0.0.1-test sdk/v0.0.1-test

# Wait 5 minutes, test SDK installation
go get github.com/AltairaLabs/PromptKit/sdk@v0.0.1-test

# Then continue with tool release...
```

**Pros:**
- ✅ Tests complete workflow
- ✅ Tests `go install` for real
- ✅ Isolated from main repo
- ✅ Can delete entire repo when done

**Cons:**
- ❌ Requires creating separate repo
- ❌ More setup time

---

### Strategy 3: Test Tags with Prefix

**Best for:** Testing in main repo with deletable tags

**⚠️ WARNING:** Only works if you push test tags immediately. Once Go proxy caches a tag (5-10 min), it's there for 24 hours even if you delete it!

```bash
# Use 'test/' prefix - can be deleted quickly
git tag test/runtime/v0.0.1
git tag test/pkg/v0.0.1
git tag test/sdk/v0.0.1
git push origin test/runtime/v0.0.1 test/pkg/v0.0.1 test/sdk/v0.0.1

# Wait 2 minutes (NOT 10!), then test SDK
go get github.com/AltairaLabs/PromptKit/sdk@test/sdk/v0.0.1

# Test tool installation
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@test/tools/arena/v0.0.1

# If it doesn't work, delete IMMEDIATELY (within 5 min)
git push origin --delete test/runtime/v0.0.1 test/pkg/v0.0.1 test/sdk/v0.0.1
git tag -d test/runtime/v0.0.1 test/pkg/v0.0.1 test/sdk/v0.0.1
```

**Pros:**
- ✅ Tests in real repo
- ✅ Tests actual `go install`
- ✅ Deletable (if quick)

**Cons:**
- ⚠️ Cached by Go proxy after 5-10 minutes
- ⚠️ Must be deleted quickly or stuck for 24h
- ⚠️ Risky if you forget

---

### Strategy 4: Release Test Branch + GitHub Actions

**Best for:** Testing automation and CI/CD

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
- ✅ Branches are deletable
- ✅ Can iterate quickly

**Cons:**
- ❌ Doesn't test actual `go install`
- ❌ Doesn't test Go proxy

---

## Recommended Testing Flow

### Phase 1: Learn the Process (Day 1)
```bash
# Test locally first
./scripts/test-release.sh arena v0.0.1-test
```

### Phase 2: Test Automation (Day 1)
```bash
# Test GitHub Actions
git checkout -b release-test/arena-v0.0.1
git push origin release-test/arena-v0.0.1
# Check workflow, then delete branch
```

### Phase 3: Full Integration Test (Day 2)
```bash
# Create test repo
gh repo create AltairaLabs/promptkit-release-test --private --clone
# Test complete release process there
# Delete repo when done
```

### Phase 4: Real Release (Day 3+)
```bash
# Now do it for real in main repo
# Use semantic version: v1.0.0
```

## Common Pitfalls to Avoid

### ❌ DON'T: Push test tags without 'test/' prefix
```bash
# BAD - will be cached by Go proxy!
git tag runtime/v0.0.1
git push origin runtime/v0.0.1
```

### ✅ DO: Use test prefix for experimental tags
```bash
# GOOD - clearly marked as test
git tag test/runtime/v0.0.1
git push origin test/runtime/v0.0.1
```

### ❌ DON'T: Leave test tags for more than 5 minutes
```bash
# If you push a test tag, delete it within 5 minutes
# or it will be cached by Go proxy for 24 hours
```

### ✅ DO: Use a separate test repository for thorough testing
```bash
# Much safer approach
gh repo create promptkit-release-test --private
```

## Go Proxy Caching Behavior

Important facts about Go's module proxy:

1. **First Request**: When someone runs `go get`, the proxy caches the module
2. **Cache Duration**: Modules are cached for **24 hours** minimum
3. **Cannot Un-publish**: Once cached, you **cannot remove** a version
4. **Test Window**: You have ~5-10 minutes before proxy caches it
5. **Best Practice**: Always test in separate repo or with test/ prefix

## Cleanup Commands

### Remove Local Test Tags
```bash
git tag -d test/runtime/v0.0.1
git tag -d test/pkg/v0.0.1
git tag -d test/sdk/v0.0.1
git tag -d test/tools/arena/v0.0.1
```

### Remove Remote Test Tags (QUICKLY!)
```bash
# Only works if done within ~5 minutes of pushing
git push origin --delete test/runtime/v0.0.1
git push origin --delete test/pkg/v0.0.1
git push origin --delete test/sdk/v0.0.1
git push origin --delete test/tools/arena/v0.0.1
```

### Delete Test Branches
```bash
# Local
git branch -d release-test/arena-v0.0.1

# Remote
git push origin --delete release-test/arena-v0.0.1
```

### Delete Test Repository
```bash
gh repo delete AltairaLabs/promptkit-release-test --yes
```

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
