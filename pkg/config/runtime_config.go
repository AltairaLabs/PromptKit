package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// RuntimeConfig represents a runtime environment configuration in K8s-style manifest format.
// It declares how to run a pack in a specific environment: providers, tool bindings,
// MCP servers, state store, logging, and (in future phases) exec bindings and hooks.
//
// The pack defines _what_ the agent does (portable). RuntimeConfig defines _how_ to
// run it (environment-specific).
type RuntimeConfig struct {
	//nolint:lll // jsonschema tags require single line
	APIVersion string `yaml:"apiVersion" json:"apiVersion" jsonschema:"const=promptkit.altairalabs.ai/v1alpha1,title=API Version,description=Schema version identifier"`
	Kind       string `yaml:"kind" json:"kind" jsonschema:"const=RuntimeConfig,title=Kind,description=Resource type"`
	//nolint:lll // jsonschema tags require single line
	Metadata ObjectMeta `yaml:"metadata,omitempty" json:"metadata,omitempty" jsonschema:"title=Metadata,description=Resource metadata"`
	//nolint:lll // jsonschema tags require single line
	Spec RuntimeConfigSpec `yaml:"spec" json:"spec" jsonschema:"title=Spec,description=Runtime configuration specification"`
}

// RuntimeConfigSpec defines the runtime environment configuration.
type RuntimeConfigSpec struct {
	// Providers configures LLM providers (credentials, models, rate limits).
	//nolint:lll // jsonschema tags require single line
	Providers []Provider `yaml:"providers,omitempty" json:"providers,omitempty" jsonschema:"title=Providers,description=LLM provider configurations"`

	// Tools binds pack tool names to implementations (HTTP, mock, or exec).
	//nolint:lll // jsonschema tags require single line
	Tools map[string]*ToolSpec `yaml:"tools,omitempty" json:"tools,omitempty" jsonschema:"title=Tools,description=Tool implementation bindings keyed by tool name"`

	// MCPServers configures MCP tool servers.
	//nolint:lll // jsonschema tags require single line
	MCPServers []MCPServerConfig `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty" jsonschema:"title=MCP Servers,description=MCP server configurations"`

	// StateStore configures conversation state persistence.
	//nolint:lll // jsonschema tags require single line
	StateStore *StateStoreConfig `yaml:"state_store,omitempty" json:"state_store,omitempty" jsonschema:"title=State Store,description=Conversation state persistence configuration"`

	// Logging configures log levels, format, and per-module settings.
	//nolint:lll // jsonschema tags require single line
	Logging *LoggingConfigSpec `yaml:"logging,omitempty" json:"logging,omitempty" jsonschema:"title=Logging,description=Logging configuration"`

	// Evals binds pack eval type names to external process implementations.
	// Eval types not bound here resolve to built-in Go handlers.
	//nolint:lll // jsonschema tags require single line
	Evals map[string]*ExecBinding `yaml:"evals,omitempty" json:"evals,omitempty" jsonschema:"title=Evals,description=External eval process bindings keyed by eval type name"`

	// Hooks configures external process hooks for pipeline lifecycle events.
	//nolint:lll // jsonschema tags require single line
	Hooks map[string]*ExecHook `yaml:"hooks,omitempty" json:"hooks,omitempty" jsonschema:"title=Hooks,description=External hook process configurations"`
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

// State store type constants.
const (
	storeTypeMemory = "memory"
	storeTypeRedis  = "redis"
)

// LoadRuntimeConfig loads and validates a RuntimeConfig from a YAML file.
func LoadRuntimeConfig(path string) (*RuntimeConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is caller-controlled
	if err != nil {
		return nil, fmt.Errorf("reading runtime config %q: %w", path, err)
	}

	var rc RuntimeConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parsing runtime config %q: %w", path, err)
	}

	if rc.APIVersion != "promptkit.altairalabs.ai/v1alpha1" {
		return nil, &ValidationError{
			Field:   "apiVersion",
			Message: "must be promptkit.altairalabs.ai/v1alpha1",
			Value:   rc.APIVersion,
		}
	}
	if rc.Kind != "RuntimeConfig" {
		return nil, &ValidationError{
			Field:   "kind",
			Message: "must be RuntimeConfig",
			Value:   rc.Kind,
		}
	}

	if err := rc.Spec.Validate(); err != nil {
		return nil, fmt.Errorf("validating runtime config %q: %w", path, err)
	}

	return &rc, nil
}

// Validate validates the RuntimeConfigSpec.
func (s *RuntimeConfigSpec) Validate() error {
	if err := s.validateProviders(); err != nil {
		return err
	}
	if err := s.validateStateStore(); err != nil {
		return err
	}
	if s.Logging != nil {
		if err := s.Logging.Validate(); err != nil {
			return err
		}
	}
	if err := s.validateMCPServers(); err != nil {
		return err
	}
	if err := s.validateEvals(); err != nil {
		return err
	}
	return s.validateHooks()
}

func (s *RuntimeConfigSpec) validateProviders() error {
	for i := range s.Providers {
		p := &s.Providers[i]
		if p.Type == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("providers[%d].type", i),
				Message: "provider type is required",
			}
		}
		if p.Model == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("providers[%d].model", i),
				Message: "provider model is required",
			}
		}
	}
	return nil
}

func (s *RuntimeConfigSpec) validateStateStore() error {
	if s.StateStore == nil {
		return nil
	}
	if s.StateStore.Type != "" && s.StateStore.Type != storeTypeMemory && s.StateStore.Type != storeTypeRedis {
		return &ValidationError{
			Field:   "state_store.type",
			Message: "must be one of: memory, redis",
			Value:   s.StateStore.Type,
		}
	}
	if s.StateStore.Type == storeTypeRedis && s.StateStore.Redis == nil {
		return &ValidationError{
			Field:   "state_store.redis",
			Message: "redis configuration is required when type is redis",
		}
	}
	return nil
}

func (s *RuntimeConfigSpec) validateMCPServers() error {
	for i, m := range s.MCPServers {
		if m.Name == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("mcp_servers[%d].name", i),
				Message: "MCP server name is required",
			}
		}
		if m.Command == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("mcp_servers[%d].command", i),
				Message: "MCP server command is required",
			}
		}
	}
	return nil
}

func (s *RuntimeConfigSpec) validateEvals() error {
	for name, b := range s.Evals {
		if b.Command == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("evals[%s].command", name),
				Message: "eval command is required",
			}
		}
	}
	return nil
}

var validHookTypes = map[string]bool{"provider": true, "tool": true, "session": true}

func (s *RuntimeConfigSpec) validateHooks() error {
	for name, h := range s.Hooks {
		if h.Command == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("hooks[%s].command", name),
				Message: "hook command is required",
			}
		}
		if h.Hook == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("hooks[%s].hook", name),
				Message: "hook type is required (provider, tool, or session)",
			}
		}
		if !validHookTypes[h.Hook] {
			return &ValidationError{
				Field:   fmt.Sprintf("hooks[%s].hook", name),
				Message: "must be one of: provider, tool, session",
				Value:   h.Hook,
			}
		}
	}
	return nil
}
