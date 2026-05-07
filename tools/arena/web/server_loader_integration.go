package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

// LoadResultsIntoStore scans outDir for run-result JSON files and loads
// each into the in-memory state store. Previously this delegated to
// JSONResultRepository.LoadResults, which reads run_ids out of
// index.json — and index.json is rewritten by each CLI invocation,
// so on server boot only the most recent batch's IDs were hydrated
// even though every prior <runID>.json sat right there on disk.
//
// Scanning the directory directly means web-triggered runs (which
// never touch index.json) and old CLI runs both flow into Previous
// Runs on initial page load.
func LoadResultsIntoStore(outDir string, store *statestore.ArenaStateStore) int {
	if outDir == "" {
		return 0
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return 0
	}
	ctx := context.Background()
	loaded := 0
	for _, e := range entries {
		if !isResultJSON(e) {
			continue
		}
		if loadOneResult(ctx, store, filepath.Join(outDir, e.Name())) {
			loaded++
		}
	}
	return loaded
}

// isResultJSON returns true for entries that look like saved run results
// (per-runID *.json files, excluding the index summary).
func isResultJSON(e os.DirEntry) bool {
	return !e.IsDir() && filepath.Ext(e.Name()) == ".json" && e.Name() != "index.json"
}

// loadOneResult reads a single run-result file, hydrates the state
// store with its conversation + run metadata, and reports whether the
// load succeeded. Failures are silent: the caller treats them as
// "this file isn't a usable run result, skip it".
func loadOneResult(ctx context.Context, store *statestore.ArenaStateStore, path string) bool {
	// #nosec G304 -- path is constrained to entries we enumerated under outDir
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var r engine.RunResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return false
	}
	if r.RunID == "" {
		return false
	}
	convState := &runtimestore.ConversationState{
		ID:       r.RunID,
		Messages: r.Messages,
		Metadata: make(map[string]interface{}),
	}
	if store.Save(ctx, convState) != nil {
		return false
	}
	meta := &statestore.RunMetadata{
		RunID:                        r.RunID,
		PromptPack:                   r.PromptPack,
		Region:                       r.Region,
		ScenarioID:                   r.ScenarioID,
		ProviderID:                   r.ProviderID,
		Params:                       r.Params,
		Commit:                       r.Commit,
		StartTime:                    r.StartTime,
		EndTime:                      r.EndTime,
		Duration:                     r.Duration,
		Error:                        r.Error,
		SelfPlay:                     r.SelfPlay,
		PersonaID:                    r.PersonaID,
		ConversationAssertionResults: r.ConversationAssertions.Results,
	}
	return store.SaveMetadata(ctx, r.RunID, meta) == nil
}
