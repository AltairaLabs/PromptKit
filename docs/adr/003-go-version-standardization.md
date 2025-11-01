# ADR-003: Go Version Standardization Strategy

**Status**: Accepted  
**Date**: 2025-11-01  
**Deciders**: Migration Team  
**Issues**: #15

## Context

During the PromptKit repository migration, the CI pipeline experienced failures due to inconsistent Go version specifications across modules. The original codebase used different Go versions (1.23 and 1.24.0), causing compilation and linting issues in the GitHub Actions workflow.

## Problem

The multi-module workspace contained mixed Go version requirements:

* Runtime module: `go 1.23`  
* SDK module: `go 1.24.0`
* CLI tools: `go 1.23`
* Examples: Various versions

This inconsistency caused:

* CI pipeline failures during compilation
* golangci-lint compatibility issues
* Cross-module dependency problems
* Inconsistent development environments

## Decision

**Standardize all modules to Go 1.23** across the entire PromptKit workspace.

## Rationale

### Technical Considerations

**Stability**: Go 1.23 is a stable, well-tested version with excellent toolchain support.

**Compatibility**: All PromptKit features function correctly with Go 1.23 - no newer language features were required.

**Toolchain Support**: golangci-lint and other development tools have mature support for Go 1.23.

**CI/CD Integration**: GitHub Actions runners have reliable Go 1.23 support with consistent behavior.

### Strategic Considerations

**Developer Experience**: Single Go version eliminates environment setup complexity.

**Maintenance**: Uniform version reduces maintenance overhead and testing matrix.

**Dependencies**: All third-party dependencies are compatible with Go 1.23.

**Future Migration**: Standardized baseline enables coordinated future upgrades.

## Implementation

### Version Standardization

Updated all `go.mod` files to specify:

```go
go 1.23
```

### Affected Modules

* `/runtime/go.mod`: Updated from mixed versions
* `/sdk/go.mod`: Downgraded from 1.24.0 to 1.23  
* `/tools/arena/go.mod`: Standardized to 1.23
* `/tools/packc/go.mod`: Standardized to 1.23
* `/tools/inspect-state/go.mod`: Standardized to 1.23
* `/examples/*/go.mod`: All standardized to 1.23

### CI/CD Configuration

GitHub Actions workflow (`.github/workflows/ci.yml`):

```yaml
strategy:
  matrix:
    go-version: [1.23]
```

### Build System Integration

Enhanced Makefile with consistent Go version validation:

```makefile
GO_VERSION := 1.23
GOCMD := go$(GO_VERSION)
```

## Validation Results

### Compilation Success

* ✅ All modules compile successfully with Go 1.23
* ✅ Cross-module dependencies resolve correctly  
* ✅ No breaking changes required in codebase
* ✅ All existing functionality preserved

### CI/CD Pipeline

* ✅ GitHub Actions workflow passes consistently
* ✅ golangci-lint runs without version conflicts
* ✅ Multi-module workspace builds successfully
* ✅ All test suites pass with Go 1.23

### Development Environment

* ✅ Consistent developer setup requirements
* ✅ Simplified onboarding documentation
* ✅ Reduced environment configuration complexity
* ✅ Uniform toolchain experience

## Consequences

### Positive Outcomes

**Operational Stability**:

* ✅ Reliable CI/CD pipeline execution
* ✅ Consistent compilation across all environments
* ✅ Eliminated version-related build failures
* ✅ Streamlined development workflow

**Maintenance Benefits**:

* ✅ Single version to maintain and upgrade
* ✅ Reduced complexity in dependency management
* ✅ Simplified debugging of version-related issues
* ✅ Consistent toolchain behavior

**Developer Experience**:

* ✅ Clear environment requirements
* ✅ Consistent development setup
* ✅ Reliable local development builds
* ✅ Unified documentation and guides

### Trade-offs

**Language Features**:

* ⚠️ Cannot use Go 1.24+ features until coordinated upgrade
* ⚠️ Slightly older language features available
* ✅ No current features require newer Go versions

**Ecosystem Integration**:

* ⚠️ Some cutting-edge dependencies may prefer newer Go
* ✅ All required dependencies work with Go 1.23
* ✅ Stable ecosystem compatibility

## Future Upgrade Strategy

### Upgrade Criteria

Future Go version upgrades should meet:

1. **Feature Requirement**: New Go features provide concrete value
2. **Ecosystem Readiness**: All dependencies support new version
3. **Toolchain Maturity**: Development tools have stable support
4. **Coordinated Migration**: All modules upgrade simultaneously

### Upgrade Process

1. **Impact Assessment**: Evaluate all modules and dependencies
2. **Testing**: Comprehensive validation in CI environment
3. **Coordinated Update**: All modules upgrade in single operation
4. **Documentation**: Update all environment setup guides

### Version Selection Philosophy

* Prioritize **stability** over cutting-edge features
* Maintain **ecosystem compatibility** 
* Ensure **CI/CD reliability**
* Support **developer productivity**

## Monitoring and Validation

### Continuous Integration

* GitHub Actions validates Go version consistency
* Automated builds test all modules with standardized version
* Dependency resolution validates cross-module compatibility

### Quality Assurance

* All test suites run with Go 1.23
* golangci-lint configuration optimized for version
* Build system validates version consistency

## Related Decisions

* **ADR-002**: Repository Migration and Multi-Module Architecture
* **ADR-004**: Enhanced Build System Design
* **ADR-005**: CLI Tool Architecture and Organization

## References

* [Go 1.23 Release Notes](https://golang.org/doc/go1.23)
* [Go Modules Version Selection](https://golang.org/ref/mod#version-queries)
* GitHub Issue: #15 - CI Pipeline Fixes
* Migration Commit: 5c15bcb

---

*This ADR establishes the foundation for consistent Go development across the entire PromptKit ecosystem, enabling reliable builds and streamlined development workflows.*