# Architecture Decision Records

This directory contains the Architecture Decision Records (ADRs) for the PromptKit project migration and design decisions.

## Active ADRs

### ADR-001: Monorepo Structure
**Status**: Accepted  
**Date**: Original repository  
**Summary**: Decision to use monorepo structure for PromptKit development.

### ADR-002: Repository Migration and Multi-Module Architecture  
**Status**: Accepted  
**Date**: 2025-11-01  
**Summary**: Migration from promptkit-wip to production repository with Go workspace and multi-module architecture.

### ADR-003: Go Version Standardization Strategy
**Status**: Accepted  
**Date**: 2025-11-01  
**Summary**: Standardization of all modules to Go 1.23 for consistency and CI/CD reliability.

### ADR-004: Enhanced Build System Design
**Status**: Accepted  
**Date**: 2025-11-01  
**Summary**: Implementation of comprehensive Makefile-based build system supporting multi-module architecture.

### ADR-005: CLI Tool Architecture and Organization  
**Status**: Accepted  
**Date**: 2025-11-01  
**Summary**: Unified CLI tool architecture under tools/ directory with standardized structure and dependencies.

## ADR Template

For creating new ADRs, use the template in `template.md`. Each ADR should follow the established format and include:

* Context and problem statement
* Decision drivers and considered options
* Decision rationale and consequences
* Implementation details and validation
* Related decisions and references

## Decision Process

1. **Identify Decision**: Document architectural decisions that impact multiple components
2. **Research Options**: Evaluate alternatives with pros/cons analysis  
3. **Stakeholder Input**: Gather input from relevant team members
4. **Document Decision**: Create ADR following template format
5. **Implementation**: Execute decision with proper validation
6. **Review and Update**: Periodic review for continued relevance

## Migration Context

These ADRs document the architectural decisions made during the comprehensive migration of PromptKit from the development repository to the production repository. The migration involved:

* Multi-module workspace architecture design
* Go version standardization across ecosystem
* Professional build system implementation  
* CLI tool architecture and organization
* Examples and documentation strategy

Each ADR captures the reasoning, alternatives considered, and outcomes achieved during this migration process.