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
}
