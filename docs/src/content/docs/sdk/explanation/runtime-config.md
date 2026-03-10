---
title: RuntimeConfig Design
description: Why RuntimeConfig exists and how it separates what from how
sidebar:
  order: 3
---
Understanding the separation between agent definition and execution environment.

## The Problem

SDK users building Go applications with PromptKit face a boilerplate problem. Wiring up providers, tools, hooks, state stores, and evaluators requires 50+ lines of imperative Go code before the first message is sent. Each deployment environment needs different wiring -- different provider API keys, different tool endpoints, different state backends -- but the agent's core behavior stays the same.

Meanwhile, PromptArena already solves this with declarative YAML configuration. Arena users describe their runtime environment in config files, and the framework handles the rest. SDK users had no way to tap into this same declarative approach. They were stuck writing and maintaining environment-specific Go code.

RuntimeConfig bridges this gap. It gives SDK users the same declarative power that Arena has, without sacrificing the programmatic flexibility that makes the SDK valuable.

## Pack vs RuntimeConfig

PromptKit draws a sharp line between two concerns:

- **What the agent does** -- its prompts, tool schemas, eval definitions, conversation structure. This is the pack.
- **How the agent runs** -- which provider to call, where tools are hosted, how to persist state. This is the RuntimeConfig.

```
agent.pack.json              <- what the agent does (portable)
production.runtime.yaml      <- how to run it in production
development.runtime.yaml     <- how to run it locally
```

The pack is platform-agnostic. You can share it across teams, check it into version control, and run it in any environment without modification. It declares _names_ for tools and evals, along with their schemas and trigger conditions, but says nothing about implementations.

RuntimeConfig is environment-specific. It binds those names to concrete implementations -- a Python script, an HTTP endpoint, a Go function. Different environments get different RuntimeConfig files while sharing the same pack.

This separation matters because it keeps the agent definition portable. A pack that works in development works in production. The only thing that changes is the RuntimeConfig that tells the runtime _how_ to execute.

## Name-Based Resolution

The connection between pack and RuntimeConfig is name-based. The pack declares a tool named `sentiment_check` with a JSON Schema describing its parameters. RuntimeConfig binds that name to an implementation -- perhaps `./tools/sentiment-check.py` in development and `https://api.internal/sentiment` in production.

At invocation time, the tool registry looks up the name and dispatches to whatever implementation the RuntimeConfig bound it to. The pack never knows whether the tool is Go code, a Python subprocess, or an HTTP endpoint. The same principle applies to evals: the pack declares an eval type like `sentiment_check`, and RuntimeConfig binds it to a handler.

This indirection is deliberate. It means the pack author and the platform operator can work independently. The pack author defines the contract (name + schema). The platform operator fulfills it (binding + implementation). Neither needs to know the details of the other's work.

## Config Struct Reuse

RuntimeConfig does not invent its own type system. It reuses the same `pkg/config` types that Arena uses internally: `Provider`, `ToolSpec`, `MCPServerConfig`, `StateStoreConfig`, `LoggingConfigSpec`, and others. The same Go structs that parse Arena's YAML configuration parse RuntimeConfig files.

This has two benefits. First, there is no duplication -- bug fixes and new fields in `pkg/config` are automatically available to both Arena and SDK users. Second, configuration knowledge transfers between the two contexts. If you know how to configure a provider in Arena, you know how to configure it in RuntimeConfig.

## Override Semantics

RuntimeConfig provides a base layer of configuration. Programmatic options applied after `WithRuntimeConfig()` take precedence over anything declared in the YAML file.

This layering is intentional. A team can maintain a shared RuntimeConfig file that covers the common case -- provider settings, standard tool bindings, logging configuration -- and individual applications can override specific settings in code. A development build might load `base.runtime.yaml` and then programmatically swap in a mock provider. A production build might load the same base file and add a custom state store.

The precedence rule is simple: code wins. If both the YAML file and a programmatic option configure the same thing, the programmatic option takes effect.

## The Polyglot Exec Protocol

One of RuntimeConfig's most important consequences is breaking the Go-only lock-in. Tools, evals, and hooks can be implemented as external subprocesses in any language. If it can read stdin and write to stdout, it can participate in a PromptKit agent.

The exec protocol supports two modes:

- **One-shot (exec)** spawns a new process for each invocation. The runtime passes input as JSON on stdin and reads the result from stdout. This is simple and stateless -- good for tools that do a single computation and return.

- **Server mode** maintains a long-running process that communicates over JSON-RPC. The runtime starts the process once and sends requests over its lifetime. This amortizes startup cost and allows the subprocess to maintain state between invocations.

Both modes are configured entirely in RuntimeConfig. The pack declares the tool name and schema; RuntimeConfig specifies the command, arguments, working directory, and environment variables needed to run the subprocess.

## Security Model

The exec protocol introduces a potential attack surface: arbitrary command execution. RuntimeConfig's design addresses this through separation of concerns.

Exec bindings live in RuntimeConfig, which is operator-controlled. They do not live in the pack, which could be shared from untrusted sources. A malicious pack cannot introduce new executables -- it can only declare tool names and schemas. The operator decides what those names resolve to.

Environment variable handling follows a similar principle. The `env` field in RuntimeConfig passes variable _names_, not values. The actual values come from the process environment at runtime. This means RuntimeConfig files can be checked into version control without leaking secrets.

Timeouts provide a final safety layer. Every exec binding can specify a timeout, preventing runaway subprocesses from consuming resources indefinitely. The runtime kills processes that exceed their timeout and returns an error to the LLM.

## See Also

- [How-To: Use RuntimeConfig](../how-to/use-runtime-config) -- practical guide to loading and applying RuntimeConfig
- RuntimeConfig Reference -- field-by-field documentation of the RuntimeConfig schema
- Exec Protocol Reference -- specification for the subprocess communication protocol
