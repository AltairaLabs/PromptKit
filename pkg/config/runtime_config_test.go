package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeConfig_Valid(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: test-config
spec:
  providers:
    - id: test-provider
      type: openai
      model: gpt-4o
      credential:
        credential_env: OPENAI_API_KEY
      defaults:
        temperature: 0.7
        max_tokens: 4096
  tools:
    search:
      mode: live
      http:
        url: https://api.example.com/search
        method: POST
      timeout_ms: 5000
  mcp_servers:
    - name: filesystem
      command: npx
      args: ["-y", "@anthropic/mcp-filesystem"]
  state_store:
    type: redis
    redis:
      address: localhost:6379
  logging:
    defaultLevel: debug
    format: json
  evals:
    sentiment_check:
      command: ./evals/sentiment-check.py
      timeout_ms: 5000
  hooks:
    pii_redactor:
      command: ./hooks/pii-redactor
      hook: provider
      phases: [before_call, after_call]
      mode: filter
`
	path := writeTemp(t, "valid.runtime.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rc.Metadata.Name != "test-config" {
		t.Errorf("metadata.name = %q, want %q", rc.Metadata.Name, "test-config")
	}
	if len(rc.Spec.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(rc.Spec.Providers))
	}
	if rc.Spec.Providers[0].Type != "openai" {
		t.Errorf("provider type = %q, want %q", rc.Spec.Providers[0].Type, "openai")
	}
	if rc.Spec.Providers[0].ID != "test-provider" {
		t.Errorf("provider id = %q, want %q", rc.Spec.Providers[0].ID, "test-provider")
	}
	if rc.Spec.Providers[0].Defaults.MaxTokens != 4096 {
		t.Errorf("provider max_tokens = %d, want %d", rc.Spec.Providers[0].Defaults.MaxTokens, 4096)
	}
	if len(rc.Spec.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(rc.Spec.Tools))
	}
	if rc.Spec.Tools["search"].Mode != "live" {
		t.Errorf("tool mode = %q, want %q", rc.Spec.Tools["search"].Mode, "live")
	}
	if len(rc.Spec.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(rc.Spec.MCPServers))
	}
	if rc.Spec.MCPServers[0].Name != "filesystem" {
		t.Errorf("MCP server name = %q, want %q", rc.Spec.MCPServers[0].Name, "filesystem")
	}
	if rc.Spec.StateStore.Type != "redis" {
		t.Errorf("state_store.type = %q, want %q", rc.Spec.StateStore.Type, "redis")
	}
	if rc.Spec.Logging.DefaultLevel != "debug" {
		t.Errorf("logging.defaultLevel = %q, want %q", rc.Spec.Logging.DefaultLevel, "debug")
	}
	if rc.Spec.Evals["sentiment_check"].Command != "./evals/sentiment-check.py" {
		t.Errorf("eval command = %q, want %q", rc.Spec.Evals["sentiment_check"].Command, "./evals/sentiment-check.py")
	}
	if rc.Spec.Hooks["pii_redactor"].Hook != "provider" {
		t.Errorf("hook type = %q, want %q", rc.Spec.Hooks["pii_redactor"].Hook, "provider")
	}
}

func TestLoadRuntimeConfig_EmptySpec(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec: {}
`
	path := writeTemp(t, "empty.runtime.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("empty spec should be valid: %v", err)
	}
	if len(rc.Spec.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(rc.Spec.Providers))
	}
}

func TestLoadRuntimeConfig_InvalidAPIVersion(t *testing.T) {
	yaml := `
apiVersion: wrong/v1
kind: RuntimeConfig
spec: {}
`
	path := writeTemp(t, "bad-api.runtime.yaml", yaml)
	_, err := LoadRuntimeConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid apiVersion")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "apiVersion" {
		t.Errorf("field = %q, want %q", ve.Field, "apiVersion")
	}
}

func TestLoadRuntimeConfig_InvalidKind(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: WrongKind
spec: {}
`
	path := writeTemp(t, "bad-kind.runtime.yaml", yaml)
	_, err := LoadRuntimeConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "kind" {
		t.Errorf("field = %q, want %q", ve.Field, "kind")
	}
}

func TestLoadRuntimeConfig_FileNotFound(t *testing.T) {
	_, err := LoadRuntimeConfig("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadRuntimeConfig_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "bad.yaml", "{{{{not yaml")
	_, err := LoadRuntimeConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestRuntimeConfigSpec_Validate_ProviderMissingType(t *testing.T) {
	s := &RuntimeConfigSpec{
		Providers: []Provider{{Model: "gpt-4o"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing provider type")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "providers[0].type" {
		t.Errorf("field = %q, want %q", ve.Field, "providers[0].type")
	}
}

func TestRuntimeConfigSpec_Validate_ProviderMissingModel(t *testing.T) {
	s := &RuntimeConfigSpec{
		Providers: []Provider{{Type: "openai"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing provider model")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if ve.Field != "providers[0].model" {
		t.Errorf("field = %q, want %q", ve.Field, "providers[0].model")
	}
}

func TestRuntimeConfigSpec_Validate_InvalidStateStoreType(t *testing.T) {
	s := &RuntimeConfigSpec{
		StateStore: &StateStoreConfig{Type: "dynamodb"},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for invalid state store type")
	}
}

func TestRuntimeConfigSpec_Validate_RedisMissingConfig(t *testing.T) {
	s := &RuntimeConfigSpec{
		StateStore: &StateStoreConfig{Type: "redis"},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for redis without config")
	}
}

func TestRuntimeConfigSpec_Validate_MCPServerMissingName(t *testing.T) {
	s := &RuntimeConfigSpec{
		MCPServers: []MCPServerConfig{{Command: "npx"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing MCP server name")
	}
}

func TestRuntimeConfigSpec_Validate_MCPServerMissingCommand(t *testing.T) {
	s := &RuntimeConfigSpec{
		MCPServers: []MCPServerConfig{{Name: "test"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing MCP server command")
	}
}

func TestRuntimeConfigSpec_Validate_EvalMissingCommand(t *testing.T) {
	s := &RuntimeConfigSpec{
		Evals: map[string]*ExecBinding{
			"test": {}, // missing command
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing eval command")
	}
}

func TestRuntimeConfigSpec_Validate_HookMissingCommand(t *testing.T) {
	s := &RuntimeConfigSpec{
		Hooks: map[string]*ExecHook{
			"test": {Hook: "provider"},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing hook command")
	}
}

func TestRuntimeConfigSpec_Validate_HookMissingType(t *testing.T) {
	s := &RuntimeConfigSpec{
		Hooks: map[string]*ExecHook{
			"test": {ExecBinding: ExecBinding{Command: "./hooks/test"}},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing hook type")
	}
}

func TestRuntimeConfigSpec_Validate_HookInvalidType(t *testing.T) {
	s := &RuntimeConfigSpec{
		Hooks: map[string]*ExecHook{
			"test": {
				ExecBinding: ExecBinding{Command: "./hooks/test"},
				Hook:        "invalid",
			},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for invalid hook type")
	}
}

func TestRuntimeConfigSpec_Validate_InvalidLogging(t *testing.T) {
	s := &RuntimeConfigSpec{
		Logging: &LoggingConfigSpec{DefaultLevel: "invalid"},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for invalid logging config")
	}
}

func TestRuntimeConfigSpec_Validate_SandboxMissingMode(t *testing.T) {
	s := &RuntimeConfigSpec{
		Sandboxes: map[string]*SandboxConfig{
			"my_sandbox": {Config: map[string]any{"image": "alpine"}},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing sandbox mode")
	}
}

func TestRuntimeConfigSpec_Validate_HookReferencesUndeclaredSandbox(t *testing.T) {
	s := &RuntimeConfigSpec{
		Hooks: map[string]*ExecHook{
			"h": {
				ExecBinding: ExecBinding{Command: "./x", Sandbox: "ghost"},
				Hook:        "provider",
			},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error referencing undeclared sandbox")
	}
}

func TestLoadRuntimeConfig_SandboxesInline(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  sandboxes:
    my_docker:
      mode: docker_run
      image: python:3.12-slim
      network: none
      mounts:
        - ./hooks:/hooks:ro
  hooks:
    pii:
      command: /hooks/pii.py
      hook: provider
      sandbox: my_docker
`
	path := writeTemp(t, "rc.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	sb, ok := rc.Spec.Sandboxes["my_docker"]
	if !ok {
		t.Fatalf("sandbox my_docker not loaded")
	}
	if sb.Mode != "docker_run" {
		t.Errorf("Mode = %q, want docker_run", sb.Mode)
	}
	if got := sb.Config["image"]; got != "python:3.12-slim" {
		t.Errorf("Config[image] = %v, want python:3.12-slim", got)
	}
	if got := sb.Config["network"]; got != "none" {
		t.Errorf("Config[network] = %v, want none", got)
	}
	hook := rc.Spec.Hooks["pii"]
	if hook.Sandbox != "my_docker" {
		t.Errorf("hook.Sandbox = %q, want my_docker", hook.Sandbox)
	}
}

func TestRuntimeConfigSpec_Validate_TTSProviderMissingType(t *testing.T) {
	s := &RuntimeConfigSpec{TTSProviders: []TTSProviderConfig{{Model: "x"}}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for missing TTS provider type")
	}
}

func TestRuntimeConfigSpec_Validate_TTSProviderInvalidType(t *testing.T) {
	s := &RuntimeConfigSpec{TTSProviders: []TTSProviderConfig{{Type: "bogus"}}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for invalid TTS provider type")
	}
}

func TestRuntimeConfigSpec_Validate_TTSProviderDuplicateID(t *testing.T) {
	s := &RuntimeConfigSpec{TTSProviders: []TTSProviderConfig{
		{Type: "openai"}, {Type: "openai"},
	}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected duplicate TTS id error")
	}
}

func TestRuntimeConfigSpec_Validate_STTProviderMissingType(t *testing.T) {
	s := &RuntimeConfigSpec{STTProviders: []STTProviderConfig{{Model: "x"}}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for missing STT provider type")
	}
}

func TestRuntimeConfigSpec_Validate_STTProviderInvalidType(t *testing.T) {
	s := &RuntimeConfigSpec{STTProviders: []STTProviderConfig{{Type: "bogus"}}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for invalid STT provider type")
	}
}

func TestRuntimeConfigSpec_Validate_STTProviderDuplicateID(t *testing.T) {
	s := &RuntimeConfigSpec{STTProviders: []STTProviderConfig{
		{Type: "openai"}, {Type: "openai"},
	}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected duplicate STT id error")
	}
}

func TestLoadRuntimeConfig_TTSAndSTTYAML(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  tts_providers:
    - id: voice
      type: elevenlabs
      model: eleven_turbo_v2
      credential:
        credential_env: ELEVEN_API_KEY
    - id: cart
      type: cartesia
      additional_config:
        ws_url: wss://api.cartesia.ai/tts/websocket
  stt_providers:
    - type: openai
      model: whisper-1
      credential:
        credential_env: OPENAI_API_KEY
`
	path := writeTemp(t, "rc.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if len(rc.Spec.TTSProviders) != 2 {
		t.Fatalf("expected 2 tts providers, got %d", len(rc.Spec.TTSProviders))
	}
	if rc.Spec.TTSProviders[0].ID != "voice" || rc.Spec.TTSProviders[0].Type != "elevenlabs" {
		t.Errorf("first tts mis-parsed: %+v", rc.Spec.TTSProviders[0])
	}
	if got := rc.Spec.TTSProviders[1].AdditionalConfig["ws_url"]; got != "wss://api.cartesia.ai/tts/websocket" {
		t.Errorf("cartesia ws_url = %v", got)
	}
	if len(rc.Spec.STTProviders) != 1 || rc.Spec.STTProviders[0].Type != "openai" {
		t.Errorf("stt providers mis-parsed: %+v", rc.Spec.STTProviders)
	}
}

func TestRuntimeConfigSpec_Validate_EmbeddingProviderMissingType(t *testing.T) {
	s := &RuntimeConfigSpec{
		EmbeddingProviders: []EmbeddingProviderConfig{{Model: "x"}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for missing embedding provider type")
	}
}

func TestRuntimeConfigSpec_Validate_EmbeddingProviderInvalidType(t *testing.T) {
	s := &RuntimeConfigSpec{
		EmbeddingProviders: []EmbeddingProviderConfig{{Type: "bogus"}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for invalid embedding provider type")
	}
}

func TestRuntimeConfigSpec_Validate_EmbeddingProviderDuplicateID(t *testing.T) {
	s := &RuntimeConfigSpec{
		EmbeddingProviders: []EmbeddingProviderConfig{
			{ID: "main", Type: "openai"},
			{ID: "main", Type: "voyageai"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestRuntimeConfigSpec_Validate_EmbeddingProviderDefaultIDFromType(t *testing.T) {
	s := &RuntimeConfigSpec{
		EmbeddingProviders: []EmbeddingProviderConfig{
			{Type: "openai"},
			{Type: "openai"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected default-id collision error")
	}
}

func TestLoadRuntimeConfig_EmbeddingProvidersYAML(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  embedding_providers:
    - id: rag
      type: openai
      model: text-embedding-3-small
      credential:
        credential_env: OPENAI_API_KEY
    - id: voyage
      type: voyageai
      model: voyage-3
      additional_config:
        dimensions: 1024
        input_type: query
`
	path := writeTemp(t, "rc.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if len(rc.Spec.EmbeddingProviders) != 2 {
		t.Fatalf("expected 2 embedding providers, got %d", len(rc.Spec.EmbeddingProviders))
	}
	if rc.Spec.EmbeddingProviders[0].ID != "rag" || rc.Spec.EmbeddingProviders[0].Type != "openai" {
		t.Errorf("first provider mis-parsed: %+v", rc.Spec.EmbeddingProviders[0])
	}
	if got := rc.Spec.EmbeddingProviders[1].AdditionalConfig["dimensions"]; got != 1024 {
		t.Errorf("voyageai dimensions = %v, want 1024", got)
	}
}

func TestRuntimeConfigSpec_Validate_SelectorMissingCommand(t *testing.T) {
	s := &RuntimeConfigSpec{
		Selectors: map[string]*SelectorConfig{
			"rerank": {TimeoutMs: 1000},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for missing selector command")
	}
}

func TestRuntimeConfigSpec_Validate_SelectorReferencesUndeclaredSandbox(t *testing.T) {
	s := &RuntimeConfigSpec{
		Selectors: map[string]*SelectorConfig{
			"rerank": {Command: "./r", Sandbox: "ghost"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for undeclared sandbox ref")
	}
}

func TestRuntimeConfigSpec_Validate_SkillsReferencesUndeclaredSelector(t *testing.T) {
	s := &RuntimeConfigSpec{
		Skills: &SkillsConfig{Selector: "ghost"},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for undeclared selector ref")
	}
}

func TestRuntimeConfigSpec_Validate_ToolSelectorReferencesUndeclared(t *testing.T) {
	s := &RuntimeConfigSpec{ToolSelector: "ghost"}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for undeclared tool_selector ref")
	}
}

func TestLoadRuntimeConfig_ToolSelector(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  selectors:
    rerank:
      command: ./selectors/rerank
  tool_selector: rerank
`
	path := writeTemp(t, "rc.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if rc.Spec.ToolSelector != "rerank" {
		t.Errorf("ToolSelector = %q, want rerank", rc.Spec.ToolSelector)
	}
}

func TestLoadRuntimeConfig_SelectorsYAML(t *testing.T) {
	yaml := `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  selectors:
    rerank:
      command: ./selectors/rerank
      args: [--endpoint, https://rerank.internal]
      env: [RERANK_API_KEY]
      timeout_ms: 2000
  skills:
    selector: rerank
`
	path := writeTemp(t, "rc.yaml", yaml)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	sel, ok := rc.Spec.Selectors["rerank"]
	if !ok {
		t.Fatalf("selector rerank not loaded")
	}
	if sel.Command != "./selectors/rerank" {
		t.Errorf("Command = %q", sel.Command)
	}
	if sel.TimeoutMs != 2000 {
		t.Errorf("TimeoutMs = %d", sel.TimeoutMs)
	}
	if rc.Spec.Skills == nil || rc.Spec.Skills.Selector != "rerank" {
		t.Errorf("skills.selector not loaded: %+v", rc.Spec.Skills)
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}
