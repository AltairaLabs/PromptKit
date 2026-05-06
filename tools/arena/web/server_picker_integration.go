package web

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
)

// runOptionsResponse is the GET /api/run-options payload — a slim view of
// loaded providers and scenarios for the picker UI in front of POST /api/run.
type runOptionsResponse struct {
	Providers []engine.ProviderInfo `json:"providers"`
	Scenarios []engine.ScenarioInfo `json:"scenarios"`
}

// handleRunOptions returns the IDs of loaded providers and scenarios so the
// frontend can render a picker before triggering a run.
func (s *Server) handleRunOptions(w http.ResponseWriter, _ *http.Request) {
	if s.engine == nil {
		http.Error(w, "engine not configured", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, runOptionsResponse{
		Providers: s.engine.ListProviders(),
		Scenarios: s.engine.ListScenarios(),
	})
}

// handleMedia serves saved-run media files (audio, image, video) under the
// configured output directory. The frontend uses this to play back historical
// runs by referencing each message part's storage_reference field.
//
// Path traversal is prevented by resolving both the request path and the
// configured outputDir to absolute paths and checking that the resolved
// target stays under outputDir.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if s.outputDir == "" {
		http.Error(w, "media directory not configured", http.StatusServiceUnavailable)
		return
	}
	rel := r.PathValue("path")
	if rel == "" {
		http.Error(w, "missing media path", http.StatusBadRequest)
		return
	}
	// storage_reference values are saved relative to the working directory
	// (e.g. "out/media/runs/.../foo.wav"). The first segment may or may not
	// be the literal output directory name; strip a leading "out/" so the
	// path resolves against outputDir regardless.
	rel = strings.TrimPrefix(rel, "out/")

	absOut, err := filepath.Abs(s.outputDir)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	target := filepath.Clean(filepath.Join(absOut, rel))
	if target != absOut && !strings.HasPrefix(target, absOut+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Permissive cache: saved files are content-addressed (sha256 in path).
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeFile(w, r, target)
}
