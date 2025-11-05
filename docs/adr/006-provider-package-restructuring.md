# ADR-006: Provider Package Restructuring

**Date**: 2025-11-05

**Status**: Accepted

**Context**: PromptKit Runtime - Provider Architecture

---

## Summary

Restructure the `runtime/providers` package from a flat 49-file structure into provider-specific subpackages (`openai/`, `gemini/`, `claude/`) to improve code organization, maintainability, and developer experience as the codebase scales.

## Problem Statement

### Current State

The `runtime/providers` package currently contains 49 Go files in a flat structure:
- 9 OpenAI files (base, multimodal, tools + tests)
- 9 Gemini files (base, multimodal, tools + tests)
- 9 Claude files (base, multimodal, tools + tests)
- 22 shared infrastructure files (registry, contracts, streaming, etc.)

All files exist in a single package namespace, making navigation difficult and increasing the likelihood of naming conflicts.

### Challenges

1. **Navigation Complexity**: IDEs and developers struggle to find specific provider implementations among 49 files
2. **Cognitive Load**: Difficult to understand which files belong to which provider
3. **Merge Conflicts**: Multiple developers working on different providers often conflict in the same package
4. **Testing Isolation**: Cannot easily run or mock tests for a single provider in isolation
5. **Scaling Issues**: Adding new providers (Azure OpenAI, Cohere, etc.) will exacerbate the problem
6. **Ownership**: Difficult to assign clear ownership of provider implementations
7. **Import Bloat**: All provider code is imported even when only one provider is used

### Requirements

1. Each provider must be independently testable
2. Shared interfaces and contracts must remain accessible to all providers
3. No runtime performance degradation
4. Migration path must be clear for existing users
5. Must support adding new providers without affecting existing ones
6. Build and test times should not increase

## Decision

**Restructure the providers package into provider-specific subpackages while maintaining shared core interfaces at the parent level.**

### Rationale

**Primary Reason - Code Organization**: Grouping related files into subpackages reduces cognitive load and improves code discoverability. Each provider becomes a clear, bounded context.

**Secondary Reason - Independent Evolution**: Each provider can evolve independently with its own version history, testing, and ownership without affecting others.

**Technical Reason - Package Encapsulation**: Go's package system naturally supports this structure, allowing each provider to expose only necessary public APIs while keeping implementation details private.

## Implementation

### Changes Required

**New Directory Structure**:

```
runtime/providers/
├── provider.go              # Core Provider interface
├── registry.go              # Provider registry
├── multimodal.go            # Shared multimodal helpers
├── contract_test.go         # Shared contract tests
├── streaming_test.go        # Shared streaming tests
├── sse.go                   # SSE utilities
│
├── openai/
│   ├── openai.go
│   ├── openai_test.go
│   ├── openai_multimodal.go
│   ├── openai_multimodal_test.go
│   ├── openai_tools.go
│   ├── openai_tools_test.go
│   ├── openai_multimodal_tools_test.go
│   └── openai_contract_test.go
│
├── gemini/
│   ├── gemini.go
│   ├── gemini_test.go
│   ├── gemini_multimodal.go
│   ├── gemini_multimodal_test.go
│   ├── gemini_tools.go
│   ├── gemini_tools_test.go
│   ├── gemini_tool_choice_test.go
│   ├── gemini_tool_loop_test.go
│   ├── gemini_tool_results_test.go
│   └── gemini_contract_test.go
│
└── claude/
    ├── claude.go
    ├── claude_test.go
    ├── claude_multimodal.go
    ├── claude_multimodal_test.go
    ├── claude_tools.go
    ├── claude_tools_test.go
    ├── claude_cache_test.go
    ├── claude_tool_results_test.go
    └── claude_contract_test.go
```

**Package Names**:
- Parent: `package providers`
- Subpackages: `package openai`, `package gemini`, `package claude`

**Export Changes**:
- Each provider struct becomes: `openai.Provider`, `gemini.Provider`, `claude.Provider`
- Factory functions: `openai.NewProvider()`, `gemini.NewProvider()`, `claude.NewProvider()`
- Keep `providers.Provider` interface at parent level

### Migration Strategy

**Phase 1: Create Subpackages (Non-Breaking)**
1. Create new subpackage directories
2. Copy files to new locations (keep originals)
3. Update package declarations in new files
4. Update internal imports within each subpackage
5. Verify all tests pass in new structure

**Phase 2: Update Consumers (Breaking)**
1. Update SDK imports from `providers.NewOpenAIProvider` to `openai.NewProvider`
2. Update examples and documentation
3. Update tool implementations (packc, arena, inspect-state)
4. Update any internal usage in runtime/pipeline

**Phase 3: Remove Old Structure**
1. Delete old flat files from providers package
2. Update go.mod if needed
3. Update CI/CD scripts
4. Archive this ADR as "Completed"

### Timeline

- **Phase 1**: Immediate (this session) - Create new structure, all tests passing
- **Phase 2**: Same session - Update all internal consumers
- **Phase 3**: Same session - Remove old files, final verification

## Consequences

### Positive Consequences

- ✅ **Improved Navigation**: Developers can quickly find provider-specific code
- ✅ **Reduced Merge Conflicts**: Teams can work on different providers independently
- ✅ **Better Testing**: Each provider can be tested in isolation with `go test ./providers/openai`
- ✅ **Clearer Ownership**: Teams can own specific provider packages
- ✅ **Easier Onboarding**: New developers understand structure at a glance
- ✅ **Future-Proof**: Adding new providers (Azure, Cohere, etc.) is straightforward
- ✅ **Selective Imports**: Projects can import only needed providers (future optimization)

### Negative Consequences

- ⚠️ **Breaking Change**: All import paths change for consumers
  - *Mitigation*: We're pre-1.0, breaking changes are acceptable. Clear migration guide provided.
- ⚠️ **Initial Migration Effort**: ~2-3 hours to refactor all code
  - *Mitigation*: Automated tooling can handle most updates, doing it in one session
- ⚠️ **Slightly Longer Import Paths**: `providers.NewOpenAIProvider` → `openai.NewProvider`
  - *Mitigation*: Import aliases can shorten paths if needed

### Neutral Consequences

- ℹ️ **More Directories**: File tree has more depth but same total file count
- ℹ️ **Test Organization**: Tests move with implementation files (standard Go practice)

## Alternatives Considered

### Alternative 1: Keep Flat Structure with Better Naming

Use stricter file naming conventions (e.g., `openai__multimodal.go`, `openai__tools.go`) to improve sorting while keeping flat structure.

**Pros**:
- No breaking changes
- Minimal effort

**Cons**:
- Doesn't solve navigation or ownership issues
- Naming becomes awkward and verbose
- Still have 49+ files in one directory
- Doesn't scale beyond current providers

**Why Rejected**: Doesn't address the fundamental scaling and organization problems.

### Alternative 2: Separate Repository Per Provider

Move each provider into its own repository (e.g., `promptkit-provider-openai`).

**Pros**:
- Maximum separation
- Independent versioning
- Clear ownership boundaries

**Cons**:
- Massive increase in complexity
- Shared code duplication or complex dependency management
- Harder to maintain consistency across providers
- Release coordination nightmare
- Over-engineering for current scale

**Why Rejected**: Too much separation for current needs. Subpackages provide enough isolation without the overhead.

### Alternative 3: Internal Packages with Package Aliasing

Keep everything in `package providers` but use Go's internal package structure.

**Pros**:
- No breaking changes to external API
- Can still organize files in directories

**Cons**:
- Confusing package structure (directory != package)
- Goes against Go idioms
- Doesn't solve import clarity issues
- Maintenance burden of non-standard structure

**Why Rejected**: Violates Go best practices and doesn't provide enough benefit over proper subpackages.

## Validation

### Success Criteria

- [x] All existing tests pass in new structure
- [x] No runtime performance degradation
- [x] All SDK code compiles with new imports
- [x] All examples and tools updated
- [x] Documentation reflects new structure
- [x] Can add a new provider by creating a new subpackage

### Testing Strategy

1. Run full test suite before refactoring: `go test ./...`
2. Create new structure and verify tests pass: `go test ./runtime/providers/...`
3. Update consumers and verify no compilation errors
4. Run full test suite again: `go test ./...`
5. Run integration tests with real API keys
6. Verify examples still work

### Rollback Plan

If critical issues discovered post-refactoring:
1. Git revert the refactoring commits
2. All code returns to flat structure
3. Zero data loss (code-only change)
4. Rollback time: < 5 minutes

## Dependencies

**Depends On**:
- None (independent change)

**Impacts**:
- SDK (`github.com/AltairaLabs/PromptKit/sdk`)
- Tools (`packc`, `arena`, `inspect-state`)
- Examples (`examples/*/`)
- Documentation (`docs/`)
- Any external consumers using the providers package

## References

- Go Package Structure Best Practices: https://go.dev/doc/modules/layout
- Flat vs Nested Packages Discussion: https://github.com/golang/go/wiki/CodeReviewComments#package-names
- Current providers directory: `runtime/providers/`
- Multimodal implementation: Phase 2 (ADRs pending)

---

**Implementation Status**: In Progress

**Last Updated**: 2025-11-05
