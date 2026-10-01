---
title: Use RuntimeConfig
description: Load providers, tools, MCP servers, hooks, state store and logging from one YAML file
sidebar:
  order: 15
verified:
  commit: a87b70f25076b0e5c6898a4450cdd1dc0793041a
  sources:
    - pkg/config/logging.go
    - pkg/config/runtime_config.go
    - pkg/config/types.go
    - runtime/credentials/resolver.go
    - runtime/credentials/types.go
    - runtime/hooks/exec_build.go
    - runtime/hooks/exec_hooks.go
    - runtime/hooks/execconfig/execconfig.go
    - runtime/providers/claude/pricing_table.go
    - sdk/conversation.go
    - sdk/exec_tools.go
    - sdk/options.go
    - sdk/provider_file.go
    - sdk/runtime_config.go
    - sdk/sdk.go
---

Configure providers, tools, MCP servers, hooks, state store and logging from a single YAML file instead of programmatic options.

---

## Quick Start

```go
package main

import (
    "log"

    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    conv, err := sdk.Open("./agent.pack.json", "assistant",
        sdk.WithRuntimeConfig("./runtime.yaml"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()
}
```

`WithRuntimeConfig` loads the YAML file and applies each section it contains. See [Combine with Programmatic Overrides](#combine-with-programmatic-overrides) for how it interacts with other options.

---

## Minimal Config

A RuntimeConfig file with only a provider:

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

This registers the same provider as `sdk.WithProvider(...)` with the same settings.

---

## Full Config

See [RuntimeConfig Reference](/sdk/reference/runtime-config/) for every section: tools, evals, MCP servers, hooks, state store and logging. Each section is optional.

---

## Combine with Programmatic Overrides

Pass `WithRuntimeConfig` alongside other options. `WithProvider`, `WithStateStore` and `WithLogger` win over the YAML file whichever side of `WithRuntimeConfig` you list them on:

```go
package main

import (
    "log"

    "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    testProvider := mock.NewProvider("test", "mock-model", false)

    conv, err := sdk.Open("./agent.pack.json", "assistant",
        sdk.WithRuntimeConfig("./base.runtime.yaml"),
        sdk.WithProvider(testProvider), // wins over the YAML provider
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()
}
```

The YAML provider stays registered in the provider pool. The YAML state store and logger apply only when you have not set one. MCP servers from the YAML file are appended to those you set programmatically, not replaced.

Use this in tests to keep the production config and swap in a mock provider.

---

## Per-Environment Configs

Create separate config files for each environment and select at startup:

```text
config/
  production.runtime.yaml    # real providers, Redis state store, JSON logging
  development.runtime.yaml   # cheaper model, local state store, text logging
  test.runtime.yaml          # mock provider, in-memory state store
```

```go
package main

import (
    "fmt"
    "log"
    "os"

    _ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock" // registers type: mock
    "github.com/AltairaLabs/PromptKit/sdk/v2"
)

func main() {
    env := os.Getenv("APP_ENV") // "production", "development", "test"
    configPath := fmt.Sprintf("./config/%s.runtime.yaml", env)

    conv, err := sdk.Open("./agent.pack.json", "assistant",
        sdk.WithRuntimeConfig(configPath),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer conv.Close()
}
```

The blank import of `runtime/providers/mock` is needed only because `test.runtime.yaml` uses `type: mock`. Without it, loading fails with `unsupported provider type: mock`.

Environment-specific settings stay out of your code.

---

## See Also

- [Exec Tools](/sdk/how-to/tools/exec-tools/): configure external process tools
- [Exec Hooks](/sdk/how-to/hooks/exec-hooks/): configure pipeline hooks
- [RuntimeConfig Reference](/sdk/reference/runtime-config/): full schema documentation
- [Configure MCP Servers](/sdk/how-to/tools/configure-mcp/): MCP server builder pattern
