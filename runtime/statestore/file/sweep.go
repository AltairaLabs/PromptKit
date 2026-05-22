package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sweepStale removes per-conversation directories whose state.json mtime is
// older than now-ttl. Called from NewStore when Options.TTL > 0. Errors on
// individual conv dirs are not fatal — partial cleanup beats none.
func (s *Store) sweepStale(now time.Time) error {
	if s.ttl <= 0 {
		return nil
	}
	cutoff := now.Add(-s.ttl)

	entries, err := os.ReadDir(s.root)
	if err != nil {
		return fmt.Errorf("filestore: sweep read root: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), convDirPrefix) {
			continue
		}
		statePath := filepath.Join(s.root, e.Name(), stateFilename)
		info, statErr := os.Stat(statePath)
		if statErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(s.root, e.Name()))
		}
	}
	return nil
}
