## Pull Request Summary

**Type of Change**
- [ ] 🐛 Bug fix (non-breaking change that fixes an issue)
- [ ] ✨ New feature (non-breaking change that adds functionality)
- [ ] 💥 Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] 📝 Documentation update
- [ ] 🔧 Configuration/infrastructure change
- [ ] ♻️ Code refactoring (no functional changes)
- [ ] ⚡ Performance improvement
- [ ] 🧪 Adding or improving tests

**Component(s) Affected**
- [ ] Arena CLI
- [ ] PackC CLI  
- [ ] SDK
- [ ] Runtime
- [ ] Examples
- [ ] Documentation
- [ ] CI/CD
- [ ] Other: ___________

## Description

**What does this PR do?**
Provide a clear and concise description of what your changes accomplish.

**Why is this change needed?**
Explain the motivation for this change. Link to any related issues.

Fixes #(issue number)
Relates to #(issue number)

## Changes Made

**Code Changes**
- Describe the key code changes made
- Highlight any important implementation details
- Mention any new dependencies added

**Configuration Changes** (if applicable)
- New configuration options added
- Changes to existing configuration
- Migration steps for existing users

## Testing

**Test Coverage**
- [ ] I have added unit tests for my changes
- [ ] I have added integration tests for my changes
- [ ] Existing tests pass with my changes
- [ ] I have tested this manually

**Manual Testing Performed**
Describe how you tested these changes:

```bash
# Commands used for testing
make test
./bin/promptarena run examples/...
```

**Test Results**
- [ ] All automated tests pass
- [ ] Manual testing completed successfully
- [ ] No regressions identified

## Documentation

**Documentation Updates**
- [ ] I have updated relevant documentation
- [ ] I have added/updated code comments
- [ ] I have updated examples if needed
- [ ] No documentation changes needed

**Breaking Changes Documentation** (if applicable)
If this is a breaking change, describe:
- What functionality is affected
- How users should migrate their code
- Version compatibility information

## Code Quality

**Code Review Checklist**
- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Code is well-commented where needed
- [ ] No debug/temporary code included
- [ ] Error handling is appropriate

**Security Considerations** (if applicable)
- [ ] No sensitive information is exposed
- [ ] Input validation is appropriate
- [ ] Security implications have been considered

## Deployment

**Deployment Considerations**
- [ ] No special deployment steps required
- [ ] Database migrations needed
- [ ] Configuration updates required
- [ ] Dependencies need to be updated

**Rollback Plan** (for significant changes)
Describe how to rollback this change if needed.

## Additional Context

**Related Work**
- Links to related PRs or issues
- Dependencies on other changes
- Follow-up work planned

**Screenshots** (if applicable)
Add screenshots for UI changes or CLI output changes.

**Performance Impact** (if applicable)
- Describe any performance implications
- Include benchmark results if relevant

## Reviewer Notes

**Areas of Focus**
Please pay special attention to:
- Specific areas where you'd like focused review
- Complex logic or algorithms
- Security-sensitive code
- Performance-critical sections

**Questions for Reviewers**
- Any specific questions you have about the implementation
- Alternative approaches you considered
- Areas where you're unsure about the approach

---

## Checklist

**Before Submitting**
- [ ] I have signed my commits with `git commit -s`
- [ ] I have read the [Contributing Guidelines](./CONTRIBUTING.md)
- [ ] I have followed the [Code of Conduct](./CODE_OF_CONDUCT.md)
- [ ] My code follows the project's coding standards
- [ ] I have performed a self-review of my code
- [ ] I have commented my code where necessary
- [ ] I have made corresponding changes to documentation
- [ ] My changes generate no new warnings
- [ ] I have added tests that prove my fix is effective or my feature works
- [ ] New and existing unit tests pass locally with my changes

**For Maintainers**
- [ ] PR title follows conventional commit format
- [ ] Appropriate labels have been added
- [ ] Milestone assigned (if applicable)
- [ ] Breaking changes are documented in CHANGELOG.md