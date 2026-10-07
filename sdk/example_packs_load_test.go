package sdk

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// TestEveryExamplePackLoads loads each pack the repository ships as an example
// with schema validation on. A spec upgrade that tightens the schema would
// otherwise break the packs users copy from without any test noticing.
func TestEveryExamplePackLoads(t *testing.T) {
	var paths []string
	for _, root := range []string{"examples", "../server/a2a/examples", "../benchmarks"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(path, ".pack.json") {
				paths = append(paths, path)
			}
			return nil
		})
		require.NoError(t, err)
	}
	require.NotEmpty(t, paths, "found no example packs; the walk roots are wrong")

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var want struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(raw, &want))

			loaded, err := pack.Load(path)
			require.NoError(t, err)
			require.Equal(t, want.ID, loaded.ID, "the loaded pack must be the one in the file")
		})
	}
}
