# Release Process

This document describes how to publish production releases of PromptKit tools (arena and packc) that can be installed via `go install`.

## Prerequisites

Before starting a release:

- [ ] All tests passing on main branch
- [ ] Documentation is up to date
- [ ] CHANGELOG is updated (if exists)
- [ ] You have tested the release process (see [testing-releases-quickstart.md](./testing-releases-quickstart.md))
- [ ] You understand Go proxy caching (versions are permanent!)

## Understanding Monorepo Releases

PromptKit uses a monorepo structure with internal dependencies:

```
tools/arena  → depends on → runtime, pkg
tools/packc  → depends on → runtime, pkg
```

**Key Principle:** Dependencies must be tagged and published **before** the tools that depend on them.

## Release Order

1. **Dependencies First:** `runtime` and `pkg` modules
2. **Wait for Go Proxy:** 5-10 minutes for caching
3. **Tools Second:** `arena` and `packc` CLI tools

## Semantic Versioning

Use semantic versioning (semver) for all releases:

- **Major** (v1.0.0): Breaking changes
- **Minor** (v1.1.0): New features, backward compatible
- **Patch** (v1.0.1): Bug fixes, backward compatible

**Important:** All modules should use the same version number for consistency.

## Step-by-Step Release Process

### Phase 1: Prepare the Release

#### 1. Update Version Numbers

```bash
# Create release preparation branch
git checkout main
git pull origin main
git checkout -b release/v1.0.0

# Update version in code (if applicable)
# - Update version constants
# - Update documentation
# - Update examples
```

#### 2. Update Documentation

```bash
# Update CHANGELOG.md (create if doesn't exist)
cat >> CHANGELOG.md << EOF

## [1.0.0] - $(date +%Y-%m-%d)

### Added
- List new features

### Changed
- List changes

### Fixed
- List bug fixes
EOF

# Commit changes
git add CHANGELOG.md
git commit -m "docs: prepare for v1.0.0 release"
git push origin release/v1.0.0
```

#### 3. Create and Merge PR

```bash
# Create PR
gh pr create --title "Release v1.0.0" --body "Release preparation for v1.0.0"

# Get approval and merge
# Wait for CI to pass
gh pr merge --squash
```

### Phase 2: Tag Dependencies

#### 1. Ensure Main is Up to Date

```bash
git checkout main
git pull origin main
```

#### 2. Tag Runtime Module

```bash
# Tag runtime
git tag runtime/v1.0.0 -m "Release runtime v1.0.0"
git push origin runtime/v1.0.0

# Verify tag
git tag -l "runtime/*"
```

#### 3. Tag Pkg Module

```bash
# Tag pkg
git tag pkg/v1.0.0 -m "Release pkg v1.0.0"
git push origin pkg/v1.0.0

# Verify tag
git tag -l "pkg/*"
```

#### 4. Verify Dependencies are Published

```bash
# Wait 5-10 minutes, then check Go proxy
curl https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/runtime/@v/v1.0.0.info

curl https://proxy.golang.org/github.com/!altaira!labs/!prompt!kit/pkg/@v/v1.0.0.info
```

### Phase 3: Update Tool Dependencies

#### 1. Create Tool Release Branch

```bash
# Create new branch for tool release
git checkout -b release/tools/arena/v1.0.0
```

#### 2. Update Arena go.mod

```bash
cd tools/arena

# Set explicit version requirements BEFORE dropping replace
go mod edit -require="github.com/AltairaLabs/PromptKit/runtime@v1.0.0"
go mod edit -require="github.com/AltairaLabs/PromptKit/pkg@v1.0.0"

# Now remove replace directives
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg

# Download and clean up dependencies
go mod download
go mod tidy

# Verify build works
go build -v ./...

cd ../..
```

#### 3. Update Packc go.mod (Same Process)

```bash
cd tools/packc

# Set explicit version requirements BEFORE dropping replace
go mod edit -require="github.com/AltairaLabs/PromptKit/runtime@v1.0.0"
go mod edit -require="github.com/AltairaLabs/PromptKit/pkg@v1.0.0"

# Now remove replace directives
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg

# Download and clean up dependencies
go mod download
go mod tidy

# Verify build works
go build -v ./...

cd ../..
```

#### 4. Commit Tool Changes

```bash
# Add modified go.mod and go.sum files
git add tools/arena/go.mod tools/arena/go.sum
git add tools/packc/go.mod tools/packc/go.sum

# Commit
git commit -m "release: update tools to runtime and pkg v1.0.0"

# Push release branch
git push origin release/tools/arena/v1.0.0
```

### Phase 4: Tag and Publish Tools

#### 1. Tag Arena

```bash
# Tag arena tool
git tag tools/arena/v1.0.0 -m "Release arena v1.0.0"
git push origin tools/arena/v1.0.0

# Verify tag
git tag -l "tools/arena/*"
```

#### 2. Tag Packc

```bash
# Tag packc tool
git tag tools/packc/v1.0.0 -m "Release packc v1.0.0"
git push origin tools/packc/v1.0.0

# Verify tag
git tag -l "tools/packc/*"
```

### Phase 5: Verify Release

#### 1. Wait for Go Proxy

Wait 5-10 minutes for Go proxy to cache the new versions.

#### 2. Test Installation

```bash
# Test arena installation
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@v1.0.0

# Test packc installation
go install github.com/AltairaLabs/PromptKit/tools/packc@v1.0.0

# Verify versions
promptarena --version
packc --version
```

#### 3. Test Functionality

```bash
# Run quick smoke tests
cd examples/customer-support
promptarena run

# Test packc
cd examples/customer-support
packc build
```

### Phase 6: Restore Development Setup

#### 1. Restore Replace Directives on Main

```bash
# Checkout main
git checkout main

# Ensure replace directives are still present for development
cat tools/arena/go.mod | grep "replace"

# If missing, restore them
cd tools/arena
go mod edit -replace=github.com/AltairaLabs/PromptKit/runtime=../../runtime
go mod edit -replace=github.com/AltairaLabs/PromptKit/pkg=../../pkg
go mod tidy

cd ../packc
go mod edit -replace=github.com/AltairaLabs/PromptKit/runtime=../../runtime
go mod edit -replace=github.com/AltairaLabs/PromptKit/pkg=../../pkg
go mod tidy

cd ../..

# Commit if needed
git add tools/*/go.mod tools/*/go.sum
git commit -m "chore: restore replace directives for development"
git push origin main
```

### Phase 7: Create GitHub Release

#### 1. Create Release Notes

```bash
# Create GitHub release with notes
gh release create v1.0.0 \
  --title "PromptKit v1.0.0" \
  --notes "$(cat << EOF
# PromptKit v1.0.0

## Installation

\`\`\`bash
# Install Arena
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@v1.0.0

# Install PackC
go install github.com/AltairaLabs/PromptKit/tools/packc@v1.0.0
\`\`\`

## What's Changed

- Feature 1
- Feature 2
- Bug fix 1

## Full Changelog

See [CHANGELOG.md](./CHANGELOG.md)
EOF
)"
```

#### 2. Attach Binaries (Optional)

```bash
# Build binaries for multiple platforms
make build-tools

# Attach to release
gh release upload v1.0.0 bin/*
```

### Phase 8: Announce Release

- [ ] Update README.md with new version
- [ ] Post to GitHub Discussions
- [ ] Update documentation site
- [ ] Announce on social media (if applicable)

## Quick Reference Commands

```bash
# Complete release flow
VERSION=v1.0.0

# 1. Tag dependencies
git tag runtime/$VERSION -m "Release runtime $VERSION"
git tag pkg/$VERSION -m "Release pkg $VERSION"
git push origin runtime/$VERSION pkg/$VERSION

# 2. Wait 10 minutes

# 3. Update tool dependencies
cd tools/arena
go mod edit -require="github.com/AltairaLabs/PromptKit/runtime@$VERSION"
go mod edit -require="github.com/AltairaLabs/PromptKit/pkg@$VERSION"
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg
go mod download
go mod tidy

cd ../packc
go mod edit -require="github.com/AltairaLabs/PromptKit/runtime@$VERSION"
go mod edit -require="github.com/AltairaLabs/PromptKit/pkg@$VERSION"
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/runtime
go mod edit -dropreplace=github.com/AltairaLabs/PromptKit/pkg
go mod download
go mod tidy

cd ../..

# 4. Commit and tag tools
git checkout -b release/tools/$VERSION
git add tools/*/go.mod tools/*/go.sum
git commit -m "release: update tools to $VERSION"
git push origin release/tools/$VERSION

git tag tools/arena/$VERSION -m "Release arena $VERSION"
git tag tools/packc/$VERSION -m "Release packc $VERSION"
git push origin tools/arena/$VERSION tools/packc/$VERSION

# 5. Wait 10 minutes and test
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@$VERSION
go install github.com/AltairaLabs/PromptKit/tools/packc@$VERSION
```

## Troubleshooting

### "Package not found" Error

**Problem:** `go install` fails with package not found.

**Solution:**
1. Verify tags exist: `git tag -l`
2. Wait longer for Go proxy (up to 10 minutes)
3. Check proxy manually: `curl https://proxy.golang.org/...`

### Build Fails After Removing Replace Directives

**Problem:** `go build` fails after removing replace directives.

**Solution:**
1. Ensure dependencies are tagged first
2. Wait for Go proxy to cache
3. Try: `go clean -modcache && go build`

### Wrong Version Installed

**Problem:** `go install` installs old version.

**Solution:**
```bash
# Clear module cache
go clean -modcache

# Force specific version
go install github.com/AltairaLabs/PromptKit/tools/arena/cmd/promptarena@v1.0.0
```

### Need to Fix a Bad Release

**Problem:** Released wrong version or buggy version.

**Solution:**
1. **DO NOT delete tags** - Go proxy has cached them
2. **Create patch release:** v1.0.1 with fixes
3. **Communicate:** Update release notes, mark old version as deprecated

## Rollback Procedure

If a release has critical issues:

```bash
# 1. Create hotfix branch
git checkout -b hotfix/v1.0.1

# 2. Apply fixes
# ... make changes ...

# 3. Follow normal release process with patch version
# Tag: v1.0.1

# 4. Update GitHub release to mark v1.0.0 as deprecated
gh release edit v1.0.0 --notes "⚠️ Deprecated: Use v1.0.1 instead"
```

## Automation (Future)

Consider automating this process with:

- GitHub Actions workflow triggered by version tags
- Automated changelog generation
- Automated binary building and uploading
- Automated announcement posting

See `docs/devops/release-automation.md` (to be created) for future automation plans.

## Checklist Template

Copy this for each release:

```markdown
## Release vX.Y.Z Checklist

### Pre-Release
- [ ] All tests passing
- [ ] Documentation updated
- [ ] CHANGELOG updated
- [ ] Tested release process in test repo

### Dependencies
- [ ] Tagged runtime/vX.Y.Z
- [ ] Tagged pkg/vX.Y.Z
- [ ] Verified on Go proxy (waited 10 min)

### Tools
- [ ] Updated arena go.mod
- [ ] Updated packc go.mod
- [ ] Built successfully
- [ ] Tagged tools/arena/vX.Y.Z
- [ ] Tagged tools/packc/vX.Y.Z

### Verification
- [ ] Installed via go install
- [ ] Tested arena functionality
- [ ] Tested packc functionality

### Post-Release
- [ ] Restored replace directives on main
- [ ] Created GitHub release
- [ ] Announced release
- [ ] Updated documentation site

### Cleanup
- [ ] Deleted release branches
- [ ] Verified all tags pushed
- [ ] Updated README install instructions
```

## Related Documentation

- [Testing Releases](./testing-releases-quickstart.md) - How to test safely first
- [CI/CD Pipelines](./ci-cd-pipelines.md) - Automated workflows
- [Release Test Workflow](../../.github/workflows/release-test.yml) - Test automation

## Support

For release issues:

1. Check troubleshooting section above
2. Review [testing releases guide](./testing-releases.md)
3. Create GitHub issue with `release` label
4. Contact @maintainers for urgent issues

---

*Last Updated: 2 November 2025*
