// Package dockerrun is an example sandbox implementation that runs each
// exec-hook invocation in a fresh disposable container via `docker run
// --rm -i`. The hook subprocess still sees the standard stdin/stdout
// JSON protocol; only the spawn mechanism changes.
//
// This is reference/example code — not imported by PromptKit core.
// Copy, adapt, or import the package directly; either path is fine.
// Register the factory in your own init or via sdk.WithSandboxFactory:
//
//	import (
//	    "github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
//	    "github.com/AltairaLabs/PromptKit/sdk/examples/sandboxes/dockerrun"
//	)
//
//	func init() {
//	    _ = sandbox.RegisterFactory(dockerrun.ModeName, dockerrun.Factory)
//	}
//
// Or programmatically, without the registry:
//
//	sb := dockerrun.New(dockerrun.Config{Image: "python:3.12-slim"})
//	hook := hooks.NewExecProviderHook(&hooks.ExecHookConfig{
//	    Command: "/hooks/pii.py",
//	    Sandbox: sb,
//	    ...
//	})
//
// Configuration (via the Factory config map):
//
//	mode: docker_run
//	image: python:3.12-slim       # required
//	network: none                 # optional; maps to --network=<value>
//	mounts:                       # optional; each value is passed as -v <spec>
//	  - ./hooks:/hooks:ro
//	extra_args:                   # optional; arbitrary extra docker flags
//	  - --memory=256m
//	  - --cpus=0.5
package dockerrun

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox/direct"
)

// ModeName is the identifier under which this backend registers with
// sandbox.RegisterFactory.
const ModeName = "docker_run"

// Config configures the docker-run backend.
type Config struct {
	// Image is the container image to run. Required.
	Image string
	// Network maps to `docker run --network=<value>`. Empty means
	// don't pass the flag (docker default).
	Network string
	// Mounts are passed as `-v <spec>` for each entry. Example:
	// "./hooks:/hooks:ro" bind-mounts the hooks directory read-only.
	Mounts []string
	// ExtraArgs are extra `docker run` arguments inserted after the
	// standard flags and before the image. Use sparingly.
	ExtraArgs []string
	// DockerPath overrides the `docker` binary name. Empty uses `docker`
	// resolved from PATH.
	DockerPath string
}

// Sandbox runs exec-hook commands inside a fresh docker container per call.
type Sandbox struct {
	name  string
	cfg   Config
	inner *direct.Sandbox
}

// New constructs a Sandbox from a Config. The inner direct backend
// handles the actual subprocess spawning, timeout, and stdin piping; we
// just rewrite argv to prepend `docker run`.
func New(cfg Config) *Sandbox {
	return NewNamed(ModeName, cfg)
}

// NewNamed is like New but lets the caller override the sandbox name
// (useful when multiple docker-run sandboxes are registered under
// different names in RuntimeConfig).
func NewNamed(name string, cfg Config) *Sandbox {
	return &Sandbox{
		name:  name,
		cfg:   cfg,
		inner: direct.New(name),
	}
}

// Factory is a sandbox.Factory compatible with RuntimeConfig-based
// resolution. It reads the config map, validates required fields, and
// returns a ready Sandbox.
func Factory(name string, cfg map[string]any) (sandbox.Sandbox, error) {
	image, _ := cfg["image"].(string)
	if image == "" {
		return nil, fmt.Errorf("dockerrun: required config field 'image' is missing or not a string")
	}
	out := Config{
		Image:      image,
		Network:    stringOr(cfg, "network"),
		Mounts:     stringSlice(cfg, "mounts"),
		ExtraArgs:  stringSlice(cfg, "extra_args"),
		DockerPath: stringOr(cfg, "docker_path"),
	}
	return NewNamed(name, out), nil
}

// Name returns the sandbox's name.
func (s *Sandbox) Name() string { return s.name }

// Spawn builds a `docker run --rm -i [flags] <image> <cmd> <args...>`
// argv and delegates to the inner direct sandbox, which handles stdin
// piping, timeout, and stdout/stderr collection. The hook subprocess
// inside the container receives req.Stdin on its stdin exactly as it
// would with the direct backend.
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

// buildArgs assembles the argv passed to `docker`. Exposed in-package
// for unit tests.
func (s *Sandbox) buildArgs(command string, args []string) []string {
	out := make([]string, 0, 8+len(s.cfg.Mounts)*2+len(s.cfg.ExtraArgs)+len(args))
	out = append(out, "run", "--rm", "-i")
	if s.cfg.Network != "" {
		out = append(out, "--network="+s.cfg.Network)
	}
	for _, m := range s.cfg.Mounts {
		out = append(out, "-v", m)
	}
	out = append(out, s.cfg.ExtraArgs...)
	out = append(out, s.cfg.Image)
	out = append(out, command)
	out = append(out, args...)
	return out
}

// stringOr returns cfg[key] as a string or "" if missing or wrong type.
func stringOr(cfg map[string]any, key string) string {
	s, _ := cfg[key].(string)
	return s
}

// stringSlice returns cfg[key] as []string or nil. Accepts either a
// []string (Go direct construction) or a []any of strings (YAML
// unmarshal).
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
