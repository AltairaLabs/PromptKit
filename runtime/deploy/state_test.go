package deploy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	original := &State{
		Provider:       "aws-lambda",
		Environment:    "production",
		LastDeployed:   "2026-01-15T10:30:00Z",
		PackVersion:    "1.0.0",
		PackChecksum:   "sha256:abc123",
		AdapterVersion: "0.5.0",
		State:          "c29tZSBvcGFxdWUgc3RhdGU=",
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil state")
	}

	// Version is always set by Save
	if loaded.Version != stateVersion {
		t.Errorf("Version = %d, want %d", loaded.Version, stateVersion)
	}
	if loaded.Provider != original.Provider {
		t.Errorf("Provider = %q, want %q", loaded.Provider, original.Provider)
	}
	if loaded.Environment != original.Environment {
		t.Errorf("Environment = %q, want %q", loaded.Environment, original.Environment)
	}
	if loaded.LastDeployed != original.LastDeployed {
		t.Errorf("LastDeployed = %q, want %q", loaded.LastDeployed, original.LastDeployed)
	}
	if loaded.PackVersion != original.PackVersion {
		t.Errorf("PackVersion = %q, want %q", loaded.PackVersion, original.PackVersion)
	}
	if loaded.PackChecksum != original.PackChecksum {
		t.Errorf("PackChecksum = %q, want %q", loaded.PackChecksum, original.PackChecksum)
	}
	if loaded.AdapterVersion != original.AdapterVersion {
		t.Errorf("AdapterVersion = %q, want %q", loaded.AdapterVersion, original.AdapterVersion)
	}
	if loaded.State != original.State {
		t.Errorf("State = %q, want %q", loaded.State, original.State)
	}
}

func TestStateStore_LoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	state, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}
	if state != nil {
		t.Fatalf("Load returned non-nil state for missing file: %+v", state)
	}
}

func TestStateStore_SaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	stateDir := filepath.Join(dir, ".promptarena")
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal(".promptarena directory should not exist before Save")
	}

	state := &State{
		Provider:    "local",
		Environment: "dev",
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf(".promptarena directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".promptarena is not a directory")
	}
}

func TestStateStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	// Save first so there's something to delete.
	state := &State{Provider: "test"}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := store.Delete(); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify file is gone.
	if _, err := os.Stat(store.statePath()); !os.IsNotExist(err) {
		t.Fatal("state file still exists after Delete")
	}
}

func TestStateStore_DeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	if err := store.Delete(); err != nil {
		t.Fatalf("Delete on non-existent file returned error: %v", err)
	}
}

func TestComputePackChecksum(t *testing.T) {
	data := []byte("hello world")
	checksum := ComputePackChecksum(data)

	// SHA-256 of "hello world" is well-known.
	expected := "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if checksum != expected {
		t.Errorf("ComputePackChecksum = %q, want %q", checksum, expected)
	}

	// Determinism: same input produces same output.
	if ComputePackChecksum(data) != checksum {
		t.Error("ComputePackChecksum is not deterministic")
	}
}

func TestNewState(t *testing.T) {
	before := time.Now().UTC()
	state := NewState("aws-lambda", "staging", "2.0.0", "sha256:deadbeef", "1.0.0")
	after := time.Now().UTC()

	if state.Version != stateVersion {
		t.Errorf("Version = %d, want %d", state.Version, stateVersion)
	}
	if state.Provider != "aws-lambda" {
		t.Errorf("Provider = %q, want %q", state.Provider, "aws-lambda")
	}
	if state.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", state.Environment, "staging")
	}
	if state.PackVersion != "2.0.0" {
		t.Errorf("PackVersion = %q, want %q", state.PackVersion, "2.0.0")
	}
	if state.PackChecksum != "sha256:deadbeef" {
		t.Errorf("PackChecksum = %q, want %q", state.PackChecksum, "sha256:deadbeef")
	}
	if state.AdapterVersion != "1.0.0" {
		t.Errorf("AdapterVersion = %q, want %q", state.AdapterVersion, "1.0.0")
	}

	ts, err := time.Parse(time.RFC3339, state.LastDeployed)
	if err != nil {
		t.Fatalf("LastDeployed is not valid RFC3339: %v", err)
	}
	if ts.Before(before.Truncate(time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("LastDeployed = %v, expected between %v and %v", ts, before, after)
	}
}

func TestStateStore_SaveNilState(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	err := store.Save(nil)
	if err == nil {
		t.Fatal("Save(nil) should return an error")
	}
}
