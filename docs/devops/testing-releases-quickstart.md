# Quick Start: Test Release Process

## TL;DR - Safe Testing

```bash
# Run this to test without any git changes:
./scripts/test-release.sh arena v0.0.1-test
```

## Three Safe Ways to Test

### 1. 🧪 Local Dry Run (Start Here)

**Zero risk - just shows what would happen**

```bash
./scripts/test-release.sh arena v0.0.1-test
```

Outputs:
- Current go.mod
- Modified go.mod (without replace directives)  
- Build test results
- Step-by-step release commands

**Does NOT:**
- Create any tags
- Modify any files
- Push anything to GitHub

---

### 2. 🔬 Test Repository (Full Testing)

**Best for testing `go install` - completely isolated**

```bash
# One-time setup
gh repo create AltairaLabs/promptkit-release-test --private --clone
cd promptkit-release-test
cp -r ../PromptKit/* .
git add .
git commit -m "Initial test setup"
git push origin main

# Now test freely!
git tag runtime/v0.0.1-test
git tag pkg/v0.0.1-test
git push origin runtime/v0.0.1-test pkg/v0.0.1-test

# ... continue with tool release ...

# When done, delete everything
cd ..
gh repo delete AltairaLabs/promptkit-release-test --yes
```

---

### 3. 🌿 Test Branch (CI/CD Testing)

**Tests GitHub Actions without creating tags**

```bash
# Create test branch
git checkout -b release-test/arena-v0.0.1
git push origin release-test/arena-v0.0.1

# Watch the workflow run in GitHub Actions

# Clean up
git push origin --delete release-test/arena-v0.0.1
git checkout main
git branch -d release-test/arena-v0.0.1
```

---

## What NOT to Do

### ❌ Don't use regular tags for testing

```bash
# BAD - gets cached by Go proxy in 5-10 minutes!
git tag runtime/v0.0.1
git push origin runtime/v0.0.1
# ^ This will be stuck for 24 hours if you change your mind
```

### ✅ Do use test/ prefix if you must tag

```bash
# BETTER - clearly marked as test
git tag test/runtime/v0.0.1  
git push origin test/runtime/v0.0.1

# Delete within 5 minutes if needed
git push origin --delete test/runtime/v0.0.1
```

### ✅ Best: Use separate test repo

```bash
# BEST - completely isolated, delete anytime
gh repo create promptkit-release-test --private
```

---

## Ready for Real Release?

Once testing looks good:

1. See **[release-process.md](./release-process.md)** for the complete step-by-step production release guide
2. Use semantic versions: `v1.0.0`, `v1.0.1`, etc.
3. Never use `test/` prefix for production releases

---

## Need Help?

- Full testing guide: `docs/devops/testing-releases.md`
- Release process: `docs/devops/release-process.md` (to be created)
- Run test script: `./scripts/test-release.sh`
