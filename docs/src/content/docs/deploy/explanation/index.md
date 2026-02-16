---
title: Explanation
sidebar:
  order: 0
---
Deep-dive documentation on Deploy concepts, architecture, and design decisions.

## Understanding Deploy

These articles explain the "why" behind Deploy's design and how it works internally.

### Core Concepts

- [**Adapter Architecture**](adapter-architecture) - Plugin pattern, JSON-RPC, stdio communication
- [**State Management**](state-management) - State file, checksums, and deployment lifecycle

## Purpose

Explanation documentation helps you understand:

- **Why** Deploy uses a plugin architecture
- **How** adapters communicate with the CLI
- **When** state is persisted and why it matters
- **What** tradeoffs were made in the design

## Who Should Read This

- Developers building custom adapters
- Teams designing deployment strategies
- Contributors to PromptKit
- Anyone wanting deeper understanding of the deploy framework

## Format

Each explanation includes:

- **Background** - Context and motivation
- **Design Principles** - Core design decisions
- **Architecture** - How components work together
- **Tradeoffs** - Benefits and limitations

## See Also

- [Reference](../reference/) - Protocol and SDK specifications
- [How-To Guides](../how-to/) - Practical tasks
- [Tutorials](../tutorials/) - Learning path
