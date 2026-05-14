// Package web provides the HTTP server and SSE event streaming for the Arena web UI.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// audioSSEPrefix identifies pre-formatted SSE frames (audio relay messages)
// vs raw JSON event payloads on the same client channel.
var audioSSEPrefix = []byte("event:")

// defaultRunTimeout is the maximum duration for background runs started via POST /api/run.
const defaultRunTimeout = 30 * time.Minute

// defaultConcurrency is the number of concurrent runs for background execution.
const defaultConcurrency = 4

const (
	resultsDirPerm  os.FileMode = 0o750
	resultsFilePerm os.FileMode = 0o600
)

// engineRunner is the subset of engine.Engine used by the web server.
// Defined as an interface to enable testing without a full engine.
type engineRunner interface {
	GenerateRunPlan(regionFilter, providerFilter, scenarioFilter, evalFilter []string) (*engine.RunPlan, error)
	ExecuteRuns(ctx context.Context, plan *engine.RunPlan, concurrency int) ([]string, error)
	GetConfig() *config.Config
	ListProviders() []engine.ProviderInfo
	ListScenarios() []engine.ScenarioInfo
}

// Server is the Arena web UI HTTP server.
type Server struct {
	adapter    *EventAdapter
	engine     engineRunner
	stateStore *statestore.ArenaStateStore
	outputDir  string
	mux        *http.ServeMux
}

// NewServer creates a new web server.
// eng and store may be nil (for testing SSE in isolation).
// outputDir is the path to the results directory (for DELETE /api/results).
//
// When both eng and adapter are non-nil, the server registers an audio
// monitor hook on the engine so per-run AudioRouters automatically attach
// to the SSE relay. Audio monitoring still requires explicit opt-in via
// engine.EnableAudioMonitor — without it, the hook never fires.
func NewServer(adapter *EventAdapter, eng *engine.Engine, store *statestore.ArenaStateStore, outputDir string) *Server {
	var runner engineRunner
	if eng != nil {
		runner = eng
		if adapter != nil {
			eng.RegisterAudioMonitorHook(func(runID string, router *arenaaudio.AudioRouter, rate int) {
				adapter.AttachAudioRouter(runID, router, rate)
			})
		}
	}
	return newServerWithRunner(adapter, runner, store, outputDir)
}

// newServerWithRunner creates a Server using the engineRunner interface (used for testing).
func newServerWithRunner(
	adapter *EventAdapter, runner engineRunner, store *statestore.ArenaStateStore, outputDir string,
) *Server {
	s := &Server{
		adapter:    adapter,
		engine:     runner,
		stateStore: store,
		outputDir:  outputDir,
		mux:        http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /api/events", s.handleSSE)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("GET /api/run-options", s.handleRunOptions)
	s.mux.HandleFunc("GET /api/results", s.handleListResults)
	s.mux.HandleFunc("GET /api/results/{id}", s.handleGetResult)
	s.mux.HandleFunc("GET /api/media/{path...}", s.handleMedia)
	s.mux.HandleFunc("DELETE /api/results", s.handleClearResults)
	s.mux.HandleFunc("POST /api/run", s.handleStartRun)

	// SPA fallback: serve embedded frontend
	sub, subErr := fs.Sub(frontendFS, "frontend/dist")
	if subErr == nil {
		fileServer := http.FileServer(http.FS(sub))
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Try serving the exact file first
			urlPath := r.URL.Path
			if urlPath == "/" {
				urlPath = "/index.html"
			}
			// Check if the file exists in the embedded FS
			if f, openErr := sub.Open(path.Clean(strings.TrimPrefix(urlPath, "/"))); openErr == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			// SPA fallback: serve index.html for client-side routing
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
		})
	}

	return s
}

// Handler returns the http.Handler for the server.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// handleSSE streams events to the client via Server-Sent Events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.adapter.Register()
	defer s.adapter.Unregister(ch)

	// Audio opt-in: clients that pass ?audio=1 also receive audio SSE events
	// on the same channel. Browser-side EventSource demuxes by event type.
	if r.URL.Query().Get("audio") == "1" {
		s.adapter.RegisterAudio(ch)
		defer s.adapter.UnregisterAudio(ch)
	}

	// Send initial keepalive so client knows connection is established
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Audio relay messages are pre-formatted SSE frames (they include
			// their own `event: audio\ndata: ...\n\n` envelope). Regular
			// events are raw JSON bytes that need wrapping with `data:`.
			if isPreformattedSSE(msg) {
				_, _ = w.Write(msg)
			} else {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			}
			flusher.Flush()
		}
	}
}

// isPreformattedSSE reports whether msg already contains a complete SSE
// frame (starts with an `event:` line). Used to distinguish audio relay
// messages from raw JSON event bytes on the same client channel.
func isPreformattedSSE(msg []byte) bool {
	return bytes.HasPrefix(msg, audioSSEPrefix)
}

// RunRequest is the JSON body for POST /api/run.
type RunRequest struct {
	Providers []string `json:"providers,omitempty"`
	Scenarios []string `json:"scenarios,omitempty"`
	Regions   []string `json:"regions,omitempty"`
}

// handleStartRun starts a new Arena run.
func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		http.Error(w, "engine not configured", http.StatusServiceUnavailable)
		return
	}

	var req RunRequest
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	plan, err := s.engine.GenerateRunPlan(req.Regions, req.Providers, req.Scenarios, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if len(plan.Combinations) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no matching run combinations"})
		return
	}

	// Start runs in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRunTimeout)
		defer cancel()
		runIDs, _ := s.engine.ExecuteRuns(ctx, plan, defaultConcurrency)
		s.persistRunResults(ctx, runIDs)
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"combinations": len(plan.Combinations),
		"status":       "started",
	})
}

// persistRunResults writes each completed run as <runID>.json under
// outputDir, matching the format JSONResultRepository.SaveResult produces
// so LoadResultsIntoStore can hydrate them on the next server boot.
func (s *Server) persistRunResults(ctx context.Context, runIDs []string) {
	if s.stateStore == nil || s.outputDir == "" || len(runIDs) == 0 {
		return
	}
	if err := os.MkdirAll(s.outputDir, resultsDirPerm); err != nil {
		return
	}
	for _, runID := range runIDs {
		if runID == "" {
			continue
		}
		result, err := s.stateStore.GetResult(ctx, runID)
		if err != nil || result == nil {
			continue
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(s.outputDir, runID+".json"), data, resultsFilePerm)
	}
}

// handleGetConfig returns the loaded arena config.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if s.engine == nil {
		http.Error(w, "engine not configured", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, s.engine.GetConfig())
}

// handleListResults returns all completed run IDs.
func (s *Server) handleListResults(w http.ResponseWriter, r *http.Request) {
	if s.stateStore == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	runIDs, err := s.stateStore.ListRunIDs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, runIDs)
}

// handleGetResult returns a single run result.
func (s *Server) handleGetResult(w http.ResponseWriter, r *http.Request) {
	if s.stateStore == nil {
		http.Error(w, "no results available", http.StatusNotFound)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing run ID", http.StatusBadRequest)
		return
	}

	// Clean up URL-encoded path separators
	id = strings.ReplaceAll(id, "%2F", "/")

	result, err := s.stateStore.GetResult(r.Context(), id)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleClearResults deletes all result files from the output directory
// and clears the in-memory state store.
func (s *Server) handleClearResults(w http.ResponseWriter, _ *http.Request) {
	if s.stateStore == nil {
		http.Error(w, "no state store", http.StatusServiceUnavailable)
		return
	}

	// Clear in-memory state
	s.stateStore.Clear()

	// Delete result files from disk
	deleted := 0
	if s.outputDir != "" {
		entries, err := os.ReadDir(s.outputDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if strings.HasSuffix(name, ".json") {
					_ = os.Remove(filepath.Join(s.outputDir, name))
					deleted++
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cleared": true,
		"deleted": deleted,
	})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
