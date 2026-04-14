// Package kubectlexec is an example sandbox implementation that runs
// each exec-hook invocation inside a sidecar container in an existing
// Kubernetes pod via `kubectl exec -i -- <cmd>`. The pattern fits
// deployments where PromptKit runs in-cluster with a long-lived
// hooks sidecar that has the hook runtime (Python, Node, etc.) and
// scripts baked in.
//
// This is reference/example code — not imported by PromptKit core.
// Register via sdk.WithSandboxFactory or sandbox.RegisterFactory, or
// construct one directly and set it on ExecHookConfig.Sandbox.
//
// Configuration:
//
//	mode: kubectl_exec
//	pod: my-agent-pod              # required
//	namespace: default             # optional; maps to -n
//	container: hooks               # optional; maps to -c
//	kubeconfig: /etc/kube.yaml     # optional; maps to --kubeconfig
//	context: prod                  # optional; maps to --context
//	extra_args: [--request-timeout=5s]
package kubectlexec

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox/direct"
)

// ModeName is the identifier under which this backend registers with
// sandbox.RegisterFactory.
const ModeName = "kubectl_exec"

// Config configures the kubectl-exec backend.
type Config struct {
	// Pod is the target pod. Required.
	Pod string
	// Namespace maps to `-n <value>`. Empty means don't pass the flag
	// (kubectl falls back to the current-context default).
	Namespace string
	// Container maps to `-c <value>`. Empty means don't pass the flag.
	Container string
	// Kubeconfig maps to `--kubeconfig=<value>`.
	Kubeconfig string
	// Context maps to `--context=<value>`.
	Context string
	// ExtraArgs are extra `kubectl exec` flags inserted after the
	// standard ones and before the `--` command separator.
	ExtraArgs []string
	// KubectlPath overrides the `kubectl` binary name. Empty uses
	// `kubectl` resolved from PATH.
	KubectlPath string
}

// Sandbox runs exec-hook commands via `kubectl exec` in an existing pod.
type Sandbox struct {
	name  string
	cfg   Config
	inner *direct.Sandbox
}

// New constructs a Sandbox from a Config.
func New(cfg Config) *Sandbox {
	return NewNamed(ModeName, cfg)
}

// NewNamed is like New but lets the caller override the sandbox name.
func NewNamed(name string, cfg Config) *Sandbox {
	return &Sandbox{
		name:  name,
		cfg:   cfg,
		inner: direct.New(name),
	}
}

// Factory is a sandbox.Factory compatible with RuntimeConfig-based
// resolution.
func Factory(name string, cfg map[string]any) (sandbox.Sandbox, error) {
	pod, _ := cfg["pod"].(string)
	if pod == "" {
		return nil, fmt.Errorf("kubectlexec: required config field 'pod' is missing or not a string")
	}
	return NewNamed(name, Config{
		Pod:         pod,
		Namespace:   stringOr(cfg, "namespace"),
		Container:   stringOr(cfg, "container"),
		Kubeconfig:  stringOr(cfg, "kubeconfig"),
		Context:     stringOr(cfg, "context"),
		ExtraArgs:   stringSlice(cfg, "extra_args"),
		KubectlPath: stringOr(cfg, "kubectl_path"),
	}), nil
}

// Name returns the sandbox's name.
func (s *Sandbox) Name() string { return s.name }

// Spawn builds a kubectl exec argv and delegates to the inner direct
// sandbox. Layout:
//
//	kubectl [--kubeconfig=...] [--context=...] exec -i \
//	        [-n <ns>] [-c <container>] <extra...> <pod> -- <cmd> <args...>
func (s *Sandbox) Spawn(ctx context.Context, req sandbox.Request) (sandbox.Response, error) {
	kubectlArgs := s.buildArgs(req.Command, req.Args)
	cmd := s.cfg.KubectlPath
	if cmd == "" {
		cmd = "kubectl"
	}
	return s.inner.Spawn(ctx, sandbox.Request{
		Command: cmd,
		Args:    kubectlArgs,
		Env:     req.Env,
		Stdin:   req.Stdin,
		Timeout: req.Timeout,
	})
}

// buildArgs assembles the argv passed to `kubectl`. Exposed for tests.
func (s *Sandbox) buildArgs(command string, args []string) []string {
	out := make([]string, 0, 10+len(s.cfg.ExtraArgs)+len(args))
	// Global flags go before the subcommand.
	if s.cfg.Kubeconfig != "" {
		out = append(out, "--kubeconfig="+s.cfg.Kubeconfig)
	}
	if s.cfg.Context != "" {
		out = append(out, "--context="+s.cfg.Context)
	}
	out = append(out, "exec", "-i")
	if s.cfg.Namespace != "" {
		out = append(out, "-n", s.cfg.Namespace)
	}
	if s.cfg.Container != "" {
		out = append(out, "-c", s.cfg.Container)
	}
	out = append(out, s.cfg.ExtraArgs...)
	out = append(out, s.cfg.Pod, "--")
	out = append(out, command)
	out = append(out, args...)
	return out
}

func stringOr(cfg map[string]any, key string) string {
	s, _ := cfg[key].(string)
	return s
}

func stringSlice(cfg map[string]any, key string) []string {
	v, ok := cfg[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
