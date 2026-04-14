// Package dockerexec is an example sandbox implementation that runs
// each exec-hook invocation inside an already-running container via
// `docker exec -i`. This is the cheap sibling of dockerrun: no
// per-call container startup cost, just a docker exec that pipes
// stdin/stdout through.
//
// Typical use case: a long-lived sidecar container in the same Compose
// stack that has the hook scripts and their runtime dependencies baked
// in. A nearby example: `kubectlexec` for the Kubernetes equivalent.
//
// This is reference/example code — not imported by PromptKit core.
// Register via sdk.WithSandboxFactory or sandbox.RegisterFactory, or
// construct one directly and set it on ExecHookConfig.Sandbox.
//
// Configuration:
//
//	mode: docker_exec
//	container: my-hooks-sidecar    # required
//	workdir: /app                  # optional; maps to --workdir
//	user: hookuser                 # optional; maps to --user
//	extra_args: [--tty]            # optional
package dockerexec

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox/direct"
)

// ModeName is the identifier under which this backend registers with
// sandbox.RegisterFactory.
const ModeName = "docker_exec"

// Config configures the docker-exec backend.
type Config struct {
	// Container is the name or ID of the running container to exec into.
	// Required.
	Container string
	// Workdir maps to `docker exec --workdir=<value>`. Empty means don't
	// pass the flag.
	Workdir string
	// User maps to `docker exec --user=<value>`. Empty means don't pass
	// the flag.
	User string
	// ExtraArgs are extra `docker exec` flags inserted after the
	// standard ones and before the container name.
	ExtraArgs []string
	// DockerPath overrides the `docker` binary name. Empty uses `docker`
	// resolved from PATH.
	DockerPath string
}

// Sandbox runs exec-hook commands via `docker exec` in an existing
// container.
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
	container, _ := cfg["container"].(string)
	if container == "" {
		return nil, fmt.Errorf("dockerexec: required config field 'container' is missing or not a string")
	}
	return NewNamed(name, Config{
		Container:  container,
		Workdir:    stringOr(cfg, "workdir"),
		User:       stringOr(cfg, "user"),
		ExtraArgs:  stringSlice(cfg, "extra_args"),
		DockerPath: stringOr(cfg, "docker_path"),
	}), nil
}

// Name returns the sandbox's name.
func (s *Sandbox) Name() string { return s.name }

// Spawn builds a `docker exec -i [flags] <container> <cmd> <args...>`
// argv and delegates to the inner direct sandbox.
func (s *Sandbox) Spawn(ctx context.Context, req sandbox.Request) (sandbox.Response, error) {
	dockerArgs := s.buildArgs(req.Command, req.Args)
	cmd := s.cfg.DockerPath
	if cmd == "" {
		cmd = "docker"
	}
	return s.inner.Spawn(ctx, sandbox.Request{
		Command: cmd,
		Args:    dockerArgs,
		Env:     req.Env,
		Stdin:   req.Stdin,
		Timeout: req.Timeout,
	})
}

// buildArgs assembles the argv passed to `docker`. Exposed for tests.
func (s *Sandbox) buildArgs(command string, args []string) []string {
	out := make([]string, 0, 4+len(s.cfg.ExtraArgs)+len(args))
	out = append(out, "exec", "-i")
	if s.cfg.Workdir != "" {
		out = append(out, "--workdir="+s.cfg.Workdir)
	}
	if s.cfg.User != "" {
		out = append(out, "--user="+s.cfg.User)
	}
	out = append(out, s.cfg.ExtraArgs...)
	out = append(out, s.cfg.Container)
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
