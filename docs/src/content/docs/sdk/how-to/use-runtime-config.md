---
title: Use RuntimeConfig
description: Configure the SDK declaratively with a YAML file
sidebar:
  order: 15
---

Replace dozens of programmatic option calls with a single YAML file that declares providers, tools, MCP servers, hooks, and more.

---

## Quick Start

```go
import "github.com/AltairaLabs/PromptKit/sdk"

conv, err := sdk.Open("./agent.pack.json", "assistant",
    sdk.WithRuntimeConfig("./runtime.yaml"),
)
if err != nil {
    log.Fatal(err)
}
defer conv.Close()
```

`WithRuntimeConfig` loads the YAML file and applies every section as if you had called the equivalent `With*` options individually.

---

## Minimal Config

A RuntimeConfig file with just a provider:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: minimal
spec:
  providers:
    - id: anthropic-main
      type: claude
      model: claude-sonnet-4-20250514
      credential:
        credential_env: ANTHROPIC_API_KEY
```

This is equivalent to calling `sdk.WithProvider(...)` with the same settings, but easier to change without recompiling.

---

## Full Config

Add tools, MCP servers, hooks, state store, and logging:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: production
spec:
  providers:
    - id: anthropic-main
      type: claude
      model: claude-sonnet-4-20250514
      credential:
        credential_env: ANTHROPIC_API_KEY

  tools:
    sentiment_check:
      exec:
        command: ./tools/sentiment-check.py
        timeout_ms: 5000
        env: [NLTK_DATA]

  evals:
    sentiment_check:
      command: ./evals/sentiment-check.py

  mcp_servers:
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem"]

  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call, after_call]
      mode: filter
      timeout_ms: 3000

  state_store:
    type: redis
    redis:
      address: localhost:6379
      ttl: 24h

  logging:
    defaultLevel: info
    format: json
```

Each section is optional. Include only what you need.

---

## Combine with Programmatic Overrides

Pass `WithRuntimeConfig` alongside other options. Programmatic options are applied after the YAML config, so they take precedence:

```go
conv, err := sdk.Open("./agent.pack.json", "assistant",
    sdk.WithRuntimeConfig("./base.runtime.yaml"),
    sdk.WithProvider(testProvider),  // overrides the YAML provider
)
```

This is useful for tests where you want the full production config but need to swap in a mock provider.

---

## Per-Environment Configs

Create separate config files for each environment and select at startup:

```
config/
  production.runtime.yaml    # real providers, Redis state store, JSON logging
  development.runtime.yaml   # cheaper model, local state store, text logging
  test.runtime.yaml          # mock provider, in-memory state store
```

```go
env := os.Getenv("APP_ENV") // "production", "development", "test"
configPath := fmt.Sprintf("./config/%s.runtime.yaml", env)

conv, err := sdk.Open("./agent.pack.json", "assistant",
    sdk.WithRuntimeConfig(configPath),
)
```

This keeps environment-specific settings out of your code and lets you change behavior without recompiling.

---

## See Also

- [Exec Tools](/sdk/reference/exec-tools/) -- configure external process tools
- [Exec Hooks](/sdk/reference/exec-hooks/) -- configure pipeline hooks
- [RuntimeConfig Reference](/sdk/reference/runtime-config/) -- full schema documentation
- [Configure MCP Servers](/sdk/how-to/configure-mcp/) -- MCP server builder pattern
