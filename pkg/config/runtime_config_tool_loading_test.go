package config

import (
	"strings"
	"testing"
)

func TestLoadRuntimeConfig_ToolLoading(t *testing.T) {
	path := writeTemp(t, "rc.yaml", `
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: RuntimeConfig
spec:
  tool_loading:
    mode: stable
`)
	rc, err := LoadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig: %v", err)
	}
	if rc.Spec.ToolLoading == nil || rc.Spec.ToolLoading.Mode != ToolLoadingModeStable {
		t.Fatalf("ToolLoading = %+v, want mode stable", rc.Spec.ToolLoading)
	}
}

func TestRuntimeConfigSpec_Validate_ToolLoadingMode(t *testing.T) {
	for _, mode := range []string{"", ToolLoadingModeRebuild, ToolLoadingModeStable} {
		s := &RuntimeConfigSpec{ToolLoading: &ToolLoadingConfig{Mode: mode}}
		if err := s.Validate(); err != nil {
			t.Errorf("mode %q: %v", mode, err)
		}
	}
	s := &RuntimeConfigSpec{ToolLoading: &ToolLoadingConfig{Mode: "lazy"}}
	err := s.Validate()
	if err == nil {
		t.Fatal("an unknown mode must fail validation")
	}
	want := `tool_loading.mode: must be "rebuild" or "stable" (got: lazy)`
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err, want)
	}
}
