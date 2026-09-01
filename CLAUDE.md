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
- Check generated API reference drift when staged source feeds it (runtime `types/providers/tools/...` or the `sdk` root / `sdk/agui` / `server/a2a` packages) — mirrors CI's Reference Drift check; fix with `make docs-reference` / `make docs-sdk-reference`

**Commit-msg hook** (`scripts/commit-msg.sh` → `.git/hooks/commit-msg`) enforces DCO:
- Rejects any commit lacking a `Signed-off-by:` trailer matching the author. Commit with `git commit -s`.

**NEVER use `--no-verify` or skip the hooks.** The pre-commit checks mirror what SonarCloud enforces in CI — if the hook fails, the PR will also fail. Fix all issues before committing, including pre-existing issues in files you've touched.

### Before committing
1. Run **`make verify`** — it is exactly what the pre-commit hook and CI run (`lint-diff` + `test-fast`). Do not reach for `golangci-lint run ./...` or `go test ./... -count=1` at the repo root; there is no root module and both fail.
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
| `schemas/v1alpha1/` | JSON schemas, fetched from promptarena and hosted at promptkit.altairalabs.ai |
| `examples/` | Example projects and SDK usage |
| `docs/` | Starlight documentation site |

The **PromptArena** and **PackC** CLIs live in a separate repository,
`github.com/AltairaLabs/promptarena`, which also **generates** the JSON schemas
(from its `arena/arenaconfig` + this repo's `pkg/config` types). PromptKit ships
the Go SDK/runtime libraries, keeps a committed copy of the schemas (its
`pkg/config` validator loads them), and hosts them at promptkit.altairalabs.ai.

## Build & Test Commands

**Use the Makefile. `make help` lists every target.** There is no root `go.mod` — `go.work`
lists the modules — so `go build ./...` and `go test ./...` from the repo root fail with
*"directory prefix . does not contain modules listed in go.work"*. The make targets
iterate the modules for you.

| Target | What it does |
|---|---|
| `make build` / `make test` | Build / test all four modules |
| `make test-race` / `make coverage` | Race detector; coverage report |
| `make verify` | `lint-diff` + `test-fast` — what pre-commit and CI run |
| `make lint` | `go vet` + `go fmt`. **Not golangci-lint** — that's `make lint-diff` |
| `make lint-diff` | golangci-lint on changed code only (`--new-from-rev=HEAD`) |
| `make modules-standalone-check` | Build each module with `GOWORK=off`, as a consumer resolves it — catches dep skew the workspace hides (#1920) |
| `make api-compat-check VERSION=vX.Y.Z` | Check the API changes fit the claimed version |

For a single package, use `go -C <module>` rather than a root-relative path:

```bash
go -C runtime test ./evals/... -count=1
```

### Regenerating

| Target | Regenerates | Check target |
|---|---|---|
| `make schemas` | `schemas/v1alpha1/` from promptarena | `make schemas-check` |
| `make packspec` | `runtime/packspec` pack types from the embedded schema | `make packspec-check` |
| `make promptpack-schema` | the embedded PromptPack schema from its release | `make promptpack-schema-check` |
| `make docs-reference` / `make docs-sdk-reference` | generated API reference pages | `…-check` variants |

### Two things the targets don't cover

- **Schema-validating tests need `PROMPTKIT_SCHEMA_SOURCE=local`.** Unset, `pkg/config`
  validates against the *hosted* schemas, so edits to `schemas/v1alpha1/` are invisible and
  the suite passes regardless of whether the change is correct. CI sets it; local runs must
  too: `env PROMPTKIT_SCHEMA_SOURCE=local go -C pkg test ./config/... -count=1`
- **Nested example modules have their own `go.mod`.** `make build` covers the four
  published modules; it does not compile `sdk/examples/*`, and neither does
  `go -C sdk build ./...`. CI catches breakage there, so check them before claiming green.

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

The JSON schemas are **generated in the promptarena repo** (it owns the
`arena/arenaconfig` types and imports this repo's `pkg/config`). PromptKit is the
host: it keeps a committed copy under `schemas/v1alpha1/` (loaded by
`pkg/config`'s validator and used by promptarena/packc via the hosted URL) and
serves it at `https://promptkit.altairalabs.ai/schemas/{v1alpha1,latest}/`.

- Refresh the committed copy from promptarena: `make schemas` (fetches via `scripts/fetch-schemas.sh`), then commit.
- CI (`schemas.yml`) runs `make schemas-check` — fails if the committed copy drifts from promptarena's generated schemas.
- `PROMPTKIT_SCHEMA_SOURCE=local` validates against in-repo `schemas/v1alpha1/`; a development-only tool that must not appear in shipped docs or example READMEs.

### Generated artifacts — never hand-edit

Three things here are generated, each with a `make` target to regenerate and a `-check`
target CI runs. Editing the output instead of the source is reverted on the next
regeneration and fails that check.

| Artifact | Source of truth | Regenerate |
|---|---|---|
| `schemas/v1alpha1/*.json` | promptarena's `tools/schema-gen` | fix promptarena, then `make schemas` |
| `runtime/prompt/schema/promptpack.schema.json` | the PromptPack spec release — a **verbatim mirror**; runtime divergence belongs in `deliberateOmission`, never in the file | `make promptpack-schema` |
| `runtime/packspec/*.go` | the embedded schema above | `make packspec` |

A generated schema can be **stricter than the spec it came from** — `schema-gen` closes
every `$def` it emits, so where the spec says `additionalProperties: true` the generated
copy may say `false`. Before concluding a rejected field was deliberately removed, check
the spec:

```bash
jq '.properties.metadata.additionalProperties' runtime/prompt/schema/promptpack.schema.json
```

Spec open + generated closed is a generator bug (promptarena#134/#169), not spec alignment.

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
1. **Validate** — semver format, version ordering, optional test suite, and a check that the **previous** release's published `go.mod` files are clean (the post-tag check can only see modules the Go proxy has picked up, and `sdk`/`server/a2a` routinely take longer than the retry budget). If it fails, the old tag cannot be repaired — re-run with `-f ignore_previous_release_check=true` when this release is the roll-forward fix.
2. **Tag libraries** — tags root `vX.Y.Z` and `runtime/` from main (runtime is a leaf — it must not depend on a sibling module, enforced by `scripts/check-runtime-is-leaf.sh`), then bumps `pkg`'s `require` on runtime to `vX.Y.Z` and drops its local `replace` on a `release/libs/` branch before tagging `pkg/` from it
3. **Update & tag SDK modules** — removes `replace` directives, tags `sdk/`, `server/a2a/`
4. **Create GitHub release** — `gh release create` (no binaries); publishing it triggers the docs/schema deployment
5. **Notify downstream** — dispatches `promptkit-release` to the deploy repos

Phases: `full` (all steps), `libs-only` (steps 1-2), `tools-only` (steps 1,3).
