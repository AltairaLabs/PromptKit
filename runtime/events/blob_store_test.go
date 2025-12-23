package events

import (
	"context"
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
