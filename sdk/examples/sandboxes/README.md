# Exec Hook Sandboxes — Examples

Reference implementations of [`sandbox.Sandbox`](../../../runtime/hooks/sandbox/sandbox.go) that route exec-hook subprocess invocations through something other than `exec.CommandContext`. These are **example code** — not imported by PromptKit core. Copy and adapt, or import them directly; both are fine.

| Package | Mode | Use when |
|---|---|---|
| [`dockerrun/`](dockerrun/) | `docker_run` | Each hook call should run in a fresh disposable container. |
| [`dockerexec/`](dockerexec/) | `docker_exec` | A long-lived sidecar container has the hook runtime baked in. |
| [`kubectlexec/`](kubectlexec/) | `kubectl_exec` | Same as `dockerexec`, but the sidecar lives in a Kubernetes pod. |

## The stdin/stdout protocol is unchanged

All three backends preserve the existing exec-hook wire protocol: PromptKit writes a JSON request to the subprocess's stdin, the subprocess writes a JSON response to stdout. The backends just change *how* bytes get to that subprocess — `docker run` spawns a fresh container and pipes through it, `docker exec` pipes into an existing container, `kubectl exec` does the same across the k8s API. Your hook scripts don't know or care which backend is active.

## Two ways to wire one in

### 1. Programmatic — construct directly, no registry

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/hooks"
    "github.com/AltairaLabs/PromptKit/sdk/examples/sandboxes/dockerrun"
)

sb := dockerrun.New(dockerrun.Config{
    Image:   "python:3.12-slim",
    Network: "none",
    Mounts:  []string{"./hooks:/hooks:ro"},
})

hook := hooks.NewExecProviderHook(&hooks.ExecHookConfig{
    Name:    "pii_redactor",
    Command: "/hooks/pii.py",
    Phases:  []string{"before_call"},
    Mode:    "filter",
    Sandbox: sb, // <-- the sandbox is injected here
})
```

### 2. Declarative — register a factory for RuntimeConfig-driven deployments

```go
import (
    "github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
    "github.com/AltairaLabs/PromptKit/sdk/examples/sandboxes/kubectlexec"
)

func init() {
    _ = sandbox.RegisterFactory(kubectlexec.ModeName, kubectlexec.Factory)
}
```

Or via an SDK option (no `init` side effects):

```go
conv, _ := sdk.Open("./pack.json", "chat",
    sdk.WithSandboxFactory(kubectlexec.ModeName, kubectlexec.Factory),
)
```

Then reference the mode in RuntimeConfig YAML *(binding coming in a follow-up PR)*:

```yaml
spec:
  sandboxes:
    hooks_sidecar:
      mode: kubectl_exec
      pod: my-agent-pod
      namespace: default
      container: hooks
  hooks:
    pii_redactor:
      command: /hooks/pii.py
      hook: provider
      phases: [before_call]
      mode: filter
      sandbox: hooks_sidecar
```

## Writing your own backend

The pattern each example uses is the same:

1. Implement `sandbox.Sandbox` (`Name()`, `Spawn(ctx, req) (Response, error)`).
2. Internally, rewrite `req.Command`/`req.Args` to the argv you actually want to run (`docker run ...`, `kubectl exec ...`, or whatever your backend needs).
3. Delegate to a `*direct.Sandbox` for the actual subprocess management — stdin piping, timeout enforcement, WaitDelay, env merging are all handled there.

For bespoke backends that aren't just "different argv around `exec`" — cloud sandbox APIs, gRPC to a hook service, whatever — implement `Spawn` however you need. The only invariants are: (a) `req.Stdin` reaches the remote process's stdin, (b) its stdout is returned as `Response.Stdout`, (c) non-zero exit, timeout, or transport failure populates `Response.Err`.
