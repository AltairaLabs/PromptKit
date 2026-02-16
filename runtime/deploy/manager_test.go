package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// createTestBinary creates a fake executable file in the given directory.
func createTestBinary(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAdapterBinaryName(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"cloudflare", "promptarena-deploy-cloudflare"},
		{"aws", "promptarena-deploy-aws"},
		{"gcp", "promptarena-deploy-gcp"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := AdapterBinaryName(tt.provider)
			if got != tt.want {
				t.Errorf("AdapterBinaryName(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestAdapterManager_Discover_ProjectLocal(t *testing.T) {
	projectDir := t.TempDir()
	adapterDir := filepath.Join(projectDir, ".promptarena", "adapters")
	if err := os.MkdirAll(adapterDir, 0o755); err != nil {
		t.Fatal(err)
	}

	binaryName := AdapterBinaryName("cloudflare")
	expectedPath := createTestBinary(t, adapterDir, binaryName)

	mgr := &AdapterManager{
		projectDir: projectDir,
		homeDir:    "", // no home dir to avoid interference
	}

	got, err := mgr.Discover("cloudflare")
	if err != nil {
		t.Fatalf("Discover returned unexpected error: %v", err)
	}
	if got != expectedPath {
		t.Errorf("Discover = %q, want %q", got, expectedPath)
	}
}

func TestAdapterManager_Discover_UserLevel(t *testing.T) {
	fakeHome := t.TempDir()
	adapterDir := filepath.Join(fakeHome, ".promptarena", "adapters")
	if err := os.MkdirAll(adapterDir, 0o755); err != nil {
		t.Fatal(err)
	}

	binaryName := AdapterBinaryName("aws")
	expectedPath := createTestBinary(t, adapterDir, binaryName)

	projectDir := t.TempDir() // empty project dir, no local adapter

	mgr := &AdapterManager{
		projectDir: projectDir,
		homeDir:    fakeHome,
	}

	got, err := mgr.Discover("aws")
	if err != nil {
		t.Fatalf("Discover returned unexpected error: %v", err)
	}
	if got != expectedPath {
		t.Errorf("Discover = %q, want %q", got, expectedPath)
	}
}

func TestAdapterManager_Discover_SystemPath(t *testing.T) {
	pathDir := t.TempDir()
	binaryName := AdapterBinaryName("gcp")
	expectedPath := createTestBinary(t, pathDir, binaryName)

	// Override PATH to include our temp directory
	t.Setenv("PATH", pathDir)

	mgr := &AdapterManager{
		projectDir: t.TempDir(), // empty project dir
		homeDir:    t.TempDir(), // empty home dir
	}

	got, err := mgr.Discover("gcp")
	if err != nil {
		t.Fatalf("Discover returned unexpected error: %v", err)
	}
	if got != expectedPath {
		t.Errorf("Discover = %q, want %q", got, expectedPath)
	}
}

func TestAdapterManager_Discover_NotFound(t *testing.T) {
	// Override PATH to empty so nothing is found on system PATH
	t.Setenv("PATH", "")

	mgr := &AdapterManager{
		projectDir: t.TempDir(),
		homeDir:    t.TempDir(),
	}

	_, err := mgr.Discover("nonexistent")
	if err == nil {
		t.Fatal("Discover should return an error when adapter is not found")
	}
}

func TestAdapterManager_Discover_Precedence(t *testing.T) {
	// Set up both project-local and user-level adapters
	projectDir := t.TempDir()
	fakeHome := t.TempDir()

	projectAdapterDir := filepath.Join(projectDir, ".promptarena", "adapters")
	userAdapterDir := filepath.Join(fakeHome, ".promptarena", "adapters")
	if err := os.MkdirAll(projectAdapterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(userAdapterDir, 0o755); err != nil {
		t.Fatal(err)
	}

	binaryName := AdapterBinaryName("cloudflare")
	projectPath := createTestBinary(t, projectAdapterDir, binaryName)
	_ = createTestBinary(t, userAdapterDir, binaryName) // user-level should be ignored

	mgr := &AdapterManager{
		projectDir: projectDir,
		homeDir:    fakeHome,
	}

	got, err := mgr.Discover("cloudflare")
	if err != nil {
		t.Fatalf("Discover returned unexpected error: %v", err)
	}
	if got != projectPath {
		t.Errorf("Discover = %q, want project-local path %q (precedence violated)", got, projectPath)
	}
}
