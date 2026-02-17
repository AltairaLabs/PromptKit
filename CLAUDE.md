# PromptKit - Claude Code Project Instructions

## Git Workflow

- **Never push directly to main** — main has branch protection enabled.
- Always use feature branches: `feat/<description>` or `feature/<issue-number>-<description>`.
- Standard flow: branch → commit → push with `-u` → create PR via `gh pr create` → monitor CI → merge via `gh pr merge --squash`.
- When continuing a previous session, check `git status`, `git log --oneline -5`, and any existing plan files before taking action.

## Pre-commit Hooks

The repo has a pre-commit hook at `.git/hooks/pre-commit` that runs on every commit:
- Lint changed files (`golangci-lint --new-from-rev=HEAD`)
- Build changed modules
- Run tests with coverage on changed packages (80% threshold on non-test files)
- Skip with `[skip-pre-commit]` in commit message

### Before committing
1. Run `golangci-lint run ./...` and `go test ./... -count=1` first
2. Fix ALL failures before attempting `git commit`

## Project Structure

Go workspace with multiple modules (see `go.work`):

| Path | Purpose |
|------|---------|
| `runtime/` | Core runtime: providers, pipeline, tools, types, a2a protocol, workflow engine |
| `sdk/` | Developer SDK: `Open()`, `OpenDuplex()`, `OpenWorkflow()`, capabilities, options |
| `pkg/` | Shared packages: config, schema validation |
| `tools/arena/` | PromptArena — prompt testing/evaluation framework |
| `tools/packc/` | Pack compiler CLI |
| `tools/schema-gen/` | JSON Schema generator for config types |
| `tools/inspect-state/` | State store inspection tool |
| `schemas/v1alpha1/` | Generated JSON schemas for Arena config types |
| `examples/` | Example packs, scenarios, and SDK usage |
| `docs/` | Starlight documentation site |

## Build & Test Commands

```bash
# Build everything
go build ./...

# Run all tests
go test ./... -count=1

# Run specific module tests
go test ./sdk/... -v -race -count=1
go test ./runtime/... -v -race -count=1
go test ./tools/arena/... -v -race -count=1

# Lint
golangci-lint run ./...

# Regenerate JSON schemas (after changing Arena config types)
go run ./tools/schema-gen/...

# Build tools
go build -o bin/promptarena ./tools/arena/cmd/promptarena
go build -o bin/packc ./tools/packc
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

## PromptArena

### Running examples
```bash
# Workflow examples (require PROMPTKIT_SCHEMA_SOURCE=local until remote schemas are updated)
cd examples/workflow-support
PROMPTKIT_SCHEMA_SOURCE=local promptarena run --ci --format html

cd examples/workflow-order-processing
PROMPTKIT_SCHEMA_SOURCE=local promptarena run --ci --format html

# Regular examples (use remote schema)
cd examples/customer-support
promptarena run --ci --format html
```

### Schema validation
- PromptArena validates scenario files against JSON schemas fetched from `https://promptkit.altairalabs.ai/schemas/v1alpha1/`
- If new fields aren't published to the remote schema yet, set `PROMPTKIT_SCHEMA_SOURCE=local` to validate against local `schemas/v1alpha1/` files
- Test init files (`engine/test_init.go`, `cmd/promptarena/test_init.go`) disable schema validation for unit tests

### Mock providers
- Examples with pre-configured mock providers (e.g., `providers/mock-provider.yaml` referencing `mock-responses.yaml`) should be run **without** the `--mock-provider` flag
- The `--mock-provider` flag replaces all providers with a generic mock that does NOT load scenario-specific response files
- Mock responses support `tool_calls` for simulating LLM-initiated tool use (e.g., `workflow__transition`)

## Go Code Standards

- **golangci-lint** config in `.golangci.yml` — line length 120, linters include errcheck, gocritic, gosec, govet, revive, staticcheck, unused
- **Test naming**: Always check for name collisions across `_test.go` files in the same package before naming types/functions
- **Formatting**: `gofmt` and `goimports` are enforced

## SonarCloud Quality Gate (CI)

SonarCloud runs on every PR and enforces quality on new code:
- Coverage >= 80% on new/changed lines
- Duplicated lines <= 3%
- Reliability, Security, Maintainability ratings: A
