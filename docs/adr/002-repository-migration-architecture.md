# ADR-002: Repository Migration and Multi-Module Architecture

**Status**: Accepted  
**Date**: 2025-11-01  
**Deciders**: Migration Team  
**Issues**: #8, #9, #10, #11, #12, #13, #15

## Context

PromptKit was originally developed in the `promptkit-wip` repository with a monorepo structure containing runtime, SDK, CLI tools, and examples. The project reached production readiness and required migration to the main `AltairaLabs/PromptKit` repository with improved architecture and organization.

## Decision Drivers

* **Production Readiness**: Need to move from development repository to production repository
* **Multi-Module Architecture**: Separate concerns between runtime, SDK, and CLI tools  
* **Independent Versioning**: Allow independent releases of SDK vs CLI tools
* **Developer Experience**: Streamlined build and development workflows
* **CI/CD Integration**: Professional continuous integration and deployment
* **Code Reusability**: Shared runtime components across all tools
* **Maintainability**: Clear separation of responsibilities and dependencies

## Considered Options

### Option 1: Single Module Migration

Migrate everything into a single Go module in the new repository.

**Pros:**

* Simpler dependency management
* Single version for entire project
* Easier initial migration

**Cons:**

* Tight coupling between components
* Cannot version SDK independently from CLI tools
* Larger dependency footprint for SDK users
* Difficult to maintain separate release cycles

### Option 2: Multi-Repository Split

Split into separate repositories for runtime, SDK, and each CLI tool.

**Pros:**

* Complete independence between components
* Very clear boundaries
* Independent release cycles
* Minimal dependencies per component

**Cons:**

* Complex cross-repository dependency management
* Difficult to coordinate breaking changes
* Developer experience complexity
* Increased maintenance overhead
* Code duplication potential

### Option 3: Multi-Module Monorepo (Selected)

Use Go workspace with multiple modules in a single repository.

**Pros:**

* Independent versioning within single repository
* Shared development experience
* Coordinated releases when needed
* Clear module boundaries
* Professional CI/CD integration
* Code sharing without duplication

**Cons:**

* More complex initial setup
* Requires Go workspace knowledge
* Multi-module build complexity

## Decision

We chose **Option 3: Multi-Module Monorepo** with the following architecture:

```
promptkit/
├── runtime/           # Core PromptKit runtime (shared library)
├── sdk/              # Developer SDK (depends on runtime)
├── tools/            # CLI tools directory
│   ├── arena/        # Testing framework CLI
│   ├── packc/        # Pack compiler CLI
│   └── inspect-state/# State debugging utility
├── examples/         # Usage examples and demos
├── go.work           # Go workspace configuration
└── Makefile          # Enhanced build system
```

## Architecture Principles

### 1. **Clear Dependency Hierarchy**
```
CLI Tools → SDK → Runtime
Examples → SDK + Runtime
```

### 2. **Independent Versioning**
- `runtime`: Core functionality versioning
- `sdk`: API stability for developers  
- `tools/*`: Feature-driven CLI versioning
- `examples/*`: Documentation versioning

### 3. **Shared Infrastructure**
- Common build system (Makefile)
- Unified CI/CD pipeline  
- Consistent Go version (1.23)
- Shared development tools

### 4. **Professional Development Workflow**
- Multi-module workspace support
- Enhanced build targets
- Comprehensive testing
- Quality assurance integration

## Implementation Strategy

### Phase 1: Foundation Migration
1. ✅ Migrate runtime components with full test coverage
2. ✅ Establish multi-module workspace (`go.work`)
3. ✅ Set up enhanced build system (Makefile)
4. ✅ Configure professional CI/CD pipeline

### Phase 2: SDK and Tools Migration  
1. ✅ Migrate SDK with comprehensive examples
2. ✅ Migrate all CLI tools (arena, packc, inspect-state)
3. ✅ Validate cross-module dependencies
4. ✅ Test complete integration

### Phase 3: Examples and Documentation
1. ✅ Migrate comprehensive example collection
2. ✅ Create usage documentation
3. 🔄 Document architectural decisions (ADRs)
4. 🔄 Complete documentation migration

### Phase 4: Production Readiness
1. ✅ Fix CI/CD pipeline configuration
2. ✅ Standardize Go version compatibility
3. ✅ Validate badge connections
4. ✅ Complete quality assurance

## Consequences

### Positive Consequences

**Developer Experience:**
- ✅ Single repository for all PromptKit development
- ✅ Streamlined build system with `make` targets
- ✅ Consistent development environment
- ✅ Clear module boundaries and responsibilities

**Architecture Benefits:**
- ✅ Independent module versioning capability
- ✅ Shared code without duplication
- ✅ Professional CI/CD integration
- ✅ Scalable for future components

**Production Benefits:**
- ✅ Professional repository structure
- ✅ Enterprise-grade build automation
- ✅ Comprehensive test coverage
- ✅ Quality assurance integration

### Negative Consequences

**Complexity:**
- ⚠️  Requires Go workspace understanding
- ⚠️  Multi-module dependency management
- ⚠️  More complex release coordination

**Migration Overhead:**
- ⚠️  Significant initial migration effort (240+ files)
- ⚠️  Need to update all import paths
- ⚠️  Comprehensive testing required

### Mitigation Strategies

**Complexity Management:**
- Comprehensive documentation of workspace setup
- Enhanced Makefile with simple targets
- Clear module dependency documentation
- Professional developer onboarding guides

**Migration Risk Mitigation:**
- ✅ Systematic migration with GitHub issue tracking
- ✅ Comprehensive test validation at each step
- ✅ Professional CI/CD pipeline integration
- ✅ Complete functionality validation

## Validation Results

### Migration Metrics
- **Files Migrated**: 240+ files across all modules
- **Test Coverage**: 100+ test cases passing
- **CLI Tools**: 3 tools fully functional (arena: 27MB, packc: 10MB, inspect-state: 4MB)
- **Examples**: 10 comprehensive examples working
- **Build Targets**: 15+ Makefile targets operational

### Quality Assurance
- ✅ All modules compile successfully
- ✅ Complete test suite passes
- ✅ CLI tools functional and tested
- ✅ Cross-module dependencies resolved
- ✅ CI/CD pipeline operational

### Production Readiness
- ✅ Professional repository structure
- ✅ Enterprise-grade automation
- ✅ Comprehensive documentation
- ✅ Quality badges connected
- ✅ Developer-friendly workflows

## Related Decisions

- **ADR-003**: Go Version Standardization Strategy
- **ADR-004**: Enhanced Build System Design  
- **ADR-005**: CLI Tool Architecture and Organization
- **ADR-006**: Examples and Documentation Strategy

## References

- [Go Workspaces Documentation](https://go.dev/doc/tutorial/workspaces)
- [Multi-Module Repository Best Practices](https://github.com/golang/go/wiki/Modules)
- GitHub Issues: #8, #9, #10, #11, #12, #13, #15
- Migration Commits: ef38855, f9b1cc8, 5c15bcb

---

*This ADR documents the foundational architectural decision for the PromptKit repository structure, establishing the framework for all subsequent development and maintenance.*