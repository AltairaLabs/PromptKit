package local_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/storage/local"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// writeTempFile drops the given bytes into a fresh file under t.TempDir()
// and returns the absolute path. Saves boilerplate in the streaming tests
// below — every one of them needs a source file on disk for FilePath.
func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestFileStore_StoreMedia_FilePathStreaming(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	srcPath := writeTempFile(t, "src.bin", []byte("streamed-bytes"))
	content := &types.MediaContent{
		FilePath: &srcPath,
		MIMEType: "application/octet-stream",
	}
	meta := &storage.MediaMetadata{
		RunID:      "run-1",
		MessageIdx: 0,
		PartIdx:    0,
		MIMEType:   "application/octet-stream",
		Timestamp:  time.Now(),
	}

	ref, err := fs.StoreMedia(ctx, content, meta)
	require.NoError(t, err)
	require.NotEmpty(t, ref)
	assert.FileExists(t, string(ref))

	written, err := os.ReadFile(string(ref))
	require.NoError(t, err)
	assert.Equal(t, "streamed-bytes", string(written))
}

func TestFileStore_StoreMedia_FilePathPrependsWAVHeaderForPCM(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	pcm := bytes.Repeat([]byte{0x10, 0x20}, 1024)
	srcPath := writeTempFile(t, "src.pcm", pcm)
	content := &types.MediaContent{
		FilePath: &srcPath,
		MIMEType: "audio/pcm",
	}
	meta := &storage.MediaMetadata{
		RunID:      "run-2",
		MessageIdx: 0,
		PartIdx:    0,
		MIMEType:   "audio/pcm",
		Timestamp:  time.Now(),
	}

	ref, err := fs.StoreMedia(ctx, content, meta)
	require.NoError(t, err)

	out, err := os.ReadFile(string(ref))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(out), 44, "expected WAV header + body")
	assert.Equal(t, []byte("RIFF"), out[0:4])
	assert.Equal(t, []byte("WAVE"), out[8:12])
	assert.Equal(t, pcm, out[44:], "PCM body should follow header unchanged")
}

func TestFileStore_StoreMedia_FilePathExceedsMaxSize(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
		MaxFileSize:  10,
	})
	require.NoError(t, err)

	srcPath := writeTempFile(t, "huge.bin", bytes.Repeat([]byte{0xFF}, 100))
	content := &types.MediaContent{
		FilePath: &srcPath,
		MIMEType: "application/octet-stream",
	}
	meta := &storage.MediaMetadata{RunID: "r", Timestamp: time.Now()}

	_, err = fs.StoreMedia(ctx, content, meta)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "exceeds limit")
}

func TestFileStore_StoreMedia_FilePathDeduplication(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:             tempDir,
		Organization:        storage.OrganizationByRun,
		EnableDeduplication: true,
	})
	require.NoError(t, err)

	src1 := writeTempFile(t, "a.bin", []byte("dedup-me"))
	src2 := writeTempFile(t, "b.bin", []byte("dedup-me"))
	mk := func(path string, runID string) (*types.MediaContent, *storage.MediaMetadata) {
		return &types.MediaContent{
				FilePath: &path,
				MIMEType: "application/octet-stream",
			},
			&storage.MediaMetadata{RunID: runID, Timestamp: time.Now()}
	}

	c1, m1 := mk(src1, "run-A")
	c2, m2 := mk(src2, "run-B")

	ref1, err := fs.StoreMedia(ctx, c1, m1)
	require.NoError(t, err)
	ref2, err := fs.StoreMedia(ctx, c2, m2)
	require.NoError(t, err)

	assert.Equal(t, ref1, ref2, "identical bytes should dedup to a single stored path")
}

func TestFileStore_GetURL_FromStreamedFile(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	fs, err := local.NewFileStore(local.FileStoreConfig{
		BaseDir:      tempDir,
		Organization: storage.OrganizationByRun,
	})
	require.NoError(t, err)

	srcPath := writeTempFile(t, "x.bin", []byte("hello"))
	content := &types.MediaContent{FilePath: &srcPath, MIMEType: "application/octet-stream"}
	meta := &storage.MediaMetadata{RunID: "r", Timestamp: time.Now()}
	ref, err := fs.StoreMedia(ctx, content, meta)
	require.NoError(t, err)

	t.Run("returns file:// URL for valid reference", func(t *testing.T) {
		url, err := fs.GetURL(ctx, ref, time.Hour)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(url, "file://"))
	})

	t.Run("rejects path outside base dir", func(t *testing.T) {
		_, err := fs.GetURL(ctx, storage.Reference("/etc/passwd"), time.Hour)
		require.Error(t, err)
	})

	t.Run("returns not-found for missing file", func(t *testing.T) {
		gone := filepath.Join(tempDir, "missing.bin")
		_, err := fs.GetURL(ctx, storage.Reference(gone), time.Hour)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}
