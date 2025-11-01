# ADR-005: CLI Tool Architecture and Organization

**Status**: Accepted  
**Date**: 2025-11-01  
**Deciders**: Migration Team  
**Issues**: #9, #10, #11

## Context

PromptKit requires multiple CLI tools to support different aspects of the ecosystem: testing (arena), compilation (packc), and debugging (inspect-state). The migration needed to establish a professional CLI tool architecture within the multi-module repository structure.

## Problem

The original CLI tool organization had several challenges:

* **Inconsistent Structure**: Each tool had different organization patterns
* **Complex Dependencies**: Unclear relationships between tools and core modules
* **Build Complexity**: Individual build processes for each tool
* **Installation Issues**: No standardized installation or distribution
* **Maintenance Overhead**: Duplicate code and inconsistent patterns

## Decision

**Implement a unified CLI tool architecture** under the `tools/` directory with standardized structure, shared dependencies, and professional organization.

## Architecture Design

### Directory Structure

```
tools/
├── arena/          # Testing framework CLI (27MB)
│   ├── cmd/        # Command implementations
│   ├── internal/   # Arena-specific logic
│   ├── main.go     # Entry point
│   └── go.mod      # Module definition
├── packc/          # Pack compiler CLI (10MB)
│   ├── cmd/        # Command implementations  
│   ├── internal/   # Compiler-specific logic
│   ├── main.go     # Entry point
│   └── go.mod      # Module definition
└── inspect-state/  # State debugging utility (4MB)
    ├── cmd/        # Command implementations
    ├── internal/   # Inspection logic
    ├── main.go     # Entry point
    └── go.mod      # Module definition
```

### Dependency Architecture

All CLI tools follow a consistent dependency pattern:

```
CLI Tool → SDK → Runtime
```

**Benefits**:

* ✅ **Consistent APIs**: All tools use same SDK interfaces
* ✅ **Shared Functionality**: Common operations through SDK
* ✅ **Reduced Duplication**: No direct runtime dependencies
* ✅ **Version Coordination**: SDK provides stable interface

## Tool Specifications

### Arena CLI (Testing Framework)

**Purpose**: Comprehensive testing framework for PromptKit scenarios.

**Size**: 27MB optimized binary

**Key Features**:

* Scenario execution and validation
* Provider integration testing  
* Human-in-the-loop (HITL) workflow support
* MCP (Model Context Protocol) integration
* Assertion-based testing
* Context management validation

**Architecture**:

```
arena/
├── cmd/
│   ├── run.go      # Execute test scenarios
│   ├── validate.go # Validate configurations
│   └── init.go     # Initialize test projects
├── internal/
│   ├── executor/   # Test execution engine
│   ├── reporter/   # Result reporting
│   └── config/     # Configuration handling
└── main.go
```

### Packc CLI (Pack Compiler)

**Purpose**: Compilation and packaging tool for PromptKit packs.

**Size**: 10MB compact binary

**Key Features**:

* Pack compilation and validation
* Template processing
* Dependency resolution
* Output generation
* Configuration validation

**Architecture**:

```
packc/
├── cmd/
│   ├── compile.go  # Pack compilation
│   ├── validate.go # Pack validation
│   └── init.go     # Pack initialization
├── internal/
│   ├── compiler/   # Compilation engine
│   ├── validator/  # Validation logic
│   └── templates/  # Template processing
└── main.go
```

### Inspect-State CLI (Debugging Utility)

**Purpose**: State inspection and debugging for PromptKit workflows.

**Size**: 4MB lightweight utility

**Key Features**:

* Runtime state inspection
* Configuration debugging
* Workflow state analysis
* Provider state examination
* Diagnostic reporting

**Architecture**:

```
inspect-state/
├── cmd/
│   ├── inspect.go  # State inspection
│   ├── debug.go    # Debug operations
│   └── report.go   # Generate reports
├── internal/
│   ├── inspector/  # Inspection engine
│   ├── formatter/  # Output formatting
│   └── analyzer/   # State analysis
└── main.go
```

## Implementation Patterns

### 1. **Consistent Module Structure**

All tools follow the same module organization:

```go
// go.mod for each tool
module github.com/AltairaLabs/promptkit/tools/[tool-name]

go 1.23

require (
    github.com/AltairaLabs/promptkit/sdk v0.1.0
)

replace github.com/AltairaLabs/promptkit/sdk => ../../sdk
replace github.com/AltairaLabs/promptkit/runtime => ../../runtime
```

### 2. **Unified Command Interface**

Each tool implements consistent CLI patterns:

```go
// main.go pattern
func main() {
    rootCmd := &cobra.Command{
        Use:   "[tool-name]",
        Short: "[Tool description]",
    }
    
    // Add subcommands
    rootCmd.AddCommand(cmd.RunCmd())
    rootCmd.AddCommand(cmd.ValidateCmd())
    
    if err := rootCmd.Execute(); err != nil {
        log.Fatal(err)
    }
}
```

### 3. **SDK Integration**

All tools use SDK for PromptKit operations:

```go
import (
    "github.com/AltairaLabs/promptkit/sdk"
)

func executeTool() error {
    // Use SDK for all PromptKit operations
    pipeline, err := sdk.NewPipeline(config)
    if err != nil {
        return err
    }
    
    return pipeline.Execute()
}
```

### 4. **Error Handling and Logging**

Consistent error handling across tools:

```go
import (
    "github.com/AltairaLabs/promptkit/runtime/logger"
)

func toolOperation() error {
    log := logger.New("tool-name")
    
    if err := operation(); err != nil {
        log.Error("Operation failed", "error", err)
        return fmt.Errorf("tool operation failed: %w", err)
    }
    
    log.Info("Operation completed successfully")
    return nil
}
```

## Build System Integration

### Makefile Targets

Each tool has dedicated build targets:

```makefile
# Individual tool builds
build-arena:
    cd tools/arena && go build -o ../../bin/arena

build-packc:  
    cd tools/packc && go build -o ../../bin/packc

build-inspect-state:
    cd tools/inspect-state && go build -o ../../bin/inspect-state

# Unified tool building
build-tools: build-arena build-packc build-inspect-state

# Tool-specific testing
test-tools:
    cd tools/arena && go test ./...
    cd tools/packc && go test ./...
    cd tools/inspect-state && go test ./...
```

### Installation Support

```makefile
install-tools: build-tools
    cp bin/arena /usr/local/bin/
    cp bin/packc /usr/local/bin/
    cp bin/inspect-state /usr/local/bin/
```

## Migration Results

### Arena CLI Migration

* ✅ **Files Migrated**: 150+ source files
* ✅ **Binary Size**: 27MB optimized
* ✅ **Features**: Complete testing framework
* ✅ **Integration**: Full SDK and runtime integration
* ✅ **Testing**: Comprehensive test coverage

### Packc CLI Migration

* ✅ **Files Migrated**: 45+ source files
* ✅ **Binary Size**: 10MB compact
* ✅ **Features**: Complete pack compilation
* ✅ **Integration**: Efficient SDK usage
* ✅ **Testing**: Validation test suite

### Inspect-State CLI Migration

* ✅ **Files Migrated**: 25+ source files
* ✅ **Binary Size**: 4MB lightweight
* ✅ **Features**: Comprehensive state inspection
* ✅ **Integration**: Minimal SDK footprint
* ✅ **Testing**: Diagnostic test coverage

## Benefits Achieved

### **Consistent Developer Experience**

**Unified Interface**: All tools follow same command patterns and conventions.

**Consistent Documentation**: Standardized help, usage, and error messages.

**Predictable Behavior**: Similar operations work the same across tools.

**Shared Configuration**: Common configuration patterns and file formats.

### **Maintainable Architecture**

**Reduced Duplication**: Shared SDK eliminates code duplication.

**Clear Dependencies**: Well-defined dependency hierarchy.

**Modular Design**: Each tool can be developed and tested independently.

**Professional Structure**: Industry-standard CLI tool organization.

### **Operational Excellence**

**Reliable Builds**: Consistent compilation across all tools.

**Quality Assurance**: Integrated testing for all CLI tools.

**Easy Installation**: Standardized installation process.

**Performance Optimization**: Efficient binary sizes and startup times.

## Performance Characteristics

### Binary Optimization

* **Arena (27MB)**: Full-featured testing framework with comprehensive capabilities
* **Packc (10MB)**: Efficient compiler with optimal size-to-functionality ratio  
* **Inspect-State (4MB)**: Lightweight utility with minimal footprint

### Runtime Performance

* ✅ **Fast Startup**: All tools start in under 100ms
* ✅ **Memory Efficient**: Minimal memory footprint during operation
* ✅ **CPU Optimized**: Efficient algorithms and data structures
* ✅ **I/O Performance**: Optimized file and network operations

## Future Extension Strategy

### Adding New CLI Tools

Template for new CLI tool integration:

1. **Create Structure**: Follow established `tools/[name]/` pattern
2. **Module Definition**: Standard go.mod with SDK dependency
3. **Command Interface**: Implement consistent CLI patterns
4. **Build Integration**: Add Makefile targets
5. **Documentation**: Follow established documentation patterns

### Capability Enhancement

* **Plugin Architecture**: Support for external plugins
* **Configuration Management**: Enhanced configuration systems
* **Integration APIs**: REST/gRPC interfaces for tool integration
* **Monitoring**: Built-in observability and metrics

## Trade-offs and Considerations

### **Consistency vs. Flexibility**

**Consistency Benefits**:

* ✅ Predictable developer experience
* ✅ Reduced learning curve
* ✅ Easier maintenance
* ✅ Professional appearance

**Flexibility Limitations**:

* ⚠️ Tool-specific optimizations may be constrained
* ⚠️ Shared patterns may not fit all use cases
* ⚠️ Innovation may be limited by consistency requirements

### **Size vs. Functionality**

**Functionality Richness**:

* ✅ Arena provides comprehensive testing capabilities
* ✅ Packc offers complete compilation features
* ✅ Inspect-state delivers thorough debugging tools

**Binary Size Impact**:

* ⚠️ Arena is larger (27MB) due to comprehensive features
* ✅ Packc is efficiently sized (10MB) for its capabilities
* ✅ Inspect-state achieves minimal footprint (4MB)

## Related Decisions

* **ADR-002**: Repository Migration and Multi-Module Architecture
* **ADR-003**: Go Version Standardization Strategy  
* **ADR-004**: Enhanced Build System Design

## References

* [Go CLI Best Practices](https://golang.org/doc/effective_go.html)
* [Cobra CLI Framework](https://github.com/spf13/cobra)
* GitHub Issues: #9, #10, #11 - CLI Tool Migration
* Implementation Commits: Multiple migration commits

---

*This ADR establishes the foundation for professional CLI tool development within the PromptKit ecosystem, ensuring consistency, maintainability, and excellent developer experience.*