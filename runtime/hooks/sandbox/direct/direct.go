// Package direct provides the default Sandbox implementation for exec
// hooks: it spawns the command as a local subprocess via exec.CommandContext.
//
// This is the built-in, zero-config behavior. When ExecHookConfig.Sandbox
// is nil, the exec hooks fall back to direct.New() — so existing
// deployments get the exact same semantics they had before the Sandbox
// abstraction was introduced.
package direct

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
)

// ModeName is the mode identifier under which this backend registers
// with sandbox.RegisterFactory.
const ModeName = "direct"

// Sandbox runs exec-hook commands as local subprocesses.
type Sandbox struct {
	name string
}

// New returns a direct-mode sandbox with the given display name. An
// empty name defaults to "direct".
func New(name string) *Sandbox {
	if name == "" {
		name = ModeName
	}
	return &Sandbox{name: name}
}

// Factory is a sandbox.Factory that ignores the config block (direct
// mode has no configuration) and returns a ready Sandbox.
func Factory(name string, _ map[string]any) (sandbox.Sandbox, error) {
	return New(name), nil
}

// init registers the default backend globally at package load so that a
// RuntimeConfig entry of `mode: direct` resolves without any consumer
// opt-in. Other backends (docker, kubectl) are examples the consumer
// opts into by importing an example package or calling
// sandbox.RegisterFactory explicitly.
//
//nolint:gochecknoinits // registry self-registration is the plug-in pattern.
func init() {
	_ = sandbox.RegisterFactory(ModeName, Factory)
}

// Name returns the configured sandbox name.
func (s *Sandbox) Name() string { return s.name }

// defaultTimeout is used when Request.Timeout is zero or negative.
const defaultTimeout = 10 * time.Second

// grandchildWaitDelay bounds how long cmd.Wait() will block on I/O
// after the context expires. Set > 0 so an orphaned grandchild that
// inherited the stdout/stderr pipes cannot hang the hook indefinitely
// after SIGKILL — e.g. a bash wrapper whose `sleep` child outlives the
// kill.
const grandchildWaitDelay = 500 * time.Millisecond

// Spawn runs req.Command locally, piping req.Stdin into its stdin and
// collecting stdout and stderr. req.Env entries are merged on top of the
// host environment: each entry is either a bare env var name (its value
// is looked up on the host and forwarded) or a "KEY=value" pair
// (forwarded verbatim). A timeout or non-zero exit surfaces as a
// non-nil Response.Err.
//
//nolint:gocritic // Request is a public interface param; pass-by-value keeps ownership clear.
func (s *Sandbox) Spawn(ctx context.Context, req sandbox.Request) (sandbox.Response, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, req.Command, req.Args...) //#nosec G204 -- command from trusted config
	cmd.Stdin = bytes.NewReader(req.Stdin)
	cmd.WaitDelay = grandchildWaitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if len(req.Env) > 0 {
		cmd.Env = buildEnv(req.Env)
	}

	runErr := cmd.Run()
	return sandbox.Response{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
		Err:    runErr,
	}, nil
}

// buildEnv returns the child-process environment: host env as the base,
// with each entry in extras applied on top. Each entry is interpreted
// as follows:
//
//   - contains '=': treated as a literal "KEY=value" pair and appended.
//   - no '=': treated as a host env var name; its value is looked up via
//     os.LookupEnv and forwarded as "KEY=value" when present.
//
// The bare-name form matches the long-standing exec hook behavior where
// Env lists var names whose host values should be forwarded; the
// literal form is the more general case sandboxes generally want.
func buildEnv(extras []string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(extras))
	out = append(out, base...)
	for _, entry := range extras {
		if i := indexOfByte(entry, '='); i >= 0 {
			out = append(out, entry)
			continue
		}
		if val, ok := os.LookupEnv(entry); ok {
			out = append(out, fmt.Sprintf("%s=%s", entry, val))
		}
	}
	return out
}

// indexOfByte is strings.IndexByte without pulling in the strings
// import; keeps this package dependency-minimal.
func indexOfByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
