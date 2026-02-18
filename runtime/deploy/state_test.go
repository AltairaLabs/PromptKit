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

func TestStateStore_LastRefreshedRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	original := &State{
		Provider:      "aws-lambda",
		Environment:   "production",
		LastDeployed:  "2026-01-15T10:30:00Z",
		LastRefreshed: "2026-02-18T14:00:00Z",
		PackVersion:   "1.0.0",
		PackChecksum:  "sha256:abc123",
		State:         "some-state",
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
	if loaded.LastRefreshed != original.LastRefreshed {
		t.Errorf("LastRefreshed = %q, want %q", loaded.LastRefreshed, original.LastRefreshed)
	}
}

func TestStateStore_LastRefreshedOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	original := &State{
		Provider:    "local",
		Environment: "dev",
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.LastRefreshed != "" {
		t.Errorf("LastRefreshed = %q, want empty string", loaded.LastRefreshed)
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

func TestStateStore_SaveAndLoadPlan(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	original := &SavedPlan{
		CreatedAt:    "2026-02-18T10:30:00Z",
		Provider:     "aws-lambda",
		Environment:  "production",
		PackChecksum: "sha256:abc123",
		Plan: &PlanResponse{
			Changes: []ResourceChange{
				{Type: "agent_runtime", Name: "my-agent", Action: ActionCreate, Detail: "new runtime"},
			},
			Summary: "1 resource to create",
		},
		Request: &PlanRequest{
			PackJSON:    `{"name":"test"}`,
			Environment: "production",
		},
	}

	if err := store.SavePlan(original); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	loaded, err := store.LoadPlan()
	if err != nil {
		t.Fatalf("LoadPlan failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadPlan returned nil plan")
	}

	// Version is always set by SavePlan
	if loaded.Version != stateVersion {
		t.Errorf("Version = %d, want %d", loaded.Version, stateVersion)
	}
	if loaded.Provider != original.Provider {
		t.Errorf("Provider = %q, want %q", loaded.Provider, original.Provider)
	}
	if loaded.Environment != original.Environment {
		t.Errorf("Environment = %q, want %q", loaded.Environment, original.Environment)
	}
	if loaded.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt = %q, want %q", loaded.CreatedAt, original.CreatedAt)
	}
	if loaded.PackChecksum != original.PackChecksum {
		t.Errorf("PackChecksum = %q, want %q", loaded.PackChecksum, original.PackChecksum)
	}
	if loaded.Plan == nil {
		t.Fatal("Plan is nil")
	}
	if loaded.Plan.Summary != original.Plan.Summary {
		t.Errorf("Plan.Summary = %q, want %q", loaded.Plan.Summary, original.Plan.Summary)
	}
	if len(loaded.Plan.Changes) != 1 {
		t.Fatalf("Plan.Changes length = %d, want 1", len(loaded.Plan.Changes))
	}
	if loaded.Plan.Changes[0].Name != "my-agent" {
		t.Errorf("Plan.Changes[0].Name = %q, want %q", loaded.Plan.Changes[0].Name, "my-agent")
	}
	if loaded.Request == nil {
		t.Fatal("Request is nil")
	}
	if loaded.Request.PackJSON != original.Request.PackJSON {
		t.Errorf("Request.PackJSON = %q, want %q", loaded.Request.PackJSON, original.Request.PackJSON)
	}
}

func TestStateStore_LoadPlanNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	plan, err := store.LoadPlan()
	if err != nil {
		t.Fatalf("LoadPlan returned unexpected error: %v", err)
	}
	if plan != nil {
		t.Fatalf("LoadPlan returned non-nil plan for missing file: %+v", plan)
	}
}

func TestStateStore_DeletePlan(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	// Save first so there's something to delete.
	plan := &SavedPlan{Provider: "test"}
	if err := store.SavePlan(plan); err != nil {
		t.Fatalf("SavePlan failed: %v", err)
	}

	if err := store.DeletePlan(); err != nil {
		t.Fatalf("DeletePlan failed: %v", err)
	}

	// Verify file is gone.
	if _, err := os.Stat(store.planPath()); !os.IsNotExist(err) {
		t.Fatal("plan file still exists after DeletePlan")
	}
}

func TestStateStore_DeletePlanNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	if err := store.DeletePlan(); err != nil {
		t.Fatalf("DeletePlan on non-existent file returned error: %v", err)
	}
}

func TestStateStore_SavePlanNilPlan(t *testing.T) {
	dir := t.TempDir()
	store := NewStateStore(dir)

	err := store.SavePlan(nil)
	if err == nil {
		t.Fatal("SavePlan(nil) should return an error")
	}
}

func TestNewSavedPlan(t *testing.T) {
	before := time.Now().UTC()
	plan := NewSavedPlan(
		"aws-lambda",
		"staging",
		"sha256:deadbeef",
		&PlanResponse{Summary: "test plan"},
		&PlanRequest{PackJSON: `{"name":"test"}`},
	)
	after := time.Now().UTC()

	if plan.Version != stateVersion {
		t.Errorf("Version = %d, want %d", plan.Version, stateVersion)
	}
	if plan.Provider != "aws-lambda" {
		t.Errorf("Provider = %q, want %q", plan.Provider, "aws-lambda")
	}
	if plan.Environment != "staging" {
		t.Errorf("Environment = %q, want %q", plan.Environment, "staging")
	}
	if plan.PackChecksum != "sha256:deadbeef" {
		t.Errorf("PackChecksum = %q, want %q", plan.PackChecksum, "sha256:deadbeef")
	}
	if plan.Plan == nil || plan.Plan.Summary != "test plan" {
		t.Errorf("Plan.Summary = %v, want %q", plan.Plan, "test plan")
	}
	if plan.Request == nil || plan.Request.PackJSON != `{"name":"test"}` {
		t.Errorf("Request.PackJSON = %v, want %q", plan.Request, `{"name":"test"}`)
	}

	ts, err := time.Parse(time.RFC3339, plan.CreatedAt)
	if err != nil {
		t.Fatalf("CreatedAt is not valid RFC3339: %v", err)
	}
	if ts.Before(before.Truncate(time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("CreatedAt = %v, expected between %v and %v", ts, before, after)
	}
}
