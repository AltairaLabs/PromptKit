package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadProvider covers LoadProvider + LoadSimpleK8sManifest for the shared
// provider-config path that remains in pkg/config after the arena config split.
func TestLoadProvider(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()
	path := filepath.Join(dir, "test.provider.yaml")
	content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    max_tokens: 1000
    top_p: 1.0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write provider: %v", err)
	}

	provider, err := LoadProvider(path)
	if err != nil {
		t.Fatalf("LoadProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("provider is nil")
	}
	if provider.ID != "provider1" {
		t.Errorf("ID = %q, want provider1", provider.ID)
	}
	if provider.Type != "openai" {
		t.Errorf("Type = %q, want openai", provider.Type)
	}
	if provider.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", provider.Model)
	}
}

func TestLoadProviderReadError(t *testing.T) {
	if _, err := LoadProvider(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadProviderSchemaError(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.provider.yaml")
	// Missing required spec fields → schema validation failure.
	if err := os.WriteFile(path, []byte("apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Provider\nmetadata:\n  name: p\nspec: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadProvider(path); err == nil {
		t.Error("expected schema validation error")
	}
}

// TestLoadSimpleK8sManifestKinds exercises the per-kind validation switch and the
// YAML-parse error branch of the shared generic loader.
func TestLoadSimpleK8sManifestParseError(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()
	path := filepath.Join(dir, "notyaml.provider.yaml")
	if err := os.WriteFile(path, []byte("kind: Provider\n\tbad: [unterminated"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSimpleK8sManifest[*ProviderConfigK8s](path, "Provider"); err == nil {
		t.Error("expected error for malformed yaml")
	}
}

// TestLoadSimpleK8sManifestKindBranches exercises the per-kind validation switch.
// Each kind routes to its validator; the provider data fails validation, which is
// expected — the point is to cover every branch of the switch.
func TestLoadSimpleK8sManifestKindBranches(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: X\nmetadata:\n  name: n\nspec: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, kind := range []string{"Scenario", kindEval, "Tool", "Persona"} {
		if _, err := LoadSimpleK8sManifest[*ProviderConfigK8s](path, kind); err == nil {
			t.Errorf("kind %q: expected validation error for mismatched data", kind)
		}
	}
}

// TestProviderConfigManifest covers the K8s manifest getters/setters that stay
// in pkg/config (manifest_helpers.go).
func TestProviderConfigManifest(t *testing.T) {
	pc := &ProviderConfig{APIVersion: "v1", Kind: "Provider"}
	pc.Metadata.Name = "p1"
	pc.SetID("id1")
	if pc.GetAPIVersion() != "v1" || pc.GetKind() != "Provider" || pc.GetName() != "p1" || pc.Spec.ID != "id1" {
		t.Errorf("ProviderConfig manifest accessors mismatch: %+v", pc)
	}

	k8s := &ProviderConfigK8s{APIVersion: "v1", Kind: "Provider"}
	k8s.Metadata.Name = "p2"
	k8s.SetID("id2")
	if k8s.GetAPIVersion() != "v1" || k8s.GetKind() != "Provider" || k8s.GetName() != "p2" || k8s.Spec.ID != "id2" {
		t.Errorf("ProviderConfigK8s manifest accessors mismatch: %+v", k8s)
	}
}

// TestResolveFilePath covers the shared path helper (helpers.go).
func TestResolveFilePath(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "abs.yaml")
	if got := ResolveFilePath("/base/config.yaml", abs); got != abs {
		t.Errorf("absolute path = %q, want %q", got, abs)
	}
	got := ResolveFilePath(filepath.Join("base", "config.yaml"), "sub/file.yaml")
	want := filepath.Join("base", "sub", "file.yaml")
	if got != want {
		t.Errorf("relative path = %q, want %q", got, want)
	}
}
