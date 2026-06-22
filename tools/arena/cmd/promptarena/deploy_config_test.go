package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// withImportGlobals saves and restores the package-level flags that
// runDeployConfigImport reads, so tests can set them in isolation.
func withImportGlobals(t *testing.T, cfgPath, provider string, skipValidate bool) {
	t.Helper()
	oldCfg, oldSkip, oldProv := deployConfig, deployConfigImportSkipValidate, deployConfigImportProvider
	t.Cleanup(func() {
		deployConfig = oldCfg
		deployConfigImportSkipValidate = oldSkip
		deployConfigImportProvider = oldProv
	})
	deployConfig = cfgPath
	deployConfigImportSkipValidate = skipValidate
	deployConfigImportProvider = provider
}

func TestReadProfileFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	content := "endpoint: https://omnia.example.com\ntoken: secret-123\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp profile: %v", err)
	}

	profile, err := readProfile(path, nil)
	if err != nil {
		t.Fatalf("readProfile() error: %v", err)
	}
	if profile["endpoint"] != "https://omnia.example.com" {
		t.Errorf("endpoint = %v, want https://omnia.example.com", profile["endpoint"])
	}
	if profile["token"] != "secret-123" {
		t.Errorf("token = %v, want secret-123", profile["token"])
	}
}

func TestReadProfileFromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	content := `{"endpoint": "https://omnia.example.com", "providers": ["openai"]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp profile: %v", err)
	}

	profile, err := readProfile(path, nil)
	if err != nil {
		t.Fatalf("readProfile() error: %v", err)
	}
	if profile["endpoint"] != "https://omnia.example.com" {
		t.Errorf("endpoint = %v, want https://omnia.example.com", profile["endpoint"])
	}
	providers, ok := profile["providers"].([]interface{})
	if !ok || len(providers) != 1 || providers[0] != "openai" {
		t.Errorf("providers = %v, want [openai]", profile["providers"])
	}
}

func TestReadProfileFromStdin(t *testing.T) {
	stdin := strings.NewReader("endpoint: https://stdin.example.com\n")
	profile, err := readProfile("-", stdin)
	if err != nil {
		t.Fatalf("readProfile() error: %v", err)
	}
	if profile["endpoint"] != "https://stdin.example.com" {
		t.Errorf("endpoint = %v, want https://stdin.example.com", profile["endpoint"])
	}
}

func TestReadProfileMissingFile(t *testing.T) {
	_, err := readProfile("/nonexistent/profile.yaml", nil)
	if err == nil {
		t.Fatal("expected error for missing profile file")
	}
}

func TestReadProfileNotAMapping(t *testing.T) {
	stdin := strings.NewReader("- just\n- a\n- list\n")
	_, err := readProfile("-", stdin)
	if err == nil {
		t.Fatal("expected error when profile is not a mapping")
	}
}

// arenaManifest wraps a spec body in the k8s-style arena manifest envelope that
// LoadConfig expects (apiVersion/kind/metadata/spec).
func arenaManifest(specBody string) []byte {
	var b strings.Builder
	b.WriteString("apiVersion: promptkit.altairalabs.ai/v1alpha1\n")
	b.WriteString("kind: Arena\n")
	b.WriteString("metadata:\n  name: test\n")
	b.WriteString("spec:\n")
	b.WriteString(specBody)
	return []byte(b.String())
}

// parseDeployConfig is a test helper that extracts spec.deploy.config from a
// merged arena manifest.
func parseDeployConfig(t *testing.T, doc []byte) map[string]interface{} {
	t.Helper()
	var parsed struct {
		Spec struct {
			Deploy struct {
				Provider string                 `yaml:"provider"`
				Config   map[string]interface{} `yaml:"config"`
			} `yaml:"deploy"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		t.Fatalf("re-parse merged doc: %v\n%s", err, doc)
	}
	return parsed.Spec.Deploy.Config
}

func TestMergeProfileIntoExistingConfig(t *testing.T) {
	doc := arenaManifest(`  deploy:
    provider: omnia
    config:
      region: us-east-1
`)
	profile := map[string]interface{}{
		"endpoint": "https://omnia.example.com",
		"token":    "secret-123",
	}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	cfg := parseDeployConfig(t, merged)
	if cfg["region"] != "us-east-1" {
		t.Errorf("existing region lost: got %v", cfg["region"])
	}
	if cfg["endpoint"] != "https://omnia.example.com" {
		t.Errorf("endpoint = %v, want merged in", cfg["endpoint"])
	}
	if cfg["token"] != "secret-123" {
		t.Errorf("token = %v, want merged in", cfg["token"])
	}
}

func TestMergeProfileOverridesExistingKey(t *testing.T) {
	doc := arenaManifest(`  deploy:
    provider: omnia
    config:
      token: old-token
`)
	profile := map[string]interface{}{"token": "new-token"}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	cfg := parseDeployConfig(t, merged)
	if cfg["token"] != "new-token" {
		t.Errorf("token = %v, want new-token (override)", cfg["token"])
	}
}

func TestMergeProfileDeepMergesNestedMaps(t *testing.T) {
	doc := arenaManifest(`  deploy:
    provider: omnia
    config:
      auth:
        mode: token
        scope: read
`)
	profile := map[string]interface{}{
		"auth": map[string]interface{}{
			"scope": "write",
			"realm": "prod",
		},
	}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	cfg := parseDeployConfig(t, merged)
	auth, ok := cfg["auth"].(map[string]interface{})
	if !ok {
		t.Fatalf("auth not a map: %v", cfg["auth"])
	}
	if auth["mode"] != "token" {
		t.Errorf("auth.mode = %v, want token (preserved)", auth["mode"])
	}
	if auth["scope"] != "write" {
		t.Errorf("auth.scope = %v, want write (overridden)", auth["scope"])
	}
	if auth["realm"] != "prod" {
		t.Errorf("auth.realm = %v, want prod (added)", auth["realm"])
	}
}

func TestMergeProfileCreatesDeploySection(t *testing.T) {
	doc := arenaManifest(`  providers:
    - file: providers/openai.yaml
`)
	profile := map[string]interface{}{"endpoint": "https://omnia.example.com"}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	var parsed struct {
		Spec struct {
			Deploy struct {
				Provider string                 `yaml:"provider"`
				Config   map[string]interface{} `yaml:"config"`
			} `yaml:"deploy"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if parsed.Spec.Deploy.Provider != "omnia" {
		t.Errorf("deploy.provider = %q, want omnia", parsed.Spec.Deploy.Provider)
	}
	if parsed.Spec.Deploy.Config["endpoint"] != "https://omnia.example.com" {
		t.Errorf("deploy.config.endpoint = %v, want merged in", parsed.Spec.Deploy.Config["endpoint"])
	}
	// The existing spec content must survive.
	if !strings.Contains(string(merged), "providers/openai.yaml") {
		t.Errorf("existing spec content lost:\n%s", merged)
	}
}

func TestMergeProfileCreatesConfigUnderExistingDeploy(t *testing.T) {
	doc := arenaManifest(`  deploy:
    provider: omnia
`)
	profile := map[string]interface{}{"endpoint": "https://omnia.example.com"}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	cfg := parseDeployConfig(t, merged)
	if cfg["endpoint"] != "https://omnia.example.com" {
		t.Errorf("deploy.config.endpoint = %v, want merged in", cfg["endpoint"])
	}
}

func TestMergeProfileDoesNotOverrideExistingProvider(t *testing.T) {
	doc := arenaManifest(`  deploy:
    provider: agentcore
    config:
      region: us-east-1
`)
	profile := map[string]interface{}{"endpoint": "https://omnia.example.com"}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	var parsed struct {
		Spec struct {
			Deploy struct {
				Provider string `yaml:"provider"`
			} `yaml:"deploy"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if parsed.Spec.Deploy.Provider != "agentcore" {
		t.Errorf("deploy.provider = %q, want agentcore (preserved)", parsed.Spec.Deploy.Provider)
	}
}

func TestMergeProfilePreservesComments(t *testing.T) {
	doc := []byte(`# Arena config for my project
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  deploy:
    provider: omnia
    config:
      # region is required
      region: us-east-1
`)
	profile := map[string]interface{}{"token": "secret-123"}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	out := string(merged)
	if !strings.Contains(out, "# Arena config for my project") {
		t.Errorf("top-level comment lost:\n%s", out)
	}
	if !strings.Contains(out, "# region is required") {
		t.Errorf("inline comment lost:\n%s", out)
	}
}

func TestMergeProfilePreservesTwoSpaceIndent(t *testing.T) {
	doc := arenaManifest(`  deploy:
    provider: omnia
    config:
      workspace: existing-ws
`)
	profile := map[string]interface{}{"token": "abc"}

	merged, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err != nil {
		t.Fatalf("mergeProfileIntoConfigDoc() error: %v", err)
	}

	out := string(merged)
	// The whole document must keep the repo's 2-space indentation; a 4-space
	// reindent (yaml.Marshal's default) would rewrite every line.
	if !strings.Contains(out, "\n  name: test\n") {
		t.Errorf("metadata.name should stay at 2-space indent:\n%s", out)
	}
	if strings.Contains(out, "\n    name: test\n") {
		t.Errorf("document was reindented to 4 spaces:\n%s", out)
	}
}

func TestMergeProfileMissingSpecErrors(t *testing.T) {
	doc := []byte("apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Arena\n")
	profile := map[string]interface{}{"endpoint": "https://omnia.example.com"}

	_, err := mergeProfileIntoConfigDoc(doc, profile, "omnia")
	if err == nil {
		t.Fatal("expected error when manifest has no spec: section")
	}
}

func TestRunDeployConfigImportWritesAndSkipsValidate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.arena.yaml")
	manifest := arenaManifest(`  deploy:
    provider: omnia
    config:
      region: us-east-1
`)
	if err := os.WriteFile(cfgPath, manifest, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	profPath := filepath.Join(dir, "profile.json")
	profile := `{"token": "abc", "endpoint": "https://omnia.example.com"}`
	if err := os.WriteFile(profPath, []byte(profile), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	withImportGlobals(t, cfgPath, "omnia", true)

	if err := runDeployConfigImport(&cobra.Command{}, []string{profPath}); err != nil {
		t.Fatalf("runDeployConfigImport() error: %v", err)
	}

	out, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	cfg := parseDeployConfig(t, out)
	if cfg["token"] != "abc" {
		t.Errorf("token = %v, want abc (merged from profile)", cfg["token"])
	}
	if cfg["endpoint"] != "https://omnia.example.com" {
		t.Errorf("endpoint = %v, want merged from profile", cfg["endpoint"])
	}
	if cfg["region"] != "us-east-1" {
		t.Errorf("region = %v, want us-east-1 (preserved)", cfg["region"])
	}
}

func TestRunDeployConfigImportFromStdin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.arena.yaml")
	manifest := arenaManifest(`  deploy:
    provider: omnia
    config: {}
`)
	if err := os.WriteFile(cfgPath, manifest, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	withImportGlobals(t, cfgPath, "omnia", true)

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("workspace: my-ws\n"))
	if err := runDeployConfigImport(cmd, []string{"-"}); err != nil {
		t.Fatalf("runDeployConfigImport() error: %v", err)
	}

	out, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read merged config: %v", err)
	}
	cfg := parseDeployConfig(t, out)
	if cfg["workspace"] != "my-ws" {
		t.Errorf("workspace = %v, want my-ws (merged from stdin)", cfg["workspace"])
	}
}

func TestRunDeployConfigImportMissingConfigErrors(t *testing.T) {
	dir := t.TempDir()
	profPath := filepath.Join(dir, "profile.json")
	if err := os.WriteFile(profPath, []byte(`{"token": "abc"}`), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	withImportGlobals(t, filepath.Join(dir, "does-not-exist.yaml"), "omnia", true)

	err := runDeployConfigImport(&cobra.Command{}, []string{profPath})
	if err == nil {
		t.Fatal("expected error when config file is missing")
	}
}
