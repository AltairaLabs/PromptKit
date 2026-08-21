# AI Agent Instructions for PromptKit Development

## 🚨 CRITICAL RULES - NEVER VIOLATE THESE

### Pre-Commit Hook
**NEVER, EVER bypass or skip the pre-commit hook.**

- ❌ NEVER use `[skip-pre-commit]` in commit messages
- ❌ NEVER suggest skipping linting errors
- ❌ NEVER commit with `--no-verify`
- ✅ ALWAYS fix linting errors before committing
- ✅ ALWAYS meet coverage requirements (≥80% per file)
- ✅ ALWAYS ensure tests pass

**If pre-commit fails:**
1. Read the error output carefully
2. Fix the actual issues (linting, coverage, tests)
3. Re-run the commit
4. Repeat until it passes

**Common fixes:**
- **Cognitive complexity**: Refactor into smaller functions
- **Coverage**: Add missing test cases
- **Linting**: Fix the specific golangci-lint errors

### Quality Gates (Enforced by Pre-Commit)

1. **Test Coverage**: ≥80% per file for changed files
2. **Linting**: Zero golangci-lint errors (no warnings exceptions)
3. **Tests**: All tests must pass
4. **Build**: Code must compile without errors
5. **Code Duplication**: <3% (checked by SonarCloud)

### Cognitive Complexity Limit: 15

When functions exceed cognitive complexity of 15:
- Extract helper functions
- Simplify nested conditionals
- Use early returns
- Break complex logic into smaller pieces

**Example refactoring:**
```go
// BAD: High complexity
func processData(data Data) error {
    if data.IsValid() {
        if data.HasItems() {
            for _, item := range data.Items {
                if item.NeedsProcessing() {
                    if err := process(item); err != nil {
                        return err
                    }
                }
            }
        }
    }
    return nil
}

// GOOD: Lower complexity
func processData(data Data) error {
    if !data.IsValid() || !data.HasItems() {
        return nil
    }
    return processItems(data.Items)
}

func processItems(items []Item) error {
    for _, item := range items {
        if err := processItem(item); err != nil {
            return err
        }
    }
    return nil
}

func processItem(item Item) error {
    if !item.NeedsProcessing() {
        return nil
    }
    return process(item)
}
```

## Development Workflow

### Making Changes
1. Create feature branch: `feature/<issue-number>-<description>`
2. Make changes with tests
3. Run `make verify` to check locally
4. Commit (pre-commit hook runs automatically)
5. Fix any issues and recommit
6. Push to GitHub

### Code Architecture Constraints

- **No Runtime/SDK changes** without explicit permission
- **Separate executors** for different execution modes (don't pollute with conditionals)
- **Composite pattern** for routing between executors
- **Registry pattern** for extensible components

### Commit Message Format
```
<type>: <description>

- Bullet point summary
- Of key changes
- And important details
```

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

## Testing Strategy

### Required Tests
- Happy path scenarios
- Error cases
- Edge cases (nil, empty, invalid)
- Integration scenarios

### Mock Strategy
- Use interfaces for dependencies
- Create simple mock implementations in test files
- Test behavior, not implementation

## Tools & Commands

- `make verify` - Run all checks locally
- `make test` - Run all tests
- `make lint` - Run linter
- `go test ./... -coverprofile=coverage.out` - Check coverage
- `golangci-lint run ./path/to/package` - Lint specific package

## Remember

The pre-commit hook exists to maintain code quality and catch issues early. **It's not optional.** Every violation you fix makes the codebase better. Every bypass you avoid maintains the project's integrity.

**When in doubt: FIX, don't SKIP.**

## Documentation is published without review

Anything under `docs/` reaches readers on the next build, and there is no review
step between writing it and publishing it. Write for someone outside the project.

- **No internal tracking.** Issue numbers, "Day N of the roadmap", milestone
  names. If a limitation is worth documenting, describe the limitation.
- **No links a reader cannot follow.** Local-only backlog files, proposals cited
  by section number.
- **No working notes.** "Spike", "v1/v2", "deferred to", single-trial findings
  written as if settled.
- **No competitor comparisons.** Say what this does and when it fits. Naming
  another tool to say it is worse ages badly and is often wrong on the details;
  naming one as a vocabulary reference ("if you know X's terminology") is fine.
- **Run the commands.** Every flag in a doc is a claim about the CLI. Check them;
  flags get renamed and docs do not notice.
- **No issue links, open or closed.** Docs are not a tracker view. An issue cited
  as a pending limitation becomes a lie the moment it closes, and nothing in the
  docs build notices — four voice caveats pointed at issues closed two months
  earlier, telling readers a shipped feature was missing.

**The other half of this rule lives on the issue.** If closing an issue would
make a documented statement untrue, updating those docs belongs in that issue's
acceptance criteria. Docs carry no links back to tickets, so the link has to run
ticket → docs or it does not exist.

`.claude/skills/docs-review/` has the full review procedure.

