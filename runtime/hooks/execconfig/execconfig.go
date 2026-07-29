// Package execconfig holds the declarative shape of exec-hook, exec-eval and
// sandbox bindings.
//
// It exists to keep runtime a leaf module. These types are consumed by
// runtime/hooks (which builds hooks from them) and declared by pkg/config
// (which parses them out of a RuntimeConfig), so putting them in either place
// makes runtime and pkg require each other — a module cycle that let every
// published tag ship a stale internal require for five releases (issue #1713).
//
// pkg/config re-exports every type here as an alias, so `config.ExecHook` and
// `execconfig.ExecHook` are the same type and existing call sites are
// unaffected. Keep the type names stable: promptarena's schema generator keys
// its `$defs` off the bare Go type name, so renaming one silently rewrites the
// published JSON schema.
//
// This package must import nothing but the standard library. Anything it pulls
// in becomes a dependency of pkg/config, and a PromptKit import here would
// re-create the cycle it exists to break.
package execconfig

// SandboxConfig declares a named sandbox backend. Mode selects a
// factory registered via runtime/hooks/sandbox.RegisterFactory; every
// other field is passed to the factory as its config map.
type SandboxConfig struct {
	// Mode names a registered sandbox factory. "direct" ships in-tree
	// and needs no extra registration.
	//nolint:lll // jsonschema tags require single line
	Mode string `yaml:"mode" json:"mode" jsonschema:"title=Mode,description=Registered sandbox factory name (direct ships in-tree; docker_run/docker_exec/kubectl_exec are examples)"`

	// Config carries mode-specific configuration. Each factory
	// documents its own keys. For example, the docker_run example
	// expects "image", "network", and "mounts".
	Config map[string]any `yaml:",inline" json:"-" jsonschema:"-"`
}

// ExecBinding defines how to invoke an external process for tool or eval execution.
type ExecBinding struct {
	// Command is the path to the executable, resolved relative to the config file.
	Command string `yaml:"command" json:"command" jsonschema:"title=Command,description=Path to the executable"`
	// Runtime selects the execution mode: "exec" (one-shot, default) or "server" (long-running JSON-RPC).
	//nolint:lll // jsonschema tags require single line
	Runtime string `yaml:"runtime,omitempty" json:"runtime,omitempty" jsonschema:"enum=exec,enum=server,default=exec,title=Runtime,description=Execution mode"`
	// Env lists environment variable names required by the process. Values come from the host environment.
	//nolint:lll // jsonschema tags require single line
	Env []string `yaml:"env,omitempty" json:"env,omitempty" jsonschema:"title=Env,description=Required environment variable names"`
	// Args are additional arguments passed to the command.
	//nolint:lll // jsonschema tags require single line
	Args []string `yaml:"args,omitempty" json:"args,omitempty" jsonschema:"title=Args,description=Additional command arguments"`
	// TimeoutMs is the per-invocation timeout in milliseconds.
	//nolint:lll // jsonschema tags require single line
	TimeoutMs int `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty" jsonschema:"title=Timeout,description=Per-invocation timeout in milliseconds"`
	// Sandbox names a sandbox backend declared under spec.sandboxes.
	// When empty the built-in "direct" backend is used. Only applies to
	// exec hooks; ignored for exec tools and exec eval handlers (which
	// reuse this struct via embedding but do not yet support sandboxing).
	//nolint:lll // jsonschema tags require single line
	Sandbox string `yaml:"sandbox,omitempty" json:"sandbox,omitempty" jsonschema:"title=Sandbox,description=Name of a sandbox declared under spec.sandboxes"`
}

// ExecHook defines an external process hook bound to a pipeline lifecycle event.
type ExecHook struct {
	ExecBinding `yaml:",inline"`
	// Hook identifies the hook interface: "provider", "tool", "session", or "eval".
	// "eval" hooks are observational and ignore Phases / Mode — they run
	// once per eval result, fire-and-forget.
	//nolint:lll // jsonschema tags require single line
	Hook string `yaml:"hook" json:"hook" jsonschema:"enum=provider,enum=tool,enum=session,enum=eval,title=Hook,description=Hook interface type"`
	// Phases lists the hook phases to intercept (e.g. before_call, after_call).
	//nolint:lll // jsonschema tags require single line
	Phases []string `yaml:"phases,omitempty" json:"phases,omitempty" jsonschema:"title=Phases,description=Hook phases to intercept"`
	// Mode is the hook execution mode: "filter" (synchronous, can modify/deny) or "observe" (async, fire-and-forget).
	//nolint:lll // jsonschema tags require single line
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=filter,enum=observe,default=filter,title=Mode,description=Hook execution mode"`
}
