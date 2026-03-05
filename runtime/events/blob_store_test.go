package events

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFileBlobStore(t *testing.T) {
	t.Run("creates directory", func(t *testing.T) {
		dir := t.TempDir()
		blobDir := filepath.Join(dir, "blobs")

		store, err := NewFileBlobStore(blobDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer store.Close()

		if _, err := os.Stat(blobDir); os.IsNotExist(err) {
			t.Error("blob directory was not created")
		}
	})
}

func TestFileBlobStore_Store(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	t.Run("stores and returns payload", func(t *testing.T) {
		data := []byte("hello world")
		payload, err := store.Store(ctx, "session-1", data, "text/plain")
		if err != nil {
			t.Fatalf("store: %v", err)
		}

		if payload.StorageRef == "" {
			t.Error("storage ref is empty")
		}
		if payload.MIMEType != "text/plain" {
			t.Errorf("expected mime type 'text/plain', got %q", payload.MIMEType)
		}
		if payload.Size != int64(len(data)) {
			t.Errorf("expected size %d, got %d", len(data), payload.Size)
		}
		if !strings.HasPrefix(payload.Checksum, "sha256:") {
			t.Errorf("expected sha256 checksum, got %q", payload.Checksum)
		}
	})

	t.Run("deduplicates identical data", func(t *testing.T) {
		data := []byte("duplicate content")

		payload1, err := store.Store(ctx, "session-1", data, "text/plain")
		if err != nil {
			t.Fatalf("store 1: %v", err)
		}

		payload2, err := store.Store(ctx, "session-1", data, "text/plain")
		if err != nil {
			t.Fatalf("store 2: %v", err)
		}

		if payload1.StorageRef != payload2.StorageRef {
			t.Error("identical data should have same storage ref")
		}
	})

	t.Run("uses correct extension for mime type", func(t *testing.T) {
		tests := []struct {
			mimeType string
			ext      string
		}{
			{"audio/wav", ".wav"},
			{"audio/mpeg", ".mp3"},
			{"audio/L16", ".pcm"},
			{"video/mp4", ".mp4"},
			{"image/png", ".png"},
			{"image/jpeg", ".jpg"},
			{"application/octet-stream", ".bin"},
		}

		for _, tt := range tests {
			payload, err := store.Store(ctx, "session-1", []byte(tt.mimeType), tt.mimeType)
			if err != nil {
				t.Fatalf("store %s: %v", tt.mimeType, err)
			}
			if !strings.HasSuffix(payload.StorageRef, tt.ext) {
				t.Errorf("expected ref to end with %s for %s, got %s",
					tt.ext, tt.mimeType, payload.StorageRef)
			}
		}
	})
}

func TestFileBlobStore_Load(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	t.Run("loads stored data", func(t *testing.T) {
		original := []byte("test data for loading")
		payload, err := store.Store(ctx, "session-1", original, "text/plain")
		if err != nil {
			t.Fatalf("store: %v", err)
		}

		loaded, err := store.Load(ctx, payload.StorageRef)
		if err != nil {
			t.Fatalf("load: %v", err)
		}

		if string(loaded) != string(original) {
			t.Errorf("data mismatch: got %q, want %q", loaded, original)
		}
	})

	t.Run("returns error for missing ref", func(t *testing.T) {
		_, err := store.Load(ctx, "file://nonexistent/path.bin")
		if err == nil {
			t.Error("expected error for missing ref")
		}
	})
}

func TestFileBlobStore_LoadReader(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	t.Run("returns reader for stored data", func(t *testing.T) {
		original := []byte("test data for reader")
		payload, err := store.Store(ctx, "session-1", original, "text/plain")
		if err != nil {
			t.Fatalf("store: %v", err)
		}

		reader, err := store.LoadReader(ctx, payload.StorageRef)
		if err != nil {
			t.Fatalf("load reader: %v", err)
		}
		defer reader.Close()

		buf := make([]byte, len(original)+10)
		n, _ := reader.Read(buf)
		if string(buf[:n]) != string(original) {
			t.Errorf("data mismatch: got %q, want %q", buf[:n], original)
		}
	})
}

func TestFileBlobStore_Delete(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	t.Run("deletes stored data", func(t *testing.T) {
		payload, err := store.Store(ctx, "session-1", []byte("to delete"), "text/plain")
		if err != nil {
			t.Fatalf("store: %v", err)
		}

		// Verify it exists
		if _, err := store.Load(ctx, payload.StorageRef); err != nil {
			t.Fatalf("load before delete: %v", err)
		}

		// Delete
		if err := store.Delete(ctx, payload.StorageRef); err != nil {
			t.Fatalf("delete: %v", err)
		}

		// Verify it's gone
		if _, err := store.Load(ctx, payload.StorageRef); err == nil {
			t.Error("expected error loading deleted blob")
		}
	})

	t.Run("no error for missing ref", func(t *testing.T) {
		err := store.Delete(ctx, "file://nonexistent/path.bin")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestFileBlobStore_StoreReader(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	t.Run("stores from reader", func(t *testing.T) {
		original := "data from reader"
		reader := strings.NewReader(original)

		payload, err := store.StoreReader(ctx, "session-1", reader, "text/plain")
		if err != nil {
			t.Fatalf("store reader: %v", err)
		}

		loaded, err := store.Load(ctx, payload.StorageRef)
		if err != nil {
			t.Fatalf("load: %v", err)
		}

		if string(loaded) != original {
			t.Errorf("data mismatch: got %q, want %q", loaded, original)
		}
	})
}

func TestFileBlobStore_ConcurrentStore(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Store the same data concurrently from multiple goroutines.
	const goroutines = 20
	data := []byte("concurrent test data")
	errs := make(chan error, goroutines)
	refs := make(chan string, goroutines)

	for range goroutines {
		go func() {
			p, err := store.Store(ctx, "session-concurrent", data, "text/plain")
			if err != nil {
				errs <- err
				return
			}
			refs <- p.StorageRef
			errs <- nil
		}()
	}

	var firstRef string
	for range goroutines {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent store: %v", err)
		}
	}
	close(refs)
	for ref := range refs {
		if firstRef == "" {
			firstRef = ref
		}
		if ref != firstRef {
			t.Error("concurrent stores of same data should return same ref")
		}
	}

	// Verify the data loads correctly.
	loaded, err := store.Load(ctx, firstRef)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(loaded) != string(data) {
		t.Errorf("data mismatch: got %q, want %q", loaded, data)
	}
}

func TestFileBlobStore_StoreReader_Streaming(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Use a pipe to prove data is streamed (not buffered into memory all at once).
	pr, pw := io.Pipe()
	original := "streaming data content for test"

	go func() {
		// Write in small chunks to simulate streaming.
		for i := 0; i < len(original); i++ {
			pw.Write([]byte{original[i]})
		}
		pw.Close()
	}()

	payload, err := store.StoreReader(ctx, "session-stream", pr, "text/plain")
	if err != nil {
		t.Fatalf("store reader: %v", err)
	}

	if payload.Size != int64(len(original)) {
		t.Errorf("size = %d, want %d", payload.Size, len(original))
	}

	loaded, err := store.Load(ctx, payload.StorageRef)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(loaded) != original {
		t.Errorf("data mismatch: got %q, want %q", loaded, original)
	}
}

func TestFileBlobStore_StoreReader_Dedup(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	original := "dedup reader data"

	// Store once via Store()
	p1, err := store.Store(ctx, "session-dedup", []byte(original), "text/plain")
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Store same data via StoreReader — should dedup via in-memory map
	p2, err := store.StoreReader(ctx, "session-dedup", strings.NewReader(original), "text/plain")
	if err != nil {
		t.Fatalf("store reader dedup: %v", err)
	}

	if p1.StorageRef != p2.StorageRef {
		t.Error("StoreReader should dedup with same ref as Store")
	}
}

func TestFileBlobStore_StoreReader_DedupFilesystem(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	original := "filesystem dedup reader data"

	// Store once via StoreReader
	p1, err := store.StoreReader(ctx, "session-fsdedup", strings.NewReader(original), "text/plain")
	if err != nil {
		t.Fatalf("store reader 1: %v", err)
	}

	// Create a fresh store instance pointing to the same directory to bypass
	// in-memory map but hit the filesystem dedup check.
	store2, err := NewFileBlobStore(store.baseDir)
	if err != nil {
		t.Fatalf("create store2: %v", err)
	}
	defer store2.Close()

	p2, err := store2.StoreReader(ctx, "session-fsdedup", strings.NewReader(original), "text/plain")
	if err != nil {
		t.Fatalf("store reader 2: %v", err)
	}

	if p1.StorageRef != p2.StorageRef {
		t.Error("StoreReader should dedup via filesystem check")
	}
}

func TestFileBlobStore_Store_FilesystemDedup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileBlobStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	data := []byte("filesystem dedup test")
	p1, err := store.Store(ctx, "session-1", data, "text/plain")
	if err != nil {
		t.Fatalf("store 1: %v", err)
	}

	// Create a fresh store to bypass in-memory map
	store2, err := NewFileBlobStore(dir)
	if err != nil {
		t.Fatalf("create store2: %v", err)
	}
	defer store2.Close()

	p2, err := store2.Store(ctx, "session-1", data, "text/plain")
	if err != nil {
		t.Fatalf("store 2: %v", err)
	}

	if p1.StorageRef != p2.StorageRef {
		t.Error("Store should dedup via filesystem check")
	}
}

func TestFileBlobStore_ContextCancelled(t *testing.T) {
	store, err := NewFileBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = store.Store(ctx, "session-1", []byte("data"), "text/plain")
	if err == nil {
		t.Error("Store with cancelled context should error")
	}

	_, err = store.StoreReader(ctx, "session-1", strings.NewReader("data"), "text/plain")
	if err == nil {
		t.Error("StoreReader with cancelled context should error")
	}

	_, err = store.Load(ctx, "file://any")
	if err == nil {
		t.Error("Load with cancelled context should error")
	}

	_, err = store.LoadReader(ctx, "file://any")
	if err == nil {
		t.Error("LoadReader with cancelled context should error")
	}

	err = store.Delete(ctx, "file://any")
	if err == nil {
		t.Error("Delete with cancelled context should error")
	}
}

func TestFileBlobStore_AtomicWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileBlobStore(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	// Store data and verify no temp files remain.
	_, err = store.Store(ctx, "session-atomic", []byte("atomic data"), "text/plain")
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Check for leftover temp files in the session directory.
	sessionDir := filepath.Join(dir, "session-atomic")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".blob-") {
			t.Errorf("leftover temp file: %s", entry.Name())
		}
	}
}

func TestNewEventStoreWithBlobs(t *testing.T) {
	dir := t.TempDir()
	store, err := NewEventStoreWithBlobs(dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	t.Run("event store works", func(t *testing.T) {
		event := &Event{
			Type:      EventMessageCreated,
			SessionID: "test-session",
			Data:      &MessageCreatedData{Role: "user", Content: "hello"},
		}

		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("append: %v", err)
		}

		events, err := store.Query(ctx, &EventFilter{SessionID: "test-session"})
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		if len(events) != 1 {
			t.Errorf("expected 1 event, got %d", len(events))
		}
	})

	t.Run("blob store works", func(t *testing.T) {
		data := []byte("audio data")
		payload, err := store.Store(ctx, "test-session", data, "audio/wav")
		if err != nil {
			t.Fatalf("store blob: %v", err)
		}

		loaded, err := store.Load(ctx, payload.StorageRef)
		if err != nil {
			t.Fatalf("load blob: %v", err)
		}

		if string(loaded) != string(data) {
			t.Error("blob data mismatch")
		}
	})
}

func TestExtensionFromMIME(t *testing.T) {
	tests := []struct {
		mimeType string
		expected string
	}{
		// Audio
		{"audio/wav", ".wav"},
		{"audio/wave", ".wav"},
		{"audio/x-wav", ".wav"},
		{"audio/mpeg", ".mp3"},
		{"audio/mp3", ".mp3"},
		{"audio/ogg", ".ogg"},
		{"audio/opus", ".opus"},
		{"audio/flac", ".flac"},
		{"audio/L16", ".pcm"},
		{"audio/pcm", ".pcm"},
		{"audio/webm", ".webm"},
		// Video
		{"video/mp4", ".mp4"},
		{"video/webm", ".webm"},
		{"video/quicktime", ".mov"},
		{"video/x-msvideo", ".avi"},
		// Images
		{"image/jpeg", ".jpg"},
		{"image/png", ".png"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"image/svg+xml", ".svg"},
		{"image/bmp", ".bmp"},
		// Unknown
		{"application/octet-stream", ".bin"},
		{"unknown/type", ".bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := extensionFromMIME(tt.mimeType)
			if got != tt.expected {
				t.Errorf("extensionFromMIME(%q) = %q, want %q", tt.mimeType, got, tt.expected)
			}
		})
	}
}
