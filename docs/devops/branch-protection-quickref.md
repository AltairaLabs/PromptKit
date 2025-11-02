# Branch Protection Quick Reference

Quick guide for developers working with protected branches.

## 🚫 What You Can't Do

- Push directly to `main` branch
- Force push to any protected branch
- Delete protected branches or tags
- Bypass CI checks
- Merge without required reviews

## ✅ What You Should Do

### Making Changes

1. **Create a feature branch** from `main`:
   ```bash
   git checkout main
   git pull origin main
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** and commit:
   ```bash
   git add .
   git commit -s -m "feat: your descriptive message"
   ```
   
   **Note**: `-s` flag signs your commit (required!)

3. **Push to your fork**:
   ```bash
   git push origin feature/your-feature-name
   ```

4. **Open a Pull Request** on GitHub

### PR Requirements Checklist

Before your PR can be merged:

- [ ] All CI checks pass (test, lint, build, coverage)
- [ ] At least 1 approving review from a maintainer
- [ ] All review comments are resolved
- [ ] Commits are signed (DCO)
- [ ] Branch is up-to-date with `main`

### Required Status Checks

Your PR must pass these automated checks:

| Check | Description | Fix If Failing |
|-------|-------------|----------------|
| `test` | Unit tests | Run `make test` locally |
| `lint` | Code linting | Run `make lint` locally |
| `build` | Build verification | Run `make build` locally |
| `coverage` | Coverage + SonarCloud | Check test coverage |

### Getting Your PR Merged

1. **Wait for CI** - All checks must pass
2. **Request review** - Tag appropriate reviewers (see CODEOWNERS)
3. **Address feedback** - Respond to all review comments
4. **Resolve conflicts** - Keep branch up-to-date with main
5. **Get approval** - At least 1 approval required
6. **Merge** - Squash merge (recommended) or rebase

## 🔧 Common Issues

### "Commits must be signed"

Your commits need a DCO sign-off:

```bash
# Sign existing commits
git rebase --exec 'git commit --amend --no-edit -n -s' -i origin/main

# Force push to your branch
git push --force-with-lease
```

### "Branch is out of date"

Update your branch with latest main:

```bash
git checkout main
git pull origin main
git checkout your-branch
git rebase main
git push --force-with-lease
```

### "CI checks failing"

Run tests locally before pushing:

```bash
# Run all checks
make test
make lint
make build

# Fix linting issues automatically
make fmt
```

### "Need code owner review"

Some files require review from specific teams (see `.github/CODEOWNERS`):

- Runtime changes → Runtime team
- SDK changes → SDK team  
- CI/CD changes → DevOps team
- Documentation → Docs team

The appropriate reviewers will be automatically assigned.

### "Conversation not resolved"

All review comments must be marked as resolved:

1. Address the comment (make changes or respond)
2. Click "Resolve conversation" button
3. Wait for reviewer to confirm

## 🏷️ Tags

Version tags are protected and immutable:

- Created automatically by release workflow
- Cannot be deleted once created
- Cannot be moved to different commit
- Pattern: `v*`, `runtime/v*`, `sdk/v*`, etc.

## 🆘 Emergency Procedures

For critical production issues:

1. Contact a repository admin
2. Admin can temporarily adjust protections
3. Emergency fix must be documented
4. Post-incident review required
5. Retroactive PR created

## 📚 More Information

- Full guide: [branch-protection.md](branch-protection.md)
- Contributing: [CONTRIBUTING.md](../../CONTRIBUTING.md)
- Release process: [release-process.md](release-process.md)
- CI/CD pipelines: [ci-cd-pipelines.md](ci-cd-pipelines.md)

## 💡 Tips

- **Smaller PRs** are reviewed faster
- **Clear descriptions** help reviewers understand context
- **Passing CI locally** before pushing saves time
- **Signing commits upfront** avoids rework
- **Descriptive commit messages** help with changelog generation
