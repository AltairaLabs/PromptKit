---
title: Sandbox exec hooks
description: Run exec hook subprocesses in a container, a Kubernetes sidecar, or a custom backend
sidebar:
  order: 18
verified:
  commit: 004b9cbbd4d40f770a2e3c76053b600a55189355
  sources:
    - pkg/config/runtime_config.go
    - runtime/hooks/exec_build.go
    - runtime/hooks/exec_hooks.go
    - runtime/hooks/execconfig/execconfig.go
    - runtime/hooks/sandbox/direct/direct.go
    - runtime/hooks/sandbox/registry.go
    - runtime/hooks/sandbox/sandbox.go
    - sdk/examples/sandboxes/dockerrun/dockerrun.go
    - sdk/examples/sandboxes/kubectlexec/kubectlexec.go
    - sdk/options.go
    - sdk/runtime_config.go
---

Exec hooks spawn their subprocess on the host by default. Declare a sandbox to launch it somewhere else: a fresh Docker container per call, a long-lived Kubernetes sidecar, or a backend you write. The hook's JSON stdin/stdout protocol does not change.

## Prerequisites

- A working [exec hook](/sdk/how-to/hooks/exec-hooks/) loaded through `sdk.WithRuntimeConfig`.
- For the reference backends: `docker` or `kubectl` on the host `PATH`, and the hook runtime inside the container or pod.

## Steps

### 1. Declare the sandbox and bind a hook to it

Sandboxes live under `spec.sandboxes`. Each hook selects one by name with `sandbox:`.

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
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

`mode` names a factory in the sandbox registry, and the runtime passes the rest of the entry to that factory as a config map. A hook without `sandbox:` runs on the host through the built-in `direct` mode, which needs no registration.

Reference backends:

| Mode | Behavior | Config keys |
|------|----------|-------------|
| `direct` | Spawns the command on the host. Built in. | none |
| `docker_run` | Fresh container per call. | `image`, `network`, `mounts`, `extra_args` |
| `docker_exec` | Execs into a running container. | see [`dockerexec`](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/sandboxes/dockerexec) |
| `kubectl_exec` | Execs into a container of a running pod. | `pod` (required), `namespace`, `container`, `kubeconfig`, `context`, `extra_args`, `kubectl_path` |

The `docker_*` and `kubectl_exec` backends are example code in [`sdk/examples/sandboxes/`](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/sandboxes). Core does not register them.

### 2. Register the factories

Register each factory before `sdk.WithRuntimeConfig` in the option list. The config load fails with `no factory registered for mode` otherwise.

```go
import (
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	"github.com/AltairaLabs/PromptKit/sdk/v2/examples/sandboxes/dockerrun"
	"github.com/AltairaLabs/PromptKit/sdk/v2/examples/sandboxes/kubectlexec"
)

conv, err := sdk.Open("./pack.json", "chat",
	sdk.WithSandboxFactory(dockerrun.ModeName, dockerrun.Factory),
	sdk.WithSandboxFactory(kubectlexec.ModeName, kubectlexec.Factory),
	sdk.WithRuntimeConfig("./runtime.yaml"),
)
```

`sdk.WithSandboxFactory` writes to a process-wide registry, and a second registration of the same mode returns an error. An application that opens several conversations registers once at startup with `sandbox.RegisterFactory` and omits the option:

```go
if err := sandbox.RegisterFactory(kubectlexec.ModeName, kubectlexec.Factory); err != nil {
	return err
}
```

### 3. Run hooks in a Kubernetes sidecar

When PromptKit runs in a pod beside a `hooks` container that holds the hook runtime and scripts, point several hooks at one `kubectl_exec` sandbox:

```yaml
spec:
  sandboxes:
    sidecar:
      mode: kubectl_exec
      pod: my-agent-pod
      namespace: default
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

RuntimeConfig does not expand environment variables, so `pod` is a literal name. To take the name from the environment, build the sandbox in Go and assign it to `hooks.ExecHookConfig.Sandbox` (step 4).

### 4. Write a custom backend

Implement `sandbox.Sandbox` and a `sandbox.Factory`:

```go
package mybackend

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/sandbox"
)

const ModeName = "my_backend"

type Sandbox struct{ name string }

func (s *Sandbox) Name() string { return s.name }

func (s *Sandbox) Spawn(ctx context.Context, req sandbox.Request) (sandbox.Response, error) {
	// Run req.Command with req.Args, pipe req.Stdin in, enforce req.Timeout.
	// Stdout is parsed as the hook verdict. Report a non-zero exit, a timeout
	// or a transport failure in Response.Err.
	var out, errb []byte
	return sandbox.Response{Stdout: out, Stderr: errb}, nil
}

func Factory(name string, cfg map[string]any) (sandbox.Sandbox, error) {
	// Read and validate cfg, return a ready Sandbox.
	return &Sandbox{name: name}, nil
}
```

Register it with `sdk.WithSandboxFactory(mybackend.ModeName, mybackend.Factory)`.

To skip the registry, construct the sandbox in code and assign it to `ExecHookConfig.Sandbox` before calling `hooks.NewExecProviderHook`, `hooks.NewExecToolHook` or `hooks.NewExecSessionHook`. RuntimeConfig is the only path that reads `spec.sandboxes`.

## Verify

Open the conversation and trigger the hooked phase. A bad setup fails at `sdk.Open`:

- `sandbox "pii_runner": sandbox: no factory registered for mode "docker_run"` means the factory is missing or registered after `WithRuntimeConfig`.
- A hook whose `sandbox:` names a sandbox absent from `spec.sandboxes` fails config validation.

If the container or pod is unreachable, a filter hook denies the operation and the denial reason carries the backend's stderr.

## Troubleshoot

- `mode "..." is already registered`: the same mode was registered twice. Register once per process.
- A hook hangs until its timeout: the backend must enforce `Request.Timeout`. The built-in and reference backends do.
- The command is not found: `command` is resolved inside the sandbox (the container or pod), not on the host.

## Related

- [Run exec hooks](/sdk/how-to/hooks/exec-hooks/): the wire protocol and phase semantics
- [`sdk/examples/sandboxes/`](https://github.com/AltairaLabs/PromptKit/tree/main/sdk/examples/sandboxes): Docker and Kubernetes reference backends
- [`runtime/hooks/sandbox/`](https://github.com/AltairaLabs/PromptKit/tree/main/runtime/hooks/sandbox): the core interface and registry
