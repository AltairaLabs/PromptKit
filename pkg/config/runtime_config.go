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

	// EmbeddingProviders configures embedding providers (OpenAI, Gemini,
	// VoyageAI, Ollama). Used for RAG retrieval and as the shared
	// provider supplied to selectors via SelectorContext.Embeddings.
	// First entry becomes the default RAG provider unless one is set
	// programmatically via WithContextRetrieval.
	//nolint:lll // jsonschema tags require single line
	EmbeddingProviders []EmbeddingProviderConfig `yaml:"embedding_providers,omitempty" json:"embedding_providers,omitempty" jsonschema:"title=Embedding Providers,description=Embedding provider configurations"`

	// TTSProviders configures text-to-speech providers (OpenAI,
	// ElevenLabs, Cartesia). First entry becomes the default TTS
	// service unless one is set programmatically via WithTTS or
	// WithVADMode.
	//nolint:lll // jsonschema tags require single line
	TTSProviders []TTSProviderConfig `yaml:"tts_providers,omitempty" json:"tts_providers,omitempty" jsonschema:"title=TTS Providers,description=Text-to-speech provider configurations"`

	// STTProviders configures speech-to-text providers (OpenAI). First
	// entry becomes the default STT service unless one is set
	// programmatically via WithVADMode.
	//nolint:lll // jsonschema tags require single line
	STTProviders []STTProviderConfig `yaml:"stt_providers,omitempty" json:"stt_providers,omitempty" jsonschema:"title=STT Providers,description=Speech-to-text provider configurations"`

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

	// Sandboxes declares named sandbox backends that exec hooks can
	// reference via their "sandbox:" field. Each entry's "mode" selects
	// a factory registered via runtime/hooks/sandbox.RegisterFactory;
	// the remaining fields are passed verbatim to that factory as the
	// config map. The built-in "direct" mode resolves without any
	// consumer opt-in; other modes (docker_run, docker_exec, kubectl_exec,
	// or custom) require that their factory has been registered before
	// RuntimeConfig is loaded.
	//nolint:lll // jsonschema tags require single line
	Sandboxes map[string]*SandboxConfig `yaml:"sandboxes,omitempty" json:"sandboxes,omitempty" jsonschema:"title=Sandboxes,description=Named sandbox backends for exec-hook subprocess launch"`

	// Selectors declares named selectors that narrow the skill or tool
	// set surfaced to the LLM. Each entry spawns an external process
	// that reads a JSON query on stdin and writes selected IDs on
	// stdout. Referenced from spec.skills.selector / spec.tools.selector
	// by name.
	//nolint:lll // jsonschema tags require single line
	Selectors map[string]*SelectorConfig `yaml:"selectors,omitempty" json:"selectors,omitempty" jsonschema:"title=Selectors,description=External selector processes narrowing skill and tool candidate sets"`

	// Skills carries skill-specific runtime configuration, most notably
	// the selector binding. Pack-level skill definitions live in the
	// pack itself; this block only covers runtime wiring.
	//nolint:lll // jsonschema tags require single line
	Skills *SkillsConfig `yaml:"skills,omitempty" json:"skills,omitempty" jsonschema:"title=Skills,description=Runtime skill configuration"`

	// ToolSelector names a selector declared under spec.selectors that
	// narrows the pack-declared tool set surfaced to the LLM each turn.
	// System tools (skill__, a2a__, workflow__, mcp__, memory__) are
	// always preserved regardless of selection. When empty, the prompt's
	// full allowedTools list is offered to the provider.
	//
	// This is a flat field (not nested under tools:) because the existing
	// spec.tools map binds exec tool implementations.
	//nolint:lll // jsonschema tags require single line
	ToolSelector string `yaml:"tool_selector,omitempty" json:"tool_selector,omitempty" jsonschema:"title=ToolSelector,description=Name of a selector declared under spec.selectors used to narrow the LLM-visible tool set per turn"`
}

// SelectorConfig declares an external selector process. Command, Args,
// Env, TimeoutMs, and Sandbox mirror the exec hook shape.
type SelectorConfig struct {
	// Command is the path to the selector executable, resolved relative
	// to the config file by the loader.
	Command string `yaml:"command" json:"command" jsonschema:"title=Command,description=Path to the selector executable"`
	// Args are additional arguments passed to the command.
	//nolint:lll // jsonschema tags require single line
	Args []string `yaml:"args,omitempty" json:"args,omitempty" jsonschema:"title=Args,description=Additional command arguments"`
	// Env lists environment variable names required by the process.
	// Values come from the host environment.
	//nolint:lll // jsonschema tags require single line
	Env []string `yaml:"env,omitempty" json:"env,omitempty" jsonschema:"title=Env,description=Required environment variable names"`
	// TimeoutMs is the per-invocation timeout in milliseconds.
	//nolint:lll // jsonschema tags require single line
	TimeoutMs int `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty" jsonschema:"title=Timeout,description=Per-invocation timeout in milliseconds"`
	// Sandbox names a sandbox backend declared under spec.sandboxes.
	// When empty the built-in "direct" backend is used.
	//nolint:lll // jsonschema tags require single line
	Sandbox string `yaml:"sandbox,omitempty" json:"sandbox,omitempty" jsonschema:"title=Sandbox,description=Name of a sandbox declared under spec.sandboxes"`
}

// SkillsConfig carries runtime-level skill wiring.
type SkillsConfig struct {
	// Selector names a selector declared under spec.selectors.
	// When empty, all eligible skills are surfaced (current behavior).
	//nolint:lll // jsonschema tags require single line
	Selector string `yaml:"selector,omitempty" json:"selector,omitempty" jsonschema:"title=Selector,description=Name of a selector declared under spec.selectors"`
}

// EmbeddingProviderConfig declares an embedding provider — the
// declarative analog of programmatic providers.NewEmbeddingProvider
// constructors. Resolved by sdk.WithRuntimeConfig and stored by ID
// for later lookup; the first declared entry becomes the default
// retrieval and selector-context provider unless one is set
// programmatically via WithContextRetrieval.
type EmbeddingProviderConfig struct {
	// ID is a stable identifier used to reference this provider from
	// other config blocks (e.g. tool_selector via SelectorContext).
	// Empty falls back to the type name.
	ID string `yaml:"id,omitempty" json:"id,omitempty" jsonschema:"title=ID,description=Stable identifier"`
	// Type selects the implementation: openai, gemini, voyageai, ollama.
	//nolint:lll // jsonschema tags require single line
	Type string `yaml:"type" json:"type" jsonschema:"enum=openai,enum=gemini,enum=voyageai,enum=ollama,title=Type,description=Embedding provider type"`
	// Model overrides the provider's default embedding model.
	Model string `yaml:"model,omitempty" json:"model,omitempty" jsonschema:"title=Model,description=Embedding model name"`
	// BaseURL overrides the provider's default API endpoint. Useful for
	// self-hosted deployments and OpenAI-compatible gateways.
	//nolint:lll // jsonschema tags require single line
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty" jsonschema:"title=BaseURL,description=API endpoint override"`
	// Credential names how to obtain the API key. Same shape as the
	// chat-provider credential block.
	//nolint:lll // jsonschema tags require single line
	Credential *CredentialConfig `yaml:"credential,omitempty" json:"credential,omitempty" jsonschema:"title=Credential,description=API key resolution"`
	// AdditionalConfig carries provider-specific extras (e.g. VoyageAI
	// dimensions, input_type). Each provider documents its own keys.
	//nolint:lll // jsonschema tags require single line
	AdditionalConfig map[string]any `yaml:"additional_config,omitempty" json:"additional_config,omitempty" jsonschema:"title=Additional Config,description=Provider-specific extras"`
}

// TTSProviderConfig declares a text-to-speech provider — the
// declarative analog of programmatic tts.NewOpenAI / NewElevenLabs /
// NewCartesia constructors. Resolved by sdk.WithRuntimeConfig.
type TTSProviderConfig struct {
	// ID is a stable identifier; defaults to the type when empty.
	ID string `yaml:"id,omitempty" json:"id,omitempty" jsonschema:"title=ID,description=Stable identifier"`
	// Type selects the implementation: openai, elevenlabs, cartesia.
	//nolint:lll // jsonschema tags require single line
	Type string `yaml:"type" json:"type" jsonschema:"enum=openai,enum=elevenlabs,enum=cartesia,title=Type,description=TTS provider type"`
	// Model overrides the provider's default voice/model.
	Model string `yaml:"model,omitempty" json:"model,omitempty" jsonschema:"title=Model,description=Voice or model name"`
	// BaseURL overrides the provider's default API endpoint.
	//nolint:lll // jsonschema tags require single line
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty" jsonschema:"title=BaseURL,description=API endpoint override"`
	// Credential names how to obtain the API key.
	//nolint:lll // jsonschema tags require single line
	Credential *CredentialConfig `yaml:"credential,omitempty" json:"credential,omitempty" jsonschema:"title=Credential,description=API key resolution"`
	// AdditionalConfig carries provider-specific extras (Cartesia
	// `ws_url`, etc.).
	//nolint:lll // jsonschema tags require single line
	AdditionalConfig map[string]any `yaml:"additional_config,omitempty" json:"additional_config,omitempty" jsonschema:"title=Additional Config,description=Provider-specific extras"`
}

// STTProviderConfig declares a speech-to-text provider — the
// declarative analog of programmatic stt.NewOpenAI constructors.
// Today only "openai" is supported; more types slot in via the
// stt.RegisterSTTFactory pattern.
type STTProviderConfig struct {
	// ID is a stable identifier; defaults to the type when empty.
	ID string `yaml:"id,omitempty" json:"id,omitempty" jsonschema:"title=ID,description=Stable identifier"`
	// Type selects the implementation. Today only "openai".
	Type string `yaml:"type" json:"type" jsonschema:"enum=openai,title=Type,description=STT provider type"`
	// Model overrides the provider's default transcription model.
	//nolint:lll // jsonschema tags require single line
	Model string `yaml:"model,omitempty" json:"model,omitempty" jsonschema:"title=Model,description=Transcription model name"`
	// BaseURL overrides the provider's default API endpoint.
	//nolint:lll // jsonschema tags require single line
	BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty" jsonschema:"title=BaseURL,description=API endpoint override"`
	// Credential names how to obtain the API key.
	//nolint:lll // jsonschema tags require single line
	Credential *CredentialConfig `yaml:"credential,omitempty" json:"credential,omitempty" jsonschema:"title=Credential,description=API key resolution"`
	// AdditionalConfig carries provider-specific extras.
	//nolint:lll // jsonschema tags require single line
	AdditionalConfig map[string]any `yaml:"additional_config,omitempty" json:"additional_config,omitempty" jsonschema:"title=Additional Config,description=Provider-specific extras"`
}

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
	if err := s.validateEmbeddingProviders(); err != nil {
		return err
	}
	if err := s.validateTTSProviders(); err != nil {
		return err
	}
	if err := s.validateSTTProviders(); err != nil {
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
	if err := s.validateSandboxes(); err != nil {
		return err
	}
	if err := s.validateHooks(); err != nil {
		return err
	}
	return s.validateSelectors()
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

var validEmbeddingTypes = map[string]bool{
	"openai": true, "gemini": true, "voyageai": true, "ollama": true,
}

func (s *RuntimeConfigSpec) validateEmbeddingProviders() error {
	seen := make(map[string]bool, len(s.EmbeddingProviders))
	for i := range s.EmbeddingProviders {
		ep := &s.EmbeddingProviders[i]
		if ep.Type == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("embedding_providers[%d].type", i),
				Message: "embedding provider type is required",
			}
		}
		if !validEmbeddingTypes[ep.Type] {
			return &ValidationError{
				Field:   fmt.Sprintf("embedding_providers[%d].type", i),
				Message: "must be one of: openai, gemini, voyageai, ollama",
				Value:   ep.Type,
			}
		}
		id := ep.ID
		if id == "" {
			id = ep.Type
		}
		if seen[id] {
			return &ValidationError{
				Field:   fmt.Sprintf("embedding_providers[%d].id", i),
				Message: "duplicate embedding provider id",
				Value:   id,
			}
		}
		seen[id] = true
	}
	return nil
}

var validTTSTypes = map[string]bool{
	"openai": true, "elevenlabs": true, "cartesia": true,
}

func (s *RuntimeConfigSpec) validateTTSProviders() error {
	seen := make(map[string]bool, len(s.TTSProviders))
	for i := range s.TTSProviders {
		tp := &s.TTSProviders[i]
		if tp.Type == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("tts_providers[%d].type", i),
				Message: "TTS provider type is required",
			}
		}
		if !validTTSTypes[tp.Type] {
			return &ValidationError{
				Field:   fmt.Sprintf("tts_providers[%d].type", i),
				Message: "must be one of: openai, elevenlabs, cartesia",
				Value:   tp.Type,
			}
		}
		id := tp.ID
		if id == "" {
			id = tp.Type
		}
		if seen[id] {
			return &ValidationError{
				Field:   fmt.Sprintf("tts_providers[%d].id", i),
				Message: "duplicate TTS provider id",
				Value:   id,
			}
		}
		seen[id] = true
	}
	return nil
}

var validSTTTypes = map[string]bool{
	"openai": true,
}

func (s *RuntimeConfigSpec) validateSTTProviders() error {
	seen := make(map[string]bool, len(s.STTProviders))
	for i := range s.STTProviders {
		sp := &s.STTProviders[i]
		if sp.Type == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("stt_providers[%d].type", i),
				Message: "STT provider type is required",
			}
		}
		if !validSTTTypes[sp.Type] {
			return &ValidationError{
				Field:   fmt.Sprintf("stt_providers[%d].type", i),
				Message: "must be one of: openai",
				Value:   sp.Type,
			}
		}
		id := sp.ID
		if id == "" {
			id = sp.Type
		}
		if seen[id] {
			return &ValidationError{
				Field:   fmt.Sprintf("stt_providers[%d].id", i),
				Message: "duplicate STT provider id",
				Value:   id,
			}
		}
		seen[id] = true
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
		transports := 0
		if m.Command != "" {
			transports++
		}
		if m.URL != "" {
			transports++
		}
		if m.Source != "" {
			transports++
		}
		if transports != 1 {
			return &ValidationError{
				Field:   fmt.Sprintf("mcp_servers[%d]", i),
				Message: "exactly one of 'command' (stdio), 'url' (sse), or 'source' (host-provisioned) must be set",
			}
		}
		if m.Source != "" {
			if m.Scope == "" {
				return &ValidationError{
					Field:   fmt.Sprintf("mcp_servers[%d].scope", i),
					Message: "scope is required when source is set (run|scenario|session)",
				}
			}
			switch m.Scope {
			case "run", "scenario", "session":
				// ok
			default:
				return &ValidationError{
					Field:   fmt.Sprintf("mcp_servers[%d].scope", i),
					Message: fmt.Sprintf("invalid scope %q (want run|scenario|session)", m.Scope),
				}
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

var validHookTypes = map[string]bool{"provider": true, "tool": true, "session": true, "eval": true}

func (s *RuntimeConfigSpec) validateSandboxes() error {
	for name, sb := range s.Sandboxes {
		if sb == nil {
			continue
		}
		if sb.Mode == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("sandboxes[%s].mode", name),
				Message: "sandbox mode is required",
			}
		}
	}
	return nil
}

func (s *RuntimeConfigSpec) validateSelectors() error {
	for name, sel := range s.Selectors {
		if err := s.validateSelectorEntry(name, sel); err != nil {
			return err
		}
	}
	if err := s.validateSelectorRef("skills.selector", s.skillsSelectorRef()); err != nil {
		return err
	}
	return s.validateSelectorRef("tool_selector", s.ToolSelector)
}

// skillsSelectorRef returns the configured skills selector name, or empty if
// skills aren't configured. Centralizes the nil-Skills guard so the ref
// validation path doesn't have to care about it.
func (s *RuntimeConfigSpec) skillsSelectorRef() string {
	if s.Skills == nil {
		return ""
	}
	return s.Skills.Selector
}

// validateSelectorEntry checks a single entry in the selectors map. Nil
// entries are accepted (YAML treats explicitly-null values as absent);
// non-nil entries must carry a command and any referenced sandbox must be
// declared.
func (s *RuntimeConfigSpec) validateSelectorEntry(name string, sel *SelectorConfig) error {
	if sel == nil {
		return nil
	}
	if sel.Command == "" {
		return &ValidationError{
			Field:   fmt.Sprintf("selectors[%s].command", name),
			Message: "selector command is required",
		}
	}
	if sel.Sandbox == "" {
		return nil
	}
	if _, ok := s.Sandboxes[sel.Sandbox]; !ok {
		return &ValidationError{
			Field:   fmt.Sprintf("selectors[%s].sandbox", name),
			Message: "references a sandbox not declared under spec.sandboxes",
			Value:   sel.Sandbox,
		}
	}
	return nil
}

// validateSelectorRef asserts that a named selector reference resolves to a
// declared entry in spec.selectors. Empty references are a no-op so callers
// can pass optional-field values directly.
func (s *RuntimeConfigSpec) validateSelectorRef(field, name string) error {
	if name == "" {
		return nil
	}
	if _, ok := s.Selectors[name]; !ok {
		return &ValidationError{
			Field:   field,
			Message: "references a selector not declared under spec.selectors",
			Value:   name,
		}
	}
	return nil
}

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
				Message: "hook type is required (provider, tool, session, or eval)",
			}
		}
		if !validHookTypes[h.Hook] {
			return &ValidationError{
				Field:   fmt.Sprintf("hooks[%s].hook", name),
				Message: "must be one of: provider, tool, session, eval",
				Value:   h.Hook,
			}
		}
		if h.Sandbox != "" {
			if _, ok := s.Sandboxes[h.Sandbox]; !ok {
				return &ValidationError{
					Field:   fmt.Sprintf("hooks[%s].sandbox", name),
					Message: "references a sandbox not declared under spec.sandboxes",
					Value:   h.Sandbox,
				}
			}
		}
	}
	return nil
}
