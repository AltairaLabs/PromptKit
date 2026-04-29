package render

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestRenderInlineImage_ResolvesStorageReferenceToRealFile is the
// end-to-end check the unit tests can't make: it stages the same layout
// arena builds at runtime — out/ next to out/media/ — externalizes a real
// payload through local.FileStore, then asserts that the report-relative
// src in the rendered <img> resolves to the actual file on disk.
//
// Catches path-mismatch bugs the unit tests miss (e.g. if the helper
// were to strip the wrong prefix, or FileStore changed its layout).
func TestRenderInlineImage_ResolvesStorageReferenceToRealFile(t *testing.T) {
	// Mirror arena's runtime layout: <tmp>/out/ holds the report,
	// <tmp>/out/media/ holds externalized files.
	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "out")
	mediaDir := filepath.Join(outDir, "media")

	store, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      mediaDir,
		Organization: storage.OrganizationByRun,
	})
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	// Use a real PNG header so the file passes any future MIME
	// sniffing; padding with zeros keeps the test cheap.
	payload := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 1024)...)
	encoded := base64.StdEncoding.EncodeToString(payload)
	media := &types.MediaContent{
		MIMEType: "image/png",
		Data:     &encoded,
	}

	ref, err := store.StoreMedia(context.Background(), media, &storage.MediaMetadata{
		RunID:    "run-test",
		MIMEType: "image/png",
	})
	if err != nil {
		t.Fatalf("StoreMedia: %v", err)
	}

	// Externalizer sets this and clears Data — mirror that here so the
	// renderer hits the StorageReference branch.
	refStr := string(ref)
	part := types.ContentPart{
		Type: types.ContentTypeImage,
		Media: &types.MediaContent{
			MIMEType:         "image/png",
			StorageReference: &refStr,
		},
	}

	html := renderInlineImage(part)

	srcMatch := regexp.MustCompile(`<img src="([^"]+)"`).FindStringSubmatch(html)
	if len(srcMatch) != 2 {
		t.Fatalf("renderInlineImage did not emit an <img src=...>: %s", html)
	}
	src := srcMatch[1]

	// Storage reference is an absolute path under <tmp>/out/media; arena
	// rewrites that to a report-relative path by stripping "out/". The
	// test layout uses <tmp> as the prefix, so the src will start with
	// the absolute path — that's fine for resolution.
	if strings.Contains(src, "data:") {
		t.Fatalf("rendered src is a data URL, expected a file path: %s", src)
	}

	// Resolve src relative to the report directory (where report.html
	// would live) and confirm the externalized file is reachable.
	var resolved string
	if filepath.IsAbs(src) {
		resolved = src
	} else {
		resolved = filepath.Join(outDir, src)
	}

	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("rendered src %q does not resolve to a real file under %q: %v\nstorage ref: %s",
			src, outDir, err, refStr)
	}

	got, err := os.ReadFile(resolved) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read resolved file: %v", err)
	}
	if len(got) != len(payload) {
		t.Errorf("file size mismatch: got %d bytes, stored %d bytes", len(got), len(payload))
	}
}
