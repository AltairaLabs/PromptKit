package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

const providerHeader = "apiVersion: promptkit.altairalabs.ai/v1alpha1\nkind: Provider\nmetadata:\n  name: p\n"

// A provider file is checked against the Go types, so a field this module
// declares loads in the release that adds it, with no schema to catch up.
func TestLoadProvider_AcceptsEveryFieldTheTypesDeclare(t *testing.T) {
	path := writeManifest(t, providerHeader+
		"spec:\n  type: openai\n  model: gpt-4o\n  message_metadata_template: \"<context>{{selection}}</context>\"\n")

	p, err := LoadProvider(path)
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}
	if p.MessageMetadataTemplate != "<context>{{selection}}</context>" {
		t.Errorf("MessageMetadataTemplate = %q", p.MessageMetadataTemplate)
	}
}

func TestLoadProvider_AllowsSchemaKey(t *testing.T) {
	path := writeManifest(t, "$schema: https://promptkit.altairalabs.ai/schemas/latest/provider.json\n"+
		providerHeader+"spec:\n  type: openai\n")

	if _, err := LoadProvider(path); err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}
}

func TestLoadProvider_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"unknown spec key", providerHeader + "spec:\n  type: openai\n  temprature: 0.2\n", "temprature"},
		{"unknown top-level key", providerHeader + "spec:\n  type: openai\nextra: 1\n", "extra"},
		{"unknown metadata key", "apiVersion: v1\nkind: Provider\nmetadata:\n  nmae: p\nspec:\n  type: openai\n", "nmae"},
		{"missing apiVersion", "kind: Provider\nspec:\n  type: openai\n", "apiVersion"},
		{"missing kind", "apiVersion: v1\nspec:\n  type: openai\n", "kind"},
		{"missing spec", "apiVersion: v1\nkind: Provider\n", "spec"},
		{"missing spec.type", providerHeader + "spec:\n  model: gpt-4o\n", "type"},
		{"unknown role", providerHeader + "spec:\n  type: openai\n  role: chat\n", "chat"},
		{"not a mapping", "- a\n- b\n", "mapping"},
		{"malformed yaml", "kind: Provider\n\tbad: [unterminated", "Provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadProvider(writeManifest(t, tt.content))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

// LoadSimpleK8sManifest serves other modules' manifest types too; it holds
// them to their own struct, whatever the kind.
type strictTestManifest struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   ObjectMeta `yaml:"metadata,omitempty"`
	Spec       struct {
		ID    string `yaml:"id,omitempty"`
		Steps int    `yaml:"steps"`
	} `yaml:"spec"`
}

func (m *strictTestManifest) GetAPIVersion() string { return m.APIVersion }
func (m *strictTestManifest) GetKind() string       { return m.Kind }
func (m *strictTestManifest) GetName() string       { return m.Metadata.Name }
func (m *strictTestManifest) SetID(id string)       { m.Spec.ID = id }

func TestLoadSimpleK8sManifest_HoldsAnyKindToItsType(t *testing.T) {
	head := "apiVersion: v1\nkind: Scenario\nmetadata:\n  name: s1\n"

	m, err := LoadSimpleK8sManifest[*strictTestManifest](writeManifest(t, head+"spec:\n  steps: 3\n"), "Scenario")
	if err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	if m.Spec.Steps != 3 || m.Spec.ID != "s1" {
		t.Errorf("decoded %+v", m.Spec)
	}

	_, err = LoadSimpleK8sManifest[*strictTestManifest](writeManifest(t, head+"spec:\n  stepz: 3\n"), "Scenario")
	if err == nil || !strings.Contains(err.Error(), "stepz") {
		t.Errorf("unknown key: err = %v, want it to name stepz", err)
	}
}
