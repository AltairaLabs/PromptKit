// Package artifacts abstracts where per-run report artifacts ultimately live.
//
// A hook always writes its outputs to a local staging directory Arena hands it
// and records {name, description, filename} in manifest.json — it never knows or
// cares about the backend. After the hook runs, Arena ingests the staged
// manifest through a Store: the local backend leaves bytes in place and returns
// a relative link; a future S3/DB backend would upload and return a URL/key.
// Flipping the backend is a config change, never a hook change.
package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// Reference is a link the report resolves to an artifact: a relative path for
// the local backend, or an absolute URL/key for a remote one.
type Reference string

// Store persists a staged artifact (written locally by a hook at localPath) and
// returns a Reference. Implementations decide where the bytes ultimately live.
type Store interface {
	Store(ctx context.Context, runID, filename, localPath string) (Reference, error)
}

// StagedEntry is what a hook records — it owns name/description/filename.
type StagedEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
}

// StagedManifest is the on-disk manifest a hook writes.
type StagedManifest struct {
	Artifacts []StagedEntry `json:"artifacts"`
}

// ResolvedEntry is what Arena writes after storing — the Reference replaces the
// hook's filename. This is what the report reads.
type ResolvedEntry struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Ref         Reference `json:"ref"`
}

// ResolvedManifest is the manifest after ingestion.
type ResolvedManifest struct {
	Artifacts []ResolvedEntry `json:"artifacts"`
}

const manifestName = "manifest.json"

// manifestPerm is the permission for the rewritten manifest file.
const manifestPerm = 0o644

// Local is the default backend: bytes already live under the report's output dir
// (the hook wrote them there), so Store is a no-op that returns the relative
// link the report can follow from disk.
type Local struct{}

// NewLocal returns the local artifact store.
func NewLocal() *Local { return &Local{} }

// Store returns the relative report link for an artifact left in place.
func (l *Local) Store(_ context.Context, runID, filename, _ string) (Reference, error) {
	return Reference(path.Join("artifacts", runID, filename)), nil
}

// Ingest reads the staged manifest in artifactsDir, persists each artifact via
// store, and rewrites the manifest with resolved References. It is a no-op when
// no manifest exists (no hook recorded anything). Malformed manifests error.
func Ingest(ctx context.Context, store Store, artifactsDir, runID string) error {
	manifestPath := filepath.Join(artifactsDir, manifestName)
	raw, readErr := os.ReadFile(manifestPath) //nolint:gosec // artifactsDir is Arena-owned
	if readErr != nil {
		return nil //nolint:nilerr // no manifest -> nothing to ingest
	}
	var staged StagedManifest
	if err := json.Unmarshal(raw, &staged); err != nil {
		return fmt.Errorf("artifacts: parse manifest %q: %w", manifestPath, err)
	}

	var resolved ResolvedManifest
	for _, e := range staged.Artifacts {
		if e.Filename == "" {
			continue
		}
		ref, storeErr := store.Store(ctx, runID, e.Filename, filepath.Join(artifactsDir, e.Filename))
		if storeErr != nil {
			return fmt.Errorf("artifacts: store %q: %w", e.Filename, storeErr)
		}
		resolved.Artifacts = append(resolved.Artifacts, ResolvedEntry{
			Name:        e.Name,
			Description: e.Description,
			Ref:         ref,
		})
	}

	out, err := json.Marshal(resolved)
	if err != nil {
		return fmt.Errorf("artifacts: marshal resolved manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, out, manifestPerm); err != nil {
		return fmt.Errorf("artifacts: write resolved manifest: %w", err)
	}
	return nil
}
