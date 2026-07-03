# PromptKit - Claude Code Project Instructions

## Git Workflow

- **Never push directly to main** — main has branch protection enabled.
- Always use feature branches: `feat/<description>` or `feature/<issue-number>-<description>`.
- Standard flow: branch → commit → push with `-u` → create PR via `gh pr create` → monitor CI → merge via `gh pr merge --squash`.
- When continuing a previous session, check `git status`, `git log --oneline -5`, and any existing plan files before taking action.
- **All commits must be signed off (DCO).** Every commit needs a `Signed-off-by: Name <email>` trailer matching the author — always commit with `git commit -s`. The `commit-msg` hook rejects commits without it. To fix the last commit: `git commit --amend -s --no-edit`. When committing via `git commit -F -`, add `-s` as well.

## Git Hooks

Install hooks once after cloning: `./scripts/install-hooks.sh` (sources are tracked under `scripts/`; the installed copies in `.git/hooks/` are not).

**Pre-commit hook** (`scripts/pre-commit.sh` → `.git/hooks/pre-commit`) runs on every commit:
- Lint changed files (`golangci-lint --new-from-rev=HEAD`)
- Build changed modules
- Run tests with coverage on changed packages (80% threshold on non-test files)

**Commit-msg hook** (`scripts/commit-msg.sh` → `.git/hooks/commit-msg`) enforces DCO:
- Rejects any commit lacking a `Signed-off-by:` trailer matching the author. Commit with `git commit -s`.

**NEVER use `--no-verify` or skip the hooks.** The pre-commit checks mirror what SonarCloud enforces in CI — if the hook fails, the PR will also fail. Fix all issues before committing, including pre-existing issues in files you've touched.

### Before committing
1. Run `golangci-lint run ./...` and `go test ./... -count=1` first
2. Fix ALL failures before attempting `git commit`
3. If the pre-commit hook reports lint or coverage failures on pre-existing code in files you changed, fix those too — SonarCloud will flag them

## Project Structure

Go workspace with multiple modules (see `go.work`):

| Path | Purpose |
|------|---------|
| `runtime/` | Core runtime: providers, pipeline, tools, types, a2a protocol, workflow engine |
| `sdk/` | Developer SDK: `Open()`, `OpenDuplex()`, `OpenWorkflow()`, capabilities, options |
| `pkg/` | Shared packages: config, schema validation |
| `server/a2a/` | A2A protocol server module |
| `tools/schema-gen/` | JSON Schema generator for the PromptArena config types |
| `schemas/v1alpha1/` | Generated JSON schemas, hosted at promptkit.altairalabs.ai |
| `examples/` | Example projects and SDK usage |
| `docs/` | Starlight documentation site |

The **PromptArena** and **PackC** CLIs live in a separate repository,
`github.com/AltairaLabs/promptarena`. This repo ships the Go SDK/runtime
libraries and hosts the JSON schemas; `tools/schema-gen` imports the config
types from the published promptarena module to generate them.

## Build & Test Commands

```bash
# Build everything
go build ./...

# Run all tests
go test ./... -count=1

# Run specific module tests
go test ./sdk/... -v -race -count=1
go test ./runtime/... -v -race -count=1

# Lint
golangci-lint run ./...

# Regenerate JSON schemas (after a PromptArena config-type change lands in the
# published promptarena module) — schemas stay hosted at promptkit.altairalabs.ai
go run ./tools/schema-gen/...   # or: make schemas

# Build the schema generator
make build-schema-gen
```

## SDK Architecture

### Capability System
- `Capability` interface: `Name()`, `Init(CapabilityContext)`, `RegisterTools(*tools.Registry)`, `Close()`
- `WorkflowCapability` — auto-inferred from `pack.Workflow`
- `A2ACapability` — auto-inferred from `pack.Agents`, or created via `WithA2ATools()` bridge
- `inferCapabilities()` in `sdk/capability.go` auto-detects from pack structure
- Capabilities register tools during pipeline construction in `buildPipelineWithParams()` / `buildStreamPipelineWithParams()`

### Key SDK patterns
- `Conversation` struct in `sdk/conversation.go` is the central type
- `Open()` / `OpenDuplex()` in `sdk/sdk.go` are the entry points
- Options pattern via `sdk/options.go`
- A2A agent tools and pack-based agent tools are unified under `A2ACapability`
- `packToRuntimePack()` converts SDK internal pack types to runtime prompt.Pack

### Circular dependency: `runtime/a2a` cannot import `sdk`
Interfaces like `Conversation` and `StreamingConversation` are defined in `a2a`; `sdk` callers wrap their implementations.

## Schemas

PromptKit generates and hosts the PromptArena JSON schemas. The config types
live in the published `github.com/AltairaLabs/promptarena` module; `tools/schema-gen`
reflects them into `schemas/v1alpha1/`.

- Schemas are served from `https://promptkit.altairalabs.ai/schemas/v1alpha1/`.
- Regenerate after a schema-relevant change: `go run ./tools/schema-gen/...` (or `make schemas`). The generated output must stay byte-identical unless the promptarena config types changed.
- `PROMPTKIT_SCHEMA_SOURCE=local` validates against in-repo `schemas/v1alpha1/` before publishing; it is a development-only tool and must not appear in shipped docs or example READMEs.

## Concurrent Agents and Worktrees

When running in a worktree or when concurrent agents may operate on the repo, **always use `git -C <path>` instead of `cd <path> && git ...`**. Compound `cd && git` commands require extra approval to prevent bare repository attacks, whereas `git -C` is safe and non-interactive.

```bash
# Good — works in worktrees and concurrent agents
git -C /Users/chaholl/repos/altairalabs/promptkit push
git -C /Users/chaholl/repos/altairalabs/promptkit log --oneline -5

# Bad — requires approval, breaks in some worktree contexts
cd /Users/chaholl/repos/altairalabs/promptkit && git push
```

## Go Code Standards

- **golangci-lint** config in `.golangci.yml` — line length 120, linters include errcheck, gocritic, gosec, govet, revive, staticcheck, unused
- **Test naming**: Always check for name collisions across `_test.go` files in the same package before naming types/functions
- **Formatting**: `gofmt` and `goimports` are enforced

## SonarCloud Quality Gate (CI)

SonarCloud runs on every PR and enforces quality on new code:
- Coverage >= 80% on new/changed lines
- Duplicated lines <= 3%
- Reliability, Security, Maintainability ratings: A

## Releases

**Never create releases manually** (no `gh release create`). Use the release pipeline:

```bash
# Trigger a full release (tags all library modules, creates a GitHub release)
gh workflow run release.yml -f version=v1.3.9 -f phase=full

# Re-run just the SDK/server module tagging (if runtime/pkg were already tagged)
gh workflow run release.yml -f version=v1.3.9 -f phase=tools-only

# Skip tests (use only if CI already passed on main)
gh workflow run release.yml -f version=v1.3.9 -f phase=full -f skip_tests=true
```

PromptKit ships Go library modules only — the PromptArena/PackC CLIs (and their
binaries, npm packages and Homebrew casks) release from the separate
`github.com/AltairaLabs/promptarena` repo. The release workflow
(`.github/workflows/release.yml`) handles:
1. **Validate** — semver format, version ordering, optional test suite
2. **Tag libraries** — tags `runtime/`, `pkg/`, and root `vX.Y.Z`, verifies Go proxy propagation
3. **Update & tag SDK modules** — removes `replace` directives, tags `sdk/`, `server/a2a/`
4. **Create GitHub release** — `gh release create` (no binaries); publishing it triggers the docs/schema deployment
5. **Notify downstream** — dispatches `promptkit-release` to the deploy repos

Phases: `full` (all steps), `libs-only` (steps 1-2), `tools-only` (steps 1,3).
