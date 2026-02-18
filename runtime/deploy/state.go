// Package deploy provides deployment state persistence for PromptKit arenas.
package deploy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	stateDir      = ".promptarena"
	stateFile     = "deploy.state"
	planFile      = "deploy.plan"
	stateVersion  = 1
	stateDirPerm  = 0o750
	stateFilePerm = 0o600
)

// State represents the persisted deployment state.
type State struct {
	Version        int    `json:"version"`
	Provider       string `json:"provider"`
	Environment    string `json:"environment"`
	LastDeployed   string `json:"last_deployed"`
	PackVersion    string `json:"pack_version"`
	PackChecksum   string `json:"pack_checksum"`
	AdapterVersion string `json:"adapter_version"`
	LastRefreshed  string `json:"last_refreshed,omitempty"`
	State          string `json:"state,omitempty"` // Opaque base64 adapter state
}

// StateStore manages deploy state persistence.
type StateStore struct {
	baseDir string
}

// NewStateStore creates a StateStore rooted at the given directory.
func NewStateStore(baseDir string) *StateStore {
	return &StateStore{baseDir: baseDir}
}

// statePath returns the full path to the state file.
func (s *StateStore) statePath() string {
	return filepath.Join(s.baseDir, stateDir, stateFile)
}

// Load reads the deploy state from disk.
// Returns nil, nil if the state file does not exist.
func (s *StateStore) Load() (*State, error) {
	data, err := os.ReadFile(s.statePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// Save writes the deploy state to disk, creating directories as needed.
func (s *StateStore) Save(state *State) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	state.Version = stateVersion

	dir := filepath.Join(s.baseDir, stateDir)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(s.statePath(), data, stateFilePerm); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// Delete removes the state file.
func (s *StateStore) Delete() error {
	err := os.Remove(s.statePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ComputePackChecksum computes a sha256 checksum of pack data.
func ComputePackChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

// SavedPlan is a serialized deployment plan that can be applied later.
type SavedPlan struct {
	Version      int           `json:"version"`
	CreatedAt    string        `json:"created_at"`
	Provider     string        `json:"provider"`
	Environment  string        `json:"environment"`
	PackChecksum string        `json:"pack_checksum"`
	Plan         *PlanResponse `json:"plan"`
	Request      *PlanRequest  `json:"request"`
}

// planPath returns the full path to the plan file.
func (s *StateStore) planPath() string {
	return filepath.Join(s.baseDir, stateDir, planFile)
}

// SavePlan persists a deployment plan to disk.
func (s *StateStore) SavePlan(plan *SavedPlan) error {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}

	plan.Version = stateVersion

	dir := filepath.Join(s.baseDir, stateDir)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	if err := os.WriteFile(s.planPath(), data, stateFilePerm); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	return nil
}

// LoadPlan reads a saved plan from disk. Returns nil, nil if not found.
func (s *StateStore) LoadPlan() (*SavedPlan, error) {
	data, err := os.ReadFile(s.planPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read plan file: %w", err)
	}

	var plan SavedPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan file: %w", err)
	}

	return &plan, nil
}

// DeletePlan removes the saved plan file. Returns nil if not found.
func (s *StateStore) DeletePlan() error {
	err := os.Remove(s.planPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// NewSavedPlan creates a SavedPlan with current timestamp.
func NewSavedPlan(provider, environment, packChecksum string, plan *PlanResponse, request *PlanRequest) *SavedPlan {
	return &SavedPlan{
		Version:      stateVersion,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Provider:     provider,
		Environment:  environment,
		PackChecksum: packChecksum,
		Plan:         plan,
		Request:      request,
	}
}

// NewState creates a new State with the given parameters and current timestamp.
func NewState(provider, environment, packVersion, packChecksum, adapterVersion string) *State {
	return &State{
		Version:        stateVersion,
		Provider:       provider,
		Environment:    environment,
		LastDeployed:   time.Now().UTC().Format(time.RFC3339),
		PackVersion:    packVersion,
		PackChecksum:   packChecksum,
		AdapterVersion: adapterVersion,
	}
}
