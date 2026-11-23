package config

import (
	"fmt"
	"reflect"
	"testing"
)

// loadRuntimeConfigErr writes body to a temp file, loads it and returns the
// error, failing the test when the load succeeds.
func loadRuntimeConfigErr(t *testing.T, body string) (path string, err error) {
	t.Helper()
	path = writeTemp(t, "strict.runtime.yaml", body)
	_, err = LoadRuntimeConfig(path)
	if err == nil {
		t.Fatalf("LoadRuntimeConfig accepted a config with an unknown key:\n%s", body)
	}
	return path, err
}

func assertUnknownKeyError(t *testing.T, path string, err error, lines ...string) {
	t.Helper()
	want := fmt.Sprintf("parsing runtime config %q: yaml: unmarshal errors:", path)
	for _, l := range lines {
		want += "\n  " + l
	}
	if err.Error() != want {
		t.Errorf("error =\n%s\nwant\n%s", err, want)
	}
}

func TestLoadRuntimeConfig_RejectsUnknownRootKey(t *testing.T) {
	body := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metdata:
  name: typo
spec: {}
`
	path, err := loadRuntimeConfigErr(t, body)
	assertUnknownKeyError(t, path, err, "line 3: field metdata not found in type config.RuntimeConfig")
}

func TestLoadRuntimeConfig_RejectsUnknownSpecKey(t *testing.T) {
	body := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  state_stroe:
    type: memory
`
	path, err := loadRuntimeConfigErr(t, body)
	assertUnknownKeyError(t, path, err, "line 4: field state_stroe not found in type config.RuntimeConfigSpec")
}

func TestLoadRuntimeConfig_RejectsUnknownProviderKey(t *testing.T) {
	body := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  providers:
    - id: main
      type: openai
      model: gpt-4o
      base_ulr: https://gateway.internal/v1
`
	path, err := loadRuntimeConfigErr(t, body)
	assertUnknownKeyError(t, path, err, "line 8: field base_ulr not found in type config.Provider")
}

// Every unknown key is reported, at every depth, including inside map values
// (tools, hooks) and an inlined struct (a hook's exec binding).
func TestLoadRuntimeConfig_ReportsEveryUnknownNestedKey(t *testing.T) {
	body := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  tools:
    search:
      mode: live
      http:
        url: https://api.example.com/search
        methd: POST
  mcp_servers:
    - name: fs
      comand: npx
  state_store:
    type: redis
    redis:
      adress: localhost:6379
  hooks:
    redact:
      command: ./redact
      hook: provider
      timeout: 5000
`
	path, err := loadRuntimeConfigErr(t, body)
	assertUnknownKeyError(t, path, err,
		"line 9: field methd not found in type tools.HTTPConfig",
		"line 12: field comand not found in type config.MCPServerConfig",
		"line 16: field adress not found in type config.RedisConfig",
		"line 21: field timeout not found in type execconfig.ExecHook",
	)
}

// $schema is allowed at the top level only, where editors look for it.
func TestLoadRuntimeConfig_SchemaKeyAllowedAtTopLevelOnly(t *testing.T) {
	top := `$schema: https://promptkit.altairalabs.ai/schemas/v1alpha1/runtimeconfig.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  state_store:
    type: memory
`
	rc, err := LoadRuntimeConfig(writeTemp(t, "schema.runtime.yaml", top))
	if err != nil {
		t.Fatalf("top-level $schema rejected: %v", err)
	}
	if rc.Spec.StateStore == nil || rc.Spec.StateStore.Type != "memory" {
		t.Errorf("state_store = %+v, want type memory", rc.Spec.StateStore)
	}

	nested := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  $schema: x
`
	path, err := loadRuntimeConfigErr(t, nested)
	assertUnknownKeyError(t, path, err, "line 4: field $schema not found in type config.RuntimeConfigSpec")
}

// The fields that are deliberately open maps keep accepting arbitrary keys,
// and the values arrive intact.
func TestLoadRuntimeConfig_OpenMapsAcceptArbitraryKeys(t *testing.T) {
	body := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
metadata:
  name: open
  labels: {team-x: "a"}
  annotations: {note-y: "b"}
spec:
  providers:
    - id: main
      type: openai
      model: gpt-4o
      headers: {X-Anything: "h"}
      additional_config: {any_key: 1}
      platform:
        type: azure
        additional_config: {plat_key: "p"}
  embedding_providers:
    - type: voyageai
      additional_config: {dimensions: 512}
  tts_providers:
    - type: cartesia
      additional_config: {ws_url: "wss://x"}
  stt_providers:
    - type: openai
      additional_config: {stt_key: true}
  inference_providers:
    - type: huggingface
      additional_config: {dedicated: true}
  tools:
    lookup:
      mode: live
      input_schema: {type: object, x-custom: 1}
      extensions: {vendor_key: "v"}
      http:
        url: https://api.example.com
        headers: {X-H: "1"}
        request:
          static_body: {deep: {nested: 1}}
  mcp_servers:
    - name: src
      source: workspace
      scope: run
      source_args: {anything: "s"}
      env: {ANY_ENV: "e"}
  logging:
    commonFields: {svc: "pk"}
  sandboxes:
    box:
      mode: docker_run
      image: alpine:3
      network: none
`
	rc, err := LoadRuntimeConfig(writeTemp(t, "open.runtime.yaml", body))
	if err != nil {
		t.Fatalf("open maps rejected: %v", err)
	}
	s := rc.Spec
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"metadata.labels", rc.Metadata.Labels["team-x"], "a"},
		{"metadata.annotations", rc.Metadata.Annotations["note-y"], "b"},
		{"providers[0].headers", s.Providers[0].Headers["X-Anything"], "h"},
		{"providers[0].additional_config", s.Providers[0].AdditionalConfig["any_key"], 1},
		{"providers[0].platform.additional_config", s.Providers[0].Platform.AdditionalConfig["plat_key"], "p"},
		{"embedding_providers[0].additional_config", s.EmbeddingProviders[0].AdditionalConfig["dimensions"], 512},
		{"tts_providers[0].additional_config", s.TTSProviders[0].AdditionalConfig["ws_url"], "wss://x"},
		{"stt_providers[0].additional_config", s.STTProviders[0].AdditionalConfig["stt_key"], true},
		{"inference_providers[0].additional_config", s.InferenceProviders[0].AdditionalConfig["dedicated"], true},
		{"tools.lookup.input_schema", s.Tools["lookup"].InputSchema,
			map[string]any{"type": "object", "x-custom": 1}},
		{"tools.lookup.extensions", s.Tools["lookup"].Extensions["vendor_key"], "v"},
		{"tools.lookup.http.headers", s.Tools["lookup"].HTTPConfig.Headers["X-H"], "1"},
		{"tools.lookup.http.request.static_body", s.Tools["lookup"].HTTPConfig.Request.StaticBody["deep"],
			map[string]any{"nested": 1}},
		{"mcp_servers[0].source_args", s.MCPServers[0].SourceArgs["anything"], "s"},
		{"mcp_servers[0].env", s.MCPServers[0].Env["ANY_ENV"], "e"},
		{"logging.commonFields", s.Logging.CommonFields["svc"], "pk"},
		{"sandboxes.box config", s.Sandboxes["box"].Config,
			map[string]any{"image": "alpine:3", "network": "none"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
}

// A top-level $schema key next to a real typo leaves only the typo in the error.
func TestLoadRuntimeConfig_SchemaKeyNotReportedBesideATypo(t *testing.T) {
	body := `$schema: https://promptkit.altairalabs.ai/schemas/v1alpha1/runtimeconfig.json
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  tool_selectr: rerank
`
	path, err := loadRuntimeConfigErr(t, body)
	assertUnknownKeyError(t, path, err, "line 5: field tool_selectr not found in type config.RuntimeConfigSpec")
}

// An empty file still fails on its missing apiVersion, as before the decode
// became strict, not with a decode error.
func TestLoadRuntimeConfig_EmptyFileFailsOnAPIVersion(t *testing.T) {
	_, err := LoadRuntimeConfig(writeTemp(t, "empty.runtime.yaml", ""))
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "apiVersion" {
		t.Errorf("field = %q, want apiVersion", ve.Field)
	}
}
