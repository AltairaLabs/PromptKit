package tools_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Issue #2081: metadata.name is the resource name (hyphens allowed by
// convention); spec.name, when set, is the function name the LLM calls.
func TestLoadK8sManifest_SpecNameIsFunctionName(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
		notWant  string
	}{
		{
			name: "spec.name wins over hyphenated metadata.name",
			manifest: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: save-order
spec:
  name: save_order
  description: Save an order
  input_schema:
    type: object
  output_schema:
    type: object
  mode: mock
`,
			want:    "save_order",
			notWant: "save-order",
		},
		{
			name: "metadata.name is the fallback when spec.name is empty",
			manifest: `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: save_order
spec:
  description: Save an order
  input_schema:
    type: object
  output_schema:
    type: object
  mode: mock
`,
			want: "save_order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := tools.NewRegistry()
			if err := registry.LoadToolFromBytes("order.tool.yaml", []byte(tt.manifest)); err != nil {
				t.Fatalf("LoadToolFromBytes: %v", err)
			}
			got := registry.Get(tt.want)
			if got == nil {
				t.Fatalf("tool %q not registered; have %v", tt.want, registry.List())
			}
			if got.Name != tt.want {
				t.Errorf("descriptor Name = %q, want %q", got.Name, tt.want)
			}
			if tt.notWant != "" && registry.Get(tt.notWant) != nil {
				t.Errorf("tool also registered under resource name %q", tt.notWant)
			}
		})
	}
}
