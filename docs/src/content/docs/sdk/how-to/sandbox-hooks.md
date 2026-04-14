---
title: Sandbox Exec Hooks
description: Run exec hook subprocesses in a container, sidecar, or custom backend
sidebar:
  order: 18
---

Exec hooks spawn subprocesses on the host by default. When host-local execution isn't what you want -- e.g. you'd rather hit a long-lived Kubernetes hooks sidecar, start each call in a fresh Docker container, or route through a bespoke cloud-sandbox -- PromptKit lets you swap the spawn mechanism via the **Sandbox** interface without changing the hook's wire protocol.

The hook subprocess still reads JSON on stdin and writes JSON on stdout. Only where it runs changes.

---

## Quick Start

Sandboxes are declared under `spec.sandboxes` in `RuntimeConfig` and referenced from individual hooks by name:

```yaml
spec:
  sandboxes:
    pii_runner:
      mode: docker_run
      image: promptkit/pii-hook:1.2
      network: none
      mounts:
        - ./hooks:/hooks:ro

  hooks:
    pii_redactor:
      command: /hooks/pii.py
      hook: provider
      phases: [before_call]
      mode: filter
      sandbox: pii_runner
```

Each `mode` names a factory registered via `sandbox.RegisterFactory` (or `sdk.WithSandboxFactory`). The rest of the entry is passed verbatim to that factory as its configuration map.

The built-in `direct` mode spawns the command on the host and ships in core; it's the default when a hook omits `sandbox:`.

---

## Available Backends

- **`direct`** (core, default) -- spawns the command on the host via `exec.CommandContext`. No configuration.
- **`docker_run`**, **`docker_exec`**, **`kubectl_exec`** -- reference implementations in [`sdk/examples/sandboxes/`](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/sandboxes). Copy, adapt, or import directly.

Reference backends aren't wired in by default. Register them in consumer code:

```go
import (
    "github.com/AltairaLabs/PromptKit/sdk"
    "github.com/AltairaLabs/PromptKit/sdk/examples/sandboxes/dockerrun"
    "github.com/AltairaLabs/PromptKit/sdk/examples/sandboxes/kubectlexec"
)

conv, err := sdk.Open("./pack.json", "chat",
    sdk.WithSandboxFactory(dockerrun.ModeName, dockerrun.Factory),
    sdk.WithSandboxFactory(kubectlexec.ModeName, kubectlexec.Factory),
    sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

Factories must be registered **before** `WithRuntimeConfig` runs; otherwise the mode lookup fails.

---

## Running Hooks in a Kubernetes Sidecar

A common production shape: PromptKit runs as the main container in a pod, alongside a long-lived `hooks` sidecar that has Python (or Node, or whatever hook runtime you need) and your hook scripts baked in. Every hook invocation is a `kubectl exec` into that sidecar.

```yaml
# runtime.yaml
spec:
  sandboxes:
    sidecar:
      mode: kubectl_exec
      pod: ${POD_NAME}
      container: hooks
      extra_args: [--request-timeout=5s]

  hooks:
    pii_redactor:
      command: python
      args: [-m, pii]
      hook: provider
      phases: [before_call]
      mode: filter
      sandbox: sidecar
    audit_logger:
      command: python
      args: [-m, audit]
      hook: session
      phases: [session_start, session_end]
      mode: observe
      sandbox: sidecar
```

```go
conv, err := sdk.Open("./pack.json", "chat",
    sdk.WithSandboxFactory(kubectlexec.ModeName, kubectlexec.Factory),
    sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

Multiple hooks can share the same sandbox. The sidecar reuses its process tree across invocations, so startup cost is one-time rather than per-call.

---

## Writing a Custom Backend

Implement `sandbox.Sandbox` and `sandbox.Factory`:

```go
package mybackend

import (
    "context"
    "fmt"

    "github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
)

const ModeName = "my_backend"

type Sandbox struct{ name string /* ... */ }

func (s *Sandbox) Name() string { return s.name }

func (s *Sandbox) Spawn(ctx context.Context, req sandbox.Request) (sandbox.Response, error) {
    // Run req.Command with req.Args, pipe req.Stdin in, return stdout/stderr.
    // The hook subprocess's stdout is what PromptKit parses as the verdict.
    return sandbox.Response{Stdout: out, Stderr: errb}, nil
}

func Factory(name string, cfg map[string]any) (sandbox.Sandbox, error) {
    // Read and validate cfg, return a ready Sandbox.
    return &Sandbox{name: name /* ... */}, nil
}
```

Register at SDK open time:

```go
sdk.WithSandboxFactory(mybackend.ModeName, mybackend.Factory)
```

The same type works programmatically without any registry: construct a `sandbox.Sandbox` and assign it to `ExecHookConfig.Sandbox` before calling `hooks.NewExecProviderHook` (or friends). RuntimeConfig only wires the declarative path.

---

## Behavior Notes

- Sandbox resolution happens at `WithRuntimeConfig` time, once per load. If a hook references a name not declared under `spec.sandboxes`, the config load fails loudly.
- When a hook omits `sandbox:`, the built-in `direct` backend is used -- existing configs keep working unchanged.
- The wire protocol (JSON stdin/stdout, timeout, phase/mode semantics) is independent of the sandbox. Anything that works under `direct` works under every other backend.
- Per-call timeouts (`timeout_ms`) are enforced by the runtime regardless of backend. A stuck container or pod exec will still be cancelled.

---

## See Also

- [Exec Hooks](/sdk/how-to/exec-hooks/) -- the wire protocol and phase semantics.
- [`sdk/examples/sandboxes/`](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/sandboxes) -- reference implementations for Docker and Kubernetes.
- [`runtime/hooks/sandbox/`](https://github.com/AltairaLabs/PromptKit/tree/main/runtime/hooks/sandbox) -- the core interface and registry.
