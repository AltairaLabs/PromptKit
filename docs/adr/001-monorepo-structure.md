# ADR-001: Monorepo Structure for Tools and SDK

**Status**: Accepted  
**Date**: 2025-11-01  
**Deciders**: PromptKit Core Team  
**Issues**: Related to overall project architecture

## Context

PromptKit consists of multiple related components: Arena CLI, Pack Compiler, SDK, and shared runtime libraries. We need to decide on the optimal repository structure for development, maintenance, and user adoption.

## Decision Drivers

* Shared code reuse between Arena and SDK
* Independent versioning for different tools
* Developer experience and build complexity
* User adoption and installation complexity
* CI/CD pipeline efficiency

## Considered Options

* **Option 1**: Separate repositories for each tool
* **Option 2**: Monorepo with Go workspaces
* **Option 3**: Single module with everything together

## Decision Outcome

Chosen option: **Monorepo with Go workspaces**, because it provides the best balance of code sharing, independent builds, and manageable complexity.

## Consequences

### Positive

* Shared packages can be easily reused across tools
* Coordinated releases ensure compatibility
* Single CI/CD pipeline covers all components
* Simplified dependency management
* Easy cross-component refactoring

### Negative

* Slightly more complex build setup
* All tools share the same repository issues/PRs
* Larger repository size
* Need to manage multiple go.mod files

### Neutral

* Go workspace feature handles module coordination
* Each tool can still be built independently
* Users can install specific tools without others

## Implementation Notes

* Use `go.work` file to coordinate modules
* Each tool gets its own `go.mod` file
* Shared packages live in `pkg/` and `runtime/`
* Build targets in Makefile for individual components
* CI runs tests for all modules but allows independent building

Structure:

```
promptkit/
├── go.work              # Workspace coordination
├── tools/arena/         # Arena CLI (go.mod)
├── tools/packc/         # Pack Compiler (go.mod)  
├── sdk/                 # PromptKit SDK (go.mod)
├── pkg/                 # Shared packages
└── runtime/            # Runtime libraries (go.mod)
```

## References

* [Go Workspaces Documentation](https://go.dev/doc/tutorial/workspaces)
* [Monorepo Best Practices](https://monorepo.tools/)