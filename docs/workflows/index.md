---
layout: docs
title: Workflows
nav_order: 8
has_children: true
description: "End-to-end development workflows using PromptKit components"
keywords: "development workflow, testing workflow, deployment workflow, full-stack development, CI/CD workflow, production deployment, DevOps"
---

# Workflows

End-to-end guides showing how PromptKit components work together.

## Overview

These workflows demonstrate complete development processes using multiple PromptKit components. Each workflow walks through a real-world scenario from start to finish.

## Available Workflows

### [Development Workflow](development-workflow.md)
Build and test an LLM application using Runtime, Arena, and PackC.

**You'll learn:**
- Setting up a new project with Runtime
- Writing tests with PromptArena
- Packaging with PackC
- Local development iteration

**Time**: 30 minutes

### [Testing Workflow](testing-workflow.md)
Comprehensive testing strategy for LLM applications.

**You'll learn:**
- Unit testing with SDK
- Integration testing with Runtime
- Evaluation testing with Arena
- CI/CD integration

**Time**: 45 minutes

### [Deployment Workflow](deployment-workflow.md)
Deploy an LLM application to production.

**You'll learn:**
- Packaging for deployment
- Configuration management
- Monitoring and observability
- Rollback strategies

**Time**: 60 minutes

### [Full-Stack Example](full-stack-example.md)
Complete application using all PromptKit components.

**You'll learn:**
- Frontend integration
- Backend architecture
- State management
- Production best practices

**Time**: 90 minutes

## When to Use Workflows

**Use workflows when you:**
- Want to see the big picture
- Need end-to-end guidance
- Are starting a new project
- Want to understand component interactions

**Don't use workflows when:**
- You need specific component details → See component docs
- You want API reference → See [Reference](../runtime/reference/index.md)
- You have a specific task → See [How-To Guides](../runtime/how-to/index.md)

## Component Overview

PromptKit has four main components:

**Runtime**
- Core library for building LLM applications
- Pipeline-based architecture
- Multi-provider support
- Production-ready features

**SDK**
- Higher-level abstractions
- Simplified conversation management
- Quick prototyping
- Built on Runtime

**PromptArena**
- Testing and evaluation tool
- Automated testing
- Provider comparison
- Quality assurance

**PackC**
- Prompt packaging tool
- Version control for prompts
- Template management
- Distribution format

## How Components Work Together

```
Development → Testing → Packaging → Deployment

1. Build with Runtime/SDK
   ├── Write application code
   ├── Configure providers
   └── Implement features

2. Test with PromptArena
   ├── Write test cases
   ├── Run evaluations
   └── Compare providers

3. Package with PackC
   ├── Create .pack files
   ├── Version prompts
   └── Manage templates

4. Deploy
   ├── Ship to production
   ├── Monitor performance
   └── Iterate
```

## Getting Started

1. **New to PromptKit?** Start with [Getting Started](../guides/getting-started.md)
2. **Building an app?** See [Development Workflow](development-workflow.md)
3. **Testing focus?** See [Testing Workflow](testing-workflow.md)
4. **Deploying?** See [Deployment Workflow](deployment-workflow.md)

## Related Documentation

- **[Architecture](../architecture/index.md)**: System design and decisions
- **[Runtime](../runtime/index.md)**: Core library documentation
- **[SDK](../sdk/index.md)**: High-level SDK documentation
- **[PromptArena](../promptarena/index.md)**: Testing tool documentation
- **[PackC](../packc/index.md)**: Packaging tool documentation

## Need Help?

- Check component-specific documentation for details
- See [Examples](../examples/index.md) for code samples
- Review [Concepts](../concepts/index.md) for foundational understanding
