package config

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScenarioConfig_K8sManifestInterface(t *testing.T) {
	cfg := &ScenarioConfig{
		APIVersion: "promptkit.altairalabs.io/v1alpha1",
		Kind:       "Scenario",
		Metadata: ObjectMeta{
			Name: "test-scenario",
		},
		Spec: Scenario{
			ID: "original-id",
		},
	}

	if cfg.GetAPIVersion() != "promptkit.altairalabs.io/v1alpha1" {
		t.Errorf("GetAPIVersion() = %v, want promptkit.altairalabs.io/v1alpha1", cfg.GetAPIVersion())
	}

	if cfg.GetKind() != "Scenario" {
		t.Errorf("GetKind() = %v, want Scenario", cfg.GetKind())
	}

	if cfg.GetName() != "test-scenario" {
		t.Errorf("GetName() = %v, want test-scenario", cfg.GetName())
	}

	cfg.SetID("new-id")
	if cfg.Spec.ID != "new-id" {
		t.Errorf("SetID() did not update Spec.ID, got %v, want new-id", cfg.Spec.ID)
	}
}

func TestProviderConfig_K8sManifestInterface(t *testing.T) {
	cfg := &ProviderConfig{
		APIVersion: "promptkit.altairalabs.io/v1alpha1",
		Kind:       "Provider",
		Metadata: ObjectMeta{
			Name: "test-provider",
		},
		Spec: Provider{
			ID: "original-id",
		},
	}

	if cfg.GetAPIVersion() != "promptkit.altairalabs.io/v1alpha1" {
		t.Errorf("GetAPIVersion() = %v, want promptkit.altairalabs.io/v1alpha1", cfg.GetAPIVersion())
	}

	if cfg.GetKind() != "Provider" {
		t.Errorf("GetKind() = %v, want Provider", cfg.GetKind())
	}

	if cfg.GetName() != "test-provider" {
		t.Errorf("GetName() = %v, want test-provider", cfg.GetName())
	}

	cfg.SetID("new-id")
	if cfg.Spec.ID != "new-id" {
		t.Errorf("SetID() did not update Spec.ID, got %v, want new-id", cfg.Spec.ID)
	}
}

func TestScenarioConfigK8s_K8sManifestInterface(t *testing.T) {
	cfg := &ScenarioConfigK8s{
		APIVersion: "promptkit.altairalabs.io/v1alpha1",
		Kind:       "Scenario",
		Metadata: metav1.ObjectMeta{
			Name: "test-scenario-k8s",
		},
		Spec: Scenario{
			ID: "original-id",
		},
	}

	if cfg.GetAPIVersion() != "promptkit.altairalabs.io/v1alpha1" {
		t.Errorf("GetAPIVersion() = %v, want promptkit.altairalabs.io/v1alpha1", cfg.GetAPIVersion())
	}

	if cfg.GetKind() != "Scenario" {
		t.Errorf("GetKind() = %v, want Scenario", cfg.GetKind())
	}

	if cfg.GetName() != "test-scenario-k8s" {
		t.Errorf("GetName() = %v, want test-scenario-k8s", cfg.GetName())
	}

	cfg.SetID("new-id")
	if cfg.Spec.ID != "new-id" {
		t.Errorf("SetID() did not update Spec.ID, got %v, want new-id", cfg.Spec.ID)
	}
}

func TestProviderConfigK8s_K8sManifestInterface(t *testing.T) {
	cfg := &ProviderConfigK8s{
		APIVersion: "promptkit.altairalabs.io/v1alpha1",
		Kind:       "Provider",
		Metadata: metav1.ObjectMeta{
			Name: "test-provider-k8s",
		},
		Spec: Provider{
			ID: "original-id",
		},
	}

	if cfg.GetAPIVersion() != "promptkit.altairalabs.io/v1alpha1" {
		t.Errorf("GetAPIVersion() = %v, want promptkit.altairalabs.io/v1alpha1", cfg.GetAPIVersion())
	}

	if cfg.GetKind() != "Provider" {
		t.Errorf("GetKind() = %v, want Provider", cfg.GetKind())
	}

	if cfg.GetName() != "test-provider-k8s" {
		t.Errorf("GetName() = %v, want test-provider-k8s", cfg.GetName())
	}

	cfg.SetID("new-id")
	if cfg.Spec.ID != "new-id" {
		t.Errorf("SetID() did not update Spec.ID, got %v, want new-id", cfg.Spec.ID)
	}
}

func TestManifestHelpers_InterfaceCompliance(t *testing.T) {
	// Test that all config types implement the expected interface methods
	t.Run("ScenarioConfig methods exist", func(t *testing.T) {
		var _ interface {
			GetAPIVersion() string
			GetKind() string
			GetName() string
			SetID(string)
		} = &ScenarioConfig{}
	})

	t.Run("ProviderConfig methods exist", func(t *testing.T) {
		var _ interface {
			GetAPIVersion() string
			GetKind() string
			GetName() string
			SetID(string)
		} = &ProviderConfig{}
	})

	t.Run("ScenarioConfigK8s methods exist", func(t *testing.T) {
		var _ interface {
			GetAPIVersion() string
			GetKind() string
			GetName() string
			SetID(string)
		} = &ScenarioConfigK8s{}
	})

	t.Run("ProviderConfigK8s methods exist", func(t *testing.T) {
		var _ interface {
			GetAPIVersion() string
			GetKind() string
			GetName() string
			SetID(string)
		} = &ProviderConfigK8s{}
	})

	t.Run("EvalConfig methods exist", func(t *testing.T) {
		var _ interface {
			GetAPIVersion() string
			GetKind() string
			GetName() string
			SetID(string)
		} = &EvalConfig{}
	})

	t.Run("EvalConfigK8s methods exist", func(t *testing.T) {
		var _ interface {
			GetAPIVersion() string
			GetKind() string
			GetName() string
			SetID(string)
		} = &EvalConfigK8s{}
	})
}

func TestEvalConfig_K8sManifestInterface(t *testing.T) {
	cfg := &EvalConfig{
		APIVersion: "promptkit.altairalabs.io/v1alpha1",
		Kind:       "Eval",
		Metadata: ObjectMeta{
			Name: "test-eval",
		},
		Spec: Eval{
			ID: "original-id",
		},
	}

	if cfg.GetAPIVersion() != "promptkit.altairalabs.io/v1alpha1" {
		t.Errorf("GetAPIVersion() = %v, want promptkit.altairalabs.io/v1alpha1", cfg.GetAPIVersion())
	}

	if cfg.GetKind() != "Eval" {
		t.Errorf("GetKind() = %v, want Eval", cfg.GetKind())
	}

	if cfg.GetName() != "test-eval" {
		t.Errorf("GetName() = %v, want test-eval", cfg.GetName())
	}

	cfg.SetID("new-id")
	if cfg.Spec.ID != "new-id" {
		t.Errorf("SetID() did not update Spec.ID, got %v, want new-id", cfg.Spec.ID)
	}
}

func TestEvalConfigK8s_K8sManifestInterface(t *testing.T) {
	cfg := &EvalConfigK8s{
		APIVersion: "promptkit.altairalabs.io/v1alpha1",
		Kind:       "Eval",
		Metadata: metav1.ObjectMeta{
			Name: "test-eval-k8s",
		},
		Spec: Eval{
			ID: "k8s-id",
		},
	}

	if cfg.GetAPIVersion() != "promptkit.altairalabs.io/v1alpha1" {
		t.Errorf("GetAPIVersion() = %v, want promptkit.altairalabs.io/v1alpha1", cfg.GetAPIVersion())
	}

	if cfg.GetKind() != "Eval" {
		t.Errorf("GetKind() = %v, want Eval", cfg.GetKind())
	}

	if cfg.GetName() != "test-eval-k8s" {
		t.Errorf("GetName() = %v, want test-eval-k8s", cfg.GetName())
	}

	cfg.SetID("new-k8s-id")
	if cfg.Spec.ID != "new-k8s-id" {
		t.Errorf("SetID() did not update Spec.ID, got %v, want new-k8s-id", cfg.Spec.ID)
	}
}
