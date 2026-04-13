// Package sandbox defines the Sandbox interface used by exec-backed hooks
// to externalize where and how the hook subprocess is spawned.
//
// An exec hook's wire protocol with its subprocess is stdin-in-JSON →
// stdout-out-JSON. A Sandbox is the pluggable mechanism that gets those
// bytes into the right process and back out. Implementations can run the
// command locally (the default "direct" backend), inside a container, in
// a remote sidecar via "kubectl exec", against a cloud sandbox API, or
// anywhere else — the subprocess itself never knows or cares.
//
// Two ways to wire a sandbox to an exec hook:
//
//  1. Programmatic: construct a Sandbox yourself and set it on an
//     ExecHookConfig.Sandbox before passing the config to NewExec*Hook.
//     The caller owns the sandbox's lifetime.
//
//  2. Declarative: register a Factory against a mode name via
//     RegisterFactory (package-init) or sdk.WithSandboxFactory (per
//     conversation). A RuntimeConfig YAML can then refer to the sandbox
//     by name under spec.sandboxes, and individual hook entries can
//     select one with the "sandbox:" field.
//
// The Sandbox interface is intentionally narrow: request/response only,
// stdin/stdout bytes only, no streaming. That matches the existing
// exec-hook protocol exactly and keeps backend implementations small.
package sandbox

import (
	"context"
	"time"
)

// Sandbox launches an exec-hook subprocess and returns its output.
// Implementations must enforce req.Timeout; exceeding it returns a
// non-nil Response.Err describing the timeout. Implementations must not
// retain req.Stdin beyond the call.
type Sandbox interface {
	// Name returns a stable identifier used in logs and observability
	// events. Typically matches the mode name or the registered sandbox
	// name from RuntimeConfig.
	Name() string

	// Spawn runs the given command, pipes Stdin to its stdin, collects
	// stdout and stderr, and returns when the process exits, the timeout
	// expires, or the transport fails. A non-zero exit code, a timeout,
	// or a transport error all surface as a non-nil Response.Err —
	// implementations should include enough detail for callers to
	// distinguish those cases (e.g. by wrapping a context.DeadlineExceeded).
	Spawn(ctx context.Context, req Request) (Response, error)
}

// Request describes a single hook-subprocess invocation.
type Request struct {
	// Command is the program to execute. Sandboxes that spawn locally
	// (direct) treat this as a file path; sandboxes that run the command
	// somewhere else (kubectl exec, docker exec) typically pass it
	// verbatim to the remote runner.
	Command string

	// Args are positional arguments passed to Command.
	Args []string

	// Env contains additional environment entries in "KEY=value" form to
	// be merged on top of whatever the sandbox considers baseline. The
	// caller is responsible for resolving any host-side lookups (e.g.
	// os.LookupEnv) before populating this slice.
	Env []string

	// Stdin is the bytes piped to the subprocess's stdin. Typically the
	// exec-hook JSON payload. The sandbox does not retain this slice.
	Stdin []byte

	// Timeout bounds the invocation. Zero means the sandbox's own
	// default; negative is treated as zero.
	Timeout time.Duration
}

// Response is the outcome of a single Spawn call.
type Response struct {
	// Stdout is the bytes read from the subprocess's stdout.
	Stdout []byte
	// Stderr is the bytes read from the subprocess's stderr. Empty when
	// the sandbox cannot separately capture stderr (e.g. a remote
	// transport that conflates the streams); in that case stderr content
	// should be appended to Stdout or included in Err instead.
	Stderr []byte
	// Err is non-nil when the process exited non-zero, the timeout
	// expired, or the transport failed. Callers distinguish these cases
	// with errors.Is against standard sentinels (context.DeadlineExceeded,
	// exec.ExitError) where applicable.
	Err error
}

// Closer is an optional interface implemented by sandboxes that hold
// long-lived resources such as an HTTP client, a gRPC connection, or a
// pooled kubectl client. The hook registry calls Close on teardown.
type Closer interface {
	Close() error
}

// Factory builds a Sandbox from a mode-specific configuration block.
// Factories are registered under a mode name via RegisterFactory; the
// RuntimeConfig YAML refers to them by that name.
type Factory func(name string, cfg map[string]any) (Sandbox, error)
