package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ArenaConfig represents a K8s-style arena configuration manifest
type ArenaConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec       config.Config     `yaml:"spec"`
}

// newTestEngine creates an engine from a config object by writing it to a temp file
// in the test's temp directory. This is a helper for migrating tests to the new
// NewEngine(configPath) API.
func newTestEngine(t *testing.T, tmpDir string, cfg *config.Config) *Engine {
	t.Helper()

	// Write config to a K8s-style manifest file
	configPath := filepath.Join(tmpDir, "arena.yaml")

	arenaConfig := ArenaConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Arena",
		Metadata: metav1.ObjectMeta{
			Name: "test-arena",
		},
		Spec: *cfg,
	}

	data, err := yaml.Marshal(arenaConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Create engine using new API
	eng, err := NewEngineFromConfigFile(configPath)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	return eng
}

// newTestEngineWithError creates an engine from a config object and returns both
// the engine and any error, allowing tests to verify error handling.
func newTestEngineWithError(t *testing.T, tmpDir string, cfg *config.Config) (*Engine, error) {
	t.Helper()

	// Write config to a K8s-style manifest file
	configPath := filepath.Join(tmpDir, "arena.yaml")

	arenaConfig := ArenaConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Arena",
		Metadata: metav1.ObjectMeta{
			Name: "test-arena",
		},
		Spec: *cfg,
	}

	data, err := yaml.Marshal(arenaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	// Create engine using new API
	return NewEngineFromConfigFile(configPath)
}
